package addon_operator

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path"
	"time"

	"github.com/flant/addon-operator/pkg/utils"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"gopkg.in/satori/go.uuid.v1"

	"github.com/flant/shell-operator/pkg/hook"
	"github.com/flant/shell-operator/pkg/kube"
	"github.com/flant/shell-operator/pkg/kube_events_manager"
	"github.com/flant/shell-operator/pkg/metrics_storage"
	"github.com/flant/shell-operator/pkg/schedule_manager"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/kube_config_manager"
	"github.com/flant/addon-operator/pkg/module_manager"
	kube_event_hook "github.com/flant/addon-operator/pkg/module_manager/hook/kube_event"
	schedule_hook "github.com/flant/addon-operator/pkg/module_manager/hook/schedule"
	"github.com/flant/addon-operator/pkg/task"
)

var (
	ModulesDir     string
	GlobalHooksDir string
	TempDir        string

	TasksQueue *task.TasksQueue

	KubeConfigManager kube_config_manager.KubeConfigManager

	// ModuleManager is the module manager object, which monitors configuration
	// and variable changes.
	ModuleManager module_manager.ModuleManager

	ScheduleManager         schedule_manager.ScheduleManager
	ScheduleHooksController schedule_hook.ScheduleHooksController

	KubeEventsManager kube_events_manager.KubeEventsManager
	KubernetesHooksController   kube_event_hook.KubernetesHooksController

	MetricsStorage *metrics_storage.MetricStorage

	// ManagersEventsHandlerStopCh is the channel object for stopping infinite loop of the ManagersEventsHandler.
	ManagersEventsHandlerStopCh chan struct{}

	BeforeHelmInitCb func()
)


// Defining delays in processing tasks from queue.
var (
	QueueIsEmptyDelay = 3 * time.Second
	FailedHookDelay   = 5 * time.Second
	FailedModuleDelay = 5 * time.Second
)

// Init gets all settings, initialize managers and create a working queue.
//
// Settings: directories for modules and global hooks, dump file path
//
// Initializing necessary objects: helm client, registry manager, module manager,
// kube config manager, kube events manager, schedule manager.
//
// Creating an empty queue with jobs.
func Init() error {
	logLabels := map[string]string {
		"operator.component": "Init",
	}
	logEntry := log.WithFields(utils.LabelsToLogFields(logLabels))

	var err error

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory of process: %s", err)
	}

	// TODO: check if directories are existed
	ModulesDir = os.Getenv("MODULES_DIR")
	if ModulesDir == "" {
		ModulesDir = path.Join(cwd, app.ModulesDir)
	}
	GlobalHooksDir = os.Getenv("GLOBAL_HOOKS_DIR")
	if GlobalHooksDir == "" {
		GlobalHooksDir = path.Join(cwd, app.GlobalHooksDir)
	}
	logEntry.Infof("Global hooks directory: %s", GlobalHooksDir)
	logEntry.Infof("Modules directory: %s", ModulesDir)

	TempDir := app.TmpDir
	err = os.MkdirAll(TempDir, os.FileMode(0777))
	if err != nil {
		return fmt.Errorf("create temp directory '%s': %s", TempDir, err)
	}
	logEntry.Infof("Temp directory: %s", TempDir)


	// init and start metrics gathering loop
	MetricsStorage = metrics_storage.Init()
	go MetricsStorage.Run()


	// Initializing the empty task queue and queue dumper
	TasksQueue = task.NewTasksQueue()
	// Initializing the queue dumper, which writes queue changes to the dump file.
	logEntry.Infof("Queue dump file path: %s", app.TasksQueueDumpFilePath)
	queueWatcher := task.NewTasksQueueDumper(app.TasksQueueDumpFilePath, TasksQueue)
	TasksQueue.AddWatcher(queueWatcher)


	// Initializing the connection to the k8s.
	err = kube.Init(kube.InitOptions{})
	if err != nil {
		return fmt.Errorf("init Kubernetes client: %s", err)
	}


	// A useful callback when addon-operator is used as library
	if BeforeHelmInitCb != nil {
		logEntry.Debugf("run BeforeHelmInitCallback")
		BeforeHelmInitCb()
	}

	err = helm.InitTillerProcess(helm.TillerOptions{
		Namespace: app.Namespace,
		HistoryMax: app.TillerMaxHistory,
		ListenAddress: app.TillerListenAddress,
		ListenPort: app.TillerListenPort,
		ProbeListenAddress: app.TillerProbeListenAddress,
		ProbeListenPort: app.TillerProbeListenPort,
	})
	if err != nil {
		return fmt.Errorf("init tiller: %s", err)
	}

	// Initializing helm client
	err = helm.InitClient()
	if err != nil {
		return fmt.Errorf("init helm client: %s", err)
	}


	// Initializing module manager.
	KubeConfigManager = kube_config_manager.NewKubeConfigManager()
	KubeConfigManager.WithNamespace(app.Namespace)
	KubeConfigManager.WithConfigMapName(app.ConfigMapName)
	KubeConfigManager.WithValuesChecksumsAnnotation(app.ValuesChecksumsAnnotation)

	err = KubeConfigManager.Init()
	if err != nil {
		return fmt.Errorf("init kube config manager: %s", err)
	}

	module_manager.Init()
	ModuleManager = module_manager.NewMainModuleManager()
	ModuleManager.WithDirectories(ModulesDir, GlobalHooksDir, TempDir)
	ModuleManager.WithKubeConfigManager(KubeConfigManager)
	err = ModuleManager.Init()
	if err != nil {
		return fmt.Errorf("init module manager: %s", err)
	}


	// Initializing the hooks schedule.
	ScheduleManager, err = schedule_manager.Init()
	if err != nil {
		return fmt.Errorf("init schedule manager: %s", err)
	}

	ScheduleHooksController = schedule_hook.NewScheduleHooksController()
	ScheduleHooksController.WithModuleManager(ModuleManager)
	ScheduleHooksController.WithScheduleManager(ScheduleManager)

	KubeEventsManager = kube_events_manager.NewKubeEventsManager()
	KubeEventsManager.WithContext(context.Background())

	KubernetesHooksController = kube_event_hook.NewKubernetesHooksController()
	KubernetesHooksController.WithModuleManager(ModuleManager)
	KubernetesHooksController.WithKubeEventsManager(KubeEventsManager)

	return nil
}

// Run runs all managers, event and queue handlers.
//
// The main process is blocked by the 'for-select' in the queue handler.
func Run() {
	// Loading the onStartup hooks into the queue and running all modules.
	// Turning tracking changes on only after startup ends.
	onStartupLabels := map[string]string {}
	onStartupLabels["event.id"] = "OperatorOnStartup"

	TasksQueue.ChangesDisable()
	CreateOnStartupTasks(onStartupLabels)
	CreateEnableGlobalKubernetesHookTasks(onStartupLabels)
	CreateReloadAllTasks(true, onStartupLabels)
	TasksQueue.ChangesEnable(true)

	go ModuleManager.Run()
	go ScheduleManager.Run()

	// Managers events handler adds task to the queue on every received event/
	go ManagersEventsHandler()

	// TasksRunner runs tasks from the queue.
	go TasksRunner()

	// Health metrics
	RunAddonOperatorMetrics()
}

func ManagersEventsHandler() {
	for {
		select {
		// Event from module manager (module restart or full restart).
		case moduleEvent := <-module_manager.EventCh:
			logLabels := map[string]string{
				"event.id": uuid.NewV4().String(),
			}
			eventLogEntry := log.WithField("operator.component", "handleManagerEvents")
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
					logEntry.WithField("module", moduleChange.Name).Infof("module values are changed, queue ModuleRun task", moduleChange.Name)
					newTask := task.NewTask(task.ModuleRun, moduleChange.Name).
						WithLogLabels(logLabels)
					TasksQueue.Add(newTask)
				}
				// As module list may have changed, hook schedule index must be re-created.
				ScheduleHooksController.UpdateScheduleHooks()
			case module_manager.GlobalChanged:
				logLabels["event.type"] = "GlobalChanged"
				logEntry := eventLogEntry.WithFields(utils.LabelsToLogFields(logLabels))
				// Global values are changed, all modules must be restarted.
				logEntry.Infof("global values are changed, queue ReloadAll tasks")
				TasksQueue.ChangesDisable()
				CreateReloadAllTasks(false, logLabels)
				TasksQueue.ChangesEnable(true)
				// As module list may have changed, hook schedule index must be re-created.
				ScheduleHooksController.UpdateScheduleHooks()
			case module_manager.AmbigousState:
				// It is the error in the module manager. The task must be added to
				// the beginning of the queue so the module manager can restore its
				// state before running other queue tasks
				logLabels["event.type"] = "AmbigousState"
				logEntry := eventLogEntry.WithFields(utils.LabelsToLogFields(logLabels))
				logEntry.Infof("module manager is in ambiguous state, queue ModuleManagerRetry task with delay")
				TasksQueue.ChangesDisable()
				newTask := task.NewTask(task.ModuleManagerRetry, "").
					WithLogLabels(logLabels)
				TasksQueue.Push(newTask)
				// It is the delay before retry.
				TasksQueue.Push(task.NewTaskDelay(FailedModuleDelay).WithLogLabels(logLabels))
				TasksQueue.ChangesEnable(true)
			}

		case crontab := <-schedule_manager.ScheduleCh:
			logLabels := map[string]string{
				"event.id": uuid.NewV4().String(),
				"binding": module_manager.ContextBindingType[module_manager.Schedule],
			}
			logEntry := log.WithFields(utils.LabelsToLogFields(logLabels))
			logEntry.Infof("Event from '%s'", crontab)

			tasks, err := ScheduleHooksController.HandleEvent(crontab, logLabels)
			if err != nil {
				logEntry.Errorf("handle schedule event '%s': %s", crontab, err)
				break
			}

			for _, t := range tasks {
				logEntry.
					WithFields(utils.LabelsToLogFields(t.GetLogLabels())).
					Infof("queue %s task", t.GetType())
				TasksQueue.Add(t)
			}

		case kubeEvent := <-kube_events_manager.KubeEventCh:
			logLabels := map[string]string{
				"event.id": uuid.NewV4().String(),
				"binding": module_manager.ContextBindingType[module_manager.KubeEvents],
			}
			logEntry := log.WithFields(utils.LabelsToLogFields(logLabels))
			if kubeEvent.Type == "Event" {
				logEntry.Infof("%s event for %s/%s/%s", kubeEvent.Event, kubeEvent.Namespace, kubeEvent.Kind, kubeEvent.Name)
			} else {
				logEntry.Infof("Synchronization")
			}

			tasks, err := KubernetesHooksController.HandleEvent(kubeEvent, logLabels)
			if err != nil {
				logEntry.Errorf("handle kubernetes event '%s': %s", kubeEvent.ConfigId, err)
				break
			}

			for _, t := range tasks {
				logEntry.
					WithFields(utils.LabelsToLogFields(t.GetLogLabels())).
					Infof("queue %s task", t.GetType())
				TasksQueue.Add(t)
			}

		case <-ManagersEventsHandlerStopCh:
			log.Infof("Stop from ManagersEventsHandlerStopCh")
			return
		}
	}
}


// TasksRunner handle tasks in queue.
//
// Task handler may delay task processing by pushing delay to the queue.
// FIXME: For now, only one TaskRunner for a TasksQueue. There should be a lock between Peek and Pop to prevent Poping tasks by other TaskRunner for multiple queues.
func TasksRunner() {
	logEntry := log.WithField("operator.component", "TaskRunner")
	for {
		if TasksQueue.IsEmpty() {
			time.Sleep(QueueIsEmptyDelay)
		}
		for {
			t, _ := TasksQueue.Peek()
			if t == nil {
				break
			}

			taskLogEntry := logEntry.WithFields(utils.LabelsToLogFields(t.GetLogLabels())).
				WithField("task.type", t.GetType())

			switch t.GetType() {
			case task.GlobalHookRun:
				taskLogEntry.Infof("Run global hook")
				err := ModuleManager.RunGlobalHook(t.GetName(), t.GetBinding(), t.GetBindingContext(), t.GetLogLabels())
				if err != nil {
					globalHook, _ := ModuleManager.GetGlobalHook(t.GetName())
					hookLabel := path.Base(globalHook.Path)

					if t.GetAllowFailure() {
						MetricsStorage.SendCounterMetric(PrefixMetric("global_hook_allowed_errors"), 1.0, map[string]string{"hook": hookLabel})
						taskLogEntry.Infof("GlobalHookRun failed, but allowed to fail. Error: %v", err)
						TasksQueue.Pop()
					} else {
						MetricsStorage.SendCounterMetric(PrefixMetric("global_hook_errors"), 1.0, map[string]string{"hook": hookLabel})
						t.IncrementFailureCount()
						taskLogEntry.Errorf("GlobalHookRun failed, queue Delay task to retry. Failed count is %d. Error: %s", t.GetFailureCount(), err)
						TasksQueue.Push(task.NewTaskDelay(FailedHookDelay))
					}
				} else {
					taskLogEntry.Infof("GlobalHookRun success")
					TasksQueue.Pop()
				}

			case task.GlobalKubernetesBindingsStart:
				taskLogEntry.Infof("Enable global hook with kubernetes binding")

				hookRunTasks, err := KubernetesHooksController.EnableGlobalKubernetesBindings(t.GetName(), t.GetLogLabels())
				if err != nil {
					globalHook, _ := ModuleManager.GetGlobalHook(t.GetName())
					hookLabel := path.Base(globalHook.Path)

					MetricsStorage.SendCounterMetric(PrefixMetric("global_hook_errors"), 1.0, map[string]string{"hook": hookLabel})
					t.IncrementFailureCount()
					taskLogEntry.Errorf("GlobalHookRun failed, queue Delay task to retry. Failed count is %d. Error: %s", t.GetFailureCount(), err)

					delayTask := task.NewTaskDelay(FailedHookDelay)
					delayTask.Name = t.GetName()
					delayTask.Binding = t.GetBinding()
					TasksQueue.Push(delayTask)
				} else {
					// Push Synchronization tasks to queue head. Informers can be started now — their events will
					// be added to the queue tail.
					taskLogEntry.Infof("Kubernetes binding for hook enabled successfully")
					TasksQueue.Pop()
					for _, hookRunTask := range hookRunTasks {
						TasksQueue.Push(hookRunTask)
					}
					KubernetesHooksController.StartGlobalInformers(t.GetName())
				}

			case task.DiscoverModulesState:
				taskLogEntry.Info("Run DiscoverModules")
				err := RunDiscoverModulesState(t, t.GetLogLabels())
				if err != nil {
					MetricsStorage.SendCounterMetric(PrefixMetric("modules_discover_errors"), 1.0, map[string]string{})
					t.IncrementFailureCount()
					taskLogEntry.Errorf("DiscoverModulesState failed, queue Delay task to retry. Failed count is %d. Error: %s", t.GetFailureCount(), err)
					TasksQueue.Push(task.NewTaskDelay(FailedModuleDelay))
				} else {
					taskLogEntry.Infof("DiscoverModulesState success")
					TasksQueue.Pop()
				}
			case task.ModuleRun:
				taskLogEntry.Info("Run module")
				err := ModuleManager.RunModule(t.GetName(), t.GetOnStartupHooks(), t.GetLogLabels(), func() error {
					tasks, err := KubernetesHooksController.EnableModuleHooks(t.GetName(), t.GetLogLabels())
					if err != nil {
						return err
					}
					for _, t := range tasks {
						hookLogEntry := taskLogEntry.WithFields(utils.LabelsToLogFields(t.GetLogLabels()))
						hookLogEntry.Info("Run module hook with type Sychronization")
						err := ModuleManager.RunModuleHook(t.GetName(), t.GetBinding(), t.GetBindingContext(), t.GetLogLabels())
						if err != nil {
							moduleHook, _ := ModuleManager.GetModuleHook(t.GetName())
							hookLabel := path.Base(moduleHook.Path)
							moduleLabel := moduleHook.Module.Name
							MetricsStorage.SendCounterMetric(PrefixMetric("module_hook_errors"), 1.0, map[string]string{"module": moduleLabel, "hook": hookLabel})
							return err
						} else {
							hookLogEntry.Infof("ModuleHookRun success")
						}
					}
					KubernetesHooksController.StartModuleInformers(t.GetName())
					return nil
				})
				if err != nil {
					MetricsStorage.SendCounterMetric(PrefixMetric("module_run_errors"), 1.0, map[string]string{"module": t.GetName()})
					t.IncrementFailureCount()
					taskLogEntry.Errorf("ModuleRun failed, queue Delay task to retry. Failed count is %d. Error: %s", t.GetFailureCount(), err)
					TasksQueue.Push(task.NewTaskDelay(FailedModuleDelay))
				} else {
					taskLogEntry.Infof("ModuleRun success")
					TasksQueue.Pop()
				}
			case task.ModuleDelete:
				taskLogEntry.Info("Delete module")
				err := ModuleManager.DeleteModule(t.GetName(), t.GetLogLabels())
				if err != nil {
					MetricsStorage.SendCounterMetric(PrefixMetric("module_delete_errors"), 1.0, map[string]string{"module": t.GetName()})
					t.IncrementFailureCount()
					taskLogEntry.Errorf("ModuleDelete failed, queue Delay task to retry. Failed count is %d. Error: %s", t.GetFailureCount(), err)
					TasksQueue.Push(task.NewTaskDelay(FailedModuleDelay))
				} else {
					taskLogEntry.Infof("ModuleDelete success")
					TasksQueue.Pop()
				}
			case task.ModuleHookRun:
				taskLogEntry.Info("Run module hook")
				err := ModuleManager.RunModuleHook(t.GetName(), t.GetBinding(), t.GetBindingContext(), t.GetLogLabels())
				if err != nil {
					moduleHook, _ := ModuleManager.GetModuleHook(t.GetName())
					hookLabel := path.Base(moduleHook.Path)
					moduleLabel := moduleHook.Module.Name

					if t.GetAllowFailure() {
						MetricsStorage.SendCounterMetric(PrefixMetric("module_hook_allowed_errors"), 1.0, map[string]string{"module": moduleLabel, "hook": hookLabel})
						taskLogEntry.Infof("ModuleHookRun failed, but allowed to fail. Error: %v", err)
						TasksQueue.Pop()
					} else {
						MetricsStorage.SendCounterMetric(PrefixMetric("module_hook_errors"), 1.0, map[string]string{"module": moduleLabel, "hook": hookLabel})
						t.IncrementFailureCount()
						taskLogEntry.Errorf("ModuleHookRun failed, queue Delay task to retry. Failed count is %d. Error: %s", t.GetFailureCount(), err)
						TasksQueue.Push(task.NewTaskDelay(FailedModuleDelay))
					}
				} else {
					taskLogEntry.Infof("ModuleHookRun success")
					TasksQueue.Pop()
				}
			case task.ModulePurge:
				// Purge is for unknown modules, so error is just ignored.
				taskLogEntry.Infof("Run module purge")
				err := helm.NewHelmCli(taskLogEntry).DeleteRelease(t.GetName())
				if err != nil {
					taskLogEntry.Errorf("ModulePurge failed, no retry. Error: %s", err)
				} else {
					taskLogEntry.Infof("ModulePurge success")
				}
				TasksQueue.Pop()
			case task.ModuleManagerRetry:
				MetricsStorage.SendCounterMetric(PrefixMetric("modules_discover_errors"), 1.0, map[string]string{})
				ModuleManager.Retry()
				TasksQueue.Pop()
				newTask := task.NewTaskDelay(FailedModuleDelay).WithLogLabels(t.GetLogLabels())
				taskLogEntry.Infof("Queue Delay task immediately to wait for success module discovery")
				TasksQueue.Push(newTask)
			case task.Delay:
				taskLogEntry.Debugf("Sleep for %s", t.GetDelay().String())
				TasksQueue.Pop()
				time.Sleep(t.GetDelay())
			case task.Stop:
				logEntry.Infof("Stop")
				TasksQueue.Pop()
				return
			}

			// Breaking, if the task queue is empty to prevent the infinite loop.
			if TasksQueue.IsEmpty() {
				logEntry.Debugf("Task queue is empty.")
				break
			}
		}
	}
}

func CreateOnStartupTasks(logLabels map[string]string) {
	logEntry := log.WithFields(utils.LabelsToLogFields(logLabels))

	onStartupHooks := ModuleManager.GetGlobalHooksInOrder(module_manager.OnStartup)

	for _, hookName := range onStartupHooks {
		hookLogLabels := utils.MergeLabels(logLabels)
		hookLogLabels["hook"] = hookName
		hookLogLabels["hook.type"] = "global"
		hookLogLabels["binding"] = module_manager.ContextBindingType[module_manager.OnStartup]

		logEntry.WithFields(utils.LabelsToLogFields(hookLogLabels)).
			Infof("queue GlobalHookRun task")
		newTask := task.NewTask(task.GlobalHookRun, hookName).
			WithBinding(module_manager.OnStartup).
			WithLogLabels(hookLogLabels).
			AppendBindingContext(module_manager.BindingContext{BindingContext: hook.BindingContext{Binding: module_manager.ContextBindingType[module_manager.OnStartup]}})
		TasksQueue.Add(newTask)
	}

	return
}

func CreateEnableGlobalKubernetesHookTasks(logLabels map[string]string) {
	logEntry := log.WithFields(utils.LabelsToLogFields(logLabels))

	// create tasks to enable all global hooks with kubernetes bindings
	enableBindingsTasks, err := KubernetesHooksController.EnableGlobalHooks(logLabels)
	if err != nil {
		// Something wrong with hook configs, cannot start informers.
		logEntry.Errorf("enable kubernetes hooks: %v", err)
		return
	}
	for _, resTask := range enableBindingsTasks {
		TasksQueue.Add(resTask)
		logEntry.Infof("queue GlobalKubernetesBindingsStart task %s@%s %s", resTask.GetType(), resTask.GetBinding(), resTask.GetName())
	}

	return
}

func CreateReloadAllTasks(onStartup bool, logLabels map[string]string) {
	logEntry := log.WithFields(utils.LabelsToLogFields(logLabels))

	// Queue beforeAll global hooks.
	beforeAllHooks := ModuleManager.GetGlobalHooksInOrder(module_manager.BeforeAll)

	for _, hookName := range beforeAllHooks {
		hookLogLabels := utils.MergeLabels(logLabels)
		hookLogLabels["hook"] = hookName
		hookLogLabels["hook.type"] = "global"
		hookLogLabels["binding"] = module_manager.ContextBindingType[module_manager.BeforeAll]

		logEntry.WithFields(utils.LabelsToLogFields(hookLogLabels)).
			Infof("queue GlobalHookRun task")
		newTask := task.NewTask(task.GlobalHookRun, hookName).
			WithBinding(module_manager.BeforeAll).
			WithLogLabels(hookLogLabels).
			AppendBindingContext(module_manager.BindingContext{BindingContext: hook.BindingContext{Binding: module_manager.ContextBindingType[module_manager.BeforeAll]}})
		TasksQueue.Add(newTask)
	}

	logEntry.Infof("queue DiscoverModulesState task")
	discoverTask := task.NewTask(task.DiscoverModulesState, "").
		WithOnStartupHooks(onStartup).
		WithLogLabels(logLabels)
	TasksQueue.Add(discoverTask)
}

func RunDiscoverModulesState(discoverTask task.Task, logLabels map[string]string) error {
	logEntry := log.WithFields(utils.LabelsToLogFields(logLabels))
	modulesState, err := ModuleManager.DiscoverModulesState(logLabels)
	if err != nil {
		return err
	}

	// queue ModuleRun tasks for enabled modules
	for _, moduleName := range modulesState.EnabledModules {
		moduleLogEntry := logEntry.WithField("module", moduleName)
		moduleLogLabels := utils.MergeLabels(logLabels)
		moduleLogLabels["module"] = moduleName

		// Run OnStartup hooks on application startup or if module become enabled
		runOnStartupHooks := discoverTask.GetOnStartupHooks()
		if !runOnStartupHooks {
			for _, name := range modulesState.NewlyEnabledModules {
				if name == moduleName {
					runOnStartupHooks = true
					break
				}
			}
		}

		newTask := task.NewTask(task.ModuleRun, moduleName).
			WithLogLabels(moduleLogLabels).
			WithOnStartupHooks(runOnStartupHooks)

		moduleLogEntry.Infof("queue ModuleRun task for %s", moduleName)
		TasksQueue.Add(newTask)
	}

	// queue ModuleDelete tasks for disabled modules
	for _, moduleName := range modulesState.ModulesToDisable {
		moduleLogEntry := logEntry.WithField("module", moduleName)
		modLogLabels := utils.MergeLabels(logLabels)
		modLogLabels["module"] = moduleName
		// TODO may be only afterHelmDelete hooks should be initialized?
		// Enable module hooks on startup to run afterHelmDelete hooks
		if discoverTask.GetOnStartupHooks() {
			// error can be ignored, DiscoverModulesState should return existed modules
			disabledModule, _ := ModuleManager.GetModule(moduleName)
			if err = ModuleManager.RegisterModuleHooks(disabledModule, modLogLabels); err != nil {
				return err
			}
		}
		moduleLogEntry.Infof("queue ModuleDelete task for %s", moduleName)
		newTask := task.NewTask(task.ModuleDelete, moduleName).
			WithLogLabels(modLogLabels)
		TasksQueue.Add(newTask)
	}

	// queue ModulePurge tasks for unknown modules
	for _, moduleName := range modulesState.ReleasedUnknownModules {
		moduleLogEntry := logEntry.WithField("module", moduleName)
		newTask := task.NewTask(task.ModulePurge, moduleName).
			WithLogLabels(logLabels)
		TasksQueue.Add(newTask)
		moduleLogEntry.Infof("queue ModulePurge task")
	}

	// Queue afterAll global hooks
	afterAllHooks := ModuleManager.GetGlobalHooksInOrder(module_manager.AfterAll)
	for _, hookName := range afterAllHooks {
		hookLogLabels := utils.MergeLabels(logLabels)
		hookLogLabels["hook"] = hookName
		hookLogLabels["hook.type"] = "global"

		logEntry.WithFields(utils.LabelsToLogFields(hookLogLabels)).
			Infof("queue GlobalHookRun task")
		newTask := task.NewTask(task.GlobalHookRun, hookName).
			WithBinding(module_manager.AfterAll).
			WithLogLabels(hookLogLabels).
			AppendBindingContext(module_manager.BindingContext{BindingContext: hook.BindingContext{Binding: module_manager.ContextBindingType[module_manager.AfterAll]}})
		TasksQueue.Add(newTask)
	}

	ScheduleHooksController.UpdateScheduleHooks()

	// TODO queue should be cleaned from hook run tasks of deleted module!
	// Disable kube events hooks for newly disabled modules
	for _, moduleName := range modulesState.ModulesToDisable {
		modLogLabels := utils.MergeLabels(logLabels)
		modLogLabels["module"] = moduleName
		err = KubernetesHooksController.DisableModuleHooks(moduleName, modLogLabels)
		if err != nil {
			return err
		}
	}

	return nil
}


func RunAddonOperatorMetrics() {
	// Addon-operator live ticks.
	go func() {
		for {
			MetricsStorage.SendCounterMetric(PrefixMetric("live_ticks"), 1.0, map[string]string{})
			time.Sleep(10 * time.Second)
		}
	}()

	go func() {
		for {
			queueLen := float64(TasksQueue.Length())
			MetricsStorage.SendGaugeMetric(PrefixMetric("tasks_queue_length"), queueLen, map[string]string{})
			time.Sleep(5 * time.Second)
		}
	}()
}

func InitHttpServer(listenAddr string, listenPort string) error {
	http.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		writer.Write([]byte(`<html>
    <head><title>Addon-operator</title></head>
    <body>
    <h1>Addon-operator</h1>
    <pre>go tool pprof goprofex http://ADDON_OPERATOR_IP:9115/debug/pprof/profile</pre>
    <p>
      <a href="/metrics">prometheus metrics</a>
      <a href="/healthz">health url</a>
      <a href="/queue">queue stats</a>
    </p>
    </body>
    </html>`))
	})
	http.Handle("/metrics", promhttp.Handler())

	http.HandleFunc("/healthz", func(writer http.ResponseWriter, request *http.Request) {
		helm.TillerHealthHandler(app.TillerProbeListenAddress, app.TillerProbeListenPort)(writer, request)
	})

	http.HandleFunc("/queue", func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(writer, TasksQueue.DumpReader())
	})

	address := fmt.Sprintf("%s:%s", listenAddr, listenPort)
	log.Infof("Listen on %s", address)

	// Check if port is available
	ln, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}

	go func() {
		if err := http.Serve(ln, nil); err != nil {
			log.Errorf("Error starting HTTP server: %s", err)
			os.Exit(1)
		}
	}()

	return nil
}

func PrefixMetric(metric string) string {
	return fmt.Sprintf("%s%s", app.PrometheusMetricsPrefix, metric)
}
