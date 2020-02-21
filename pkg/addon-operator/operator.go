package addon_operator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"gopkg.in/satori/go.uuid.v1"
	"sigs.k8s.io/yaml"

	sh_app "github.com/flant/shell-operator/pkg/app"
	. "github.com/flant/shell-operator/pkg/hook/binding_context"
	"github.com/flant/shell-operator/pkg/hook/controller"
	. "github.com/flant/shell-operator/pkg/hook/types"
	"github.com/flant/shell-operator/pkg/kube_events_manager/types"
	"github.com/flant/shell-operator/pkg/shell-operator"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"

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

// InitModuleManager initialize objects for addon-operator.
// This method should be run after Init().
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

	// TODO make tiller cancelable
	err = helm.InitTillerProcess(helm.TillerOptions{
		Namespace:          app.Namespace,
		HistoryMax:         app.TillerMaxHistory,
		ListenAddress:      app.TillerListenAddress,
		ListenPort:         app.TillerListenPort,
		ProbeListenAddress: app.TillerProbeListenAddress,
		ProbeListenPort:    app.TillerProbeListenPort,
	})
	if err != nil {
		return fmt.Errorf("init tiller: %s", err)
	}

	// Initializing helm client
	helm.WithKubeClient(op.KubeClient)
	err = helm.NewClient().InitAndVersion()
	if err != nil {
		return fmt.Errorf("init helm client: %s", err)
	}

	// Initializing ConfigMap storage for values
	op.KubeConfigManager = kube_config_manager.NewKubeConfigManager()
	op.KubeConfigManager.WithKubeClient(op.KubeClient)
	op.KubeConfigManager.WithContext(op.ctx)
	op.KubeConfigManager.WithNamespace(app.Namespace)
	op.KubeConfigManager.WithConfigMapName(app.ConfigMapName)
	op.KubeConfigManager.WithValuesChecksumsAnnotation(app.ValuesChecksumsAnnotation)

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
	err = op.ModuleManager.Init()
	if err != nil {
		return fmt.Errorf("init module manager: %s", err)
	}

	op.DefineEventHandlers()

	// Init helm resources manager
	op.HelmResourcesManager = helm_resources_manager.NewHelmResourcesManager()
	op.HelmResourcesManager.WithContext(op.ctx)
	op.HelmResourcesManager.WithKubeClient(op.KubeClient)
	op.HelmResourcesManager.WithDefaultNamespace(app.Namespace)

	op.ModuleManager.WithHelmResourcesManager(op.HelmResourcesManager)

	return nil
}

func (op *AddonOperator) DefineEventHandlers() {
	op.ManagerEventsHandler.WithScheduleEventHandler(func(crontab string) []sh_task.Task {
		logLabels := map[string]string{
			"event.id": uuid.NewV4().String(),
			"binding":  ContextBindingType[Schedule],
		}
		logEntry := log.WithFields(utils.LabelsToLogFields(logLabels))
		logEntry.Debugf("Create tasks for 'schedule' event '%s'", crontab)

		var tasks []sh_task.Task
		err := op.ModuleManager.HandleScheduleEvent(crontab,
			func(globalHook *module_manager.GlobalHook, info controller.BindingExecutionInfo) {
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
			"binding":  ContextBindingType[OnKubernetesEvent],
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

	mainQueue := tqs.GetMain()
	mainQueue.ChangesDisable()

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
		op.TaskQueues.GetMain().AddLast(newTask)

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
		op.TaskQueues.GetMain().AddLast(newTask)

		logEntry.WithFields(utils.LabelsToLogFields(newTask.LogLabels)).
			Infof("queue task %s", newTask.GetDescription())
	}

	// Create "ReloadAll" set of tasks with onStartup flag to discover modules state for the first time.
	op.CreateReloadAllTasks(true, onStartupLabels, "PrepopulateMainQueue")
}

// CreateReloadAllTasks
func (op *AddonOperator) CreateReloadAllTasks(onStartup bool, logLabels map[string]string, eventDescription string) {
	logEntry := log.WithFields(utils.LabelsToLogFields(logLabels))

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

		// bc := module_manager.BindingContext{BindingContext: hook.BindingContext{Binding: module_manager.ContextBindingType[module_manager.BeforeAll]}}
		// bc.KubernetesSnapshots := ModuleManager.GetGlobalHook(hookName).HookController.KubernetesSnapshots()

		beforeAllBc := BindingContext{
			Binding: ContextBindingType[BeforeAll],
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
		op.TaskQueues.GetMain().AddLast(newTask)

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
	op.TaskQueues.GetMain().AddLast(discoverTask)
	logEntry.WithFields(utils.LabelsToLogFields(discoverTask.LogLabels)).
		Infof("queue task %s", discoverTask.GetDescription())
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

	return
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
						hasTask := QueueHasModuleRunTask(op.TaskQueues.GetMain(), moduleChange.Name)
						if !hasTask {
							logEntry.WithField("module", moduleChange.Name).Infof("module values are changed, queue ModuleRun task")
							newTask := sh_task.NewTask(task.ModuleRun).
								WithLogLabels(logLabels).
								WithQueueName("main").
								WithMetadata(task.HookMetadata{
									EventDescription: "ModuleValuesChanged",
									ModuleName:       moduleChange.Name,
								})
							op.TaskQueues.GetMain().AddLast(newTask)
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
					// Stop all resource monitors before run modules discovery.
					op.HelmResourcesManager.StopMonitors()
					op.CreateReloadAllTasks(false, logLabels, "GlobalConfigValuesChanged")
					// TODO Check if this is needed?
					// As module list may have changed, hook schedule index must be re-created.
					//ScheduleHooksController.UpdateScheduleHooks()
				case module_manager.AmbigousState:
					// It is the error in the module manager. The task must be added to
					// the beginning of the queue so the module manager can restore its
					// state before running other queue tasks
					logLabels["event.type"] = "AmbiguousState"
					//TasksQueue.ChangesDisable()
					newTask := sh_task.NewTask(task.ModuleManagerRetry).
						WithLogLabels(logLabels).
						WithQueueName("main")
					op.TaskQueues.GetMain().AddFirst(newTask)
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
				hasTask := QueueHasModuleRunTask(op.TaskQueues.GetMain(), absentResourcesEvent.ModuleName)
				if !hasTask {
					newTask := sh_task.NewTask(task.ModuleRun).
						WithLogLabels(logLabels).
						WithQueueName("main").
						WithMetadata(task.HookMetadata{
							EventDescription: "DetectAbsentHelmResources",
							ModuleName:       absentResourcesEvent.ModuleName,
						})
					op.TaskQueues.GetMain().AddLast(newTask)
					eventLogEntry.WithFields(utils.LabelsToLogFields(newTask.LogLabels)).
						Infof("queue task %s - got %d absent module resources, queue ModuleRun task", newTask.GetDescription(), len(absentResourcesEvent.Absent))
				} else {
					eventLogEntry.Infof("Got %d absent module resources, ModuleRun task exists", len(absentResourcesEvent.Absent))
				}
			}
		}
	}()
}

// TasksRunner handle tasks in queue.
//
// Task handler may delay task processing by pushing delay to the queue.
// FIXME: For now, only one TaskRunner for a TasksQueue. There should be a lock between Peek and Pop to prevent Poping tasks by other TaskRunner for multiple queues.
func (op *AddonOperator) TaskHandler(t sh_task.Task) queue.TaskResult {
	var logEntry = log.WithField("operator.component", "taskRunner").
		WithFields(utils.LabelsToLogFields(t.GetLogLabels()))
	var res queue.TaskResult

	switch t.GetType() {
	case task.GlobalHookRun:
		taskLogEntry := logEntry.WithFields(utils.LabelsToLogFields(t.GetLogLabels()))
		taskLogEntry.Infof("Global hook run")
		hm := task.HookMetadataAccessor(t)

		// TODO create metadata flag that indicate whether to add reload all task on values changes
		beforeChecksum, afterChecksum, err := op.ModuleManager.RunGlobalHook(hm.HookName, hm.BindingType, hm.BindingContext, t.GetLogLabels())
		if err != nil {
			globalHook := op.ModuleManager.GetGlobalHook(hm.HookName)
			hookLabel := path.Base(globalHook.Path)

			if hm.AllowFailure {
				op.MetricStorage.SendCounter("global_hook_allowed_errors", 1.0, map[string]string{"hook": hookLabel})
				taskLogEntry.Infof("Global hook failed, but allowed to fail. Error: %v", err)
				res.Status = "Success"
			} else {
				op.MetricStorage.SendCounter("global_hook_errors", 1.0, map[string]string{"hook": hookLabel})
				taskLogEntry.Errorf("Global hook failed, requeue task to retry after delay. Failed count is %d. Error: %s", t.GetFailureCount()+1, err)
				res.Status = "Fail"
			}
		} else {
			taskLogEntry.Infof("Global hook success")
			taskLogEntry.Debugf("GlobalHookRun checksums: before=%s after=%s saved=%s", beforeChecksum, afterChecksum, hm.ValuesChecksum)
			res.Status = "Success"

			reloadAll := false
			eventDescription := ""
			switch hm.BindingType {
			case Schedule:
				if beforeChecksum != afterChecksum {
					reloadAll = true
					eventDescription = "ScheduleChangeGlobalValues"
				}
			case OnKubernetesEvent:
				// Ignore values changes from Synchronization runs
				if hm.ReloadAllOnValuesChanges && beforeChecksum != afterChecksum {
					reloadAll = true
					eventDescription = "KubernetesChangeGlobalValues"
				}
			case AfterAll:
				// values are changed when afterAll hooks are executed
				if hm.LastAfterAllHook && afterChecksum != hm.ValuesChecksum {
					reloadAll = true
					eventDescription = "AfterAllHooksChangeGlobalValues"
				}
			}
			if reloadAll {
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
				op.CreateReloadAllTasks(false, t.GetLogLabels(), eventDescription)
			}
		}

	case task.GlobalHookEnableKubernetesBindings:
		taskLogEntry := logEntry.WithFields(utils.LabelsToLogFields(t.GetLogLabels()))
		taskLogEntry.Infof("Global hook enable kubernetes bindings")
		hm := task.HookMetadataAccessor(t)
		globalHook := op.ModuleManager.GetGlobalHook(hm.HookName)

		hookRunTasks := []sh_task.Task{}

		eventDescription := hm.EventDescription
		if !strings.Contains(eventDescription, "HandleGlobalEnableKubernetesBindings") {
			eventDescription += ".HandleGlobalEnableKubernetesBindings"
		}

		newLogLabels := utils.MergeLabels(t.GetLogLabels())
		delete(newLogLabels, "task.id")

		err := op.ModuleManager.HandleGlobalEnableKubernetesBindings(hm.HookName, func(hook *module_manager.GlobalHook, info controller.BindingExecutionInfo) {
			newTask := sh_task.NewTask(task.GlobalHookRun).
				WithLogLabels(newLogLabels).
				WithQueueName(info.QueueName).
				WithMetadata(task.HookMetadata{
					EventDescription:         eventDescription,
					HookName:                 hook.GetName(),
					BindingType:              OnKubernetesEvent,
					BindingContext:           info.BindingContext,
					AllowFailure:             info.AllowFailure,
					ReloadAllOnValuesChanges: false, // Ignore global values changes
				})
			hookRunTasks = append(hookRunTasks, newTask)
		})

		if err != nil {
			hookLabel := path.Base(globalHook.Path)

			op.MetricStorage.SendCounter("global_hook_errors", 1.0, map[string]string{"hook": hookLabel})
			taskLogEntry.Errorf("Global hook enable kubernetes bindings failed, requeue task to retry after delay. Failed count is %d. Error: %s", t.GetFailureCount()+1, err)
			res.Status = "Fail"
		} else {
			// Push Synchronization tasks to queue head. Informers can be started now — their events will
			// be added to the queue tail.
			taskLogEntry.Infof("Global hook enable kubernetes bindings success")

			globalHook.HookController.StartMonitors()
			globalHook.HookController.EnableScheduleBindings()

			res.Status = "Success"
			res.HeadTasks = hookRunTasks
		}

	case task.DiscoverModulesState:
		taskLogEntry := logEntry.WithFields(utils.LabelsToLogFields(t.GetLogLabels()))
		taskLogEntry.Info("Discover modules start")
		tasks, err := op.RunDiscoverModulesState(t, t.GetLogLabels())
		if err != nil {
			op.MetricStorage.SendCounter("modules_discover_errors", 1.0, map[string]string{})
			taskLogEntry.Errorf("Discover modules failed, requeue task to retry after delay. Failed count is %d. Error: %s", t.GetFailureCount()+1, err)
			res.Status = "Fail"
		} else {
			taskLogEntry.Infof("Discover modules success")
			res.Status = "Success"
			res.AfterTasks = tasks
		}

	case task.ModuleRun:
		// This is complicated task. It runs OnStartup hooks, then kubernetes hooks with Synchronization
		// binding context, beforeHelm hooks, helm upgrade and afterHelm hooks.
		// If something goes wrong, then this process is restarted.
		// If process is succeeded, then OnStartup and Synchronization will not run the next time.
		taskLogEntry := logEntry.WithFields(utils.LabelsToLogFields(t.GetLogLabels()))
		taskLogEntry.Info("Module run start")
		hm := task.HookMetadataAccessor(t)

		// Module hooks are now registered and queues can be started.
		if hm.OnStartupHooks {
			op.InitAndStartHookQueues()
		}

		valuesChanged, err := op.ModuleManager.RunModule(hm.ModuleName, hm.OnStartupHooks, t.GetLogLabels(), func() error {
			// EnableKubernetesBindings and StartInformers for all kubernetes bindings
			// after running all OnStartup hooks.
			hookRunTasks := []sh_task.Task{}

			err := op.ModuleManager.HandleModuleEnableKubernetesBindings(hm.ModuleName, func(hook *module_manager.ModuleHook, info controller.BindingExecutionInfo) {
				hookLogLabels := utils.MergeLabels(t.GetLogLabels(), map[string]string{
					"module":    hm.ModuleName,
					"hook":      hook.GetName(),
					"hook.type": "module",
					"queue":     info.QueueName,
				})
				if len(info.BindingContext) > 0 {
					hookLogLabels["binding.name"] = info.BindingContext[0].Binding
				}
				delete(hookLogLabels, "task.id")
				newTask := sh_task.NewTask(task.ModuleHookRun).
					WithLogLabels(hookLogLabels).
					WithQueueName(info.QueueName).
					WithMetadata(task.HookMetadata{
						ModuleName:     hm.ModuleName,
						HookName:       hook.GetName(),
						BindingType:    OnKubernetesEvent,
						BindingContext: info.BindingContext,
						AllowFailure:   info.AllowFailure,
					})

				hookRunTasks = append(hookRunTasks, newTask)
				taskLogEntry.WithFields(utils.LabelsToLogFields(newTask.LogLabels)).
					Infof("queue task %s - Synchronization after onStartup", newTask.GetDescription())
			})
			if err != nil {
				return err
			}
			// Run OnKubernetesEvent@Synchronization tasks immediately
			for _, t := range hookRunTasks {
				hookLogEntry := taskLogEntry.WithFields(utils.LabelsToLogFields(t.GetLogLabels()))
				hookLogEntry.Info("Module hook start with type Synchronization")
				hm := task.HookMetadataAccessor(t)
				err := op.ModuleManager.RunModuleHook(hm.HookName, hm.BindingType, hm.BindingContext, t.GetLogLabels())
				if err != nil {
					moduleHook := op.ModuleManager.GetModuleHook(hm.HookName)
					hookLabel := path.Base(moduleHook.Path)
					moduleLabel := moduleHook.Module.Name
					op.MetricStorage.SendCounter("module_hook_errors", 1.0, map[string]string{"module": moduleLabel, "hook": hookLabel})
					return err
				} else {
					hookLogEntry.Infof("Module hook success for Synchronization")
				}
			}
			op.ModuleManager.StartModuleHooks(hm.ModuleName)
			return nil
		})
		if err != nil {
			op.MetricStorage.SendCounter("module_run_errors", 1.0, map[string]string{"module": hm.ModuleName})
			logEntry.Errorf("Module run failed, requeue task to retry after delay. Failed count is %d. Error: %s", t.GetFailureCount()+1, err)
			res.Status = "Fail"
		} else {
			res.Status = "Success"
			logEntry.Infof("Module run success")
			if valuesChanged {
				// One of afterHelm hooks changes values, run ModuleRun again.
				// copy task and reset RunOnStartupHooks if needed
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
				res.AfterTasks = []sh_task.Task{newTask}
			}
		}
	case task.ModuleDelete:
		taskLogEntry := logEntry.WithFields(utils.LabelsToLogFields(t.GetLogLabels()))
		taskLogEntry.Info("Module delete")
		// TODO wait while module's tasks in other queues are done.
		hm := task.HookMetadataAccessor(t)
		err := op.ModuleManager.DeleteModule(hm.ModuleName, t.GetLogLabels())
		if err != nil {
			op.MetricStorage.SendCounter("module_delete_errors", 1.0, map[string]string{"module": hm.ModuleName})
			taskLogEntry.Errorf("Module delete failed, requeue task to retry after delay. Failed count is %d. Error: %s", t.GetFailureCount()+1, err)
			res.Status = "Fail"
		} else {
			taskLogEntry.Infof("Module delete success")
			res.Status = "Success"
		}
	case task.ModuleHookRun:
		taskLogEntry := logEntry.WithFields(utils.LabelsToLogFields(t.GetLogLabels()))
		taskLogEntry.Info("Module hook start")
		hm := task.HookMetadataAccessor(t)

		// Pause resources monitor
		op.HelmResourcesManager.PauseMonitor(hm.ModuleName)

		err := op.ModuleManager.RunModuleHook(hm.HookName, hm.BindingType, hm.BindingContext, t.GetLogLabels())
		if err != nil {
			moduleHook := op.ModuleManager.GetModuleHook(hm.HookName)
			hookLabel := path.Base(moduleHook.Path)
			moduleLabel := moduleHook.Module.Name

			if hm.AllowFailure {
				op.MetricStorage.SendCounter("module_hook_allowed_errors", 1.0, map[string]string{"module": moduleLabel, "hook": hookLabel})
				taskLogEntry.Infof("Module hook failed, but allowed to fail. Error: %v", err)
				res.Status = "Success"
				op.HelmResourcesManager.ResumeMonitor(hm.ModuleName)
			} else {
				op.MetricStorage.SendCounter("module_hook_errors", 1.0, map[string]string{"module": moduleLabel, "hook": hookLabel})
				taskLogEntry.Errorf("Module hook failed, requeue task to retry after delay. Failed count is %d. Error: %s", t.GetFailureCount()+1, err)
				res.Status = "Fail"
			}
		} else {
			taskLogEntry.Infof("Module hook success")
			res.Status = "Success"
			op.HelmResourcesManager.ResumeMonitor(hm.ModuleName)
		}

	case task.ModulePurge:
		// Purge is for unknown modules, so error is just ignored.
		taskLogEntry := logEntry.WithFields(utils.LabelsToLogFields(t.GetLogLabels()))
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
		taskLogEntry := logEntry.WithFields(utils.LabelsToLogFields(t.GetLogLabels()))
		op.MetricStorage.SendCounter("modules_discover_errors", 1.0, map[string]string{})
		op.ModuleManager.Retry()
		taskLogEntry.Infof("Module manager retry is requested, now wait before run module discovery again")

		res.Status = "Success"
		res.DelayBeforeNextTask = queue.DelayOnFailedTask
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
		// TODO may be only afterHelmDelete hooks should be initialized?
		// Enable module hooks on startup to run afterHelmDelete hooks
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
			Binding: ContextBindingType[AfterAll],
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
			op.MetricStorage.SendCounter("live_ticks", 1.0, map[string]string{})
			time.Sleep(10 * time.Second)
		}
	}()

	go func() {
		for {
			// task queue length
			op.TaskQueues.Iterate(func(queue *queue.TaskQueue) {
				queueLen := float64(queue.Length())
				op.MetricStorage.SendGauge("tasks_queue_length", queueLen, map[string]string{"queue": queue.Name})
			})
			time.Sleep(5 * time.Second)
		}
	}()
}

func (op *AddonOperator) SetupDebugServerHandles() {
	op.DebugServer.Router.Get("/global/{type:(config|values)}.{format:(json|yaml)}", func(writer http.ResponseWriter, request *http.Request) {
		valType := chi.URLParam(request, "type")
		format := chi.URLParam(request, "format")

		var values utils.Values
		var err error
		switch valType {
		case "config":
			values = op.ModuleManager.GlobalConfigValues()
		case "values":
			values, err = op.ModuleManager.GlobalValues()
		}

		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			writer.Write([]byte(err.Error()))
			return
		}

		outBytes, err := values.AsBytes(format)
		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			writer.Write([]byte(err.Error()))
			return
		}
		writer.Write(outBytes)
	})

	op.DebugServer.Router.Get("/global/patches.json", func(writer http.ResponseWriter, request *http.Request) {
		jp := op.ModuleManager.GlobalValuesPatches()
		data, err := json.Marshal(jp)
		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			writer.Write([]byte(err.Error()))
			return
		}
		writer.Write(data)
	})

	op.DebugServer.Router.Get("/module/list.{format:(json|yaml|text)}", func(writer http.ResponseWriter, request *http.Request) {
		format := chi.URLParam(request, "format")

		fmt.Fprintf(writer, "Dump modules in %s format.\n", format)

		for _, mName := range op.ModuleManager.GetModuleNamesInOrder() {
			fmt.Fprintf(writer, "%s \n", mName)
		}

	})

	op.DebugServer.Router.Get("/module/{name}/{type:(config|values)}.{format:(json|yaml)}", func(writer http.ResponseWriter, request *http.Request) {
		modName := chi.URLParam(request, "name")
		valType := chi.URLParam(request, "type")
		format := chi.URLParam(request, "format")

		m := op.ModuleManager.GetModule(modName)
		if m == nil {
			writer.WriteHeader(http.StatusNotFound)
			writer.Write([]byte("Module not found"))
			return
		}

		var values utils.Values
		var err error
		switch valType {
		case "config":
			values = m.ConfigValues()
		case "values":
			values, err = m.Values()
		}

		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			writer.Write([]byte(err.Error()))
			return
		}

		outBytes, err := values.AsBytes(format)
		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			writer.Write([]byte(err.Error()))
			return
		}
		writer.Write(outBytes)
	})

	op.DebugServer.Router.Get("/module/{name}/patches.json", func(writer http.ResponseWriter, request *http.Request) {
		modName := chi.URLParam(request, "name")

		m := op.ModuleManager.GetModule(modName)
		if m == nil {
			writer.WriteHeader(http.StatusNotFound)
			writer.Write([]byte("Module not found"))
			return
		}

		jp := m.ValuesPatches()
		data, err := json.Marshal(jp)
		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			writer.Write([]byte(err.Error()))
			return
		}
		writer.Write(data)
	})

	op.DebugServer.Router.Get("/module/resource-monitor.{format:(json|yaml)}", func(writer http.ResponseWriter, request *http.Request) {
		format := chi.URLParam(request, "format")

		dump := map[string]interface{}{}

		for _, moduleName := range op.ModuleManager.GetModuleNamesInOrder() {
			if !op.HelmResourcesManager.HasMonitor(moduleName) {
				dump[moduleName] = "No monitor"
				continue
			}
			manifests := op.ModuleManager.GetModule(moduleName).LastReleaseManifests
			info := []string{}
			for _, m := range manifests {
				info = append(info, m.Id())
			}
			dump[moduleName] = info
		}

		var outBytes []byte
		var err error
		switch format {
		case "yaml":
			outBytes, err = yaml.Marshal(dump)
		case "json":
			outBytes, err = json.Marshal(dump)
		}
		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(writer, "Error: %s", err)
		}
		writer.Write(outBytes)
	})

}

func (op *AddonOperator) SetupHttpServerHandles() {
	http.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		writer.Write([]byte(`<html>
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
		helm.TillerHealthHandler(app.TillerProbeListenAddress, app.TillerProbeListenPort)(writer, request)
	})
}

func DefaultOperator() *AddonOperator {
	operator := NewAddonOperator()
	operator.WithContext(context.Background())
	return operator
}

func InitAndStart(operator *AddonOperator) error {
	operator.SetupHttpServerHandles()

	err := operator.StartHttpServer(sh_app.ListenAddress, sh_app.ListenPort)
	if err != nil {
		log.Errorf("HTTP SERVER start failed: %v", err)
		return err
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

func QueueHasModuleRunTask(q *queue.TaskQueue, moduleName string) bool {
	hasTask := false
	q.Filter(func(t sh_task.Task) bool {
		if t.GetType() == task.ModuleRun {
			hm := task.HookMetadataAccessor(t)
			if hm.ModuleName == moduleName {
				hasTask = true
			}
		}
		return true
	})
	return hasTask
}
