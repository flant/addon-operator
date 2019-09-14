package addon_operator

import (
	"fmt"
	"io"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/romana/rlog"

	schedule_hook "github.com/flant/shell-operator/pkg/hook/schedule"
	"github.com/flant/shell-operator/pkg/kube"
	"github.com/flant/shell-operator/pkg/kube_events_manager"
	"github.com/flant/shell-operator/pkg/metrics_storage"
	"github.com/flant/shell-operator/pkg/schedule_manager"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/kube_config_manager"
	"github.com/flant/addon-operator/pkg/module_manager"
	kube_event_hook "github.com/flant/addon-operator/pkg/module_manager/hook/kube_event"
	"github.com/flant/addon-operator/pkg/task"
)

var (
	ModulesDir     string
	GlobalHooksDir string
	TempDir        string

	TasksQueue *task.TasksQueue

	// ModuleManager is the module manager object, which monitors configuration
	// and variable changes.
	ModuleManager module_manager.ModuleManager

	ScheduleManager schedule_manager.ScheduleManager
	ScheduledHooks  schedule_hook.ScheduledHooksStorage

	KubeEventsManager kube_events_manager.KubeEventsManager
	KubeEventsHooks   kube_event_hook.KubeEventsHooksController

	MetricsStorage *metrics_storage.MetricStorage

	// ManagersEventsHandlerStopCh is the channel object for stopping infinite loop of the ManagersEventsHandler.
	ManagersEventsHandlerStopCh chan struct{}
)


// Defining delays in processing tasks from queue.
var (
	QueueIsEmptyDelay = 3 * time.Second
	FailedHookDelay   = 5 * time.Second
	FailedModuleDelay = 5 * time.Second
)

// Init gets all settings, initialize managers and create a working queue.
//
// Settings: directories for modules, global hooks, host name, dump file, tiller namespace.
//
// Initializing necessary objects: helm/tiller, registry manager, module manager,
// kube events manager.
//
// Creating an empty queue with jobs.
func Init() error {
	rlog.Debug("INIT: started")

	var err error

	cwd, err := os.Getwd()
	if err != nil {
		rlog.Errorf("INIT: Cannot get current working directory of process: %s", err)
		return err
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
	rlog.Infof("INIT: Modules: '%s', Global hooks: '%s'", ModulesDir, GlobalHooksDir)

	TempDir := app.TmpDir
	err = os.MkdirAll(TempDir, os.FileMode(0777))
	if err != nil {
		rlog.Errorf("INIT: Cannot create temporary dir '%s': %s", TempDir, err)
		return err
	}
	rlog.Infof("INIT: Temporary dir: %s", TempDir)

	// Initializing the connection to the k8s.
	err = kube.Init(kube.InitOptions{})
	if err != nil {
		rlog.Errorf("INIT: Cannot initialize Kubernetes client: %s", err)
		return err
	}

	err = helm.InitTillerSidecar()
	if err != nil {
		rlog.Errorf("INIT TILLER: cannot add sidecar: %s", err)
		return err
	}

	// Initializing helm client
	err = helm.InitClient()
	if err != nil {
		rlog.Errorf("INIT HELM: %s", err)
		return err
	}

	// Initializing module manager.

	kube_config_manager.ConfigMapName = app.ConfigMapName
	kube_config_manager.ValuesChecksumsAnnotation = app.ValuesChecksumsAnnotation
	module_manager.Init()
	ModuleManager = module_manager.NewMainModuleManager().
		WithDirectories(ModulesDir, GlobalHooksDir, TempDir)
	err = ModuleManager.Init()
	if err != nil {
		rlog.Errorf("INIT: Cannot initialize module manager: %s", err)
		return err
	}

	// Initializing the empty task queue.
	TasksQueue = task.NewTasksQueue()

	// Initializing the queue dumper, which writes queue changes to the dump file.
	rlog.Debugf("INIT: Tasks queue dump file: '%s'", app.TasksQueueDumpFilePath)
	queueWatcher := task.NewTasksQueueDumper(app.TasksQueueDumpFilePath, TasksQueue)
	TasksQueue.AddWatcher(queueWatcher)

	// Initializing the hooks schedule.
	ScheduleManager, err = schedule_manager.Init()
	if err != nil {
		rlog.Errorf("INIT: Cannot initialize schedule manager: %s", err)
		return err
	}

	KubeEventsManager, err = kube_events_manager.Init()
	if err != nil {
		rlog.Errorf("INIT: Cannot initialize kube events manager: %s", err)
		return err
	}
	KubeEventsHooks = kube_event_hook.NewMainKubeEventsHooksController()

	MetricsStorage = metrics_storage.Init()

	return nil
}

// Run runs all managers, event and queue handlers.
//
// The main process is blocked by the 'for-select' in the queue handler.
func Run() {
	// Loading the onStartup hooks into the queue and running all modules.
	// Turning tracking changes on only after startup ends.
	rlog.Info("MAIN: Start. Trigger onStartup event.")
	TasksQueue.ChangesDisable()

	CreateOnStartupTasks()
	CreateReloadAllTasks(true)

	_ = KubeEventsHooks.EnableGlobalHooks(ModuleManager, KubeEventsManager)

	TasksQueue.ChangesEnable(true)

	go ModuleManager.Run()
	go ScheduleManager.Run()

	// Metric add handler
	go MetricsStorage.Run()

	// Managers events handler adds task to the queue on every received event/
	go ManagersEventsHandler()

	// TasksRunner runs tasks from the queue.
	go TasksRunner()

	RunAddonOperatorMetrics()
}

func ManagersEventsHandler() {
	for {
		select {
		// Event from module manager (module restart or full restart).
		case moduleEvent := <-module_manager.EventCh:
			// Event from module manager can come if modules list have changed,
			// so event hooks need to be re-register with:
			// RegisterScheduledHooks()
			// RegisterKubeEventHooks()
			switch moduleEvent.Type {
			// Some modules have changed.
			case module_manager.ModulesChanged:
				for _, moduleChange := range moduleEvent.ModulesChanges {
					rlog.Infof("EVENT ModulesChanged, type=Changed")
					newTask := task.NewTask(task.ModuleRun, moduleChange.Name)
					TasksQueue.Add(newTask)
					rlog.Infof("QUEUE add ModuleRun %s", newTask.Name)
				}
				// As module list may have changed, hook schedule index must be re-created.
				ScheduledHooks = UpdateScheduleHooks(ScheduledHooks)
			case module_manager.GlobalChanged:
				// Global values are changed, all modules must be restarted.
				rlog.Infof("EVENT GlobalChanged")
				TasksQueue.ChangesDisable()
				CreateReloadAllTasks(false)
				TasksQueue.ChangesEnable(true)
				// Re-creating schedule hook index
				ScheduledHooks = UpdateScheduleHooks(ScheduledHooks)
			case module_manager.AmbigousState:
				rlog.Infof("EVENT AmbiguousState")
				TasksQueue.ChangesDisable()
				// It is the error in the module manager. The task must be added to
				// the beginning of the queue so the module manager can restore its
				// state before running other queue tasks
				newTask := task.NewTask(task.ModuleManagerRetry, "")
				TasksQueue.Push(newTask)
				// It is the delay before retry.
				TasksQueue.Push(task.NewTaskDelay(FailedModuleDelay))
				TasksQueue.ChangesEnable(true)
				rlog.Infof("QUEUE push ModuleManagerRetry, push FailedModuleDelay")
			}
		case crontab := <-schedule_manager.ScheduleCh:
			scheduleHooks := ScheduledHooks.GetHooksForSchedule(crontab)
			for _, hook := range scheduleHooks {
				var getHookErr error

				_, getHookErr = ModuleManager.GetGlobalHook(hook.Name)
				if getHookErr == nil {
					for _, scheduleConfig := range hook.Schedule {
						bindingName := scheduleConfig.Name
						if bindingName == "" {
							bindingName = module_manager.ContextBindingType[module_manager.Schedule]
						}
						newTask := task.NewTask(task.GlobalHookRun, hook.Name).
							WithBinding(module_manager.Schedule).
							AppendBindingContext(module_manager.BindingContext{Binding: bindingName}).
							WithAllowFailure(scheduleConfig.AllowFailure)
						TasksQueue.Add(newTask)
						rlog.Debugf("QUEUE add GlobalHookRun@Schedule '%s'", hook.Name)
					}
					continue
				}

				_, getHookErr = ModuleManager.GetModuleHook(hook.Name)
				if getHookErr == nil {
					for _, scheduleConfig := range hook.Schedule {
						bindingName := scheduleConfig.Name
						if bindingName == "" {
							bindingName = module_manager.ContextBindingType[module_manager.Schedule]
						}
						newTask := task.NewTask(task.ModuleHookRun, hook.Name).
							WithBinding(module_manager.Schedule).
							AppendBindingContext(module_manager.BindingContext{Binding: bindingName}).
							WithAllowFailure(scheduleConfig.AllowFailure)
						TasksQueue.Add(newTask)
						rlog.Debugf("QUEUE add ModuleHookRun@Schedule '%s'", hook.Name)
					}
					continue
				}

				rlog.Errorf("MAIN_LOOP hook '%s' scheduled but not found by module_manager", hook.Name)
			}
		case kubeEvent := <-kube_events_manager.KubeEventCh:
			rlog.Infof("EVENT Kube event '%s'", kubeEvent.ConfigId)

			res, err := KubeEventsHooks.HandleEvent(kubeEvent)
			if err != nil {
				rlog.Errorf("MAIN_LOOP error handling kube event '%s': %s", kubeEvent.ConfigId, err)
				break
			}

			for _, task := range res.Tasks {
				TasksQueue.Add(task)
				rlog.Infof("QUEUE add %s@%s %s", task.GetType(), task.GetBinding(), task.GetName())
			}
		case <-ManagersEventsHandlerStopCh:
			rlog.Infof("EVENT Stop")
			return
		}
	}
}

func runDiscoverModulesState(t task.Task) error {
	modulesState, err := ModuleManager.DiscoverModulesState()
	if err != nil {
		return err
	}

	for _, moduleName := range modulesState.EnabledModules {
		newTask := task.NewTask(task.ModuleRun, moduleName).
			WithOnStartupHooks(t.GetOnStartupHooks())

		TasksQueue.Add(newTask)
		rlog.Infof("QUEUE add ModuleRun %s", moduleName)
	}

	for _, moduleName := range modulesState.ModulesToDisable {
		newTask := task.NewTask(task.ModuleDelete, moduleName)
		TasksQueue.Add(newTask)
		rlog.Infof("QUEUE add ModuleDelete %s", moduleName)
	}

	for _, moduleName := range modulesState.ReleasedUnknownModules {
		newTask := task.NewTask(task.ModulePurge, moduleName)
		TasksQueue.Add(newTask)
		rlog.Infof("QUEUE add ModulePurge %s", moduleName)
	}

	// Queue afterAll global hooks
	afterAllHooks := ModuleManager.GetGlobalHooksInOrder(module_manager.AfterAll)
	for _, hookName := range afterAllHooks {
		newTask := task.NewTask(task.GlobalHookRun, hookName).
			WithBinding(module_manager.AfterAll).
			AppendBindingContext(module_manager.BindingContext{Binding: module_manager.ContextBindingType[module_manager.AfterAll]})
		TasksQueue.Add(newTask)
		rlog.Debugf("QUEUE add GlobalHookRun@AfterAll '%s'", hookName)
	}
	if len(afterAllHooks) > 0 {
		rlog.Infof("QUEUE add all GlobalHookRun@AfterAll")
	}

	ScheduledHooks = UpdateScheduleHooks(nil)

	// Enable kube events hooks for newly enabled modules
	// FIXME convert to a task that run after AfterHelm if there is a flag in binding config to start informers after CRD installation.
	for _, moduleName := range modulesState.EnabledModules {
		err = KubeEventsHooks.EnableModuleHooks(moduleName, ModuleManager, KubeEventsManager)
		if err != nil {
			return err
		}
	}

	// TODO is queue should be cleaned from hook run tasks of deleted module?
	// Disable kube events hooks for newly disabled modules
	for _, moduleName := range modulesState.ModulesToDisable {
		err = KubeEventsHooks.DisableModuleHooks(moduleName, ModuleManager, KubeEventsManager)
		if err != nil {
			return err
		}
	}

	return nil
}

// TasksRunner handle tasks in queue.
//
// Task handler may delay task processing by pushing delay to the queue.
// FIXME: For now, only one TaskRunner for a TasksQueue. There should be a lock between Peek and Pop to prevent Poping tasks from other TaskRunner
func TasksRunner() {
	for {
		if TasksQueue.IsEmpty() {
			time.Sleep(QueueIsEmptyDelay)
		}
		for {
			t, _ := TasksQueue.Peek()
			if t == nil {
				break
			}

			switch t.GetType() {
			case task.DiscoverModulesState:
				rlog.Infof("TASK_RUN DiscoverModulesState")
				err := runDiscoverModulesState(t)
				if err != nil {
					MetricsStorage.SendCounterMetric(PrefixMetric("modules_discover_errors"), 1.0, map[string]string{})
					t.IncrementFailureCount()
					rlog.Errorf("TASK_RUN %s failed. Will retry after delay. Failed count is %d. Error: %s", t.GetType(), t.GetFailureCount(), err)
					TasksQueue.Push(task.NewTaskDelay(FailedModuleDelay))
					rlog.Infof("QUEUE push FailedModuleDelay")
					break
				}

				TasksQueue.Pop()

			case task.ModuleRun:
				rlog.Infof("TASK_RUN ModuleRun %s", t.GetName())
				err := ModuleManager.RunModule(t.GetName(), t.GetOnStartupHooks())
				if err != nil {
					MetricsStorage.SendCounterMetric(PrefixMetric("module_run_errors"), 1.0, map[string]string{"module": t.GetName()})
					t.IncrementFailureCount()
					rlog.Errorf("TASK_RUN ModuleRun '%s' failed. Will retry after delay. Failed count is %d. Error: %s", t.GetName(), t.GetFailureCount(), err)
					TasksQueue.Push(task.NewTaskDelay(FailedModuleDelay))
					rlog.Infof("QUEUE push FailedModuleDelay")
				} else {
					TasksQueue.Pop()
				}
			case task.ModuleDelete:
				rlog.Infof("TASK_RUN ModuleDelete %s", t.GetName())
				err := ModuleManager.DeleteModule(t.GetName())
				if err != nil {
					MetricsStorage.SendCounterMetric(PrefixMetric("module_delete_errors"), 1.0, map[string]string{"module": t.GetName()})
					t.IncrementFailureCount()
					rlog.Errorf("%s '%s' failed. Will retry after delay. Failed count is %d. Error: %s", t.GetType(), t.GetName(), t.GetFailureCount(), err)
					TasksQueue.Push(task.NewTaskDelay(FailedModuleDelay))
					rlog.Infof("QUEUE push FailedModuleDelay")
				} else {
					TasksQueue.Pop()
				}
			case task.ModuleHookRun:
				rlog.Infof("TASK_RUN ModuleHookRun@%s %s", t.GetBinding(), t.GetName())
				err := ModuleManager.RunModuleHook(t.GetName(), t.GetBinding(), t.GetBindingContext())
				if err != nil {
					moduleHook, _ := ModuleManager.GetModuleHook(t.GetName())
					hookLabel := path.Base(moduleHook.Path)
					moduleLabel := moduleHook.Module.Name

					if t.GetAllowFailure() {
						MetricsStorage.SendCounterMetric(PrefixMetric("module_hook_allowed_errors"), 1.0, map[string]string{"module": moduleLabel, "hook": hookLabel})
						TasksQueue.Pop()
					} else {
						MetricsStorage.SendCounterMetric(PrefixMetric("module_hook_errors"), 1.0, map[string]string{"module": moduleLabel, "hook": hookLabel})
						t.IncrementFailureCount()
						rlog.Errorf("%s '%s' failed. Will retry after delay. Failed count is %d. Error: %s", t.GetType(), t.GetName(), t.GetFailureCount(), err)
						TasksQueue.Push(task.NewTaskDelay(FailedModuleDelay))
						rlog.Infof("QUEUE push FailedModuleDelay")
					}
				} else {
					TasksQueue.Pop()
				}
			case task.GlobalHookRun:
				rlog.Infof("TASK_RUN GlobalHookRun@%s %s", t.GetBinding(), t.GetName())
				err := ModuleManager.RunGlobalHook(t.GetName(), t.GetBinding(), t.GetBindingContext())
				if err != nil {
					globalHook, _ := ModuleManager.GetGlobalHook(t.GetName())
					hookLabel := path.Base(globalHook.Path)

					if t.GetAllowFailure() {
						MetricsStorage.SendCounterMetric(PrefixMetric("global_hook_allowed_errors"), 1.0, map[string]string{"hook": hookLabel})
						TasksQueue.Pop()
					} else {
						MetricsStorage.SendCounterMetric(PrefixMetric("global_hook_errors"), 1.0, map[string]string{"hook": hookLabel})
						t.IncrementFailureCount()
						rlog.Errorf("TASK_RUN %s '%s' on '%s' failed. Will retry after delay. Failed count is %d. Error: %s", t.GetType(), t.GetName(), t.GetBinding(), t.GetFailureCount(), err)
						TasksQueue.Push(task.NewTaskDelay(FailedHookDelay))
					}
				} else {
					TasksQueue.Pop()
				}
			case task.ModulePurge:
				rlog.Infof("TASK_RUN ModulePurge %s", t.GetName())
				// Module for purge is unknown so log deletion error is enough.
				err := helm.Client.DeleteRelease(t.GetName())
				if err != nil {
					rlog.Errorf("TASK_RUN %s Helm delete '%s' failed. Error: %s", t.GetType(), t.GetName(), err)
				}
				TasksQueue.Pop()
			case task.ModuleManagerRetry:
				rlog.Infof("TASK_RUN ModuleManagerRetry")
				MetricsStorage.SendCounterMetric(PrefixMetric("modules_discover_errors"), 1.0, map[string]string{})
				ModuleManager.Retry()
				TasksQueue.Pop()
				// Adding a delay before retrying module/hook task.
				TasksQueue.Push(task.NewTaskDelay(FailedModuleDelay))
				rlog.Infof("QUEUE push FailedModuleDelay")
			case task.Delay:
				rlog.Infof("TASK_RUN Delay for %s", t.GetDelay().String())
				TasksQueue.Pop()
				time.Sleep(t.GetDelay())
			case task.Stop:
				rlog.Infof("TASK_RUN Stop: Exiting TASK_RUN loop.")
				TasksQueue.Pop()
				return
			}

			// Breaking, if the task queue is empty to prevent the infinite loop.
			if TasksQueue.IsEmpty() {
				rlog.Debug("Task queue is empty. Will sleep now.")
				break
			}
		}
	}
}

// UpdateScheduleHooks creates the new ScheduledHooks.
// Calculates the difference between the old and the new schedule,
// removes what was in the old but is missing in the new schedule.
func UpdateScheduleHooks(storage schedule_hook.ScheduledHooksStorage) schedule_hook.ScheduledHooksStorage {
	if ScheduleManager == nil {
		return nil
	}

	oldCrontabs := map[string]bool{}
	if storage != nil {
		for _, crontab := range storage.GetCrontabs() {
			oldCrontabs[crontab] = false
		}
	}

	newScheduledTasks := schedule_hook.ScheduledHooksStorage{}

	globalHooks := ModuleManager.GetGlobalHooksInOrder(module_manager.Schedule)
LOOP_GLOBAL_HOOKS:
	for _, globalHookName := range globalHooks {
		globalHook, _ := ModuleManager.GetGlobalHook(globalHookName)
		for _, schedule := range globalHook.Config.Schedule {
			_, err := ScheduleManager.Add(schedule.Crontab)
			if err != nil {
				rlog.Errorf("Schedule: cannot add '%s' for global hook '%s': %s", schedule.Crontab, globalHookName, err)
				continue LOOP_GLOBAL_HOOKS
			}
			rlog.Debugf("Schedule: add '%s' for global hook '%s'", schedule.Crontab, globalHookName)
		}
		newScheduledTasks.AddHook(globalHook.Name, globalHook.Config.Schedule)
	}

	modules := ModuleManager.GetModuleNamesInOrder()
	for _, moduleName := range modules {
		moduleHooks, _ := ModuleManager.GetModuleHooksInOrder(moduleName, module_manager.Schedule)
	LOOP_MODULE_HOOKS:
		for _, moduleHookName := range moduleHooks {
			moduleHook, _ := ModuleManager.GetModuleHook(moduleHookName)
			for _, schedule := range moduleHook.Config.Schedule {
				_, err := ScheduleManager.Add(schedule.Crontab)
				if err != nil {
					rlog.Errorf("Schedule: cannot add '%s' for hook '%s': %s", schedule.Crontab, moduleHookName, err)
					continue LOOP_MODULE_HOOKS
				}
				rlog.Debugf("Schedule: add '%s' for hook '%s'", schedule.Crontab, moduleHookName)
			}
			newScheduledTasks.AddHook(moduleHook.Name, moduleHook.Config.Schedule)
		}
	}

	if len(oldCrontabs) > 0 {
		// Creates a new set of schedules. If the schedule is in oldCrontabs, then sets it to true.
		newCrontabs := newScheduledTasks.GetCrontabs()
		for _, crontab := range newCrontabs {
			if _, has_crontab := oldCrontabs[crontab]; has_crontab {
				oldCrontabs[crontab] = true
			}
		}

		// Goes through the old set of schedules and removes from processing schedules with false.
		for crontab, _ := range oldCrontabs {
			if !oldCrontabs[crontab] {
				ScheduleManager.Remove(crontab)
			}
		}
	}

	return newScheduledTasks
}

func CreateOnStartupTasks() {
	rlog.Infof("QUEUE add all GlobalHookRun@OnStartup")

	onStartupHooks := ModuleManager.GetGlobalHooksInOrder(module_manager.OnStartup)

	for _, hookName := range onStartupHooks {
		newTask := task.NewTask(task.GlobalHookRun, hookName).
			WithBinding(module_manager.OnStartup).
			AppendBindingContext(module_manager.BindingContext{Binding: module_manager.ContextBindingType[module_manager.OnStartup]})
		TasksQueue.Add(newTask)
		rlog.Debugf("QUEUE add GlobalHookRun@OnStartup '%s'", hookName)
	}

	return
}

func CreateReloadAllTasks(onStartup bool) {
	rlog.Infof("QUEUE add all GlobalHookRun@BeforeAll, add DiscoverModulesState")

	// Queue beforeAll global hooks.
	beforeAllHooks := ModuleManager.GetGlobalHooksInOrder(module_manager.BeforeAll)

	for _, hookName := range beforeAllHooks {
		newTask := task.NewTask(task.GlobalHookRun, hookName).
			WithBinding(module_manager.BeforeAll).
			AppendBindingContext(module_manager.BindingContext{Binding: module_manager.ContextBindingType[module_manager.BeforeAll]})

		TasksQueue.Add(newTask)
		rlog.Debugf("QUEUE GlobalHookRun@BeforeAll '%s'", module_manager.BeforeAll, hookName)
	}

	TasksQueue.Add(task.NewTask(task.DiscoverModulesState, "").WithOnStartupHooks(onStartup))
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

func InitHttpServer(listenAddr string, listenPort string) {
	http.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		writer.Write([]byte(`<html>
    <head><title>Addon-operator</title></head>
    <body>
    <h1>Addon-operator</h1>
    <pre>go tool pprof goprofex http://ADDON_OPERATOR_IP:9115/debug/pprof/profile</pre>
    </body>
    </html>`))
	})
	http.Handle("/metrics", promhttp.Handler())

	http.HandleFunc("/queue", func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(writer, TasksQueue.DumpReader())
	})

	go func() {
		address := fmt.Sprintf("%s:%s", listenAddr, listenPort)
		rlog.Infof("HTTP SERVER Listening on %s", address)
		if err := http.ListenAndServe(address, nil); err != nil {
			rlog.Errorf("Error starting HTTP server: %s", err)
		}
	}()
}

func PrefixMetric(metric string) string {
	return fmt.Sprintf("%s%s", app.PrometheusMetricsPrefix, metric)
}
