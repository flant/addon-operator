package addon_operator

import (
	"context"
	"fmt"
	_ "net/http/pprof" // Webserver pprof endpoint injection
	"path"
	"runtime/trace"
	"strings"
	"time"

	"github.com/gofrs/uuid/v5"
	log "github.com/sirupsen/logrus"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/helm_resources_manager"
	hookTypes "github.com/flant/addon-operator/pkg/hook/types"
	"github.com/flant/addon-operator/pkg/kube_config_manager"
	"github.com/flant/addon-operator/pkg/kube_config_manager/config"
	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/task"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/kube-client/client"
	sh_app "github.com/flant/shell-operator/pkg/app"
	runtimeConfig "github.com/flant/shell-operator/pkg/config"
	bc "github.com/flant/shell-operator/pkg/hook/binding_context"
	"github.com/flant/shell-operator/pkg/hook/controller"
	htypes "github.com/flant/shell-operator/pkg/hook/types"
	"github.com/flant/shell-operator/pkg/kube_events_manager/types"
	shell_operator "github.com/flant/shell-operator/pkg/shell-operator"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
	utils2 "github.com/flant/shell-operator/pkg/utils/file"
	"github.com/flant/shell-operator/pkg/utils/measure"
)

// AddonOperator extends ShellOperator with modules and global hooks
// and with a value storage.
type AddonOperator struct {
	engine *shell_operator.ShellOperator
	ctx    context.Context
	cancel context.CancelFunc

	runtimeConfig *runtimeConfig.Config

	// KubeConfigManager monitors changes in ConfigMap.
	KubeConfigManager *kube_config_manager.KubeConfigManager

	// ModuleManager is the module manager object, which monitors configuration
	// and variable changes.
	ModuleManager *module_manager.ModuleManager

	Helm *helm.ClientFactory

	// HelmResourcesManager monitors absent resources created for modules.
	HelmResourcesManager helm_resources_manager.HelmResourcesManager

	// converge state
	ConvergeState *ConvergeState

	// Initial KubeConfig to bypass initial loading from the ConfigMap.
	InitialKubeConfig *config.KubeConfig

	// AdmissionServer handles validation and mutation admission webhooks
	AdmissionServer *AdmissionServer

	// ExplicitlyPurgeModules temporary way to purge explicitly set modules
	// linked with pkg/module_manager/module_manager.go#L555
	ExplicitlyPurgeModules []string
}

func NewAddonOperator(ctx context.Context) *AddonOperator {
	cctx, cancel := context.WithCancel(ctx)
	so := shell_operator.NewShellOperator(cctx)

	// Have to initialize common operator to have all common dependencies below
	err := so.AssembleCommonOperator(app.ListenAddress, app.ListenPort)
	if err != nil {
		panic(err)
	}

	ao := &AddonOperator{
		ctx:                    cctx,
		cancel:                 cancel,
		engine:                 so,
		ConvergeState:          NewConvergeState(),
		ExplicitlyPurgeModules: make([]string, 0),
	}

	ao.AdmissionServer = NewAdmissionServer(app.AdmissionServerListenPort, app.AdmissionServerCertsDir)

	return ao
}

func (op *AddonOperator) Setup() error {
	// Helm client factory.
	helmClient, err := helm.InitHelmClientFactory(op.engine.KubeClient)
	if err != nil {
		return fmt.Errorf("initialize Helm: %s", err)
	}

	// Helm resources monitor.
	// It uses a separate client-go instance. (Metrics are registered when 'main' client is initialized).
	helmResourcesManager, err := InitDefaultHelmResourcesManager(op.ctx, op.engine.MetricStorage)
	if err != nil {
		return fmt.Errorf("initialize Helm resources manager: %s", err)
	}

	op.Helm = helmClient
	op.HelmResourcesManager = helmResourcesManager

	globalHooksDir, err := utils2.RequireExistingDirectory(app.GlobalHooksDir)
	if err != nil {
		return fmt.Errorf("global hooks directory: %s", err)
	}
	log.Infof("Global hooks directory: %s", globalHooksDir)

	tempDir, err := utils2.EnsureTempDirectory(sh_app.TempDir)
	if err != nil {
		return fmt.Errorf("temp directory: %s", err)
	}

	if op.KubeConfigManager == nil {
		return fmt.Errorf("KubeConfigManager must be set before Setup")
	}

	op.SetupModuleManager(app.ModulesDir, globalHooksDir, tempDir)

	return nil
}

// Start runs all managers, event and queue handlers.
func (op *AddonOperator) Start() error {
	if err := op.bootstrap(); err != nil {
		return err
	}

	// start http server with metrics
	op.engine.APIServer.Start(op.ctx)

	log.Info("Start first converge for modules")
	// Loading the onStartup hooks into the queue and running all modules.
	// Turning tracking changes on only after startup ends.

	// Bootstrap main queue with tasks to run Startup process.
	op.BootstrapMainQueue(op.engine.TaskQueues)
	// Start main task queue handler
	op.engine.TaskQueues.StartMain()

	// Global hooks are registered, initialize their queues.
	op.CreateAndStartQueuesForGlobalHooks()

	// ManagerEventsHandler handle events for kubernetes and schedule bindings.
	// Start it before start all informers to catch all kubernetes events (#42)
	op.engine.ManagerEventsHandler.Start()

	// Enable events from schedule manager.
	op.engine.ScheduleManager.Start()

	op.KubeConfigManager.Start()
	op.ModuleManager.Start()
	op.StartModuleManagerEventHandler()

	return nil
}

func (op *AddonOperator) Stop() {
	op.KubeConfigManager.Stop()
	op.engine.Shutdown()

	if op.cancel != nil {
		op.cancel()
	}
}

// KubeClient returns default common kubernetes client initialized by shell-operator
func (op *AddonOperator) KubeClient() *client.Client {
	return op.engine.KubeClient
}

func (op *AddonOperator) IsStartupConvergeDone() bool {
	return op.ConvergeState.firstRunPhase == firstDone
}

// InitModuleManager initialize KubeConfigManager and ModuleManager,
// reads values from ConfigMap for the first time and sets handlers
// for kubernetes and schedule events.
func (op *AddonOperator) InitModuleManager() error {
	var err error

	// Initializing ConfigMap storage for values
	err = op.KubeConfigManager.Init()
	if err != nil {
		return fmt.Errorf("init kube config manager: %s", err)
	}

	err = op.ModuleManager.Init()
	if err != nil {
		return fmt.Errorf("init module manager: %s", err)
	}

	// Load existing config values from ConfigMap.
	// Also, it is possible to override initial KubeConfig to give global hooks a chance
	// to handle the ConfigMap content later.
	if op.InitialKubeConfig == nil {
		op.KubeConfigManager.SafeReadConfig(func(config *config.KubeConfig) {
			_, err = op.ModuleManager.HandleNewKubeConfig(config)
		})
		if err != nil {
			return fmt.Errorf("init module manager: load initial config for KubeConfigManager: %s", err)
		}
	} else {
		_, err = op.ModuleManager.HandleNewKubeConfig(op.InitialKubeConfig)
		if err != nil {
			return fmt.Errorf("init module manager: load overridden initial config: %s", err)
		}
	}

	// Initialize 'valid kube config' flag.
	op.ModuleManager.SetKubeConfigValid(true)

	// ManagerEventsHandlers created, register handlers to create tasks from events.
	op.RegisterManagerEventsHandlers()

	return nil
}

// AllowHandleScheduleEvent returns false if the Schedule event can be ignored.
func (op *AddonOperator) AllowHandleScheduleEvent(hook *module_manager.CommonHook) bool {
	// Always allow if first converge is done.
	if op.IsStartupConvergeDone() {
		return true
	}

	// Allow when first converge is still in progress,
	// but hook explicitly enable schedules after OnStartup.
	return hook.ShouldEnableSchedulesOnStartup()
}

func (op *AddonOperator) RegisterManagerEventsHandlers() {
	op.engine.ManagerEventsHandler.WithScheduleEventHandler(func(crontab string) []sh_task.Task {
		logLabels := map[string]string{
			"event.id": uuid.Must(uuid.NewV4()).String(),
			"binding":  string(htypes.Schedule),
		}
		logEntry := log.WithFields(utils.LabelsToLogFields(logLabels))
		logEntry.Debugf("Create tasks for 'schedule' event '%s'", crontab)

		var tasks []sh_task.Task
		err := op.ModuleManager.HandleScheduleEvent(crontab,
			func(globalHook *module_manager.GlobalHook, info controller.BindingExecutionInfo) {
				if !op.AllowHandleScheduleEvent(globalHook.CommonHook) {
					return
				}

				hookLabels := utils.MergeLabels(logLabels, map[string]string{
					"hook":      globalHook.GetName(),
					"hook.type": "module",
					"queue":     info.QueueName,
				})
				if len(info.BindingContext) > 0 {
					hookLabels["binding.name"] = info.BindingContext[0].Binding
				}
				delete(hookLabels, "task.id")
				newTask := sh_task.NewTask(task.GlobalHookRun).
					WithLogLabels(hookLabels).
					WithQueueName(info.QueueName).
					WithMetadata(task.HookMetadata{
						EventDescription:         "Schedule",
						HookName:                 globalHook.GetName(),
						BindingType:              htypes.Schedule,
						BindingContext:           info.BindingContext,
						AllowFailure:             info.AllowFailure,
						ReloadAllOnValuesChanges: true,
					})

				tasks = append(tasks, newTask)
			},
			func(module *module_manager.Module, moduleHook *module_manager.ModuleHook, info controller.BindingExecutionInfo) {
				if !op.AllowHandleScheduleEvent(moduleHook.CommonHook) {
					return
				}

				hookLabels := utils.MergeLabels(logLabels, map[string]string{
					"module":    module.Name,
					"hook":      moduleHook.GetName(),
					"hook.type": "module",
					"queue":     info.QueueName,
				})
				if len(info.BindingContext) > 0 {
					hookLabels["binding.name"] = info.BindingContext[0].Binding
				}
				delete(hookLabels, "task.id")
				newTask := sh_task.NewTask(task.ModuleHookRun).
					WithLogLabels(hookLabels).
					WithQueueName(info.QueueName).
					WithMetadata(task.HookMetadata{
						EventDescription: "Schedule",
						ModuleName:       module.Name,
						HookName:         moduleHook.GetName(),
						BindingType:      htypes.Schedule,
						BindingContext:   info.BindingContext,
						AllowFailure:     info.AllowFailure,
					})

				tasks = append(tasks, newTask)
			})
		if err != nil {
			logEntry.Errorf("handle schedule event '%s': %s", crontab, err)
			return []sh_task.Task{}
		}

		op.logTaskAdd(logEntry, "Schedule event received, append", tasks...)
		return tasks
	})

	op.engine.ManagerEventsHandler.WithKubeEventHandler(func(kubeEvent types.KubeEvent) []sh_task.Task {
		logLabels := map[string]string{
			"event.id": uuid.Must(uuid.NewV4()).String(),
			"binding":  string(htypes.OnKubernetesEvent),
		}
		logEntry := log.WithFields(utils.LabelsToLogFields(logLabels))
		logEntry.Debugf("Create tasks for 'kubernetes' event '%s'", kubeEvent.String())

		var tasks []sh_task.Task
		op.ModuleManager.HandleKubeEvent(kubeEvent,
			func(globalHook *module_manager.GlobalHook, info controller.BindingExecutionInfo) {
				hookLabels := utils.MergeLabels(logLabels, map[string]string{
					"hook":      globalHook.GetName(),
					"hook.type": "global",
					"queue":     info.QueueName,
				})
				if len(info.BindingContext) > 0 {
					hookLabels["binding.name"] = info.BindingContext[0].Binding
					hookLabels["watchEvent"] = string(info.BindingContext[0].WatchEvent)
				}
				delete(hookLabels, "task.id")
				newTask := sh_task.NewTask(task.GlobalHookRun).
					WithLogLabels(hookLabels).
					WithQueueName(info.QueueName).
					WithMetadata(task.HookMetadata{
						EventDescription:         "Kubernetes",
						HookName:                 globalHook.GetName(),
						BindingType:              htypes.OnKubernetesEvent,
						BindingContext:           info.BindingContext,
						AllowFailure:             info.AllowFailure,
						Binding:                  info.Binding,
						ReloadAllOnValuesChanges: true,
					})

				tasks = append(tasks, newTask)
			},
			func(module *module_manager.Module, moduleHook *module_manager.ModuleHook, info controller.BindingExecutionInfo) {
				hookLabels := utils.MergeLabels(logLabels, map[string]string{
					"module":    module.Name,
					"hook":      moduleHook.GetName(),
					"hook.type": "module",
					"queue":     info.QueueName,
				})
				if len(info.BindingContext) > 0 {
					hookLabels["binding.name"] = info.BindingContext[0].Binding
					hookLabels["watchEvent"] = string(info.BindingContext[0].WatchEvent)
				}
				delete(hookLabels, "task.id")
				newTask := sh_task.NewTask(task.ModuleHookRun).
					WithLogLabels(hookLabels).
					WithQueueName(info.QueueName).
					WithMetadata(task.HookMetadata{
						EventDescription: "Kubernetes",
						ModuleName:       module.Name,
						HookName:         moduleHook.GetName(),
						Binding:          info.Binding,
						BindingType:      htypes.OnKubernetesEvent,
						BindingContext:   info.BindingContext,
						AllowFailure:     info.AllowFailure,
					})

				tasks = append(tasks, newTask)
			})

		op.logTaskAdd(logEntry, "Kubernetes event received, append", tasks...)
		return tasks
	})
}

// BootstrapMainQueue adds tasks to initiate Startup sequence:
//
// - Run onStartup hooks.
// - Enable global schedule bindings.
// - Enable kubernetes bindings: run Synchronization tasks.
// - Purge unknown Helm releases.
// - Start reload all modules.
func (op *AddonOperator) BootstrapMainQueue(tqs *queue.TaskQueueSet) {
	logLabels := map[string]string{
		"event.type": "OperatorStartup",
	}
	// create onStartup for global hooks
	logEntry := log.WithFields(utils.LabelsToLogFields(logLabels))

	// Prepopulate main queue with 'onStartup' and 'enable kubernetes bindings' tasks for
	// global hooks and add a task to discover modules state.
	tqs.WithMainName("main")
	tqs.NewNamedQueue("main", op.TaskHandler)

	tasks := op.CreateBootstrapTasks(logLabels)
	op.logTaskAdd(logEntry, "append", tasks...)
	for _, tsk := range tasks {
		op.engine.TaskQueues.GetMain().AddLast(tsk)
	}
}

func (op *AddonOperator) CreateBootstrapTasks(logLabels map[string]string) []sh_task.Task {
	const eventDescription = "Operator-Startup"
	tasks := make([]sh_task.Task, 0)
	queuedAt := time.Now()

	// 'OnStartup' global hooks.
	onStartupHooks := op.ModuleManager.GetGlobalHooksInOrder(htypes.OnStartup)
	for _, hookName := range onStartupHooks {
		hookLogLabels := utils.MergeLabels(logLabels, map[string]string{
			"hook":      hookName,
			"hook.type": "global",
			"queue":     "main",
			"binding":   string(htypes.OnStartup),
		})

		onStartupBindingContext := bc.BindingContext{Binding: string(htypes.OnStartup)}
		onStartupBindingContext.Metadata.BindingType = htypes.OnStartup

		newTask := sh_task.NewTask(task.GlobalHookRun).
			WithLogLabels(hookLogLabels).
			WithQueueName("main").
			WithMetadata(task.HookMetadata{
				EventDescription:         eventDescription,
				HookName:                 hookName,
				BindingType:              htypes.OnStartup,
				BindingContext:           []bc.BindingContext{onStartupBindingContext},
				ReloadAllOnValuesChanges: false,
			})
		tasks = append(tasks, newTask.WithQueuedAt(queuedAt))
	}

	// 'Schedule' global hooks.
	schedHooks := op.ModuleManager.GetGlobalHooksInOrder(htypes.Schedule)
	for _, hookName := range schedHooks {
		hookLogLabels := utils.MergeLabels(logLabels, map[string]string{
			"hook":      hookName,
			"hook.type": "global",
			"queue":     "main",
			"binding":   string(task.GlobalHookEnableScheduleBindings),
		})

		newTask := sh_task.NewTask(task.GlobalHookEnableScheduleBindings).
			WithLogLabels(hookLogLabels).
			WithQueueName("main").
			WithMetadata(task.HookMetadata{
				EventDescription: eventDescription,
				HookName:         hookName,
			})
		tasks = append(tasks, newTask.WithQueuedAt(queuedAt))
	}

	// Tasks to enable kubernetes events for all global hooks with kubernetes bindings.
	kubeHooks := op.ModuleManager.GetGlobalHooksInOrder(htypes.OnKubernetesEvent)
	for _, hookName := range kubeHooks {
		hookLogLabels := utils.MergeLabels(logLabels, map[string]string{
			"hook":      hookName,
			"hook.type": "global",
			"queue":     "main",
			"binding":   string(task.GlobalHookEnableKubernetesBindings),
		})

		newTask := sh_task.NewTask(task.GlobalHookEnableKubernetesBindings).
			WithLogLabels(hookLogLabels).
			WithQueueName("main").
			WithMetadata(task.HookMetadata{
				EventDescription: eventDescription,
				HookName:         hookName,
			})
		tasks = append(tasks, newTask.WithQueuedAt(queuedAt))
	}

	// Task to wait for kubernetes.Synchronization.
	waitLogLabels := utils.MergeLabels(logLabels, map[string]string{
		"queue":   "main",
		"binding": string(task.GlobalHookWaitKubernetesSynchronization),
	})
	waitTask := sh_task.NewTask(task.GlobalHookWaitKubernetesSynchronization).
		WithLogLabels(waitLogLabels).
		WithQueueName("main").
		WithMetadata(task.HookMetadata{
			EventDescription: eventDescription,
		})
	tasks = append(tasks, waitTask.WithQueuedAt(queuedAt))

	// Add "DiscoverHelmReleases" task to detect unknown releases and purge them.
	discoverLabels := utils.MergeLabels(logLabels, map[string]string{
		"queue":   "main",
		"binding": string(task.DiscoverHelmReleases),
	})
	discoverTask := sh_task.NewTask(task.DiscoverHelmReleases).
		WithLogLabels(discoverLabels).
		WithQueueName("main").
		WithMetadata(task.HookMetadata{
			EventDescription: eventDescription,
		})
	tasks = append(tasks, discoverTask.WithQueuedAt(queuedAt))

	// Add "ConvergeModules" task to run modules converge sequence for the first time.
	convergeLabels := utils.MergeLabels(logLabels, map[string]string{
		"queue":   "main",
		"binding": string(task.ConvergeModules),
	})
	convergeTask := NewConvergeModulesTask(eventDescription, OperatorStartup, convergeLabels)
	tasks = append(tasks, convergeTask.WithQueuedAt(queuedAt))

	return tasks
}

// CreatePurgeTasks returns ModulePurge tasks for each unknown Helm release.
func (op *AddonOperator) CreatePurgeTasks(modulesToPurge []string, t sh_task.Task) []sh_task.Task {
	newTasks := make([]sh_task.Task, 0, len(modulesToPurge))
	queuedAt := time.Now()

	hm := task.HookMetadataAccessor(t)

	// Add ModulePurge tasks to purge unknown helm releases at start.
	for _, moduleName := range modulesToPurge {
		newLogLabels := utils.MergeLabels(t.GetLogLabels())
		newLogLabels["module"] = moduleName
		delete(newLogLabels, "task.id")

		newTask := sh_task.NewTask(task.ModulePurge).
			WithLogLabels(newLogLabels).
			WithQueueName("main").
			WithMetadata(task.HookMetadata{
				EventDescription: hm.EventDescription,
				ModuleName:       moduleName,
			})
		newTasks = append(newTasks, newTask.WithQueuedAt(queuedAt))
	}

	return newTasks
}

// HandleConvergeModules is a multi-phase task.
func (op *AddonOperator) HandleConvergeModules(t sh_task.Task, logLabels map[string]string) (res queue.TaskResult) {
	defer trace.StartRegion(context.Background(), "ConvergeModules").End()
	logEntry := log.WithFields(utils.LabelsToLogFields(logLabels))

	taskEvent, ok := t.GetProp(ConvergeEventProp).(ConvergeEvent)
	if !ok {
		logEntry.Errorf("Possible bug! Wrong prop type in ConvergeModules: got %T(%#[1]v) instead string.", t.GetProp("event"))
		res.Status = queue.Fail
		return res
	}
	hm := task.HookMetadataAccessor(t)

	var handleErr error

	if taskEvent == KubeConfigChanged {
		logEntry.Debugf("ConvergeModules: handle KubeConfigChanged")
		var state *module_manager.ModulesState
		op.KubeConfigManager.SafeReadConfig(func(config *config.KubeConfig) {
			state, handleErr = op.ModuleManager.HandleNewKubeConfig(config)
		})
		if handleErr == nil {
			// KubeConfigChanged task should be removed.
			res.Status = queue.Success

			if state == nil {
				logEntry.Infof("ConvergeModules: kube config modification detected, no changes in config values")
				return
			}

			// Skip queueing additional Converge tasks during run global hooks at startup.
			if op.ConvergeState.firstRunPhase == firstNotStarted {
				logEntry.Infof("ConvergeModules: kube config modification detected, ignore until starting first converge")
				return
			}

			if len(state.ModulesToReload) > 0 {
				// Append ModuleRun tasks if ModuleRun is not queued already.
				reloadTasks := op.CreateReloadModulesTasks(state.ModulesToReload, t.GetLogLabels(), "KubeConfig-Changed-Modules")
				if len(reloadTasks) > 0 {
					logEntry.Infof("ConvergeModules: kube config modification detected, append %d tasks to reload modules %+v", len(reloadTasks), state.ModulesToReload)
					// Reset delay if error-loop.
					res.DelayBeforeNextTask = 0
				}
				res.TailTasks = reloadTasks
				op.logTaskAdd(logEntry, "tail", res.TailTasks...)
				return
			}

			// No modules to reload -> full reload is required.
			// If Converge is in progress, drain converge tasks after current task
			// and put ConvergeModules at start.
			// Else put ConvergeModules to the end of the main queue.
			convergeDrained := RemoveCurrentConvergeTasks(op.engine.TaskQueues.GetMain(), t.GetId())
			if convergeDrained {
				logEntry.Infof("ConvergeModules: kube config modification detected,  restart current converge process (%s)", op.ConvergeState.Phase)
				res.AfterTasks = []sh_task.Task{
					NewConvergeModulesTask("InPlace-ReloadAll-After-KubeConfigChange", ReloadAllModules, t.GetLogLabels()),
				}
				op.logTaskAdd(logEntry, "after", res.AfterTasks...)
			} else {
				logEntry.Infof("ConvergeModules: kube config modification detected, reload all modules required")
				res.TailTasks = []sh_task.Task{
					NewConvergeModulesTask("Delayed-ReloadAll-After-KubeConfigChange", ReloadAllModules, t.GetLogLabels()),
				}
				op.logTaskAdd(logEntry, "tail", res.TailTasks...)
			}
			// ConvergeModules may be in progress Reset converge state.
			op.ConvergeState.Phase = StandBy
			return
		}
	} else {
		// Handle other converge events: OperatorStartup, GlobalValuesChanged, ReloadAllModules.
		if op.ConvergeState.Phase == StandBy {
			logEntry.Debugf("ConvergeModules: start")

			// Deduplicate tasks: remove ConvergeModules tasks right after the current task.
			RemoveAdjacentConvergeModules(op.engine.TaskQueues.GetByName(t.GetQueueName()), t.GetId())

			op.ConvergeState.Phase = RunBeforeAll
		}

		if op.ConvergeState.Phase == RunBeforeAll {
			// Put BeforeAll tasks before current task.
			tasks := op.CreateBeforeAllTasks(t.GetLogLabels(), hm.EventDescription)
			op.ConvergeState.Phase = WaitBeforeAll
			if len(tasks) > 0 {
				res.HeadTasks = tasks
				res.Status = queue.Keep
				op.logTaskAdd(logEntry, "head", res.HeadTasks...)
				return
			}
		}

		if op.ConvergeState.Phase == WaitBeforeAll {
			logEntry.Infof("ConvergeModules: beforeAll hooks done, run modules")
			var state *module_manager.ModulesState
			state, handleErr = op.ModuleManager.RefreshEnabledState(t.GetLogLabels())
			if handleErr == nil {
				// TODO disable hooks before was done in DiscoverModulesStateRefresh. Should we stick to this solution or disable events later during the handling each ModuleDelete task?
				// Disable events for disabled modules.
				for _, moduleName := range state.ModulesToDisable {
					op.ModuleManager.DisableModuleHooks(moduleName)
					// op.DrainModuleQueues(moduleName)
				}
				// Set ModulesToEnable list to properly run onStartup hooks for first converge.
				if !op.IsStartupConvergeDone() {
					state.ModulesToEnable = state.AllEnabledModules
				}
				tasks := op.CreateConvergeModulesTasks(state, t.GetLogLabels(), string(taskEvent))
				op.ConvergeState.Phase = WaitDeleteAndRunModules
				if len(tasks) > 0 {
					res.HeadTasks = tasks
					res.Status = queue.Keep
					op.logTaskAdd(logEntry, "head", res.HeadTasks...)
					return
				}
			}
		}

		if op.ConvergeState.Phase == WaitDeleteAndRunModules {
			logEntry.Infof("ConvergeModules: ModuleRun tasks done, execute AfterAll global hooks")
			// Put AfterAll tasks before current task.
			tasks, handleErr := op.CreateAfterAllTasks(t.GetLogLabels(), hm.EventDescription)
			if handleErr == nil {
				op.ConvergeState.Phase = WaitAfterAll
				if len(tasks) > 0 {
					res.HeadTasks = tasks
					res.Status = queue.Keep
					op.logTaskAdd(logEntry, "head", res.HeadTasks...)
					return
				}
			}
		}

		// It is the last phase of ConvergeModules task, reset operator's Converge phase.
		if op.ConvergeState.Phase == WaitAfterAll {
			op.ConvergeState.Phase = StandBy
			logEntry.Infof("ConvergeModules task done")
			res.Status = queue.Success
			op.EmitModulesSync()
			return res
		}
	}

	if handleErr != nil {
		res.Status = queue.Fail
		logEntry.Errorf("ConvergeModules failed in phase '%s', requeue task to retry after delay. Failed count is %d. Error: %s", op.ConvergeState.Phase, t.GetFailureCount()+1, handleErr)
		op.engine.MetricStorage.CounterAdd("{PREFIX}modules_discover_errors_total", 1.0, map[string]string{})
		t.UpdateFailureMessage(handleErr.Error())
		t.WithQueuedAt(time.Now())
		return res
	}

	logEntry.Debugf("ConvergeModules success")
	res.Status = queue.Success
	return res
}

// CreateBeforeAllTasks returns tasks to run BeforeAll global hooks.
func (op *AddonOperator) CreateBeforeAllTasks(logLabels map[string]string, eventDescription string) []sh_task.Task {
	tasks := make([]sh_task.Task, 0)
	queuedAt := time.Now()

	// Get 'beforeAll' global hooks.
	beforeAllHooks := op.ModuleManager.GetGlobalHooksInOrder(hookTypes.BeforeAll)

	for _, hookName := range beforeAllHooks {
		hookLogLabels := utils.MergeLabels(logLabels, map[string]string{
			"hook":      hookName,
			"hook.type": "global",
			"queue":     "main",
			"binding":   string(hookTypes.BeforeAll),
		})
		// remove task.id — it is set by NewTask
		delete(hookLogLabels, "task.id")

		// bc := module_manager.BindingContext{BindingContext: hook.BindingContext{Binding: stringmodule_manager.BeforeAll)}}
		// bc.KubernetesSnapshots := ModuleManager.GetGlobalHook(hookName).HookController.KubernetesSnapshots()

		beforeAllBc := bc.BindingContext{
			Binding: string(hookTypes.BeforeAll),
		}
		beforeAllBc.Metadata.BindingType = hookTypes.BeforeAll
		beforeAllBc.Metadata.IncludeAllSnapshots = true

		newTask := sh_task.NewTask(task.GlobalHookRun).
			WithLogLabels(hookLogLabels).
			WithQueueName("main").
			WithMetadata(task.HookMetadata{
				EventDescription:         eventDescription,
				HookName:                 hookName,
				BindingType:              hookTypes.BeforeAll,
				BindingContext:           []bc.BindingContext{beforeAllBc},
				ReloadAllOnValuesChanges: false,
			})
		tasks = append(tasks, newTask.WithQueuedAt(queuedAt))
	}
	return tasks
}

// CreateAfterAllTasks returns tasks to run AfterAll global hooks.
func (op *AddonOperator) CreateAfterAllTasks(logLabels map[string]string, eventDescription string) ([]sh_task.Task, error) {
	tasks := make([]sh_task.Task, 0)
	queuedAt := time.Now()

	// Get 'afterAll' global hooks.
	afterAllHooks := op.ModuleManager.GetGlobalHooksInOrder(hookTypes.AfterAll)

	for i, hookName := range afterAllHooks {
		hookLogLabels := utils.MergeLabels(logLabels, map[string]string{
			"hook":      hookName,
			"hook.type": "global",
			"queue":     "main",
			"binding":   string(hookTypes.AfterAll),
		})
		delete(hookLogLabels, "task.id")

		afterAllBc := bc.BindingContext{
			Binding: string(hookTypes.AfterAll),
		}
		afterAllBc.Metadata.BindingType = hookTypes.AfterAll
		afterAllBc.Metadata.IncludeAllSnapshots = true

		taskMetadata := task.HookMetadata{
			EventDescription: eventDescription,
			HookName:         hookName,
			BindingType:      hookTypes.AfterAll,
			BindingContext:   []bc.BindingContext{afterAllBc},
		}
		if i == len(afterAllHooks)-1 {
			taskMetadata.LastAfterAllHook = true
			globalValues, err := op.ModuleManager.GlobalValues()
			if err != nil {
				return nil, err
			}
			taskMetadata.ValuesChecksum = globalValues.Checksum()
			taskMetadata.DynamicEnabledChecksum = op.ModuleManager.DynamicEnabledChecksum()
		}

		newTask := sh_task.NewTask(task.GlobalHookRun).
			WithLogLabels(hookLogLabels).
			WithQueueName("main").
			WithMetadata(taskMetadata)
		tasks = append(tasks, newTask.WithQueuedAt(queuedAt))
	}

	return tasks, nil
}

// CreateAndStartQueue creates a named queue and starts it.
// It returns false is queue is already created
func (op *AddonOperator) CreateAndStartQueue(queueName string) bool {
	if op.engine.TaskQueues.GetByName(queueName) != nil {
		return false
	}
	op.engine.TaskQueues.NewNamedQueue(queueName, op.TaskHandler)
	op.engine.TaskQueues.GetByName(queueName).Start()
	return true
}

// CreateAndStartQueuesForGlobalHooks creates queues for all registered global hooks.
// It is safe to run this method multiple times, as it checks
// for existing queues.
func (op *AddonOperator) CreateAndStartQueuesForGlobalHooks() {
	for _, hookName := range op.ModuleManager.GetGlobalHooksNames() {
		h := op.ModuleManager.GetGlobalHook(hookName)
		for _, hookBinding := range h.Config.Schedules {
			if op.CreateAndStartQueue(hookBinding.Queue) {
				log.Debugf("Queue '%s' started for global 'schedule' hook %s", hookBinding.Queue, hookName)
			}
		}
		for _, hookBinding := range h.Config.OnKubernetesEvents {
			if op.CreateAndStartQueue(hookBinding.Queue) {
				log.Debugf("Queue '%s' started for global 'kubernetes' hook %s", hookBinding.Queue, hookName)
			}
		}
	}
}

// CreateAndStartQueuesForModuleHooks creates queues for registered module hooks.
// It is safe to run this method multiple times, as it checks
// for existing queues.
func (op *AddonOperator) CreateAndStartQueuesForModuleHooks(moduleName string) {
	for _, hookName := range op.ModuleManager.GetModuleHookNames(moduleName) {
		h := op.ModuleManager.GetModuleHook(hookName)
		for _, hookBinding := range h.Config.Schedules {
			if op.CreateAndStartQueue(hookBinding.Queue) {
				log.Debugf("Queue '%s' started for module 'schedule' hook %s", hookBinding.Queue, hookName)
			}
		}
		for _, hookBinding := range h.Config.OnKubernetesEvents {
			if op.CreateAndStartQueue(hookBinding.Queue) {
				log.Debugf("Queue '%s' started for module 'kubernetes' hook %s", hookBinding.Queue, hookName)
			}
		}
	}
}

// CreateAndStartQueuesForAllModuleHooks creates queues for all registered modules.
// It is safe to run this method multiple times, as it checks
// for existing queues.
func (op *AddonOperator) CreateAndStartQueuesForAllModuleHooks() {
	for _, moduleName := range op.ModuleManager.GetEnabledModuleNames() {
		op.CreateAndStartQueuesForModuleHooks(moduleName)
	}
}

func (op *AddonOperator) DrainModuleQueues(modName string) {
	for _, hookName := range op.ModuleManager.GetModuleHookNames(modName) {
		h := op.ModuleManager.GetModuleHook(hookName)
		for _, hookBinding := range h.Config.Schedules {
			DrainNonMainQueue(op.engine.TaskQueues.GetByName(hookBinding.Queue))
		}
		for _, hookBinding := range h.Config.OnKubernetesEvents {
			DrainNonMainQueue(op.engine.TaskQueues.GetByName(hookBinding.Queue))
		}
	}
}

func (op *AddonOperator) StartModuleManagerEventHandler() {
	go func() {
		logEntry := log.WithField("operator.component", "handleManagerEvents")
		for {
			select {
			case kubeConfigEvent := <-op.KubeConfigManager.KubeConfigEventCh():
				logLabels := map[string]string{
					"event.id": uuid.Must(uuid.NewV4()).String(),
				}
				eventLogEntry := logEntry.WithFields(utils.LabelsToLogFields(logLabels))

				if kubeConfigEvent == config.KubeConfigInvalid {
					op.ModuleManager.SetKubeConfigValid(false)
					eventLogEntry.Infof("KubeConfig become invalid")
				}

				if kubeConfigEvent == config.KubeConfigChanged {
					if !op.ModuleManager.GetKubeConfigValid() {
						eventLogEntry.Infof("KubeConfig become valid")
					}
					// Config is valid now, add task to update ModuleManager state.
					op.ModuleManager.SetKubeConfigValid(true)
					// Run ConvergeModules task asap.
					convergeTask := NewConvergeModulesTask(
						fmt.Sprintf("ConfigMap-%s", kubeConfigEvent),
						KubeConfigChanged,
						logLabels,
					)
					op.engine.TaskQueues.GetMain().AddFirst(convergeTask)
					// Cancel delay in case the head task is stuck in the error loop.
					op.engine.TaskQueues.GetMain().CancelTaskDelay()
					op.logTaskAdd(eventLogEntry, "KubeConfig is changed, put first", convergeTask)
				}

			case absentResourcesEvent := <-op.HelmResourcesManager.Ch():
				logLabels := map[string]string{
					"event.id": uuid.Must(uuid.NewV4()).String(),
					"module":   absentResourcesEvent.ModuleName,
				}
				eventLogEntry := logEntry.WithFields(utils.LabelsToLogFields(logLabels))

				// Do not add ModuleRun task if it is already queued.
				hasTask := QueueHasPendingModuleRunTask(op.engine.TaskQueues.GetMain(), absentResourcesEvent.ModuleName)
				if !hasTask {
					newTask := sh_task.NewTask(task.ModuleRun).
						WithLogLabels(logLabels).
						WithQueueName("main").
						WithMetadata(task.HookMetadata{
							EventDescription: "DetectAbsentHelmResources",
							ModuleName:       absentResourcesEvent.ModuleName,
						})
					op.engine.TaskQueues.GetMain().AddLast(newTask.WithQueuedAt(time.Now()))
					taskAddDescription := fmt.Sprintf("got %d absent module resources, append", len(absentResourcesEvent.Absent))
					op.logTaskAdd(logEntry, taskAddDescription, newTask)
				} else {
					eventLogEntry.WithField("task.flow", "noop").Infof("Got %d absent module resources, ModuleRun task already queued", len(absentResourcesEvent.Absent))
				}
			}
		}
	}()
}

// TaskHandler handles tasks in queue.
func (op *AddonOperator) TaskHandler(t sh_task.Task) queue.TaskResult {
	taskLogLabels := t.GetLogLabels()
	taskLogEntry := log.WithFields(utils.LabelsToLogFields(taskLogLabels))
	var res queue.TaskResult

	op.logTaskStart(taskLogEntry, t)

	op.UpdateWaitInQueueMetric(t)

	switch t.GetType() {
	case task.GlobalHookRun:
		res = op.HandleGlobalHookRun(t, taskLogLabels)

	case task.GlobalHookEnableScheduleBindings:
		hm := task.HookMetadataAccessor(t)
		globalHook := op.ModuleManager.GetGlobalHook(hm.HookName)
		globalHook.HookController.EnableScheduleBindings()
		res.Status = queue.Success

	case task.GlobalHookEnableKubernetesBindings:
		res = op.HandleGlobalHookEnableKubernetesBindings(t, taskLogLabels)

	case task.GlobalHookWaitKubernetesSynchronization:
		res.Status = queue.Success
		if op.ModuleManager.GlobalSynchronizationNeeded() && !op.ModuleManager.GlobalSynchronizationState().IsComplete() {
			// dump state
			op.ModuleManager.GlobalSynchronizationState().DebugDumpState(taskLogEntry)
			t.WithQueuedAt(time.Now())
			res.Status = queue.Repeat
		} else {
			taskLogEntry.Info("Synchronization done for all global hooks")
		}

	case task.DiscoverHelmReleases:
		res = op.HandleDiscoverHelmReleases(t, taskLogLabels)

	case task.ConvergeModules:
		res = op.HandleConvergeModules(t, taskLogLabels)

	case task.ModuleRun:
		res = op.HandleModuleRun(t, taskLogLabels)

	case task.ModuleDelete:
		res.Status = op.HandleModuleDelete(t, taskLogLabels)

	case task.ModuleHookRun:
		res = op.HandleModuleHookRun(t, taskLogLabels)

	case task.ModulePurge:
		res.Status = op.HandleModulePurge(t, taskLogLabels)
	}

	if res.Status == queue.Success {
		origAfterHandle := res.AfterHandle
		res.AfterHandle = func() {
			op.CheckConvergeStatus(t)
			if origAfterHandle != nil {
				origAfterHandle()
			}
		}
	}

	op.logTaskEnd(taskLogEntry, t, res)

	return res
}

// UpdateWaitInQueueMetric increases task_wait_in_queue_seconds_total counter for the task type.
// TODO pass queue name from handler, not from task
func (op *AddonOperator) UpdateWaitInQueueMetric(t sh_task.Task) {
	metricLabels := map[string]string{
		"module":  "",
		"hook":    "",
		"binding": string(t.GetType()),
		"queue":   t.GetQueueName(),
	}

	hm := task.HookMetadataAccessor(t)

	switch t.GetType() {
	case task.GlobalHookRun,
		task.GlobalHookEnableScheduleBindings,
		task.GlobalHookEnableKubernetesBindings,
		task.GlobalHookWaitKubernetesSynchronization:
		metricLabels["hook"] = hm.HookName

	case task.ModuleRun,
		task.ModuleDelete,
		task.ModuleHookRun,
		task.ModulePurge:
		metricLabels["module"] = hm.ModuleName

	case task.ConvergeModules,
		task.DiscoverHelmReleases:
		// no action required
	}

	if t.GetType() == task.GlobalHookRun {
		// set binding name instead of type
		metricLabels["binding"] = hm.Binding
	}
	if t.GetType() == task.ModuleHookRun {
		// set binding name instead of type
		metricLabels["hook"] = hm.HookName
		metricLabels["binding"] = hm.Binding
	}

	taskWaitTime := time.Since(t.GetQueuedAt()).Seconds()
	op.engine.MetricStorage.CounterAdd("{PREFIX}task_wait_in_queue_seconds_total", taskWaitTime, metricLabels)
}

// HandleGlobalHookEnableKubernetesBindings add Synchronization tasks.
func (op *AddonOperator) HandleGlobalHookEnableKubernetesBindings(t sh_task.Task, labels map[string]string) (res queue.TaskResult) {
	defer trace.StartRegion(context.Background(), "DiscoverHelmReleases").End()

	logEntry := log.WithFields(utils.LabelsToLogFields(labels))
	logEntry.Debugf("Global hook enable kubernetes bindings")

	hm := task.HookMetadataAccessor(t)
	globalHook := op.ModuleManager.GetGlobalHook(hm.HookName)

	mainSyncTasks := make([]sh_task.Task, 0)
	parallelSyncTasks := make([]sh_task.Task, 0)
	parallelSyncTasksToWait := make([]sh_task.Task, 0)
	queuedAt := time.Now()

	newLogLabels := utils.MergeLabels(t.GetLogLabels())
	delete(newLogLabels, "task.id")

	err := op.ModuleManager.HandleGlobalEnableKubernetesBindings(hm.HookName, func(hook *module_manager.GlobalHook, info controller.BindingExecutionInfo) {
		taskLogLabels := utils.MergeLabels(t.GetLogLabels(), map[string]string{
			"binding":   string(htypes.OnKubernetesEvent) + "Synchronization",
			"hook":      hook.GetName(),
			"hook.type": "global",
			"queue":     info.QueueName,
		})
		if len(info.BindingContext) > 0 {
			taskLogLabels["binding.name"] = info.BindingContext[0].Binding
		}
		delete(taskLogLabels, "task.id")

		kubernetesBindingID := uuid.Must(uuid.NewV4()).String()
		newTask := sh_task.NewTask(task.GlobalHookRun).
			WithLogLabels(taskLogLabels).
			WithQueueName(info.QueueName).
			WithMetadata(task.HookMetadata{
				EventDescription:         hm.EventDescription,
				HookName:                 hook.GetName(),
				BindingType:              htypes.OnKubernetesEvent,
				BindingContext:           info.BindingContext,
				AllowFailure:             info.AllowFailure,
				ReloadAllOnValuesChanges: false, // Ignore global values changes in global Synchronization tasks.
				KubernetesBindingId:      kubernetesBindingID,
				WaitForSynchronization:   info.KubernetesBinding.WaitForSynchronization,
				MonitorIDs:               []string{info.KubernetesBinding.Monitor.Metadata.MonitorId},
				ExecuteOnSynchronization: info.KubernetesBinding.ExecuteHookOnSynchronization,
			})
		newTask.WithQueuedAt(queuedAt)

		if info.QueueName == t.GetQueueName() {
			// Ignore "waitForSynchronization: false" for hooks in the main queue.
			// There is no way to not wait for these hooks.
			mainSyncTasks = append(mainSyncTasks, newTask)
		} else {
			// Do not wait for parallel hooks on "waitForSynchronization: false".
			if info.KubernetesBinding.WaitForSynchronization {
				parallelSyncTasksToWait = append(parallelSyncTasksToWait, newTask)
			} else {
				parallelSyncTasks = append(parallelSyncTasks, newTask)
			}
		}
	})
	if err != nil {
		hookLabel := path.Base(globalHook.Path)
		// TODO use separate metric, as in shell-operator?
		op.engine.MetricStorage.CounterAdd("{PREFIX}global_hook_errors_total", 1.0, map[string]string{
			"hook":       hookLabel,
			"binding":    "GlobalEnableKubernetesBindings",
			"queue":      t.GetQueueName(),
			"activation": "OperatorStartup",
		})
		logEntry.Errorf("Global hook enable kubernetes bindings failed, requeue task to retry after delay. Failed count is %d. Error: %s", t.GetFailureCount()+1, err)
		t.UpdateFailureMessage(err.Error())
		t.WithQueuedAt(queuedAt)
		res.Status = queue.Fail
		return
	}
	// Substitute current task with Synchronization tasks for the main queue.
	// Other Synchronization tasks are queued into specified queues.
	// Informers can be started now — their events will be added to the queue tail.
	logEntry.Debugf("Global hook enable kubernetes bindings success")

	// "Wait" tasks are queued first
	for _, tsk := range parallelSyncTasksToWait {
		q := op.engine.TaskQueues.GetByName(tsk.GetQueueName())
		if q == nil {
			log.Errorf("Queue %s is not created while run GlobalHookEnableKubernetesBindings task!", tsk.GetQueueName())
		} else {
			// Skip state creation if WaitForSynchronization is disabled.
			thm := task.HookMetadataAccessor(tsk)
			q.AddLast(tsk)
			op.ModuleManager.GlobalSynchronizationState().QueuedForBinding(thm)
		}
	}
	op.logTaskAdd(logEntry, "append", parallelSyncTasksToWait...)

	for _, tsk := range parallelSyncTasks {
		q := op.engine.TaskQueues.GetByName(tsk.GetQueueName())
		if q == nil {
			log.Errorf("Queue %s is not created while run GlobalHookEnableKubernetesBindings task!", tsk.GetQueueName())
		} else {
			q.AddLast(tsk)
		}
	}
	op.logTaskAdd(logEntry, "append", parallelSyncTasks...)

	// Note: No need to add "main" Synchronization tasks to the GlobalSynchronizationState.
	res.HeadTasks = mainSyncTasks
	op.logTaskAdd(logEntry, "head", mainSyncTasks...)

	res.Status = queue.Success

	return
}

// HandleDiscoverHelmReleases runs RefreshStateFromHelmReleases to detect modules state at start.
func (op *AddonOperator) HandleDiscoverHelmReleases(t sh_task.Task, labels map[string]string) (res queue.TaskResult) {
	defer trace.StartRegion(context.Background(), "DiscoverHelmReleases").End()

	logEntry := log.WithFields(utils.LabelsToLogFields(labels))
	logEntry.Debugf("Discover Helm releases state")

	state, err := op.ModuleManager.RefreshStateFromHelmReleases(t.GetLogLabels())
	if err != nil {
		res.Status = queue.Fail
		logEntry.Errorf("Discover helm releases failed, requeue task to retry after delay. Failed count is %d. Error: %s", t.GetFailureCount()+1, err)
		t.UpdateFailureMessage(err.Error())
		t.WithQueuedAt(time.Now())
	} else {
		res.Status = queue.Success
		state.ModulesToPurge = append(state.ModulesToPurge, op.ExplicitlyPurgeModules...)

		log.Debugf("Next Modules will be purged: %v", state.ModulesToPurge)

		tasks := op.CreatePurgeTasks(state.ModulesToPurge, t)
		res.AfterTasks = tasks
		op.logTaskAdd(logEntry, "after", res.AfterTasks...)
	}
	return
}

// HandleModulePurge run helm purge for unknown module.
func (op *AddonOperator) HandleModulePurge(t sh_task.Task, labels map[string]string) (status queue.TaskStatus) {
	defer trace.StartRegion(context.Background(), "ModulePurge").End()

	logEntry := log.WithFields(utils.LabelsToLogFields(labels))
	logEntry.Debugf("Module purge start")

	hm := task.HookMetadataAccessor(t)
	err := op.Helm.NewClient(t.GetLogLabels()).DeleteRelease(hm.ModuleName)
	if err != nil {
		// Purge is for unknown modules, just print warning.
		logEntry.Warnf("Module purge failed, no retry. Error: %s", err)
	} else {
		logEntry.Debugf("Module purge success")
	}
	status = queue.Success
	return
}

// HandleModuleDelete deletes helm release for known module.
func (op *AddonOperator) HandleModuleDelete(t sh_task.Task, labels map[string]string) (status queue.TaskStatus) {
	defer trace.StartRegion(context.Background(), "ModuleDelete").End()

	hm := task.HookMetadataAccessor(t)
	module := op.ModuleManager.GetModule(hm.ModuleName)

	logEntry := log.WithFields(utils.LabelsToLogFields(labels))
	logEntry.Debugf("Module delete '%s'", hm.ModuleName)

	// Register module hooks to run afterHelmDelete hooks on startup.
	// It's a noop if registration is done before.
	err := op.ModuleManager.RegisterModuleHooks(module, t.GetLogLabels())

	// TODO disable events and drain queues here or earlier during ConvergeModules.RunBeforeAll phase?
	if err == nil {
		// Disable events
		// op.ModuleManager.DisableModuleHooks(hm.ModuleName)
		// Remove all hooks from parallel queues.
		op.DrainModuleQueues(hm.ModuleName)
		err = op.ModuleManager.DeleteModule(hm.ModuleName, t.GetLogLabels())
	}

	module.State.LastModuleErr = err
	if err != nil {
		op.engine.MetricStorage.CounterAdd("{PREFIX}module_delete_errors_total", 1.0, map[string]string{"module": hm.ModuleName})
		logEntry.Errorf("Module delete failed, requeue task to retry after delay. Failed count is %d. Error: %s", t.GetFailureCount()+1, err)
		t.UpdateFailureMessage(err.Error())
		t.WithQueuedAt(time.Now())
		status = queue.Fail
	} else {
		logEntry.Debugf("Module delete success '%s'", hm.ModuleName)
		status = queue.Success
	}

	return
}

// HandleModuleRun starts a module by executing module hooks and installing a Helm chart.
//
// Execution sequence:
// - Run onStartup hooks.
// - Queue kubernetes hooks as Synchronization tasks.
// - Wait until Synchronization tasks are done to fill all snapshots.
// - Enable kubernetes events.
// - Enable schedule events.
// - Run beforeHelm hooks
// - Run Helm to install or upgrade a module's chart.
// - Run afterHelm hooks.
//
// ModuleRun is restarted if hook or chart is failed.
// After first HandleModuleRun success, no onStartup and kubernetes.Synchronization tasks will run.
func (op *AddonOperator) HandleModuleRun(t sh_task.Task, labels map[string]string) (res queue.TaskResult) {
	defer trace.StartRegion(context.Background(), "ModuleRun").End()
	logEntry := log.WithFields(utils.LabelsToLogFields(labels))

	hm := task.HookMetadataAccessor(t)
	module := op.ModuleManager.GetModule(hm.ModuleName)

	// Break error loop when module becomes disabled.
	if !op.ModuleManager.IsModuleEnabled(module.Name) {
		res.Status = queue.Success
		return
	}

	metricLabels := map[string]string{
		"module":     hm.ModuleName,
		"activation": labels["event.type"],
	}

	defer measure.Duration(func(d time.Duration) {
		op.engine.MetricStorage.HistogramObserve("{PREFIX}module_run_seconds", d.Seconds(), metricLabels, nil)
	})()

	var moduleRunErr error
	valuesChanged := false

	// First module run on operator startup or when module is enabled.
	if module.State.Phase == module_manager.Startup {
		// Register module hooks on every enable.
		moduleRunErr = op.ModuleManager.RegisterModuleHooks(module, labels)
		if moduleRunErr == nil {
			if hm.DoModuleStartup {
				logEntry.Debugf("ModuleRun '%s' phase", module.State.Phase)

				treg := trace.StartRegion(context.Background(), "ModuleRun-OnStartup")

				// Start queues for module hooks.
				op.CreateAndStartQueuesForModuleHooks(module.Name)

				// Run onStartup hooks.
				moduleRunErr = module.RunOnStartup(t.GetLogLabels())
				if moduleRunErr == nil {
					module.State.Phase = module_manager.OnStartupDone
				}
				treg.End()
			} else {
				module.State.Phase = module_manager.OnStartupDone
			}
		}
	}

	if module.State.Phase == module_manager.OnStartupDone {
		logEntry.Debugf("ModuleRun '%s' phase", module.State.Phase)
		if module.HasKubernetesHooks() {
			module.State.Phase = module_manager.QueueSynchronizationTasks
		} else {
			// Skip Synchronization process if there are no kubernetes hooks.
			module.State.Phase = module_manager.EnableScheduleBindings
		}
	}

	// Note: All hooks should be queued to fill snapshots before proceed to beforeHelm hooks.
	if module.State.Phase == module_manager.QueueSynchronizationTasks {
		logEntry.Debugf("ModuleRun '%s' phase", module.State.Phase)

		// ModuleHookRun.Synchronization tasks for bindings with the "main" queue.
		mainSyncTasks := make([]sh_task.Task, 0)
		// ModuleHookRun.Synchronization tasks to add in parallel queues.
		parallelSyncTasks := make([]sh_task.Task, 0)
		// Wait for these ModuleHookRun.Synchronization tasks from parallel queues.
		parallelSyncTasksToWait := make([]sh_task.Task, 0)

		// Start monitors for each kubernetes binding in each module hook.
		err := op.ModuleManager.HandleModuleEnableKubernetesBindings(hm.ModuleName, func(hook *module_manager.ModuleHook, info controller.BindingExecutionInfo) {
			queueName := info.QueueName
			taskLogLabels := utils.MergeLabels(t.GetLogLabels(), map[string]string{
				"binding":   string(htypes.OnKubernetesEvent) + "Synchronization",
				"module":    hm.ModuleName,
				"hook":      hook.GetName(),
				"hook.type": "module",
				"queue":     queueName,
			})
			if len(info.BindingContext) > 0 {
				taskLogLabels["binding.name"] = info.BindingContext[0].Binding
			}
			delete(taskLogLabels, "task.id")

			kubernetesBindingID := uuid.Must(uuid.NewV4()).String()
			taskMeta := task.HookMetadata{
				EventDescription:         hm.EventDescription,
				ModuleName:               hm.ModuleName,
				HookName:                 hook.GetName(),
				BindingType:              htypes.OnKubernetesEvent,
				BindingContext:           info.BindingContext,
				AllowFailure:             info.AllowFailure,
				KubernetesBindingId:      kubernetesBindingID,
				WaitForSynchronization:   info.KubernetesBinding.WaitForSynchronization,
				MonitorIDs:               []string{info.KubernetesBinding.Monitor.Metadata.MonitorId},
				ExecuteOnSynchronization: info.KubernetesBinding.ExecuteHookOnSynchronization,
			}
			newTask := sh_task.NewTask(task.ModuleHookRun).
				WithLogLabels(taskLogLabels).
				WithQueueName(queueName).
				WithMetadata(taskMeta)
			newTask.WithQueuedAt(time.Now())

			if info.QueueName == t.GetQueueName() {
				// Ignore "waitForSynchronization: false" for hooks in the main queue.
				// There is no way to not wait for these hooks.
				mainSyncTasks = append(mainSyncTasks, newTask)
			} else {
				// Do not wait for parallel hooks on "waitForSynchronization: false".
				if info.KubernetesBinding.WaitForSynchronization {
					parallelSyncTasksToWait = append(parallelSyncTasksToWait, newTask)
				} else {
					parallelSyncTasks = append(parallelSyncTasks, newTask)
				}
			}
		})
		if err != nil {
			// Fail to enable bindings: cannot start Kubernetes monitors.
			moduleRunErr = err
		} else {
			// Queue parallel tasks that should be waited.
			for _, tsk := range parallelSyncTasksToWait {
				q := op.engine.TaskQueues.GetByName(tsk.GetQueueName())
				if q == nil {
					logEntry.Errorf("queue %s is not found while EnableKubernetesBindings task", tsk.GetQueueName())
				} else {
					thm := task.HookMetadataAccessor(tsk)
					q.AddLast(tsk)
					module.State.Synchronization().QueuedForBinding(thm)
				}
			}
			op.logTaskAdd(logEntry, "append", parallelSyncTasksToWait...)

			// Queue regular parallel tasks.
			for _, tsk := range parallelSyncTasks {
				q := op.engine.TaskQueues.GetByName(tsk.GetQueueName())
				if q == nil {
					logEntry.Errorf("queue %s is not found while EnableKubernetesBindings task", tsk.GetQueueName())
				} else {
					q.AddLast(tsk)
				}
			}
			op.logTaskAdd(logEntry, "append", parallelSyncTasks...)

			if len(parallelSyncTasksToWait) == 0 {
				// Skip waiting tasks in parallel queues, proceed to schedule bindings.
				module.State.Phase = module_manager.EnableScheduleBindings
			} else {
				// There are tasks to wait.
				module.State.Phase = module_manager.WaitForSynchronization
				logEntry.WithField("module.state", "wait-for-synchronization").
					Debugf("ModuleRun wait for Synchronization")
			}

			// Put Synchronization tasks for kubernetes hooks before ModuleRun task.
			if len(mainSyncTasks) > 0 {
				res.HeadTasks = mainSyncTasks
				res.Status = queue.Keep
				op.logTaskAdd(logEntry, "head", mainSyncTasks...)
				return
			}
		}
	}

	// Repeat ModuleRun if there are running Synchronization tasks to wait.
	if module.State.Phase == module_manager.WaitForSynchronization {
		if module.State.Synchronization().IsComplete() {
			// Proceed with the next phase.
			module.State.Phase = module_manager.EnableScheduleBindings
			logEntry.Info("Synchronization done for module hooks")
		} else {
			// Debug messages every fifth second: print Synchronization state.
			if time.Now().UnixNano()%5000000000 == 0 {
				logEntry.Debugf("ModuleRun wait Synchronization state: moduleStartup:%v syncNeeded:%v syncQueued:%v syncDone:%v", hm.DoModuleStartup, module.SynchronizationNeeded(), module.State.Synchronization().HasQueued(), module.State.Synchronization().IsComplete())
				module.State.Synchronization().DebugDumpState(logEntry)
			}
			logEntry.Debugf("Synchronization not complete, keep ModuleRun task in repeat mode")
			t.WithQueuedAt(time.Now())
			res.Status = queue.Repeat
			return
		}
	}

	// Enable schedule events once at module start.
	if module.State.Phase == module_manager.EnableScheduleBindings {
		logEntry.Debugf("ModuleRun '%s' phase", module.State.Phase)

		op.ModuleManager.EnableModuleScheduleBindings(hm.ModuleName)
		module.State.Phase = module_manager.CanRunHelm
	}

	// Module start is done, module is ready to run hooks and helm chart.
	if module.State.Phase == module_manager.CanRunHelm {
		logEntry.Debugf("ModuleRun '%s' phase", module.State.Phase)
		// run beforeHelm, helm, afterHelm
		valuesChanged, moduleRunErr = module.Run(t.GetLogLabels())
	}

	module.State.LastModuleErr = moduleRunErr
	if moduleRunErr != nil {
		res.Status = queue.Fail
		logEntry.Errorf("ModuleRun failed in phase '%s'. Requeue task to retry after delay. Failed count is %d. Error: %s", module.State.Phase, t.GetFailureCount()+1, moduleRunErr)
		op.engine.MetricStorage.CounterAdd("{PREFIX}module_run_errors_total", 1.0, map[string]string{"module": hm.ModuleName})
		t.UpdateFailureMessage(moduleRunErr.Error())
		t.WithQueuedAt(time.Now())
	} else {
		res.Status = queue.Success
		if valuesChanged {
			logEntry.Infof("ModuleRun success, values changed, restart module")
			// One of afterHelm hooks changes values, run ModuleRun again: copy task, but disable startup hooks.
			hm.DoModuleStartup = false
			hm.EventDescription = "AfterHelm-Hooks-Change-Values"
			newLabels := utils.MergeLabels(t.GetLogLabels())
			delete(newLabels, "task.id")
			newTask := sh_task.NewTask(task.ModuleRun).
				WithLogLabels(newLabels).
				WithQueueName(t.GetQueueName()).
				WithMetadata(hm)

			res.AfterTasks = []sh_task.Task{newTask.WithQueuedAt(time.Now())}
			op.logTaskAdd(logEntry, "after", res.AfterTasks...)
		} else {
			logEntry.Infof("ModuleRun success, module is ready")
		}
	}
	return
}

func (op *AddonOperator) HandleModuleHookRun(t sh_task.Task, labels map[string]string) (res queue.TaskResult) {
	defer trace.StartRegion(context.Background(), "ModuleHookRun").End()

	logEntry := log.WithFields(utils.LabelsToLogFields(labels))

	hm := task.HookMetadataAccessor(t)
	taskHook := op.ModuleManager.GetModuleHook(hm.HookName)

	// Prevent hook running in parallel queue if module is disabled in "main" queue.
	if !op.ModuleManager.IsModuleEnabled(taskHook.Module.Name) {
		res.Status = queue.Success
		return
	}

	err := taskHook.RateLimitWait(context.Background())
	if err != nil {
		// This could happen when the Context is
		// canceled, or the expected wait time exceeds the Context's Deadline.
		// The best we can do without proper context usage is to repeat the task.
		res.Status = queue.Repeat
		return
	}

	metricLabels := map[string]string{
		"module":     hm.ModuleName,
		"hook":       hm.HookName,
		"binding":    string(hm.BindingType),
		"queue":      t.GetQueueName(),
		"activation": labels["event.type"],
	}

	defer measure.Duration(func(d time.Duration) {
		op.engine.MetricStorage.HistogramObserve("{PREFIX}module_hook_run_seconds", d.Seconds(), metricLabels, nil)
	})()

	shouldRunHook := true

	isSynchronization := hm.IsSynchronization()
	if isSynchronization {
		// Synchronization is not a part of v0 contract, skip hook execution.
		if taskHook.Config.Version == "v0" {
			shouldRunHook = false
			res.Status = queue.Success
		}
		// Check for "executeOnSynchronization: false".
		if !hm.ExecuteOnSynchronization {
			shouldRunHook = false
			res.Status = queue.Success
		}
	}

	// Combine tasks in the queue and compact binding contexts for v1 hooks.
	if shouldRunHook && taskHook.Config.Version == "v1" {
		combineResult := op.engine.CombineBindingContextForHook(op.engine.TaskQueues.GetByName(t.GetQueueName()), t, func(tsk sh_task.Task) bool {
			thm := task.HookMetadataAccessor(tsk)
			// Stop combine process when Synchronization tasks have different
			// values in WaitForSynchronization or ExecuteOnSynchronization fields.
			if hm.KubernetesBindingId != "" && thm.KubernetesBindingId != "" {
				if hm.WaitForSynchronization != thm.WaitForSynchronization {
					return true
				}
				if hm.ExecuteOnSynchronization != thm.ExecuteOnSynchronization {
					return true
				}
			}
			// Task 'tsk' will be combined, so remove it from the SynchronizationState.
			if thm.IsSynchronization() {
				logEntry.Debugf("Synchronization task for %s/%s is combined, mark it as Done: id=%s", thm.HookName, thm.Binding, thm.KubernetesBindingId)
				taskHook.Module.State.Synchronization().DoneForBinding(thm.KubernetesBindingId)
			}
			return false // do not stop combine process on this task
		})

		if combineResult != nil {
			hm.BindingContext = combineResult.BindingContexts
			// Extra monitor IDs can be returned if several Synchronization binding contexts are combined.
			if len(combineResult.MonitorIDs) > 0 {
				hm.MonitorIDs = append(hm.MonitorIDs, combineResult.MonitorIDs...)
			}
			logEntry.Debugf("Got monitorIDs: %+v", hm.MonitorIDs)
			t.UpdateMetadata(hm)
		}
	}

	if shouldRunHook {
		// Module hook can recreate helm objects, so pause resources monitor.
		// Parallel hooks can interfere, so pause-resume only for hooks in the main queue.
		// FIXME pause-resume for parallel hooks
		if t.GetQueueName() == "main" {
			op.HelmResourcesManager.PauseMonitor(hm.ModuleName)
			defer op.HelmResourcesManager.ResumeMonitor(hm.ModuleName)
		}

		errors := 0.0
		success := 0.0
		allowed := 0.0

		beforeChecksum, afterChecksum, err := op.ModuleManager.RunModuleHook(hm.HookName, hm.BindingType, hm.BindingContext, t.GetLogLabels())
		if err != nil {
			if hm.AllowFailure {
				allowed = 1.0
				logEntry.Infof("Module hook failed, but allowed to fail. Error: %v", err)
				res.Status = queue.Success
				taskHook.Module.State.SetLastHookErr(hm.HookName, nil)
			} else {
				errors = 1.0
				logEntry.Errorf("Module hook failed, requeue task to retry after delay. Failed count is %d. Error: %s", t.GetFailureCount()+1, err)
				t.UpdateFailureMessage(err.Error())
				t.WithQueuedAt(time.Now())
				res.Status = queue.Fail
				taskHook.Module.State.SetLastHookErr(hm.HookName, err)
			}
		} else {
			success = 1.0
			logEntry.Debugf("Module hook success '%s'", hm.HookName)
			res.Status = queue.Success
			taskHook.Module.State.SetLastHookErr(hm.HookName, nil)

			// Handle module values change.
			reloadModule := false
			eventDescription := ""
			switch hm.BindingType {
			case htypes.Schedule:
				if beforeChecksum != afterChecksum {
					logEntry.Infof("Module hook changed values, will restart ModuleRun.")
					reloadModule = true
					eventDescription = "Schedule-Change-ModuleValues"
				}
			case htypes.OnKubernetesEvent:
				// Do not reload module on changes during Synchronization.
				if beforeChecksum != afterChecksum {
					if hm.IsSynchronization() {
						logEntry.Infof("Module hook changed values, but restart ModuleRun is ignored for the Synchronization task.")
					} else {
						logEntry.Infof("Module hook changed values, will restart ModuleRun.")
						reloadModule = true
						eventDescription = "Kubernetes-Change-ModuleValues"
					}
				}
			}
			if reloadModule {
				// relabel
				logLabels := t.GetLogLabels()
				// Save event source info to add it as props to the task and use in logger later.
				triggeredBy := log.Fields{
					"event.triggered-by.hook":         logLabels["hook"],
					"event.triggered-by.binding":      logLabels["binding"],
					"event.triggered-by.binding.name": logLabels["binding.name"],
					"event.triggered-by.watchEvent":   logLabels["watchEvent"],
				}
				delete(logLabels, "hook")
				delete(logLabels, "hook.type")
				delete(logLabels, "binding")
				delete(logLabels, "binding.name")
				delete(logLabels, "watchEvent")

				// Do not add ModuleRun task if it is already queued.
				hasTask := QueueHasPendingModuleRunTask(op.engine.TaskQueues.GetMain(), hm.ModuleName)
				if !hasTask {
					newTask := sh_task.NewTask(task.ModuleRun).
						WithLogLabels(logLabels).
						WithQueueName("main").
						WithMetadata(task.HookMetadata{
							EventDescription: eventDescription,
							ModuleName:       hm.ModuleName,
						})
					newTask.SetProp("triggered-by", triggeredBy)

					op.engine.TaskQueues.GetMain().AddLast(newTask.WithQueuedAt(time.Now()))
					op.logTaskAdd(logEntry, "module values are changed, append", newTask)
				} else {
					logEntry.WithField("task.flow", "noop").Infof("module values are changed, ModuleRun task already queued")
				}
			}
		}

		op.engine.MetricStorage.CounterAdd("{PREFIX}module_hook_allowed_errors_total", allowed, metricLabels)
		op.engine.MetricStorage.CounterAdd("{PREFIX}module_hook_errors_total", errors, metricLabels)
		op.engine.MetricStorage.CounterAdd("{PREFIX}module_hook_success_total", success, metricLabels)
	}

	if isSynchronization && res.Status == queue.Success {
		taskHook.Module.State.Synchronization().DoneForBinding(hm.KubernetesBindingId)
		// Unlock Kubernetes events for all monitors when Synchronization task is done.
		logEntry.Debug("Synchronization done, unlock Kubernetes events")
		for _, monitorID := range hm.MonitorIDs {
			taskHook.HookController.UnlockKubernetesEventsFor(monitorID)
		}
	}

	return res
}

func (op *AddonOperator) HandleGlobalHookRun(t sh_task.Task, labels map[string]string) (res queue.TaskResult) {
	defer trace.StartRegion(context.Background(), "GlobalHookRun").End()

	logEntry := log.WithFields(utils.LabelsToLogFields(labels))

	hm := task.HookMetadataAccessor(t)
	taskHook := op.ModuleManager.GetGlobalHook(hm.HookName)

	err := taskHook.RateLimitWait(context.Background())
	if err != nil {
		// This could happen when the Context is
		// canceled, or the expected wait time exceeds the Context's Deadline.
		// The best we can do without proper context usage is to repeat the task.
		return queue.TaskResult{
			Status: "Repeat",
		}
	}

	metricLabels := map[string]string{
		"hook":       hm.HookName,
		"binding":    string(hm.BindingType),
		"queue":      t.GetQueueName(),
		"activation": labels["event.type"],
	}

	defer measure.Duration(func(d time.Duration) {
		op.engine.MetricStorage.HistogramObserve("{PREFIX}global_hook_run_seconds", d.Seconds(), metricLabels, nil)
	})()

	isSynchronization := hm.IsSynchronization()
	shouldRunHook := true
	if isSynchronization {
		// Synchronization is not a part of v0 contract, skip hook execution.
		if taskHook.Config.Version == "v0" {
			logEntry.Infof("Execute on Synchronization ignored for v0 hooks")
			shouldRunHook = false
			res.Status = queue.Success
		}
		// Check for "executeOnSynchronization: false".
		if !hm.ExecuteOnSynchronization {
			logEntry.Infof("Execute on Synchronization disabled in hook config: ExecuteOnSynchronization=false")
			shouldRunHook = false
			res.Status = queue.Success
		}
	}

	if shouldRunHook && taskHook.Config.Version == "v1" {
		// Combine binding contexts in the queue.
		combineResult := op.engine.CombineBindingContextForHook(op.engine.TaskQueues.GetByName(t.GetQueueName()), t, func(tsk sh_task.Task) bool {
			thm := task.HookMetadataAccessor(tsk)
			// Stop combine process when Synchronization tasks have different
			// values in WaitForSynchronization or ExecuteOnSynchronization fields.
			if hm.KubernetesBindingId != "" && thm.KubernetesBindingId != "" {
				if hm.WaitForSynchronization != thm.WaitForSynchronization {
					return true
				}
				if hm.ExecuteOnSynchronization != thm.ExecuteOnSynchronization {
					return true
				}
			}
			// Task 'tsk' will be combined, so remove it from the GlobalSynchronizationState.
			if thm.IsSynchronization() {
				logEntry.Debugf("Synchronization task for %s/%s is combined, mark it as Done: id=%s", thm.HookName, thm.Binding, thm.KubernetesBindingId)
				op.ModuleManager.GlobalSynchronizationState().DoneForBinding(thm.KubernetesBindingId)
			}
			return false // Combine tsk.
		})

		if combineResult != nil {
			hm.BindingContext = combineResult.BindingContexts
			// Extra monitor IDs can be returned if several Synchronization binding contexts are combined.
			if len(combineResult.MonitorIDs) > 0 {
				logEntry.Debugf("Task monitorID: %s, combined monitorIDs: %+v", hm.MonitorIDs, combineResult.MonitorIDs)
				hm.MonitorIDs = combineResult.MonitorIDs
			}
			logEntry.Debugf("Got monitorIDs: %+v", hm.MonitorIDs)
			t.UpdateMetadata(hm)
		}
	}

	// TODO create metadata flag that indicate whether to add reload all task on values changes
	// op.HelmResourcesManager.PauseMonitors()

	if shouldRunHook {
		logEntry.Debugf("Global hook run")

		errors := 0.0
		success := 0.0
		allowed := 0.0
		// Save a checksum of *Enabled values.
		dynamicEnabledChecksumBeforeHookRun := op.ModuleManager.DynamicEnabledChecksum()
		// Run Global hook.
		beforeChecksum, afterChecksum, err := op.ModuleManager.RunGlobalHook(hm.HookName, hm.BindingType, hm.BindingContext, t.GetLogLabels())
		if err != nil {
			if hm.AllowFailure {
				allowed = 1.0
				logEntry.Infof("Global hook failed, but allowed to fail. Error: %v", err)
				res.Status = queue.Success
			} else {
				errors = 1.0
				logEntry.Errorf("Global hook failed, requeue task to retry after delay. Failed count is %d. Error: %s", t.GetFailureCount()+1, err)
				t.UpdateFailureMessage(err.Error())
				t.WithQueuedAt(time.Now())
				res.Status = queue.Fail
			}
		} else {
			// Calculate new checksum of *Enabled values.
			dynamicEnabledChecksumAfterHookRun := op.ModuleManager.DynamicEnabledChecksum()
			success = 1.0
			logEntry.Debugf("GlobalHookRun success, checksums: before=%s after=%s saved=%s", beforeChecksum, afterChecksum, hm.ValuesChecksum)
			res.Status = queue.Success

			reloadAll := false
			eventDescription := ""
			switch hm.BindingType {
			case htypes.Schedule:
				if beforeChecksum != afterChecksum {
					logEntry.Infof("Global hook changed values, will run ReloadAll.")
					reloadAll = true
					eventDescription = "Schedule-Change-GlobalValues"
				}
				if dynamicEnabledChecksumBeforeHookRun != dynamicEnabledChecksumAfterHookRun {
					logEntry.Infof("Global hook changed dynamic enabled modules list, will run ReloadAll.")
					reloadAll = true
					if eventDescription == "" {
						eventDescription = "Schedule-Change-DynamicEnabled"
					} else {
						eventDescription += "-And-DynamicEnabled"
					}
				}
			case htypes.OnKubernetesEvent:
				if beforeChecksum != afterChecksum {
					if hm.ReloadAllOnValuesChanges {
						logEntry.Infof("Global hook changed values, will run ReloadAll.")
						reloadAll = true
						eventDescription = "Kubernetes-Change-GlobalValues"
					} else {
						logEntry.Infof("Global hook changed values, but ReloadAll ignored for the Synchronization task.")
					}
				}
				if dynamicEnabledChecksumBeforeHookRun != dynamicEnabledChecksumAfterHookRun {
					logEntry.Infof("Global hook changed dynamic enabled modules list, will run ReloadAll.")
					reloadAll = true
					if eventDescription == "" {
						eventDescription = "Kubernetes-Change-DynamicEnabled"
					} else {
						eventDescription += "-And-DynamicEnabled"
					}
				}
			case hookTypes.AfterAll:
				if !hm.LastAfterAllHook && afterChecksum != beforeChecksum {
					logEntry.Infof("Global hook changed values, but ReloadAll ignored: more AfterAll hooks to execute.")
				}

				// values are changed when afterAll hooks are executed
				if hm.LastAfterAllHook && afterChecksum != hm.ValuesChecksum {
					logEntry.Infof("Global values changed by AfterAll hooks, will run ReloadAll.")
					reloadAll = true
					eventDescription = "AfterAll-Hooks-Change-GlobalValues"
				}

				// values are changed when afterAll hooks are executed
				if hm.LastAfterAllHook && dynamicEnabledChecksumAfterHookRun != hm.DynamicEnabledChecksum {
					logEntry.Infof("Dynamic enabled modules list changed by AfterAll hooks, will run ReloadAll.")
					reloadAll = true
					if eventDescription == "" {
						eventDescription = "AfterAll-Hooks-Change-DynamicEnabled"
					} else {
						eventDescription += "-And-DynamicEnabled"
					}
				}
			}
			// Queue ReloadAllModules task
			if reloadAll {
				// Stop and remove all resource monitors to prevent excessive ModuleRun tasks
				op.HelmResourcesManager.StopMonitors()
				logLabels := t.GetLogLabels()
				// Save event source info to add it as props to the task and use in logger later.
				triggeredBy := log.Fields{
					"event.triggered-by.hook":         logLabels["hook"],
					"event.triggered-by.binding":      logLabels["binding"],
					"event.triggered-by.binding.name": logLabels["binding.name"],
					"event.triggered-by.watchEvent":   logLabels["watchEvent"],
				}
				delete(logLabels, "hook")
				delete(logLabels, "hook.type")
				delete(logLabels, "binding")
				delete(logLabels, "binding.name")
				delete(logLabels, "watchEvent")

				// Reload all using "ConvergeModules" task.
				newTask := NewConvergeModulesTask(eventDescription, GlobalValuesChanged, logLabels)
				newTask.SetProp("triggered-by", triggeredBy)
				op.engine.TaskQueues.GetMain().AddLast(newTask)
				op.logTaskAdd(logEntry, "global values are changed, append", newTask)
			}
			// TODO rethink helm monitors pause-resume. It is not working well with parallel hooks without locks. But locks will destroy parallelization.
			// else {
			//	op.HelmResourcesManager.ResumeMonitors()
			//}
		}

		op.engine.MetricStorage.CounterAdd("{PREFIX}global_hook_allowed_errors_total", allowed, metricLabels)
		op.engine.MetricStorage.CounterAdd("{PREFIX}global_hook_errors_total", errors, metricLabels)
		op.engine.MetricStorage.CounterAdd("{PREFIX}global_hook_success_total", success, metricLabels)
	}

	if isSynchronization && res.Status == queue.Success {
		op.ModuleManager.GlobalSynchronizationState().DoneForBinding(hm.KubernetesBindingId)
		// Unlock Kubernetes events for all monitors when Synchronization task is done.
		logEntry.Debugf("Synchronization done, unlock Kubernetes events")
		for _, monitorID := range hm.MonitorIDs {
			taskHook.HookController.UnlockKubernetesEventsFor(monitorID)
		}
	}

	return res
}

func (op *AddonOperator) CreateReloadModulesTasks(moduleNames []string, logLabels map[string]string, eventDescription string) []sh_task.Task {
	newTasks := make([]sh_task.Task, 0, len(moduleNames))
	queuedAt := time.Now()

	queuedModuleNames := ModulesWithPendingModuleRun(op.engine.TaskQueues.GetMain())

	// Add ModuleRun tasks to reload only specific modules.
	for _, moduleName := range moduleNames {
		// No task is required if ModuleRun is already in queue.
		if _, has := queuedModuleNames[moduleName]; has {
			continue
		}

		newLogLabels := utils.MergeLabels(logLabels)
		newLogLabels["module"] = moduleName
		delete(newLogLabels, "task.id")

		newTask := sh_task.NewTask(task.ModuleRun).
			WithLogLabels(newLogLabels).
			WithQueueName("main").
			WithMetadata(task.HookMetadata{
				EventDescription: eventDescription,
				ModuleName:       moduleName,
			})
		newTasks = append(newTasks, newTask.WithQueuedAt(queuedAt))
	}
	return newTasks
}

// CreateConvergeModulesTasks creates ModuleRun/ModuleDelete tasks based on moduleManager state.
func (op *AddonOperator) CreateConvergeModulesTasks(state *module_manager.ModulesState, logLabels map[string]string, eventDescription string) []sh_task.Task {
	newTasks := make([]sh_task.Task, 0, len(state.ModulesToDisable)+len(state.AllEnabledModules))
	queuedAt := time.Now()

	// Add ModuleDelete tasks to delete helm releases of disabled modules.
	for _, moduleName := range state.ModulesToDisable {
		newLogLabels := utils.MergeLabels(logLabels)
		newLogLabels["module"] = moduleName
		delete(newLogLabels, "task.id")

		newTask := sh_task.NewTask(task.ModuleDelete).
			WithLogLabels(newLogLabels).
			WithQueueName("main").
			WithMetadata(task.HookMetadata{
				EventDescription: eventDescription,
				ModuleName:       moduleName,
			})
		newTasks = append(newTasks, newTask.WithQueuedAt(queuedAt))
	}

	// Add ModuleRun tasks to install or reload enabled modules.
	newlyEnabled := utils.ListToMapStringStruct(state.ModulesToEnable)
	for _, moduleName := range state.AllEnabledModules {
		newLogLabels := utils.MergeLabels(logLabels)
		newLogLabels["module"] = moduleName
		delete(newLogLabels, "task.id")

		// Run OnStartup and Kubernetes.Synchronization hooks
		// on application startup or if module become enabled.
		doModuleStartup := false
		if _, has := newlyEnabled[moduleName]; has {
			doModuleStartup = true
		}

		newTask := sh_task.NewTask(task.ModuleRun).
			WithLogLabels(newLogLabels).
			WithQueueName("main").
			WithMetadata(task.HookMetadata{
				EventDescription: eventDescription,
				ModuleName:       moduleName,
				DoModuleStartup:  doModuleStartup,
				IsReloadAll:      true,
			})
		newTasks = append(newTasks, newTask.WithQueuedAt(queuedAt))
	}

	return newTasks
}

// CheckConvergeStatus detects if converge process is started and
// updates ConvergeState. It updates metrics on converge finish.
func (op *AddonOperator) CheckConvergeStatus(t sh_task.Task) {
	convergeTasks := ConvergeTasksInQueue(op.engine.TaskQueues.GetMain())

	// Converge state is 'Started'. Update StartedAt and
	// Activation if the converge process is just started.
	if convergeTasks > 0 && op.ConvergeState.StartedAt == 0 {
		op.ConvergeState.StartedAt = time.Now().UnixNano()
		op.ConvergeState.Activation = t.GetLogLabels()["event.type"]
	}

	// Converge state is 'Done'. Update convergence_* metrics,
	// reset StartedAt and Activation if the converge process is just stopped.
	if convergeTasks == 0 && op.ConvergeState.StartedAt != 0 {
		convergeSeconds := time.Duration(time.Now().UnixNano() - op.ConvergeState.StartedAt).Seconds()
		op.engine.MetricStorage.CounterAdd("{PREFIX}convergence_seconds", convergeSeconds, map[string]string{"activation": op.ConvergeState.Activation})
		op.engine.MetricStorage.CounterAdd("{PREFIX}convergence_total", 1.0, map[string]string{"activation": op.ConvergeState.Activation})
		op.ConvergeState.StartedAt = 0
		op.ConvergeState.Activation = ""
	}

	// Update field for the first converge.
	op.UpdateFirstConvergeStatus(convergeTasks)

	// Report modules left to process.
	if convergeTasks > 0 && (t.GetType() == task.ModuleRun || t.GetType() == task.ModuleDelete) {
		moduleTasks := ConvergeModulesInQueue(op.engine.TaskQueues.GetMain())
		log.Infof("Converge modules in progress: %d modules left to process in queue 'main'", moduleTasks)
	}
}

// UpdateFirstConvergeStatus checks first converge status and prints log messages if first converge
// is in progress.
func (op *AddonOperator) UpdateFirstConvergeStatus(convergeTasks int) {
	switch op.ConvergeState.firstRunPhase {
	case firstDone:
		return
	case firstNotStarted:
		// Switch to 'started' state if there are 'converge' tasks in the queue.
		if convergeTasks > 0 {
			op.ConvergeState.setFirstRunPhase(firstStarted)
		}
	case firstStarted:
		// Switch to 'done' state after first converge is started and when no 'converge' tasks left in the queue.
		if convergeTasks == 0 {
			log.Infof("First converge is finished. Operator is ready now.")
			op.ConvergeState.setFirstRunPhase(firstDone)
		}
	}
}

// taskDescriptionForTaskFlowLog returns a human friendly description of the task.
func taskDescriptionForTaskFlowLog(tsk sh_task.Task, action string, phase string, status string) string {
	hm := task.HookMetadataAccessor(tsk)

	parts := make([]string, 0)

	switch action {
	case "start":
		parts = append(parts, fmt.Sprintf("%s task", tsk.GetType()))
	case "end":
		parts = append(parts, fmt.Sprintf("%s task done, result is '%s'", tsk.GetType(), status))
	default:
		parts = append(parts, fmt.Sprintf("%s task %s", action, tsk.GetType()))
	}

	parts = append(parts, "for")

	switch tsk.GetType() {
	case task.GlobalHookRun, task.ModuleHookRun:
		// Examples:
		// head task GlobalHookRun for 'beforeAll' binding, trigger AfterAll-Hooks-Change-DynamicEnabled
		// GlobalHookRun task for 'beforeAll' binding, trigger AfterAll-Hooks-Change-DynamicEnabled
		// GlobalHookRun task done, result 'Repeat' for 'beforeAll' binding, trigger AfterAll-Hooks-Change-DynamicEnabled
		// GlobalHookRun task for 'onKubernetes/cni_name' binding, trigger Kubernetes
		// GlobalHookRun task done, result 'Repeat' for 'onKubernetes/cni_name' binding, trigger Kubernetes
		// GlobalHookRun task for 'main' group binding, trigger Schedule
		// GlobalHookRun task done, result 'Fail' for 'main' group binding, trigger Schedule
		// GlobalHookRun task for 'main' group and 2 more bindings, trigger Schedule
		// GlobalHookRun task done, result 'Fail' for 'main' group and 2 more bindings, trigger Schedule
		// GlobalHookRun task for Synchronization of 'kubernetes/cni_name' binding, trigger KubernetesEvent
		// GlobalHookRun task done, result 'Success' for Synchronization of 'kubernetes/cni_name' binding, trigger KubernetesEvent

		if len(hm.BindingContext) > 0 {
			if hm.BindingContext[0].IsSynchronization() {
				parts = append(parts, "Synchronization of")
			}

			bindingType := hm.BindingContext[0].Metadata.BindingType

			group := hm.BindingContext[0].Metadata.Group
			if group == "" {
				name := hm.BindingContext[0].Binding
				if bindingType == htypes.OnKubernetesEvent || bindingType == htypes.Schedule {
					name = fmt.Sprintf("'%s/%s'", bindingType, name)
				} else {
					name = string(bindingType)
				}
				parts = append(parts, name)
			} else {
				parts = append(parts, fmt.Sprintf("'%s' group", group))
			}

			if len(hm.BindingContext) > 1 {
				parts = append(parts, "and %d more bindings")
			} else {
				parts = append(parts, "binding")
			}
		} else {
			parts = append(parts, "no binding")
		}

	case task.ConvergeModules:
		// Examples:
		// ConvergeModules task for ReloadAllModules in phase 'WaitBeforeAll', trigger Operator-Startup
		// ConvergeModules task for KubeConfigChanged, trigger Operator-Startup
		// ConvergeModules task done, result is 'Keep' for converge phase 'WaitBeforeAll', trigger Operator-Startup
		if taskEvent, ok := tsk.GetProp(ConvergeEventProp).(ConvergeEvent); ok {
			parts = append(parts, string(taskEvent))
			if taskEvent != KubeConfigChanged {
				parts = append(parts, fmt.Sprintf("in phase '%s'", phase))
			}
		}

	case task.ModuleRun:
		parts = append(parts, fmt.Sprintf("module '%s', phase '%s'", hm.ModuleName, phase))
		if hm.DoModuleStartup {
			parts = append(parts, "with doModuleStartup")
		}

	case task.ModulePurge, task.ModuleDelete:
		parts = append(parts, fmt.Sprintf("module '%s'", hm.ModuleName))

	case task.GlobalHookEnableKubernetesBindings, task.GlobalHookWaitKubernetesSynchronization, task.GlobalHookEnableScheduleBindings:
		// Eaxmples:
		// GlobalHookEnableKubernetesBindings for the hook, trigger Operator-Startup
		// GlobalHookEnableKubernetesBindings done, result 'Success' for the hook, trigger Operator-Startup
		parts = append(parts, "the hook")

	case task.DiscoverHelmReleases:
		// Examples:
		// DiscoverHelmReleases task, trigger Operator-Startup
		// DiscoverHelmReleases task done, result is 'Success', trigger Operator-Startup
		// Remove "for"
		parts = parts[:len(parts)-1]
	}

	triggeredBy := hm.EventDescription
	if triggeredBy != "" {
		triggeredBy = ", trigger is " + triggeredBy
	}

	return fmt.Sprintf("%s%s", strings.Join(parts, " "), triggeredBy)
}

// logTaskAdd prints info about queued tasks.
func (op *AddonOperator) logTaskAdd(logEntry *log.Entry, action string, tasks ...sh_task.Task) {
	logger := logEntry.WithField("task.flow", "add")
	for _, tsk := range tasks {
		logger.Infof(taskDescriptionForTaskFlowLog(tsk, action, "", ""))
	}
}

// logTaskStart prints info about task at start. Also prints event source info from task props.
func (op *AddonOperator) logTaskStart(logEntry *log.Entry, tsk sh_task.Task) {
	// Prevent excess messages for highly frequent tasks.
	if tsk.GetType() == task.GlobalHookWaitKubernetesSynchronization {
		return
	}
	if tsk.GetType() == task.ModuleRun {
		hm := task.HookMetadataAccessor(tsk)
		module := op.ModuleManager.GetModule(hm.ModuleName)
		if module.State.Phase == module_manager.WaitForSynchronization {
			return
		}
	}

	logger := logEntry.
		WithField("task.flow", "start").
		WithFields(utils.LabelsToLogFields(tsk.GetLogLabels()))
	if triggeredBy, ok := tsk.GetProp("triggered-by").(log.Fields); ok {
		logger = logger.WithFields(triggeredBy)
	}

	logger.Infof(taskDescriptionForTaskFlowLog(tsk, "start", op.taskPhase(tsk), ""))
}

// logTaskEnd prints info about task at the end. Info level used only for the ConvergeModules task.
func (op *AddonOperator) logTaskEnd(logEntry *log.Entry, tsk sh_task.Task, result queue.TaskResult) {
	logger := logEntry.
		WithField("task.flow", "end").
		WithFields(utils.LabelsToLogFields(tsk.GetLogLabels()))

	level := log.DebugLevel
	if tsk.GetType() == task.ConvergeModules {
		level = log.InfoLevel
	}
	logger.Log(level, taskDescriptionForTaskFlowLog(tsk, "end", op.taskPhase(tsk), string(result.Status)))
}

func (op *AddonOperator) taskPhase(tsk sh_task.Task) string {
	switch tsk.GetType() {
	case task.ConvergeModules:
		return string(op.ConvergeState.Phase)
	case task.ModuleRun:
		hm := task.HookMetadataAccessor(tsk)
		module := op.ModuleManager.GetModule(hm.ModuleName)
		return string(module.State.Phase)
	}
	return ""
}

// OnFirstConvergeDone run addon-operator business logic when first converge is done and operator is ready
func (op *AddonOperator) OnFirstConvergeDone() {
	go func() {
		<-op.ConvergeState.firstRunDoneC
		// after the first convergence, the service endpoints do not appear instantly.
		// Let's add a short pause so that the service gets online and we don't get a rejection from the validation webhook (timeout reason)
		time.Sleep(3 * time.Second) // wait for service to be resolved

		err := op.ModuleManager.SyncModulesCR(op.KubeClient())
		if err != nil {
			log.Errorf("Modules CR registration failed: %s", err)
		}
	}()
}

// EmitModulesSync emit modules CR synchronization
func (op *AddonOperator) EmitModulesSync() {
	if !op.IsStartupConvergeDone() {
		return
	}

	err := op.ModuleManager.SyncModulesCR(op.KubeClient())
	if err != nil {
		log.Errorf("Modules CR registration failed: %s", err)
	}
}
