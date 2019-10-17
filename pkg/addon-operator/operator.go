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

	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"

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
	log.Debug("INIT: started")

	var err error

	cwd, err := os.Getwd()
	if err != nil {
		log.Errorf("INIT: Cannot get current working directory of process: %s", err)
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
	log.Infof("INIT: Modules: '%s', Global hooks: '%s'", ModulesDir, GlobalHooksDir)

	TempDir := app.TmpDir
	err = os.MkdirAll(TempDir, os.FileMode(0777))
	if err != nil {
		log.Errorf("INIT: Cannot create temporary dir '%s': %s", TempDir, err)
		return err
	}
	log.Infof("INIT: Temporary dir: %s", TempDir)


	// init and start metrics gathering loop
	MetricsStorage = metrics_storage.Init()
	go MetricsStorage.Run()


	// Initializing the empty task queue and queue dumper
	TasksQueue = task.NewTasksQueue()
	// Initializing the queue dumper, which writes queue changes to the dump file.
	log.Debugf("INIT: Tasks queue dump file: '%s'", app.TasksQueueDumpFilePath)
	queueWatcher := task.NewTasksQueueDumper(app.TasksQueueDumpFilePath, TasksQueue)
	TasksQueue.AddWatcher(queueWatcher)


	// Initializing the connection to the k8s.
	err = kube.Init(kube.InitOptions{})
	if err != nil {
		log.Errorf("INIT: Cannot initialize Kubernetes client: %s", err)
		return err
	}


	// A useful callback when addon-operator is used as library
	if BeforeHelmInitCb != nil {
		log.Debugf("INIT: run BeforeHelmInitCallback")
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
		log.Errorf("INIT: Tiller is failed to start: %s", err)
		return err
	}

	// Initializing helm client
	err = helm.InitClient()
	if err != nil {
		log.Errorf("INIT: helm client: %s", err)
		return err
	}


	// Initializing module manager.
	KubeConfigManager = kube_config_manager.NewKubeConfigManager()
	KubeConfigManager.WithNamespace(app.Namespace)
	KubeConfigManager.WithConfigMapName(app.ConfigMapName)
	KubeConfigManager.WithValuesChecksumsAnnotation(app.ValuesChecksumsAnnotation)

	err = KubeConfigManager.Init()
	if err != nil {
		return err
	}

	module_manager.Init()
	ModuleManager = module_manager.NewMainModuleManager()
	ModuleManager.WithDirectories(ModulesDir, GlobalHooksDir, TempDir)
	ModuleManager.WithKubeConfigManager(KubeConfigManager)
	err = ModuleManager.Init()
	if err != nil {
		log.Errorf("INIT: Cannot initialize module manager: %s", err)
		return err
	}


	// Initializing the hooks schedule.
	ScheduleManager, err = schedule_manager.Init()
	if err != nil {
		log.Errorf("INIT: Cannot initialize schedule manager: %s", err)
		return err
	}

	ScheduleHooksController = schedule_hook.NewScheduleHooksController()
	ScheduleHooksController.WithModuleManager(ModuleManager)
	ScheduleHooksController.WithScheduleManager(ScheduleManager)

	KubeEventsManager = kube_events_manager.NewKubeEventsManager()
	KubeEventsManager.WithContext(context.Background())

	KubernetesHooksController = kube_event_hook.NewKubernetesHooksController()
	KubernetesHooksController.WithModuleManager(ModuleManager)
	KubernetesHooksController.WithKubeEventsManager(KubeEventsManager)


	//// Initialize kube events
	//KubeEventsManager, err = kube_events_manager.Init()
	//if err != nil {
	//	log.Errorf("INIT: Cannot initialize kube events manager: %s", err)
	//	return err
	//}
	//KubernetesHooksController = kube_event_hook.NewKubernetesHooksController()

	return nil
}

// Run runs all managers, event and queue handlers.
//
// The main process is blocked by the 'for-select' in the queue handler.
func Run() {
	// Loading the onStartup hooks into the queue and running all modules.
	// Turning tracking changes on only after startup ends.
	log.Info("MAIN: Start. Trigger onStartup event.")
	TasksQueue.ChangesDisable()

	CreateOnStartupTasks()
	CreateReloadAllTasks(true)

	err := KubernetesHooksController.EnableGlobalHooks()
	if err != nil {
		// Something wrong with global hook configs, cannot start informers.
		log.Errorf("Start informers for global kubernetes hooks: %v", err)
		return
	}
	// Start all created informers
	KubeEventsManager.Start()

	TasksQueue.ChangesEnable(true)

	go ModuleManager.Run()
	go ScheduleManager.Run()

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
					log.Infof("EVENT ModulesChanged, type=Changed")
					newTask := task.NewTask(task.ModuleRun, moduleChange.Name)
					TasksQueue.Add(newTask)
					log.Infof("QUEUE add ModuleRun %s", newTask.Name)
				}
				// As module list may have changed, hook schedule index must be re-created.
				ScheduleHooksController.UpdateScheduleHooks()
			case module_manager.GlobalChanged:
				// Global values are changed, all modules must be restarted.
				log.Infof("EVENT GlobalChanged")
				TasksQueue.ChangesDisable()
				CreateReloadAllTasks(false)
				TasksQueue.ChangesEnable(true)
				// As module list may have changed, hook schedule index must be re-created.
				ScheduleHooksController.UpdateScheduleHooks()
			case module_manager.AmbigousState:
				log.Infof("EVENT AmbiguousState")
				TasksQueue.ChangesDisable()
				// It is the error in the module manager. The task must be added to
				// the beginning of the queue so the module manager can restore its
				// state before running other queue tasks
				newTask := task.NewTask(task.ModuleManagerRetry, "")
				TasksQueue.Push(newTask)
				// It is the delay before retry.
				TasksQueue.Push(task.NewTaskDelay(FailedModuleDelay))
				TasksQueue.ChangesEnable(true)
				log.Infof("QUEUE push ModuleManagerRetry, push FailedModuleDelay")
			}
		case crontab := <-schedule_manager.ScheduleCh:
			log.Infof("EVENT Schedule event '%s'", crontab)

			tasks, err := ScheduleHooksController.HandleEvent(crontab)
			if err != nil {
				log.Errorf("MAIN_LOOP Schedule event '%s': %s", crontab, err)
				break
			}

			for _, resTask := range tasks {
				TasksQueue.Add(resTask)
				log.Infof("QUEUE add %s@%s %s", resTask.GetType(), resTask.GetBinding(), resTask.GetName())
			}

		case kubeEvent := <-kube_events_manager.KubeEventCh:
			log.Infof("EVENT Kube event '%s'", kubeEvent.ConfigId)

			tasks, err := KubernetesHooksController.HandleEvent(kubeEvent)
			if err != nil {
				log.Errorf("MAIN_LOOP error handling kube event '%s': %s", kubeEvent.ConfigId, err)
				break
			}

			for _, t := range tasks {
				TasksQueue.Add(t)
				log.Infof("QUEUE add %s@%s %s", t.GetType(), t.GetBinding(), t.GetName())
			}
		case <-ManagersEventsHandlerStopCh:
			log.Infof("EVENT Stop")
			return
		}
	}
}

func runDiscoverModulesState(discoverTask task.Task) error {
	modulesState, err := ModuleManager.DiscoverModulesState()
	if err != nil {
		return err
	}

	for _, moduleName := range modulesState.EnabledModules {
		isNewlyEnabled := false
		for _, name := range modulesState.NewlyEnabledModules {
			if name == moduleName {
				isNewlyEnabled = true
				break
			}
		}
		runOnStartupHooks := discoverTask.GetOnStartupHooks() || isNewlyEnabled

		newTask := task.NewTask(task.ModuleRun, moduleName).
			WithOnStartupHooks(runOnStartupHooks)

		TasksQueue.Add(newTask)
		log.Infof("QUEUE add ModuleRun %s", moduleName)
	}

	for _, moduleName := range modulesState.ModulesToDisable {
		// TODO may be only afterHelmDelete hooks should be initialized?
		// Enable module hooks on startup to run afterHelmDelete hooks
		if discoverTask.GetOnStartupHooks() {
			// error can be ignored, DiscoverModulesState should return existed modules
			disabledModule, _ := ModuleManager.GetModule(moduleName)
			if err = ModuleManager.RegisterModuleHooks(disabledModule); err != nil {
				return err
			}
		}
		newTask := task.NewTask(task.ModuleDelete, moduleName)
		TasksQueue.Add(newTask)
		log.Infof("QUEUE add ModuleDelete %s", moduleName)
	}

	for _, moduleName := range modulesState.ReleasedUnknownModules {
		newTask := task.NewTask(task.ModulePurge, moduleName)
		TasksQueue.Add(newTask)
		log.Infof("QUEUE add ModulePurge %s", moduleName)
	}

	// Queue afterAll global hooks
	afterAllHooks := ModuleManager.GetGlobalHooksInOrder(module_manager.AfterAll)
	for _, hookName := range afterAllHooks {
		newTask := task.NewTask(task.GlobalHookRun, hookName).
			WithBinding(module_manager.AfterAll).
			AppendBindingContext(module_manager.BindingContext{BindingContext: hook.BindingContext{Binding: module_manager.ContextBindingType[module_manager.AfterAll]}})
		TasksQueue.Add(newTask)
		log.Debugf("QUEUE add GlobalHookRun@AfterAll '%s'", hookName)
	}
	if len(afterAllHooks) > 0 {
		log.Infof("QUEUE add all GlobalHookRun@AfterAll")
	}

	ScheduleHooksController.UpdateScheduleHooks()

	// Enable kube events hooks for newly enabled modules
	// FIXME convert to a task that run after AfterHelm if there is a flag in binding config to start informers after CRD installation.
	for _, moduleName := range modulesState.EnabledModules {
		err = KubernetesHooksController.EnableModuleHooks(moduleName)
		if err != nil {
			return err
		}
	}

	// TODO is queue should be cleaned from hook run tasks of deleted module?
	// Disable kube events hooks for newly disabled modules
	for _, moduleName := range modulesState.ModulesToDisable {
		err = KubernetesHooksController.DisableModuleHooks(moduleName)
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
				log.Infof("TASK_RUN DiscoverModulesState")
				err := runDiscoverModulesState(t)
				if err != nil {
					MetricsStorage.SendCounterMetric(PrefixMetric("modules_discover_errors"), 1.0, map[string]string{})
					t.IncrementFailureCount()
					log.Errorf("TASK_RUN %s failed. Will retry after delay. Failed count is %d. Error: %s", t.GetType(), t.GetFailureCount(), err)
					TasksQueue.Push(task.NewTaskDelay(FailedModuleDelay))
					log.Infof("QUEUE push FailedModuleDelay")
					break
				}

				TasksQueue.Pop()

			case task.ModuleRun:
				log.Infof("TASK_RUN ModuleRun %s", t.GetName())
				err := ModuleManager.RunModule(t.GetName(), t.GetOnStartupHooks())
				if err != nil {
					MetricsStorage.SendCounterMetric(PrefixMetric("module_run_errors"), 1.0, map[string]string{"module": t.GetName()})
					t.IncrementFailureCount()
					log.Errorf("TASK_RUN ModuleRun '%s' failed. Will retry after delay. Failed count is %d. Error: %s", t.GetName(), t.GetFailureCount(), err)
					TasksQueue.Push(task.NewTaskDelay(FailedModuleDelay))
					log.Infof("QUEUE push FailedModuleDelay")
				} else {
					TasksQueue.Pop()
				}
			case task.ModuleDelete:
				log.Infof("TASK_RUN ModuleDelete %s", t.GetName())
				err := ModuleManager.DeleteModule(t.GetName())
				if err != nil {
					MetricsStorage.SendCounterMetric(PrefixMetric("module_delete_errors"), 1.0, map[string]string{"module": t.GetName()})
					t.IncrementFailureCount()
					log.Errorf("%s '%s' failed. Will retry after delay. Failed count is %d. Error: %s", t.GetType(), t.GetName(), t.GetFailureCount(), err)
					TasksQueue.Push(task.NewTaskDelay(FailedModuleDelay))
					log.Infof("QUEUE push FailedModuleDelay")
				} else {
					TasksQueue.Pop()
				}
			case task.ModuleHookRun:
				log.Infof("TASK_RUN ModuleHookRun@%s %s", t.GetBinding(), t.GetName())
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
						log.Errorf("%s '%s' failed. Will retry after delay. Failed count is %d. Error: %s", t.GetType(), t.GetName(), t.GetFailureCount(), err)
						TasksQueue.Push(task.NewTaskDelay(FailedModuleDelay))
						log.Infof("QUEUE push FailedModuleDelay")
					}
				} else {
					TasksQueue.Pop()
				}
			case task.GlobalHookRun:
				log.Infof("TASK_RUN GlobalHookRun@%s %s", t.GetBinding(), t.GetName())
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
						log.Errorf("TASK_RUN %s '%s' on '%s' failed. Will retry after delay. Failed count is %d. Error: %s", t.GetType(), t.GetName(), t.GetBinding(), t.GetFailureCount(), err)
						TasksQueue.Push(task.NewTaskDelay(FailedHookDelay))
					}
				} else {
					TasksQueue.Pop()
				}
			case task.ModulePurge:
				log.
					WithField("operator.component", "taskRunner").
					WithField("task", "ModulePurge").
					WithField("module", t.GetName()).
					Debugf("run task")
				logEntry := log.WithField("module", t.GetName()).WithField("phase", "purge")
				// Module for purge is unknown so log deletion error is enough.
				err := helm.NewHelmCli(logEntry).DeleteRelease(t.GetName())
				if err != nil {
					log.Errorf("TASK_RUN %s Helm delete '%s' failed. Error: %s", t.GetType(), t.GetName(), err)
				}
				TasksQueue.Pop()
			case task.ModuleManagerRetry:
				log.
					WithField("operator.component", "taskRunner").
					WithField("task", "ModuleManagerRetry").
					WithField("module", t.GetName()).
					Infof("Retry")
				MetricsStorage.SendCounterMetric(PrefixMetric("modules_discover_errors"), 1.0, map[string]string{})
				ModuleManager.Retry()
				TasksQueue.Pop()
				// Adding a delay before retrying module/hook task.
				newTask := task.NewTaskDelay(FailedModuleDelay)
				newTask.Name = t.GetName() 
				TasksQueue.Push(newTask)
				log.Infof("QUEUE push FailedModuleDelay")
			case task.Delay:
				log.Infof("TASK_RUN Delay for %s", t.GetDelay().String())
				TasksQueue.Pop()
				time.Sleep(t.GetDelay())
			case task.Stop:
				log.Infof("TASK_RUN Stop: Exiting TASK_RUN loop.")
				TasksQueue.Pop()
				return
			}

			// Breaking, if the task queue is empty to prevent the infinite loop.
			if TasksQueue.IsEmpty() {
				log.Debug("Task queue is empty. Will sleep now.")
				break
			}
		}
	}
}

func CreateOnStartupTasks() {
	log.Infof("QUEUE add all GlobalHookRun@OnStartup")

	onStartupHooks := ModuleManager.GetGlobalHooksInOrder(module_manager.OnStartup)

	for _, hookName := range onStartupHooks {
		newTask := task.NewTask(task.GlobalHookRun, hookName).
			WithBinding(module_manager.OnStartup).
			AppendBindingContext(module_manager.BindingContext{BindingContext: hook.BindingContext{Binding: module_manager.ContextBindingType[module_manager.OnStartup]}})
		TasksQueue.Add(newTask)
		log.Debugf("QUEUE add GlobalHookRun@OnStartup '%s'", hookName)
	}

	return
}

func CreateReloadAllTasks(onStartup bool) {
	log.Infof("QUEUE add all GlobalHookRun@BeforeAll, add DiscoverModulesState")

	// Queue beforeAll global hooks.
	beforeAllHooks := ModuleManager.GetGlobalHooksInOrder(module_manager.BeforeAll)

	for _, hookName := range beforeAllHooks {
		newTask := task.NewTask(task.GlobalHookRun, hookName).
			WithBinding(module_manager.BeforeAll).
			AppendBindingContext(module_manager.BindingContext{BindingContext: hook.BindingContext{Binding: module_manager.ContextBindingType[module_manager.BeforeAll]}})

		TasksQueue.Add(newTask)
		log.Debugf("QUEUE GlobalHookRun@BeforeAll '%s'", module_manager.BeforeAll, hookName)
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

func InitHttpServer(listenAddr string, listenPort string) error {
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

	http.HandleFunc("/healthz", func(writer http.ResponseWriter, request *http.Request) {
		helm.TillerHealthHandler(app.TillerProbeListenAddress, app.TillerProbeListenPort)(writer, request)
	})

	http.HandleFunc("/queue", func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(writer, TasksQueue.DumpReader())
	})

	address := fmt.Sprintf("%s:%s", listenAddr, listenPort)
	log.Infof("HTTP SERVER Listening on %s", address)

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
