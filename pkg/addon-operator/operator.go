package addon_operator

import (
	"context"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path"
	"runtime/trace"
	"strings"
	"time"

	klient "github.com/flant/kube-client/client"
	sh_app "github.com/flant/shell-operator/pkg/app"
	. "github.com/flant/shell-operator/pkg/hook/binding_context"
	"github.com/flant/shell-operator/pkg/hook/controller"
	. "github.com/flant/shell-operator/pkg/hook/types"
	"github.com/flant/shell-operator/pkg/kube_events_manager/types"
	"github.com/flant/shell-operator/pkg/metric_storage"
	shell_operator "github.com/flant/shell-operator/pkg/shell-operator"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
	. "github.com/flant/shell-operator/pkg/utils/measure"
	"github.com/go-chi/chi"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	uuid "gopkg.in/satori/go.uuid.v1"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/helm_resources_manager"
	. "github.com/flant/addon-operator/pkg/hook/types"
	"github.com/flant/addon-operator/pkg/kube_config_manager"
	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/task"
	"github.com/flant/addon-operator/pkg/utils"
)

// AddonOperator extends ShellOperator with modules and global hooks
// and with a value storage.
type AddonOperator struct {
	*shell_operator.ShellOperator
	ctx    context.Context
	cancel context.CancelFunc

	ModulesDir     string
	GlobalHooksDir string

	KubeConfigManager kube_config_manager.KubeConfigManager

	// ModuleManager is the module manager object, which monitors configuration
	// and variable changes.
	ModuleManager module_manager.ModuleManager

	HelmResourcesManager helm_resources_manager.HelmResourcesManager

	// converge state
	StartupConvergeStarted bool
	StartupConvergeDone    bool
	ConvergeStarted        int64
	ConvergeActivation     string

	HelmMonitorKubeClientMetricLabels map[string]string
}

func NewAddonOperator() *AddonOperator {
	return &AddonOperator{
		ShellOperator: &shell_operator.ShellOperator{},
	}
}

func (op *AddonOperator) WithModulesDir(dir string) {
	op.ModulesDir = dir
}

func (op *AddonOperator) WithGlobalHooksDir(dir string) {
	op.GlobalHooksDir = dir
}

func (op *AddonOperator) WithContext(ctx context.Context) *AddonOperator {
	op.ctx, op.cancel = context.WithCancel(ctx)
	op.ShellOperator.WithContext(op.ctx)
	return op
}

func (op *AddonOperator) Stop() {
	if op.cancel != nil {
		op.cancel()
	}
}

func (op *AddonOperator) IsStartupConvergeDone() bool {
	return op.StartupConvergeDone
}

func (op *AddonOperator) SetStartupConvergeDone() {
	op.StartupConvergeDone = true
}

// InitMetricStorage creates new MetricStorage instance in AddonOperator
// if it is not initialized yet. Run it before Init() to override default
// MetricStorage instance in shell-operator.
func (op *AddonOperator) InitMetricStorage() {
	if op.MetricStorage != nil {
		return
	}
	// Metric storage.
	metricStorage := metric_storage.NewMetricStorage()
	metricStorage.WithContext(op.ctx)
	metricStorage.WithPrefix(sh_app.PrometheusMetricsPrefix)
	metricStorage.Start()
	RegisterAddonOperatorMetrics(metricStorage)
	op.MetricStorage = metricStorage
}

// InitModuleManager initialize objects for addon-operator.
// This method should run after Init().
//
// Addon-operator settings:
//
// - directory with modules
// - directory with global hooks
// - dump file path
//
// Objects:
// - helm client
// - kube config manager
// - module manager
//
// Also set handlers for task types and handlers to emit tasks.
func (op *AddonOperator) InitModuleManager() error {
	logLabels := map[string]string{
		"operator.component": "Init",
	}
	logEntry := log.WithFields(utils.LabelsToLogFields(logLabels))

	var err error

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory of process: %s", err)
	}

	// TODO: check if directories are existed
	op.ModulesDir = os.Getenv("MODULES_DIR")
	if op.ModulesDir == "" {
		op.ModulesDir = path.Join(cwd, app.ModulesDir)
	}
	op.GlobalHooksDir = os.Getenv("GLOBAL_HOOKS_DIR")
	if op.GlobalHooksDir == "" {
		op.GlobalHooksDir = path.Join(cwd, app.GlobalHooksDir)
	}
	logEntry.Infof("Global hooks directory: %s", op.GlobalHooksDir)
	logEntry.Infof("Modules directory: %s", op.ModulesDir)

	logEntry.Infof("Addon-operator namespace: %s", app.Namespace)

	// Initialize helm client, choose helm3 or helm2+tiller
	err = helm.Init(op.KubeClient)
	if err != nil {
		return err
	}

	// Initializing ConfigMap storage for values
	op.KubeConfigManager = kube_config_manager.NewKubeConfigManager()
	op.KubeConfigManager.WithKubeClient(op.KubeClient)
	op.KubeConfigManager.WithContext(op.ctx)
	op.KubeConfigManager.WithNamespace(app.Namespace)
	op.KubeConfigManager.WithConfigMapName(app.ConfigMapName)

	err = op.KubeConfigManager.Init()
	if err != nil {
		return fmt.Errorf("init kube config manager: %s", err)
	}

	op.ModuleManager = module_manager.NewMainModuleManager()
	op.ModuleManager.WithContext(op.ctx)
	op.ModuleManager.WithDirectories(op.ModulesDir, op.GlobalHooksDir, op.TempDir)
	op.ModuleManager.WithKubeConfigManager(op.KubeConfigManager)
	op.ModuleManager.WithScheduleManager(op.ScheduleManager)
	op.ModuleManager.WithKubeEventManager(op.KubeEventsManager)
	op.ModuleManager.WithKubeObjectPatcher(op.ObjectPatcher)
	op.ModuleManager.WithMetricStorage(op.MetricStorage)
	op.ModuleManager.WithHookMetricStorage(op.HookMetricStorage)
	err = op.ModuleManager.Init()
	if err != nil {
		return fmt.Errorf("init module manager: %s", err)
	}

	op.DefineEventHandlers()

	// Helm resources monitor.
	// Use separate client-go instance. (Metrics are registered when 'main' client is initialized).
	helmMonitorKubeClient, err := op.InitHelmMonitorKubeClient()
	if err != nil {
		log.Errorf("MAIN Fatal: initialize kube client for helm: %s\n", err)
		return err
	}
	// Init helm resources manager.
	op.HelmResourcesManager = helm_resources_manager.NewHelmResourcesManager()
	op.HelmResourcesManager.WithContext(op.ctx)
	op.HelmResourcesManager.WithKubeClient(helmMonitorKubeClient)
	op.HelmResourcesManager.WithDefaultNamespace(app.Namespace)
	op.ModuleManager.WithHelmResourcesManager(op.HelmResourcesManager)

	return nil
}

func (op *AddonOperator) NeedAddCrontabTask(hook *module_manager.CommonHook) bool {
	if op.IsStartupConvergeDone() {
		return true
	}

	// converge not done into next lines

	// shell hooks will be scheduled after converge done
	// TODO maybe need to add parameter to ShellOperator same to go hooks
	if hook.GoHook == nil {
		return false
	}

	s := hook.GoHook.Config().Settings

	if s != nil && s.EnableSchedulesOnStartup {
		return true
	}

	return false
}

func (op *AddonOperator) DefineEventHandlers() {
	op.ManagerEventsHandler.WithScheduleEventHandler(func(crontab string) []sh_task.Task {
		logLabels := map[string]string{
			"event.id": uuid.NewV4().String(),
			"binding":  string(Schedule),
		}
		logEntry := log.WithFields(utils.LabelsToLogFields(logLabels))
		logEntry.Debugf("Create tasks for 'schedule' event '%s'", crontab)

		var tasks []sh_task.Task
		err := op.ModuleManager.HandleScheduleEvent(crontab,
			func(globalHook *module_manager.GlobalHook, info controller.BindingExecutionInfo) {
				if !op.NeedAddCrontabTask(globalHook.CommonHook) {
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
						BindingType:              Schedule,
						BindingContext:           info.BindingContext,
						AllowFailure:             info.AllowFailure,
						ReloadAllOnValuesChanges: true,
					})

				tasks = append(tasks, newTask)
			},
			func(module *module_manager.Module, moduleHook *module_manager.ModuleHook, info controller.BindingExecutionInfo) {
				if !op.NeedAddCrontabTask(moduleHook.CommonHook) {
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
						BindingType:      Schedule,
						BindingContext:   info.BindingContext,
						AllowFailure:     info.AllowFailure,
					})

				tasks = append(tasks, newTask)
			})

		if err != nil {
			logEntry.Errorf("handle schedule event '%s': %s", crontab, err)
			return []sh_task.Task{}
		}

		return tasks
	})

	op.ManagerEventsHandler.WithKubeEventHandler(func(kubeEvent types.KubeEvent) []sh_task.Task {
		logLabels := map[string]string{
			"event.id": uuid.NewV4().String(),
			"binding":  string(OnKubernetesEvent),
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
						BindingType:              OnKubernetesEvent,
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
						BindingType:      OnKubernetesEvent,
						BindingContext:   info.BindingContext,
						AllowFailure:     info.AllowFailure,
					})

				tasks = append(tasks, newTask)
			})

		return tasks
	})
}

// Run runs all managers, event and queue handlers.
//
// The main process is blocked by the 'for-select' in the queue handler.
func (op *AddonOperator) Start() {
	log.Info("start addon-operator")
	// Loading the onStartup hooks into the queue and running all modules.
	// Turning tracking changes on only after startup ends.

	// Start emit "live" metrics
	op.RunAddonOperatorMetrics()

	// Prepopulate main queue with onStartup tasks and enable kubernetes bindings tasks.
	op.PrepopulateMainQueue(op.TaskQueues)
	// Start main task queue handler
	op.TaskQueues.StartMain()

	op.InitAndStartHookQueues()

	// Managers are generating events. This go-routine handles all events and converts them into queued tasks.
	// Start it before start all informers to catch all kubernetes events (#42)
	op.ManagerEventsHandler.Start()

	// add schedules to schedule manager
	//op.HookManager.EnableScheduleBindings()
	op.ScheduleManager.Start()

	op.ModuleManager.Start()
	op.StartModuleManagerEventHandler()
}

// PrepopulateMainQueue adds tasks to run hooks with OnStartup bindings
// and tasks to enable kubernetes bindings.
func (op *AddonOperator) PrepopulateMainQueue(tqs *queue.TaskQueueSet) {
	onStartupLabels := map[string]string{}
	onStartupLabels["event.type"] = "OperatorStartup"

	// create onStartup for global hooks
	logEntry := log.WithFields(utils.LabelsToLogFields(onStartupLabels))

	// Prepopulate main queue with 'onStartup' and 'enable kubernetes bindings' tasks for
	// global hooks and add a task to discover modules state.
	tqs.WithMainName("main")
	tqs.NewNamedQueue("main", op.TaskHandler)

	onStartupHooks := op.ModuleManager.GetGlobalHooksInOrder(OnStartup)

	for _, hookName := range onStartupHooks {
		hookLogLabels := utils.MergeLabels(onStartupLabels, map[string]string{
			"hook":      hookName,
			"hook.type": "global",
			"queue":     "main",
			"binding":   string(OnStartup),
		})
		//delete(hookLogLabels, "task.id")

		onStartupBindingContext := BindingContext{Binding: string(OnStartup)}
		onStartupBindingContext.Metadata.BindingType = OnStartup

		newTask := sh_task.NewTask(task.GlobalHookRun).
			WithLogLabels(hookLogLabels).
			WithQueueName("main").
			WithMetadata(task.HookMetadata{
				EventDescription:         "PrepopulateMainQueue",
				HookName:                 hookName,
				BindingType:              OnStartup,
				BindingContext:           []BindingContext{onStartupBindingContext},
				ReloadAllOnValuesChanges: false,
			})
		op.TaskQueues.GetMain().AddLast(newTask.WithQueuedAt(time.Now()))

		logEntry.WithFields(utils.LabelsToLogFields(newTask.LogLabels)).
			Infof("queue task %s", newTask.GetDescription())
	}

	schedHooks := op.ModuleManager.GetGlobalHooksInOrder(Schedule)
	for _, hookName := range schedHooks {
		hookLogLabels := utils.MergeLabels(onStartupLabels, map[string]string{
			"hook":      hookName,
			"hook.type": "global",
			"queue":     "main",
			"binding":   string(task.GlobalHookEnableScheduleBindings),
		})

		newTask := sh_task.NewTask(task.GlobalHookEnableScheduleBindings).
			WithLogLabels(hookLogLabels).
			WithQueueName("main").
			WithMetadata(task.HookMetadata{
				EventDescription: "PrepopulateMainQueue",
				HookName:         hookName,
			})
		op.TaskQueues.GetMain().AddLast(newTask.WithQueuedAt(time.Now()))

		logEntry.WithFields(utils.LabelsToLogFields(newTask.LogLabels)).
			Infof("queue task %s", newTask.GetDescription())
	}

	// create tasks to enable kubernetes events for all global hooks with kubernetes bindings
	kubeHooks := op.ModuleManager.GetGlobalHooksInOrder(OnKubernetesEvent)
	for _, hookName := range kubeHooks {
		hookLogLabels := utils.MergeLabels(onStartupLabels, map[string]string{
			"hook":      hookName,
			"hook.type": "global",
			"queue":     "main",
			"binding":   string(task.GlobalHookEnableKubernetesBindings),
		})
		//delete(hookLogLabels, "task.id")

		newTask := sh_task.NewTask(task.GlobalHookEnableKubernetesBindings).
			WithLogLabels(hookLogLabels).
			WithQueueName("main").
			WithMetadata(task.HookMetadata{
				EventDescription: "PrepopulateMainQueue",
				HookName:         hookName,
			})
		op.TaskQueues.GetMain().AddLast(newTask.WithQueuedAt(time.Now()))

		logEntry.WithFields(utils.LabelsToLogFields(newTask.LogLabels)).
			Infof("queue task %s", newTask.GetDescription())
	}

	// wait for kubernetes.Synchronization
	waitLogLabels := utils.MergeLabels(onStartupLabels, map[string]string{
		"queue":   "main",
		"binding": string(task.GlobalHookWaitKubernetesSynchronization),
	})
	waitTask := sh_task.NewTask(task.GlobalHookWaitKubernetesSynchronization).
		WithLogLabels(waitLogLabels).
		WithQueueName("main").
		WithMetadata(task.HookMetadata{
			EventDescription: "PrepopulateMainQueue",
		})
	op.TaskQueues.GetMain().AddLast(waitTask.WithQueuedAt(time.Now()))

	logEntry.WithFields(utils.LabelsToLogFields(waitTask.LogLabels)).
		Infof("queue task %s", waitTask.GetDescription())

	// Create "ReloadAllModules" task with onStartup flag turned on to discover modules state for the first time.
	logLabels := utils.MergeLabels(onStartupLabels, map[string]string{
		"queue":   "main",
		"binding": string(task.ReloadAllModules),
	})
	reloadAllModulesTask := sh_task.NewTask(task.ReloadAllModules).
		WithLogLabels(logLabels).
		WithQueueName("main").
		WithMetadata(task.HookMetadata{
			EventDescription: "PrepopulateMainQueue",
			OnStartupHooks:   true,
		})
	op.TaskQueues.GetMain().AddLast(reloadAllModulesTask.WithQueuedAt(time.Now()))
}

// CreateReloadAllTasks
func (op *AddonOperator) CreateReloadAllTasks(onStartup bool, logLabels map[string]string, eventDescription string) []sh_task.Task {
	logEntry := log.WithFields(utils.LabelsToLogFields(logLabels))
	var tasks = make([]sh_task.Task, 0)

	// Queue beforeAll global hooks.
	beforeAllHooks := op.ModuleManager.GetGlobalHooksInOrder(BeforeAll)

	for _, hookName := range beforeAllHooks {
		hookLogLabels := utils.MergeLabels(logLabels, map[string]string{
			"hook":      hookName,
			"hook.type": "global",
			"queue":     "main",
			"binding":   string(BeforeAll),
		})
		// remove task.id — it is set by NewTask
		delete(hookLogLabels, "task.id")

		// bc := module_manager.BindingContext{BindingContext: hook.BindingContext{Binding: stringmodule_manager.BeforeAll)}}
		// bc.KubernetesSnapshots := ModuleManager.GetGlobalHook(hookName).HookController.KubernetesSnapshots()

		beforeAllBc := BindingContext{
			Binding: string(BeforeAll),
		}
		beforeAllBc.Metadata.BindingType = BeforeAll
		beforeAllBc.Metadata.IncludeAllSnapshots = true

		newTask := sh_task.NewTask(task.GlobalHookRun).
			WithLogLabels(hookLogLabels).
			WithQueueName("main").
			WithMetadata(task.HookMetadata{
				EventDescription:         eventDescription,
				HookName:                 hookName,
				BindingType:              BeforeAll,
				BindingContext:           []BindingContext{beforeAllBc},
				ReloadAllOnValuesChanges: false,
			})
		tasks = append(tasks, newTask)

		logEntry.WithFields(utils.LabelsToLogFields(newTask.LogLabels)).
			Infof("queue task %s", newTask.GetDescription())
	}

	discoverLogLabels := utils.MergeLabels(logLabels, map[string]string{
		"queue": "main",
	})
	// remove task.id — it is set by NewTask
	delete(discoverLogLabels, "task.id")
	discoverTask := sh_task.NewTask(task.DiscoverModulesState).
		WithLogLabels(logLabels).
		WithQueueName("main").
		WithMetadata(task.HookMetadata{
			EventDescription: eventDescription,
			OnStartupHooks:   onStartup,
		})
	tasks = append(tasks, discoverTask)
	logEntry.WithFields(utils.LabelsToLogFields(discoverTask.LogLabels)).
		Infof("queue task %s", discoverTask.GetDescription())
	return tasks
}

// CreateQueues create all queues defined in hooks
func (op *AddonOperator) InitAndStartHookQueues() {
	schHooks := op.ModuleManager.GetGlobalHooksInOrder(Schedule)
	for _, hookName := range schHooks {
		h := op.ModuleManager.GetGlobalHook(hookName)
		for _, hookBinding := range h.Config.Schedules {
			if op.TaskQueues.GetByName(hookBinding.Queue) == nil {
				op.TaskQueues.NewNamedQueue(hookBinding.Queue, op.TaskHandler)
				op.TaskQueues.GetByName(hookBinding.Queue).Start()
				log.Infof("Queue '%s' started for global 'schedule' hook %s", hookBinding.Queue, hookName)
			}
		}
	}

	kubeHooks := op.ModuleManager.GetGlobalHooksInOrder(OnKubernetesEvent)
	for _, hookName := range kubeHooks {
		h := op.ModuleManager.GetGlobalHook(hookName)
		for _, hookBinding := range h.Config.OnKubernetesEvents {
			if op.TaskQueues.GetByName(hookBinding.Queue) == nil {
				op.TaskQueues.NewNamedQueue(hookBinding.Queue, op.TaskHandler)
				op.TaskQueues.GetByName(hookBinding.Queue).Start()
				log.Infof("Queue '%s' started for global 'kubernetes' hook %s", hookBinding.Queue, hookName)
			}
		}
	}

	// module hooks
	modules := op.ModuleManager.GetModuleNamesInOrder()
	for _, modName := range modules {
		schHooks := op.ModuleManager.GetModuleHooksInOrder(modName, Schedule)
		for _, hookName := range schHooks {
			h := op.ModuleManager.GetModuleHook(hookName)
			for _, hookBinding := range h.Config.Schedules {
				if op.TaskQueues.GetByName(hookBinding.Queue) == nil {
					op.TaskQueues.NewNamedQueue(hookBinding.Queue, op.TaskHandler)
					op.TaskQueues.GetByName(hookBinding.Queue).Start()
					log.Infof("Queue '%s' started for module 'schedule' hook %s", hookBinding.Queue, hookName)
				}
			}
		}

		kubeHooks := op.ModuleManager.GetModuleHooksInOrder(modName, OnKubernetesEvent)
		for _, hookName := range kubeHooks {
			h := op.ModuleManager.GetModuleHook(hookName)
			for _, hookBinding := range h.Config.OnKubernetesEvents {
				if op.TaskQueues.GetByName(hookBinding.Queue) == nil {
					op.TaskQueues.NewNamedQueue(hookBinding.Queue, op.TaskHandler)
					op.TaskQueues.GetByName(hookBinding.Queue).Start()
					log.Infof("Queue '%s' started for module 'kubernetes' hook %s", hookBinding.Queue, hookName)
				}
			}
		}
	}
}

func (op *AddonOperator) DrainModuleQueues(modName string) {
	drainQueue := func(queueName string) {
		if queueName == "main" {
			return
		}
		q := op.TaskQueues.GetByName(queueName)
		if q == nil {
			return
		}

		// Remove all tasks.
		q.Filter(func(_ sh_task.Task) bool {
			return false
		})
	}

	schHooks := op.ModuleManager.GetModuleHooksInOrder(modName, Schedule)
	for _, hookName := range schHooks {
		h := op.ModuleManager.GetModuleHook(hookName)
		for _, hookBinding := range h.Config.Schedules {
			drainQueue(hookBinding.Queue)
		}
	}

	kubeHooks := op.ModuleManager.GetModuleHooksInOrder(modName, OnKubernetesEvent)
	for _, hookName := range kubeHooks {
		h := op.ModuleManager.GetModuleHook(hookName)
		for _, hookBinding := range h.Config.OnKubernetesEvents {
			drainQueue(hookBinding.Queue)
		}
	}
}

func (op *AddonOperator) StartModuleManagerEventHandler() {
	go func() {
		for {
			select {
			// Event from module manager (module restart or full restart).
			case moduleEvent := <-op.ModuleManager.Ch():
				logLabels := map[string]string{
					"event.id": uuid.NewV4().String(),
				}
				eventLogEntry := log.WithField("operator.component", "handleManagerEvents").
					WithFields(utils.LabelsToLogFields(logLabels))
				// Event from module manager can come if modules list have changed,
				// so event hooks need to be re-register with:
				// RegisterScheduledHooks()
				// RegisterKubeEventHooks()
				switch moduleEvent.Type {
				// Some modules have changed.
				case module_manager.ModulesChanged:
					logLabels["event.type"] = "ModulesChanged"

					logEntry := eventLogEntry.WithFields(utils.LabelsToLogFields(logLabels))
					for _, moduleChange := range moduleEvent.ModulesChanges {
						// Do not add ModuleRun task if it is already queued.
						hasTask := QueueHasPendingModuleRunTask(op.TaskQueues.GetMain(), moduleChange.Name)
						if !hasTask {
							logEntry.WithField("module", moduleChange.Name).Infof("module values are changed, queue ModuleRun task")
							newLabels := utils.MergeLabels(logLabels)
							newLabels["module"] = moduleChange.Name
							newTask := sh_task.NewTask(task.ModuleRun).
								WithLogLabels(newLabels).
								WithQueueName("main").
								WithMetadata(task.HookMetadata{
									EventDescription: "ModuleValuesChanged",
									ModuleName:       moduleChange.Name,
								})
							op.TaskQueues.GetMain().AddLast(newTask.WithQueuedAt(time.Now()))
							logEntry.WithFields(utils.LabelsToLogFields(newTask.LogLabels)).
								Infof("queue task %s", newTask.GetDescription())
						} else {
							logEntry.WithField("module", moduleChange.Name).Infof("module values are changed, ModuleRun task already exists")
						}
					}
					// As module list may have changed, hook schedule index must be re-created.
					// TODO SNAPSHOT: Check this
					//ScheduleHooksController.UpdateScheduleHooks()
				case module_manager.GlobalChanged:
					// Global values are changed, all modules must be restarted.
					logLabels["event.type"] = "GlobalChanged"
					logEntry := eventLogEntry.WithFields(utils.LabelsToLogFields(logLabels))
					logEntry.Infof("queue tasks for ReloadAll: global config values are changed")

					// Stop and remove all resource monitors before run modules discovery.
					op.HelmResourcesManager.StopMonitors()

					// Create "ReloadAllModules" task with onStartup flag turned off.
					reloadAllModulesTask := sh_task.NewTask(task.ReloadAllModules).
						WithLogLabels(logLabels).
						WithQueueName("main").
						WithMetadata(task.HookMetadata{
							EventDescription: "GlobalConfigValuesChanged",
							OnStartupHooks:   false,
						})
					op.TaskQueues.GetMain().AddLast(reloadAllModulesTask.WithQueuedAt(time.Now()))

					// TODO Check if this is needed?
					// As module list may have changed, hook schedule index must be re-created.
					//ScheduleHooksController.UpdateScheduleHooks()
				case module_manager.AmbiguousState:
					// It is the error in the module manager. The task must be added to
					// the beginning of the queue so the module manager can restore its
					// state before running other queue tasks
					logLabels["event.type"] = "AmbiguousState"
					//TasksQueue.ChangesDisable()
					newTask := sh_task.NewTask(task.ModuleManagerRetry).
						WithLogLabels(logLabels).
						WithQueueName("main")
					op.TaskQueues.GetMain().AddFirst(newTask.WithQueuedAt(time.Now()))
					eventLogEntry.WithFields(utils.LabelsToLogFields(newTask.LogLabels)).
						Infof("queue task %s - module manager is in ambiguous state", newTask.GetDescription())
				}
			case absentResourcesEvent := <-op.HelmResourcesManager.Ch():
				logLabels := map[string]string{
					"event.id": uuid.NewV4().String(),
					"module":   absentResourcesEvent.ModuleName,
				}
				eventLogEntry := log.WithField("operator.component", "handleManagerEvents").
					WithFields(utils.LabelsToLogFields(logLabels))

				//eventLogEntry.Debugf("Got %d absent resources from module", len(absentResourcesEvent.Absent))

				// Do not add ModuleRun task if it is already queued.
				hasTask := QueueHasPendingModuleRunTask(op.TaskQueues.GetMain(), absentResourcesEvent.ModuleName)
				if !hasTask {
					newTask := sh_task.NewTask(task.ModuleRun).
						WithLogLabels(logLabels).
						WithQueueName("main").
						WithMetadata(task.HookMetadata{
							EventDescription: "DetectAbsentHelmResources",
							ModuleName:       absentResourcesEvent.ModuleName,
						})
					op.TaskQueues.GetMain().AddLast(newTask.WithQueuedAt(time.Now()))
					eventLogEntry.WithFields(utils.LabelsToLogFields(newTask.LogLabels)).
						Infof("queue task %s - got %d absent module resources", newTask.GetDescription(), len(absentResourcesEvent.Absent))
				} else {
					eventLogEntry.Infof("Got %d absent module resources, ModuleRun task already queued", len(absentResourcesEvent.Absent))
				}
			}
		}
	}()
}

// TasksRunner handle tasks in queue.
func (op *AddonOperator) TaskHandler(t sh_task.Task) queue.TaskResult {
	var taskLogLabels = utils.MergeLabels(map[string]string{
		"operator.component": "taskRunner",
	}, t.GetLogLabels())
	var taskLogEntry = log.WithFields(utils.LabelsToLogFields(taskLogLabels))
	var res queue.TaskResult

	op.UpdateWaitInQueueMetric(t)

	switch t.GetType() {
	case task.GlobalHookRun:
		res = op.HandleGlobalHookRun(t, taskLogLabels)

	case task.GlobalHookEnableScheduleBindings:
		taskLogEntry.Infof("Global hook enable schedule bindings")
		hm := task.HookMetadataAccessor(t)
		globalHook := op.ModuleManager.GetGlobalHook(hm.HookName)
		globalHook.HookController.EnableScheduleBindings()
		res.Status = "Success"

	case task.GlobalHookEnableKubernetesBindings:
		taskLogEntry.Infof("Global hook enable kubernetes bindings")
		hm := task.HookMetadataAccessor(t)
		globalHook := op.ModuleManager.GetGlobalHook(hm.HookName)

		var mainSyncTasks = make([]sh_task.Task, 0)
		var parallelSyncTasks = make([]sh_task.Task, 0)
		var waitSyncTasks = make(map[string]sh_task.Task)

		eventDescription := hm.EventDescription
		if !strings.Contains(eventDescription, "HandleGlobalEnableKubernetesBindings") {
			eventDescription += ".HandleGlobalEnableKubernetesBindings"
		}

		newLogLabels := utils.MergeLabels(t.GetLogLabels())
		delete(newLogLabels, "task.id")

		err := op.ModuleManager.HandleGlobalEnableKubernetesBindings(hm.HookName, func(hook *module_manager.GlobalHook, info controller.BindingExecutionInfo) {
			hookLogLabels := utils.MergeLabels(t.GetLogLabels(), map[string]string{
				"hook":      hook.GetName(),
				"hook.type": "global",
				"queue":     info.QueueName,
				"binding":   string(OnKubernetesEvent),
			})
			delete(hookLogLabels, "task.id")

			kubernetesBindingID := uuid.NewV4().String()
			newTask := sh_task.NewTask(task.GlobalHookRun).
				WithLogLabels(hookLogLabels).
				WithQueueName(info.QueueName).
				WithMetadata(task.HookMetadata{
					EventDescription:         eventDescription,
					HookName:                 hook.GetName(),
					BindingType:              OnKubernetesEvent,
					BindingContext:           info.BindingContext,
					AllowFailure:             info.AllowFailure,
					ReloadAllOnValuesChanges: false, // Ignore global values changes in global Synchronization tasks.
					KubernetesBindingId:      kubernetesBindingID,
					WaitForSynchronization:   info.KubernetesBinding.WaitForSynchronization,
					MonitorIDs:               []string{info.KubernetesBinding.Monitor.Metadata.MonitorId},
					ExecuteOnSynchronization: info.KubernetesBinding.ExecuteHookOnSynchronization,
				})

			// Ignore "waitForSynchronization: true" if Synchronization is not required.
			if info.QueueName == t.GetQueueName() {
				mainSyncTasks = append(mainSyncTasks, newTask)
			} else {
				if info.KubernetesBinding.WaitForSynchronization && info.KubernetesBinding.ExecuteHookOnSynchronization {
					waitSyncTasks[kubernetesBindingID] = newTask
				} else {
					parallelSyncTasks = append(parallelSyncTasks, newTask)
				}
			}
		})

		if err != nil {
			hookLabel := path.Base(globalHook.Path)
			// TODO use separate metric, as in shell-operator?
			op.MetricStorage.CounterAdd("{PREFIX}global_hook_errors_total", 1.0, map[string]string{
				"hook":       hookLabel,
				"binding":    "GlobalEnableKubernetesBindings",
				"queue":      t.GetQueueName(),
				"activation": "",
			})
			taskLogEntry.Errorf("Global hook enable kubernetes bindings failed, requeue task to retry after delay. Failed count is %d. Error: %s", t.GetFailureCount()+1, err)
			t.UpdateFailureMessage(err.Error())
			t.WithQueuedAt(time.Now())
			res.Status = "Fail"
		} else {
			// Substitute current task with Synchronization tasks for the main queue.
			// Other Synchronization tasks are queued into specified queues.
			// Informers can be started now — their events will be added to the queue tail.
			taskLogEntry.Infof("Global hook enable kubernetes bindings success")

			// "Wait" tasks are queued first
			for id, tsk := range waitSyncTasks {
				q := op.TaskQueues.GetByName(tsk.GetQueueName())
				if q == nil {
					log.Errorf("Queue %s is not created while run GlobalHookEnableKubernetesBindings task!", tsk.GetQueueName())
				} else {
					// Skip state creation if WaitForSynchronization is disabled.
					taskLogEntry.Infof("queue task %s - Synchronization after onStartup, id=%s", tsk.GetDescription(), id)
					taskLogEntry.Infof("Queue Synchronization '%s'", id)
					op.ModuleManager.SynchronizationQueued(id)
					q.AddLast(tsk.WithQueuedAt(time.Now()))
				}
			}

			for _, tsk := range parallelSyncTasks {
				q := op.TaskQueues.GetByName(tsk.GetQueueName())
				if q == nil {
					log.Errorf("Queue %s is not created while run GlobalHookEnableKubernetesBindings task!", tsk.GetQueueName())
				} else {
					q.AddLast(tsk.WithQueuedAt(time.Now()))
				}
			}

			res.Status = "Success"
			for _, tsk := range mainSyncTasks {
				tsk.WithQueuedAt(time.Now())
			}
			res.HeadTasks = mainSyncTasks
		}

	case task.GlobalHookWaitKubernetesSynchronization:
		res.Status = "Success"
		if op.ModuleManager.GlobalSynchronizationNeeded() && !op.ModuleManager.GlobalSynchronizationDone() {
			// dump state
			op.ModuleManager.DumpState()
			t.WithQueuedAt(time.Now())
			res.Status = "Repeat"
		} else {
			taskLogEntry.Infof("Global 'Synchronization' is done")
		}

	case task.ReloadAllModules:
		taskLogEntry.Info("queue beforeAll and discoverModulesState tasks")
		hm := task.HookMetadataAccessor(t)

		// Remove adjacent ReloadAllModules tasks
		stopFilter := false
		op.TaskQueues.GetByName(t.GetQueueName()).Filter(func(tsk sh_task.Task) bool {
			// Ignore current task
			if tsk.GetId() == t.GetId() {
				return true
			}
			if tsk.GetType() != task.ReloadAllModules {
				stopFilter = true
			}
			return stopFilter
		})

		res.Status = "Success"
		reloadAllTasks := op.CreateReloadAllTasks(hm.OnStartupHooks, t.GetLogLabels(), hm.EventDescription)
		for _, tsk := range reloadAllTasks {
			tsk.WithQueuedAt(time.Now())
		}
		res.AfterTasks = reloadAllTasks

	case task.DiscoverModulesState:
		taskLogEntry.Info("Discover modules start")
		tasks, err := op.RunDiscoverModulesState(t, t.GetLogLabels())
		if err != nil {
			op.MetricStorage.CounterAdd("{PREFIX}modules_discover_errors_total", 1.0, map[string]string{})
			taskLogEntry.Errorf("Discover modules failed, requeue task to retry after delay. Failed count is %d. Error: %s", t.GetFailureCount()+1, err)
			t.UpdateFailureMessage(err.Error())
			t.WithQueuedAt(time.Now())
			res.Status = "Fail"
		} else {
			taskLogEntry.Infof("Discover modules success")
			res.Status = "Success"
			for _, tsk := range tasks {
				tsk.WithQueuedAt(time.Now())
			}
			res.AfterTasks = tasks
		}

	case task.ModuleRun:
		res = op.HandleModuleRun(t, taskLogLabels)

	case task.ModuleDelete:
		// TODO wait while module's tasks in other queues are done.
		hm := task.HookMetadataAccessor(t)
		taskLogEntry.Infof("Module delete '%s'", hm.ModuleName)
		// Remove all hooks from parallel queues.
		op.DrainModuleQueues(hm.ModuleName)
		err := op.ModuleManager.DeleteModule(hm.ModuleName, t.GetLogLabels())
		if err != nil {
			op.MetricStorage.CounterAdd("{PREFIX}module_delete_errors_total", 1.0, map[string]string{"module": hm.ModuleName})
			taskLogEntry.Errorf("Module delete failed, requeue task to retry after delay. Failed count is %d. Error: %s", t.GetFailureCount()+1, err)
			t.UpdateFailureMessage(err.Error())
			t.WithQueuedAt(time.Now())
			res.Status = "Fail"
		} else {
			taskLogEntry.Infof("Module delete success '%s'", hm.ModuleName)
			res.Status = "Success"
		}

	case task.ModuleHookRun:
		res = op.HandleModuleHookRun(t, taskLogLabels)

	case task.ModulePurge:
		// Purge is for unknown modules, so error is just ignored.
		taskLogEntry.Infof("Module purge start")
		hm := task.HookMetadataAccessor(t)

		err := helm.NewClient(t.GetLogLabels()).DeleteRelease(hm.ModuleName)
		if err != nil {
			taskLogEntry.Warnf("Module purge failed, no retry. Error: %s", err)
		} else {
			taskLogEntry.Infof("Module purge success")
		}
		res.Status = "Success"

	case task.ModuleManagerRetry:
		op.MetricStorage.CounterAdd("{PREFIX}modules_discover_errors_total", 1.0, map[string]string{})
		op.ModuleManager.Retry()
		taskLogEntry.Infof("Module manager retry is requested, now wait before run module discovery again")

		res.Status = "Success"
		res.DelayBeforeNextTask = queue.DelayOnFailedTask
	}

	if res.Status == "Success" {
		origAfterHandle := res.AfterHandle
		res.AfterHandle = func() {
			op.CheckConvergeStatus(t)
			if origAfterHandle != nil {
				origAfterHandle()
			}
		}
	}

	return res
}

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

	case task.ReloadAllModules,
		task.DiscoverModulesState,
		task.ModuleManagerRetry:
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
	op.MetricStorage.CounterAdd("{PREFIX}task_wait_in_queue_seconds_total", taskWaitTime, metricLabels)
}

// ModuleRun starts module: execute module hook and install a Helm chart.
// Execution sequence:
// - onStartup hooks
// - kubernetes.Synchronization hooks
// - wait while all Synchronization tasks are done
// - beforeHelm hooks
// - install or upgrade a Helm chart
// - afterHelm hooks
// ModuleRun is restarted if hook or chart is failed.
// If ModuleRun is succeeded, then onStartup and kubernetes.Synchronization hooks will not run the next time.
func (op *AddonOperator) HandleModuleRun(t sh_task.Task, labels map[string]string) (res queue.TaskResult) {
	defer trace.StartRegion(context.Background(), "ModuleRun").End()
	logEntry := log.WithFields(utils.LabelsToLogFields(labels))

	hm := task.HookMetadataAccessor(t)
	module := op.ModuleManager.GetModule(hm.ModuleName)

	metricLabels := map[string]string{
		"module":     hm.ModuleName,
		"activation": labels["event.type"],
	}

	defer Duration(func(d time.Duration) {
		op.MetricStorage.HistogramObserve("{PREFIX}module_run_seconds", d.Seconds(), metricLabels, nil)
	})()

	var syncQueueName = fmt.Sprintf("main-subqueue-kubernetes-Synchronization-module-%s", hm.ModuleName)
	var moduleRunErr error
	var valuesChanged = false

	// First module run on operator startup or when module is enabled.
	if hm.OnStartupHooks && !module.State.OnStartupDone {
		treg := trace.StartRegion(context.Background(), "ModuleRun-OnStartup")
		logEntry.WithField("module.state", "startup").
			Info("ModuleRun 'StartupHooks' phase")

		// DiscoverModules registered all module hooks, so queues can be started now.
		op.InitAndStartHookQueues()

		// run onStartup hooks
		moduleRunErr = module.RunOnStartup(t.GetLogLabels())
		if moduleRunErr == nil {
			module.State.OnStartupDone = true
		}
		treg.End()
	}

	// Phase 'Handle Synchronization tasks'

	// Prevent tasks queueing and waiting if there is no kubernetes hooks with executeHookOnSynchronization
	if module.State.OnStartupDone && !module.SynchronizationNeeded() {
		module.State.SynchronizationTasksQueued = true
		module.State.SynchronizationDone = true
	}

	// Queue Synchronization tasks if needed
	if module.State.OnStartupDone && !module.State.SynchronizationDone && !module.State.SynchronizationTasksQueued {
		logEntry.WithField("module.state", "synchronization").
			Info("ModuleRun queue Synchronization tasks")
		var mainSyncTasks = make([]sh_task.Task, 0)
		var parallelSyncTasks = make([]sh_task.Task, 0)
		var waitSyncTasks = make(map[string]sh_task.Task)

		eventDescription := hm.EventDescription
		if !strings.Contains(eventDescription, "EnableKubernetesBindings") {
			eventDescription += ".EnableKubernetesBindings"
		}

		err := op.ModuleManager.HandleModuleEnableKubernetesBindings(hm.ModuleName, func(hook *module_manager.ModuleHook, info controller.BindingExecutionInfo) {
			queueName := info.QueueName
			if queueName == t.GetQueueName() {
				// main
				queueName = syncQueueName
			}
			hookLogLabels := utils.MergeLabels(t.GetLogLabels(), map[string]string{
				"module":    hm.ModuleName,
				"hook":      hook.GetName(),
				"hook.type": "module",
				"queue":     queueName,
			})
			if len(info.BindingContext) > 0 {
				hookLogLabels["binding.name"] = info.BindingContext[0].Binding
			}
			delete(hookLogLabels, "task.id")

			kubernetesBindingID := uuid.NewV4().String()
			taskMeta := task.HookMetadata{
				EventDescription:         eventDescription,
				ModuleName:               hm.ModuleName,
				HookName:                 hook.GetName(),
				BindingType:              OnKubernetesEvent,
				BindingContext:           info.BindingContext,
				AllowFailure:             info.AllowFailure,
				KubernetesBindingId:      kubernetesBindingID,
				WaitForSynchronization:   info.KubernetesBinding.WaitForSynchronization,
				MonitorIDs:               []string{info.KubernetesBinding.Monitor.Metadata.MonitorId},
				ExecuteOnSynchronization: info.KubernetesBinding.ExecuteHookOnSynchronization,
			}
			newTask := sh_task.NewTask(task.ModuleHookRun).
				WithLogLabels(hookLogLabels).
				WithQueueName(queueName).
				WithMetadata(taskMeta)

			// Ignore "waitForSynchronization: true" if Synchronization is not required.
			if info.QueueName == t.GetQueueName() {
				mainSyncTasks = append(mainSyncTasks, newTask)
			} else {
				if info.KubernetesBinding.WaitForSynchronization && info.KubernetesBinding.ExecuteHookOnSynchronization {
					waitSyncTasks[kubernetesBindingID] = newTask
				} else {
					parallelSyncTasks = append(parallelSyncTasks, newTask)
				}
			}
		})
		if err != nil {
			// Fail to enable bindings: cannot start Kubernetes monitors.
			moduleRunErr = err
		} else {
			// queue created tasks

			// Wait tasks are queued first
			for id, tsk := range waitSyncTasks {
				q := op.TaskQueues.GetByName(tsk.GetQueueName())
				if q == nil {
					logEntry.Errorf("queue %s is not found while EnableKubernetesBindings task", tsk.GetQueueName())
				} else {
					logEntry.Infof("queue task %s - Synchronization after onStartup, id=%s", tsk.GetDescription(), id)
					thm := task.HookMetadataAccessor(tsk)
					mHook := op.ModuleManager.GetModuleHook(thm.HookName)
					// TODO move behind SynchronizationQueued(id string)
					// State is created only for tasks that need waiting.
					mHook.KubernetesBindingSynchronizationState[id] = &module_manager.KubernetesBindingSynchronizationState{
						Queued: true,
						Done:   false,
					}
					q.AddLast(tsk.WithQueuedAt(time.Now()))
				}
			}

			for _, tsk := range parallelSyncTasks {
				q := op.TaskQueues.GetByName(tsk.GetQueueName())
				if q == nil {
					logEntry.Errorf("queue %s is not found while EnableKubernetesBindings task", tsk.GetQueueName())
				} else {
					q.AddLast(tsk.WithQueuedAt(time.Now()))
				}
			}

			if len(mainSyncTasks) > 0 {
				// EnableKubernetesBindings and StartInformers for all kubernetes bindings.
				op.TaskQueues.NewNamedQueue(syncQueueName, op.TaskHandler)
				syncSubQueue := op.TaskQueues.GetByName(syncQueueName)

				for _, tsk := range mainSyncTasks {
					logEntry.WithFields(utils.LabelsToLogFields(tsk.GetLogLabels())).
						Infof("queue task %s - Synchronization after onStartup", tsk.GetDescription())
					thm := task.HookMetadataAccessor(tsk)
					mHook := op.ModuleManager.GetModuleHook(thm.HookName)
					// TODO move behind SynchronizationQueued(id string)
					// State is created only for tasks that need waiting.
					mHook.KubernetesBindingSynchronizationState[thm.KubernetesBindingId] = &module_manager.KubernetesBindingSynchronizationState{
						Queued: true,
						Done:   false,
					}
					syncSubQueue.AddLast(tsk.WithQueuedAt(time.Now()))
				}
				logEntry.Infof("Queue '%s' started for module 'kubernetes.Synchronization' hooks", syncQueueName)
				syncSubQueue.Start()
			}

			// TODO should it be another subqueue for bindings with main queue and disabled WaitForSynchronization?

			// asserts
			if len(waitSyncTasks) > 0 && !module.SynchronizationQueued() {
				logEntry.Errorf("Possible bug!!! Synchronization is needed, %d tasks should be queued in named queues and waited, but module has state 'Synchronization is not queued'", len(waitSyncTasks))
			}
			if len(mainSyncTasks) > 0 && !module.SynchronizationQueued() {
				logEntry.Errorf("Possible bug!!! Synchronization is needed, %d tasks should be waited before run beforeHelm, but module has state 'Synchronization is not queued'", len(waitSyncTasks))
			}

			// Enter wait loop if there are tasks that should be waited.
			// Synchronization is there are no tasks to wait.
			if len(mainSyncTasks)+len(waitSyncTasks) == 0 {
				module.State.SynchronizationDone = true
			} else {
				module.State.ShouldWaitForSynchronization = true
			}
			// prevent task queueing on next ModuleRun
			module.State.SynchronizationTasksQueued = true
		}
	}

	// Wait while all Synchronization task are done. Set SynchronizationDone state when hooks are finished.
	if moduleRunErr == nil && module.State.OnStartupDone && !module.State.SynchronizationDone && module.State.ShouldWaitForSynchronization {
		if !module.State.WaitStarted {
			logEntry.WithField("module.state", "wait-for-synchronization").
				Infof("ModuleRun wait for Synchronization")
			module.State.WaitStarted = true
		}

		// Throttle debug messages: print hooks state every 5s
		if time.Now().UnixNano()%5000000000 == 0 {
			logEntry.Debugf("ModuleRun wait Synchronization state: onStartup:%v syncNeeded:%v syncQueued:%v syncDone:%v", hm.OnStartupHooks, module.SynchronizationNeeded(), module.SynchronizationQueued(), module.SynchronizationDone())
			for _, hName := range op.ModuleManager.GetModuleHooksInOrder(hm.ModuleName, OnKubernetesEvent) {
				hook := op.ModuleManager.GetModuleHook(hName)
				logEntry.Debugf("  hook '%s': %d, %+v", hook.Name, len(hook.KubernetesBindingSynchronizationState), hook.KubernetesBindingSynchronizationState)
			}
		}

		if module.SynchronizationDone() {
			module.State.SynchronizationDone = true
			// remove temporary subqueue
			op.TaskQueues.Remove(syncQueueName)
		} else {
			logEntry.Debugf("Module run repeat")
			t.WithQueuedAt(time.Now())
			res.Status = "Repeat"
			return
		}
	}

	// Start schedule events when Synchronization is done.
	if module.State.SynchronizationDone && !module.State.MonitorsStarted {
		// Unlock schedule events.
		op.ModuleManager.StartModuleHooks(hm.ModuleName)
		module.State.MonitorsStarted = true
	}

	// Phase with helm hooks and helm chart.
	if moduleRunErr == nil && module.State.OnStartupDone && module.State.SynchronizationDone {
		logEntry.Info("ModuleRun 'Helm' phase")
		// run beforeHelm, helm, afterHelm
		valuesChanged, moduleRunErr = module.Run(t.GetLogLabels())
	}

	if moduleRunErr != nil {
		op.MetricStorage.CounterAdd("{PREFIX}module_run_errors_total", 1.0, map[string]string{"module": hm.ModuleName})
		logEntry.WithField("module.state", "failed").
			Errorf("ModuleRun failed. Requeue task to retry after delay. Failed count is %d. Error: %s", t.GetFailureCount()+1, moduleRunErr)
		t.UpdateFailureMessage(moduleRunErr.Error())
		t.WithQueuedAt(time.Now())
		res.Status = "Fail"
	} else {
		res.Status = "Success"
		if valuesChanged {
			logEntry.WithField("module.state", "restart").
				Infof("ModuleRun success, values changed, restart module")
			// One of afterHelm hooks changes values, run ModuleRun again: copy task and unset RunOnStartupHooks.
			hm.OnStartupHooks = false
			eventDescription := hm.EventDescription
			if !strings.Contains(eventDescription, "AfterHelmHooksChangeModuleValues") {
				eventDescription += ".AfterHelmHooksChangeModuleValues"
			}
			hm.EventDescription = eventDescription
			newLabels := utils.MergeLabels(t.GetLogLabels())
			delete(newLabels, "task.id")
			newTask := sh_task.NewTask(task.ModuleRun).
				WithLogLabels(newLabels).
				WithQueueName(t.GetQueueName()).
				WithMetadata(hm)
			res.AfterTasks = []sh_task.Task{newTask.WithQueuedAt(time.Now())}
		} else {
			logEntry.WithField("module.state", "ready").
				Infof("ModuleRun success, module is ready")
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
	if !taskHook.Module.State.Enabled {
		res.Status = "Success"
		return
	}

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
		"module":     hm.ModuleName,
		"hook":       hm.HookName,
		"binding":    string(hm.BindingType),
		"queue":      t.GetQueueName(),
		"activation": labels["event.type"],
	}

	defer Duration(func(d time.Duration) {
		op.MetricStorage.HistogramObserve("{PREFIX}module_hook_run_seconds", d.Seconds(), metricLabels, nil)
	})()

	isSynchronization := hm.IsSynchronization()
	shouldRunHook := true
	if isSynchronization {
		// There were no Synchronization for v0 hooks, skip hook execution.
		if taskHook.Config.Version == "v0" {
			shouldRunHook = false
			res.Status = "Success"
		}
		// Explicit "executeOnSynchronization: false"
		if !hm.ExecuteOnSynchronization {
			shouldRunHook = false
			res.Status = "Success"
		}
	}

	if shouldRunHook && taskHook.Config.Version == "v1" {
		// Retrieve all KubernetesBindingIds for a hook.
		kubeIds := make(map[string]bool)
		op.TaskQueues.GetByName(t.GetQueueName()).Iterate(func(tsk sh_task.Task) {
			thm := task.HookMetadataAccessor(tsk)
			logEntry.Debugf("kubeId: hook %s id %s", thm.HookName, thm.KubernetesBindingId)
			if thm.HookName == hm.HookName && thm.KubernetesBindingId != "" {
				kubeIds[thm.KubernetesBindingId] = false
			}
		})
		logEntry.Debugf("kubeIds: %+v", kubeIds)

		// Combine binding contexts in the queue.
		combineResult := op.CombineBindingContextForHook(op.TaskQueues.GetByName(t.GetQueueName()), t, func(tsk sh_task.Task) bool {
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
			return false // do not stop combine process
		})

		if combineResult != nil {
			hm.BindingContext = combineResult.BindingContexts
			// Extra monitor IDs can be returned if several Synchronization binding contexts are combined.
			if len(combineResult.MonitorIDs) > 0 {
				hm.MonitorIDs = append(hm.MonitorIDs, combineResult.MonitorIDs...)
			}
			logEntry.Infof("Got monitorIDs: %+v", hm.MonitorIDs)
			t.UpdateMetadata(hm)
		}

		// mark remain kubernetes binding ids
		op.TaskQueues.GetByName(t.GetQueueName()).Iterate(func(tsk sh_task.Task) {
			thm := task.HookMetadataAccessor(tsk)
			if thm.HookName == hm.HookName && thm.KubernetesBindingId != "" {
				kubeIds[thm.KubernetesBindingId] = true
			}
		})
		logEntry.Debugf("kubeIds: %+v", kubeIds)

		// remove ids from state for removed tasks
		for kubeId, v := range kubeIds {
			if !v {
				logEntry.Debugf("delete kubeId '%s'", kubeId)
				delete(taskHook.KubernetesBindingSynchronizationState, kubeId)
			}
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

		err = op.ModuleManager.RunModuleHook(hm.HookName, hm.BindingType, hm.BindingContext, t.GetLogLabels())
		if err != nil {
			if hm.AllowFailure {
				allowed = 1.0
				logEntry.Infof("Module hook failed, but allowed to fail. Error: %v", err)
				res.Status = "Success"
			} else {
				errors = 1.0
				logEntry.Errorf("Module hook failed, requeue task to retry after delay. Failed count is %d. Error: %s", t.GetFailureCount()+1, err)
				t.UpdateFailureMessage(err.Error())
				t.WithQueuedAt(time.Now())
				res.Status = "Fail"
			}
		} else {
			success = 1.0
			logEntry.Infof("Module hook success '%s'", hm.HookName)
			res.Status = "Success"
		}

		op.MetricStorage.CounterAdd("{PREFIX}module_hook_allowed_errors_total", allowed, metricLabels)
		op.MetricStorage.CounterAdd("{PREFIX}module_hook_errors_total", errors, metricLabels)
		op.MetricStorage.CounterAdd("{PREFIX}module_hook_success_total", success, metricLabels)
	}

	if isSynchronization && res.Status == "Success" {
		if state, ok := taskHook.KubernetesBindingSynchronizationState[hm.KubernetesBindingId]; ok {
			state.Done = true
		}
		// Unlock Kubernetes events for all monitors when Synchronization task is done.
		logEntry.Info("Unlock kubernetes.Event tasks")
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

	defer Duration(func(d time.Duration) {
		op.MetricStorage.HistogramObserve("{PREFIX}global_hook_run_seconds", d.Seconds(), metricLabels, nil)
	})()

	isSynchronization := hm.IsSynchronization()
	shouldRunHook := true
	if isSynchronization {
		// There were no Synchronization for v0 hooks, skip hook execution.
		if taskHook.Config.Version == "v0" {
			shouldRunHook = false
			res.Status = "Success"
		}
		// Explicit "executeOnSynchronization: false"
		if !hm.ExecuteOnSynchronization {
			shouldRunHook = false
			res.Status = "Success"
		}
	}

	if shouldRunHook && taskHook.Config.Version == "v1" {
		// Retrieve all KubernetesBindingIds for a hook.
		kubeIds := make(map[string]bool)
		op.TaskQueues.GetByName(t.GetQueueName()).Iterate(func(tsk sh_task.Task) {
			thm := task.HookMetadataAccessor(tsk)
			logEntry.Debugf("kubeId: hook %s id %s", thm.HookName, thm.KubernetesBindingId)
			if thm.HookName == hm.HookName && thm.KubernetesBindingId != "" {
				kubeIds[thm.KubernetesBindingId] = false
			}
		})
		logEntry.Debugf("global kubeIds: %+v", kubeIds)

		// Combine binding contexts in the queue.
		combineResult := op.CombineBindingContextForHook(op.TaskQueues.GetByName(t.GetQueueName()), t, func(tsk sh_task.Task) bool {
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
			return false // do not stop combine process
		})

		if combineResult != nil {
			hm.BindingContext = combineResult.BindingContexts
			// Extra monitor IDs can be returned if several Synchronization binding contexts are combined.
			if len(combineResult.MonitorIDs) > 0 {
				logEntry.Infof("Task monitorID: %s, combined monitorIDs: %+v", hm.MonitorIDs, combineResult.MonitorIDs)
				hm.MonitorIDs = combineResult.MonitorIDs
			}
			logEntry.Infof("Got monitorIDs: %+v", hm.MonitorIDs)
			t.UpdateMetadata(hm)
		}

		// mark remain kubernetes binding ids.
		op.TaskQueues.GetByName(t.GetQueueName()).Iterate(func(tsk sh_task.Task) {
			thm := task.HookMetadataAccessor(tsk)
			if thm.HookName == hm.HookName && thm.KubernetesBindingId != "" {
				kubeIds[thm.KubernetesBindingId] = true
			}
		})
		logEntry.Debugf("global kubeIds: %+v", kubeIds)

		// remove ids from state for removed tasks
		// TODO How does it work on error?
		for kubeId, v := range kubeIds {
			if !v {
				logEntry.Debugf("global delete kubeId '%s'", kubeId)
				op.ModuleManager.SynchronizationDone(kubeId)
			}
		}
	}

	// TODO create metadata flag that indicate whether to add reload all task on values changes
	//op.HelmResourcesManager.PauseMonitors()

	if shouldRunHook {
		logEntry.Infof("Global hook run")

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
				res.Status = "Success"
			} else {
				errors = 1.0
				logEntry.Errorf("Global hook failed, requeue task to retry after delay. Failed count is %d. Error: %s", t.GetFailureCount()+1, err)
				t.UpdateFailureMessage(err.Error())
				t.WithQueuedAt(time.Now())
				res.Status = "Fail"
			}
		} else {
			// Calculate new checksum of *Enabled values.
			dynamicEnabledChecksumAfterHookRun := op.ModuleManager.DynamicEnabledChecksum()
			success = 1.0
			logEntry.Infof("Global hook success '%s'", taskHook.Name)
			logEntry.Debugf("GlobalHookRun checksums: before=%s after=%s saved=%s", beforeChecksum, afterChecksum, hm.ValuesChecksum)
			res.Status = "Success"

			reloadAll := false
			eventDescription := ""
			switch hm.BindingType {
			case Schedule:
				if beforeChecksum != afterChecksum {
					reloadAll = true
					eventDescription = fmt.Sprintf("Schedule-Change-GlobalValues(%s)", hm.GetHookName())
				}
				if dynamicEnabledChecksumBeforeHookRun != dynamicEnabledChecksumAfterHookRun {
					reloadAll = true
					if eventDescription == "" {
						eventDescription = fmt.Sprintf("Schedule-Change-DynamicEnabled(%s)", hm.GetHookName())
					} else {
						eventDescription += "-And-DynamicEnabled"
					}
				}
			case OnKubernetesEvent:
				// Ignore values changes from Synchronization runs
				if hm.ReloadAllOnValuesChanges && beforeChecksum != afterChecksum {
					reloadAll = true
					eventDescription = fmt.Sprintf("Kubernetes-Change-GlobalValues(%s)", hm.GetHookName())
				}
				if dynamicEnabledChecksumBeforeHookRun != dynamicEnabledChecksumAfterHookRun {
					reloadAll = true
					if eventDescription == "" {
						eventDescription = fmt.Sprintf("Kubernetes-Change-DynamicEnabled(%s)", hm.GetHookName())
					} else {
						eventDescription += "-And-DynamicEnabled"
					}
				}
			case AfterAll:
				// values are changed when afterAll hooks are executed
				if hm.LastAfterAllHook && afterChecksum != hm.ValuesChecksum {
					reloadAll = true
					eventDescription = "AfterAll-Hooks-Change-GlobalValues"
				}

				// values are changed when afterAll hooks are executed
				if hm.LastAfterAllHook && dynamicEnabledChecksumAfterHookRun != hm.DynamicEnabledChecksum {
					reloadAll = true
					if eventDescription == "" {
						eventDescription = "AfterAll-Hooks-Change-DynamicEnabled"
					} else {
						eventDescription = eventDescription + "-And-DynamicEnabled"
					}
				}
			}
			// Queue ReloadAllModules task
			if reloadAll {
				// Stop and remove all resource monitors to prevent excessive ModuleRun tasks
				op.HelmResourcesManager.StopMonitors()
				// relabel
				logLabels := t.GetLogLabels()
				if hookLabel, ok := logLabels["hook"]; ok {
					logLabels["event.triggered-by.hook"] = hookLabel
					delete(logLabels, "hook")
					delete(logLabels, "hook.type")
				}
				if label, ok := logLabels["binding"]; ok {
					logLabels["event.triggered-by.binding"] = label
					delete(logLabels, "binding")
				}
				if label, ok := logLabels["binding.name"]; ok {
					logLabels["event.triggered-by.binding.name"] = label
					delete(logLabels, "binding.name")
				}
				if label, ok := logLabels["watchEvent"]; ok {
					logLabels["event.triggered-by.watchEvent"] = label
					delete(logLabels, "watchEvent")
				}
				// Put "ReloadAllModules" task with onStartup flag turned off to the end of the 'main' queue.
				reloadAllModulesTask := sh_task.NewTask(task.ReloadAllModules).
					WithLogLabels(t.GetLogLabels()).
					WithQueueName("main").
					WithMetadata(task.HookMetadata{
						EventDescription: eventDescription,
						OnStartupHooks:   false,
					}).
					WithQueuedAt(time.Now())
				op.TaskQueues.GetMain().AddLast(reloadAllModulesTask.WithQueuedAt(time.Now()))
				logEntry.Infof("ReloadAllModules queued by hook '%s' (task: %s)", taskHook.Name, hm.GetDescription())
			}
			// TODO rethink helm monitors pause-resume. It is not working well with parallel hooks without locks. But locks will destroy parallelization.
			//else {
			//	op.HelmResourcesManager.ResumeMonitors()
			//}
		}

		op.MetricStorage.CounterAdd("{PREFIX}global_hook_allowed_errors_total", allowed, metricLabels)
		op.MetricStorage.CounterAdd("{PREFIX}global_hook_errors_total", errors, metricLabels)
		op.MetricStorage.CounterAdd("{PREFIX}global_hook_success_total", success, metricLabels)
	}

	if isSynchronization && res.Status == "Success" {
		kubernetesBindingId := hm.KubernetesBindingId
		if kubernetesBindingId != "" {
			logEntry.Infof("Done Synchronization '%s'", kubernetesBindingId)
			op.ModuleManager.SynchronizationDone(kubernetesBindingId)
		}
		// Unlock Kubernetes events for all monitors when Synchronization task is done.
		logEntry.Info("Unlock kubernetes.Event tasks")
		for _, monitorID := range hm.MonitorIDs {
			taskHook.HookController.UnlockKubernetesEventsFor(monitorID)
		}
	}

	return res
}

func (op *AddonOperator) RunDiscoverModulesState(discoverTask sh_task.Task, logLabels map[string]string) ([]sh_task.Task, error) {
	logEntry := log.WithFields(utils.LabelsToLogFields(logLabels))
	modulesState, err := op.ModuleManager.DiscoverModulesState(logLabels)
	if err != nil {
		return nil, err
	}

	var newTasks []sh_task.Task

	hm := task.HookMetadataAccessor(discoverTask)

	eventDescription := hm.EventDescription
	if !strings.Contains(eventDescription, "DiscoverModulesState") {
		eventDescription += ".DiscoverModulesState"
	}

	// queue ModuleRun tasks for enabled modules
	for _, moduleName := range modulesState.EnabledModules {
		newLogLabels := utils.MergeLabels(logLabels)
		newLogLabels["module"] = moduleName
		delete(newLogLabels, "task.id")

		// Run OnStartup hooks on application startup or if module become enabled
		runOnStartupHooks := hm.OnStartupHooks
		if !runOnStartupHooks {
			for _, name := range modulesState.NewlyEnabledModules {
				if name == moduleName {
					runOnStartupHooks = true
					break
				}
			}
		}

		newTask := sh_task.NewTask(task.ModuleRun).
			WithLogLabels(newLogLabels).
			WithQueueName("main").
			WithMetadata(task.HookMetadata{
				EventDescription: eventDescription,
				ModuleName:       moduleName,
				OnStartupHooks:   runOnStartupHooks,
			})
		newTasks = append(newTasks, newTask)

		logEntry.WithFields(utils.LabelsToLogFields(newTask.LogLabels)).
			Infof("queue task %s", newTask.GetDescription())
	}

	// queue ModuleDelete tasks for disabled modules
	for _, moduleName := range modulesState.ModulesToDisable {
		newLogLabels := utils.MergeLabels(logLabels)
		newLogLabels["module"] = moduleName
		delete(newLogLabels, "task.id")
		// Register module hooks for deleted modules to able to
		// run afterHelmDelete hooks on Addon-operator startup.
		if hm.OnStartupHooks {
			// error can be ignored, DiscoverModulesState should return existed modules
			disabledModule := op.ModuleManager.GetModule(moduleName)
			if err = op.ModuleManager.RegisterModuleHooks(disabledModule, newLogLabels); err != nil {
				return nil, err
			}
		}

		newTask := sh_task.NewTask(task.ModuleDelete).
			WithLogLabels(newLogLabels).
			WithQueueName("main").
			WithMetadata(task.HookMetadata{
				EventDescription: eventDescription,
				ModuleName:       moduleName,
			})
		newTasks = append(newTasks, newTask)

		logEntry.WithFields(utils.LabelsToLogFields(newTask.LogLabels)).
			Infof("queue task %s", newTask.GetDescription())
	}

	// queue ModulePurge tasks for unknown modules
	for _, moduleName := range modulesState.ReleasedUnknownModules {
		newLogLabels := utils.MergeLabels(logLabels)
		newLogLabels["module"] = moduleName
		delete(newLogLabels, "task.id")

		newTask := sh_task.NewTask(task.ModulePurge).
			WithLogLabels(newLogLabels).
			WithQueueName("main").
			WithMetadata(task.HookMetadata{
				EventDescription: eventDescription,
				ModuleName:       moduleName,
			})
		newTasks = append(newTasks, newTask)

		logEntry.WithFields(utils.LabelsToLogFields(newTask.LogLabels)).
			Infof("queue task %s", newTask.GetDescription())
	}

	// Queue afterAll global hooks
	afterAllHooks := op.ModuleManager.GetGlobalHooksInOrder(AfterAll)
	for i, hookName := range afterAllHooks {
		hookLogLabels := utils.MergeLabels(logLabels, map[string]string{
			"hook":      hookName,
			"hook.type": "global",
			"queue":     "main",
			"binding":   string(AfterAll),
		})
		delete(hookLogLabels, "task.id")

		afterAllBc := BindingContext{
			Binding: string(AfterAll),
		}
		afterAllBc.Metadata.BindingType = AfterAll
		afterAllBc.Metadata.IncludeAllSnapshots = true

		taskMetadata := task.HookMetadata{
			EventDescription: eventDescription,
			HookName:         hookName,
			BindingType:      AfterAll,
			BindingContext:   []BindingContext{afterAllBc},
		}
		if i == len(afterAllHooks)-1 {
			taskMetadata.LastAfterAllHook = true
			globalValues, err := op.ModuleManager.GlobalValues()
			if err != nil {
				return nil, err
			}
			taskMetadata.ValuesChecksum, err = globalValues.Checksum()
			taskMetadata.DynamicEnabledChecksum = op.ModuleManager.DynamicEnabledChecksum()
			if err != nil {
				return nil, err
			}
		}

		newTask := sh_task.NewTask(task.GlobalHookRun).
			WithLogLabels(hookLogLabels).
			WithQueueName("main").
			WithMetadata(taskMetadata)
		newTasks = append(newTasks, newTask)

		logEntry.WithFields(utils.LabelsToLogFields(newTask.LogLabels)).
			Infof("queue task %s", newTask.GetDescription())
	}

	// TODO queues should be cleaned from hook run tasks of deleted module!
	// Disable kubernetes informers and schedule
	for _, moduleName := range modulesState.ModulesToDisable {
		op.ModuleManager.DisableModuleHooks(moduleName)
	}

	return newTasks, nil
}

func (op *AddonOperator) RunAddonOperatorMetrics() {
	// Addon-operator live ticks.
	go func() {
		for {
			op.MetricStorage.CounterAdd("{PREFIX}live_ticks", 1.0, map[string]string{})
			time.Sleep(10 * time.Second)
		}
	}()

	go func() {
		for {
			// task queue length
			op.TaskQueues.Iterate(func(queue *queue.TaskQueue) {
				queueLen := float64(queue.Length())
				op.MetricStorage.GaugeSet("{PREFIX}tasks_queue_length", queueLen, map[string]string{"queue": queue.Name})
			})
			time.Sleep(5 * time.Second)
		}
	}()
}

func (op *AddonOperator) SetupDebugServerHandles() {
	op.DebugServer.Route("/global/list.{format:(json|yaml)}", func(_ *http.Request) (interface{}, error) {
		return map[string]interface{}{
			"globalHooks": op.ModuleManager.GetGlobalHooksNames(),
		}, nil
	})

	op.DebugServer.Route("/global/values.{format:(json|yaml)}", func(_ *http.Request) (interface{}, error) {
		return op.ModuleManager.GlobalValues()
	})

	op.DebugServer.Route("/global/config.{format:(json|yaml)}", func(_ *http.Request) (interface{}, error) {
		return op.ModuleManager.GlobalConfigValues(), nil
	})

	op.DebugServer.Route("/global/patches.{format:(json|yaml)}", func(_ *http.Request) (interface{}, error) {
		return op.ModuleManager.GlobalValuesPatches(), nil
	})

	op.DebugServer.Route("/global/snapshots.{format:(json|yaml)}", func(r *http.Request) (interface{}, error) {
		kubeHookNames := op.ModuleManager.GetGlobalHooksInOrder(OnKubernetesEvent)
		snapshots := make(map[string]interface{})
		for _, hName := range kubeHookNames {
			h := op.ModuleManager.GetGlobalHook(hName)
			snapshots[hName] = h.HookController.SnapshotsDump()
		}

		return snapshots, nil
	})

	op.DebugServer.Route("/module/list.{format:(json|yaml|text)}", func(_ *http.Request) (interface{}, error) {
		return map[string][]string{"enabledModules": op.ModuleManager.GetModuleNamesInOrder()}, nil
	})

	op.DebugServer.Route("/module/{name}/{type:(config|values)}.{format:(json|yaml)}", func(r *http.Request) (interface{}, error) {
		modName := chi.URLParam(r, "name")
		valType := chi.URLParam(r, "type")

		m := op.ModuleManager.GetModule(modName)
		if m == nil {
			return nil, fmt.Errorf("Module not found")
		}

		switch valType {
		case "config":
			return m.ConfigValues(), nil
		case "values":
			return m.Values()
		}
		return "no values", nil
	})

	op.DebugServer.Route("/module/{name}/render", func(r *http.Request) (interface{}, error) {
		modName := chi.URLParam(r, "name")

		m := op.ModuleManager.GetModule(modName)
		if m == nil {
			return nil, fmt.Errorf("Module not found")
		}

		valuesPath, err := m.PrepareValuesYamlFile()
		if err != nil {
			return nil, err
		}
		defer os.Remove(valuesPath)

		helmCl := helm.NewClient()
		return helmCl.Render(m.Name, m.Path, []string{valuesPath}, nil, app.Namespace)
	})

	op.DebugServer.Route("/module/{name}/patches.json", func(r *http.Request) (interface{}, error) {
		modName := chi.URLParam(r, "name")

		m := op.ModuleManager.GetModule(modName)
		if m == nil {
			return nil, fmt.Errorf("Module not found")
		}

		return m.ValuesPatches(), nil
	})

	op.DebugServer.Route("/module/resource-monitor.{format:(json|yaml)}", func(_ *http.Request) (interface{}, error) {
		dump := map[string]interface{}{}

		for _, moduleName := range op.ModuleManager.GetModuleNamesInOrder() {
			if !op.HelmResourcesManager.HasMonitor(moduleName) {
				dump[moduleName] = "No monitor"
				continue
			}

			ids := op.HelmResourcesManager.GetMonitor(moduleName).ResourceIds()
			dump[moduleName] = ids
		}

		return dump, nil
	})

	op.DebugServer.Route("/module/{name}/snapshots.{format:(json|yaml)}", func(r *http.Request) (interface{}, error) {
		modName := chi.URLParam(r, "name")

		m := op.ModuleManager.GetModule(modName)
		if m == nil {
			return nil, fmt.Errorf("Module not found")
		}

		mHookNames := op.ModuleManager.GetModuleHookNames(m.Name)
		snapshots := make(map[string]interface{})
		for _, hName := range mHookNames {
			h := op.ModuleManager.GetModuleHook(hName)
			snapshots[hName] = h.HookController.SnapshotsDump()
		}

		return snapshots, nil
	})

}

func (op *AddonOperator) SetupHttpServerHandles() {
	http.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`<html>
    <head><title>Addon-operator</title></head>
    <body>
    <h1>Addon-operator</h1>
    <pre>go tool pprof goprofex http://ADDON_OPERATOR_IP:9115/debug/pprof/profile</pre>
    <p>
      <a href="/metrics">prometheus metrics</a>
      <a href="/healthz">health url</a>
    </p>
    </body>
    </html>`))
	})
	http.Handle("/metrics", promhttp.Handler())

	http.HandleFunc("/healthz", func(writer http.ResponseWriter, request *http.Request) {
		if helm.HealthzHandler == nil {
			writer.WriteHeader(http.StatusOK)
			return
		}
		helm.HealthzHandler(writer, request)
	})

	http.HandleFunc("/ready", func(w http.ResponseWriter, request *http.Request) {
		if op.IsStartupConvergeDone() {
			w.WriteHeader(200)
			_, _ = w.Write([]byte("Startup converge done.\n"))
		} else {
			w.WriteHeader(500)
			_, _ = w.Write([]byte("Startup converge in progress\n"))
		}
	})

	http.HandleFunc("/status/converge", func(writer http.ResponseWriter, request *http.Request) {
		convergeTasks := op.MainQueueHasConvergeTasks()

		statusLines := make([]string, 0)
		if op.IsStartupConvergeDone() {
			statusLines = append(statusLines, "STARTUP_CONVERGE_DONE")
			if convergeTasks > 0 {
				statusLines = append(statusLines, fmt.Sprintf("CONVERGE_IN_PROGRESS: %d tasks", convergeTasks))
			} else {
				statusLines = append(statusLines, "CONVERGE_WAIT_TASK")
			}
		} else {
			if op.StartupConvergeStarted {
				if convergeTasks > 0 {
					statusLines = append(statusLines, fmt.Sprintf("STARTUP_CONVERGE_IN_PROGRESS: %d tasks", convergeTasks))
				} else {
					statusLines = append(statusLines, "STARTUP_CONVERGE_DONE")
				}
			} else {
				statusLines = append(statusLines, "STARTUP_CONVERGE_WAIT_TASKS")
			}
		}

		_, _ = writer.Write([]byte(strings.Join(statusLines, "\n") + "\n"))
	})
}

func (op *AddonOperator) MainQueueHasConvergeTasks() int {
	convergeTasks := 0
	op.TaskQueues.GetMain().Iterate(func(t sh_task.Task) {
		ttype := t.GetType()
		switch ttype {
		case task.ModuleRun, task.DiscoverModulesState, task.ModuleDelete, task.ModulePurge, task.ModuleManagerRetry, task.ReloadAllModules, task.GlobalHookEnableKubernetesBindings, task.GlobalHookEnableScheduleBindings:
			convergeTasks++
			return
		}

		hm := task.HookMetadataAccessor(t)
		if ttype == task.GlobalHookRun {
			switch hm.BindingType {
			case BeforeAll, AfterAll:
				convergeTasks++
				return
			}
		}
	})

	return convergeTasks
}

func (op *AddonOperator) CheckConvergeStatus(t sh_task.Task) {
	convergeTasks := op.MainQueueHasConvergeTasks()

	logEntry := log.WithFields(utils.LabelsToLogFields(t.GetLogLabels()))
	logEntry.Infof("Queue 'main' contains %d converge tasks after handle '%s'", convergeTasks, string(t.GetType()))

	// Trigger Started.
	if convergeTasks > 0 {
		if !op.StartupConvergeStarted {
			logEntry.Infof("First converge is started.")
			op.StartupConvergeStarted = true
		}
		if op.ConvergeStarted == 0 {
			op.ConvergeStarted = time.Now().UnixNano()
			op.ConvergeActivation = t.GetLogLabels()["event.type"]
		}
	}

	// Trigger Done.
	if convergeTasks == 0 {
		if !op.IsStartupConvergeDone() && op.StartupConvergeStarted {
			logEntry.Infof("First converge is finished. Operator is ready now.")
			op.SetStartupConvergeDone()
		}
		if op.ConvergeStarted != 0 {
			convergeSeconds := time.Duration(time.Now().UnixNano() - op.ConvergeStarted).Seconds()
			op.MetricStorage.CounterAdd("{PREFIX}convergence_seconds", convergeSeconds, map[string]string{"activation": op.ConvergeActivation})
			op.MetricStorage.CounterAdd("{PREFIX}convergence_total", 1.0, map[string]string{"activation": op.ConvergeActivation})
			op.ConvergeStarted = 0
		}
	}
}

func (op *AddonOperator) Shutdown() {
	op.KubeConfigManager.Stop()
	op.ShellOperator.Shutdown()
}

func DefaultOperator() *AddonOperator {
	operator := NewAddonOperator()
	operator.WithContext(context.Background())
	return operator
}

func InitAndStart(operator *AddonOperator) error {
	err := operator.StartHttpServer(sh_app.ListenAddress, sh_app.ListenPort, http.DefaultServeMux)
	if err != nil {
		log.Errorf("HTTP SERVER start failed: %v", err)
		return err
	}
	// Override shell-operator's metricStorage and register metrics specific for addon-operator.
	operator.InitMetricStorage()
	operator.SetupHttpServerHandles()

	err = operator.SetupHookMetricStorageAndServer()
	if err != nil {
		log.Errorf("HTTP SERVER for hook metrics start failed: %v", err)
		return err
	}

	// Create a default 'main' Kubernetes client if not initialized externally.
	// Register metrics for kubernetes client with the default custom label "component".
	if operator.KubeClient == nil {
		// Register metrics for client-go.
		//nolint:staticcheck
		klient.RegisterKubernetesClientMetrics(operator.MetricStorage, operator.GetMainKubeClientMetricLabels())
		// Initialize 'main' Kubernetes client.
		operator.KubeClient, err = operator.InitMainKubeClient()
		if err != nil {
			log.Errorf("MAIN Fatal: initialize kube client for hooks: %s\n", err)
			return err
		}
	}

	err = operator.Init()
	if err != nil {
		log.Errorf("INIT failed: %v", err)
		return err
	}

	operator.ShellOperator.SetupDebugServerHandles()
	operator.SetupDebugServerHandles()

	err = operator.InitModuleManager()
	if err != nil {
		log.Errorf("INIT ModuleManager failed: %s", err)
		return err
	}

	operator.Start()
	return nil
}

// QueueHasPendingModuleRunTask returns true if queue has pending tasks
// with the type "ModuleRun" related to the module "moduleName".
func QueueHasPendingModuleRunTask(q *queue.TaskQueue, moduleName string) bool {
	hasTask := false
	firstTask := true

	q.Iterate(func(t sh_task.Task) {
		// Skip the first task in the queue as it can be executed already, i.e. "not pending".
		if firstTask {
			firstTask = false
			return
		}

		if t.GetType() == task.ModuleRun && task.HookMetadataAccessor(t).ModuleName == moduleName {
			hasTask = true
		}
	})

	return hasTask
}
