package main

import (
	"flag"
	"io"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/romana/rlog"

	"github.com/flant/antiopa/pkg/antiopa"
	"github.com/flant/antiopa/pkg/docker_registry_manager"
	"github.com/flant/shell-operator/pkg/executor"
	"github.com/flant/antiopa/pkg/helm"
	"github.com/flant/antiopa/pkg/kube"
	"github.com/flant/antiopa/pkg/kube_events_manager"
	"github.com/flant/shell-operator/pkg/metrics_storage"
	"github.com/flant/antiopa/pkg/module_manager"
	"github.com/flant/shell-operator/pkg/schedule_manager"
	"github.com/flant/antiopa/pkg/task"
	"github.com/flant/antiopa/pkg/utils"
)

var (
	WorkingDir string
	TempDir    string

	// The hostname is the same as the pod name. Can be used for API requests.
	Hostname string

	// TasksQueueDumpFilePath is the name of the file to which the queue will be dumped.
	TasksQueueDumpFilePath string

	TasksQueue *task.TasksQueue

	// ModuleManager is the module manager object, which monitors configuration
	// and variable changes.
	ModuleManager module_manager.ModuleManager

	// RegistryManager is the object for the registry manager, which watches
	// for Antiopa image updates.
	RegistryManager docker_registry_manager.DockerRegistryManager

	ScheduleManager schedule_manager.ScheduleManager
	ScheduledHooks  ScheduledHooksStorage

	KubeEventsManager kube_events_manager.KubeEventsManager
	KubeEventsHooks   antiopa.KubeEventsHooksController

	MetricsStorage *metrics_storage.MetricStorage

	// ManagersEventsHandlerStopCh is the channel object for stopping infinite loop of the ManagersEventsHandler.
	ManagersEventsHandlerStopCh chan struct{}

	// HelmClient is the object for Helm client.
	HelmClient helm.HelmClient
)

const DefaultTasksQueueDumpFilePath = "/tmp/antiopa-tasks-queue"

// Defining delays in processing tasks from queue.
var (
	QueueIsEmptyDelay = 3 * time.Second
	FailedHookDelay   = 5 * time.Second
	FailedModuleDelay = 5 * time.Second
)

// Collecting the settings: directory, host name, dump file, tiller namespace.
// Initializing all necessary objects: helm, registry manager, module manager,
// kube events manager.
// Creating an empty queue with jobs.
func Init() {
	rlog.Debug("Init")

	var err error

	WorkingDir, err = os.Getwd()
	if err != nil {
		rlog.Errorf("MAIN Fatal: Cannot determine Antiopa working dir: %s", err)
		os.Exit(1)
	}
	rlog.Infof("Antiopa working dir: %s", WorkingDir)

	TempDir := "/tmp/antiopa"
	err = os.Mkdir(TempDir, os.FileMode(0777))
	if err != nil {
		rlog.Errorf("MAIN Fatal: Cannot create Antiopa temporary dir: %s", err)
		os.Exit(1)
	}
	rlog.Infof("Antiopa temporary dir: %s", TempDir)

	Hostname, err = os.Hostname()
	if err != nil {
		rlog.Errorf("MAIN Fatal: Cannot get pod name from hostname: %s", err)
		os.Exit(1)
	}
	rlog.Infof("Antiopa hostname: %s", Hostname)

	// Initializing the connection to the k8s.
	kube.InitKube()

 // Инициализация слежения за образом
	// TODO Antiopa может и не следить, если кластер заморожен?
	RegistryManager, err = docker_registry_manager.Init(Hostname)
	if err != nil {
		rlog.Errorf("MAIN Fatal: Cannot initialize registry manager: %s", err)
		os.Exit(1)
	}

	// Initializing helm. Installing Tiller, if it is missing.
	tillerNamespace := kube.KubernetesAntiopaNamespace
	rlog.Debugf("Antiopa namespace for Tiller: %s", tillerNamespace)
	HelmClient, err = helm.Init(tillerNamespace)
	if err != nil {
		rlog.Errorf("MAIN Fatal: cannot initialize Helm: %s", err)
		os.Exit(1)
	}

	// Initializing module manager.
	ModuleManager, err = module_manager.Init(WorkingDir, TempDir, HelmClient)
	if err != nil {
		rlog.Errorf("MAIN Fatal: Cannot initialize module manager: %s", err)
		os.Exit(1)
	}

	// Initializing the empty task queue.
	TasksQueue = task.NewTasksQueue()

	// Initializing the queue dumper, which writes queue changes to the dump file.
	TasksQueueDumpFilePath = DefaultTasksQueueDumpFilePath
	rlog.Debugf("Antiopa tasks queue dump file: '%s'", TasksQueueDumpFilePath)
	queueWatcher := task.NewTasksQueueDumper(TasksQueueDumpFilePath, TasksQueue)
	TasksQueue.AddWatcher(queueWatcher)

	// Initializing the hooks schedule.
	ScheduleManager, err = schedule_manager.Init()
	if err != nil {
		rlog.Errorf("MAIN Fatal: Cannot initialize schedule manager: %s", err)
		os.Exit(1)
	}

	KubeEventsManager, err = kube_events_manager.Init()
	if err != nil {
		rlog.Errorf("MAIN Fatal: Cannot initialize kube events manager: %s", err)
		os.Exit(1)
	}
	KubeEventsHooks = antiopa.NewMainKubeEventsHooksController()

	MetricsStorage = metrics_storage.Init()
}

// Run runs all managers, event and queue handlers.
// The main process is blocked by the 'for-select' in the queue handler.
func Run() {
	rlog.Info("MAIN: run main loop")

	// Loading the onStartup hooks into the queue and running all modules.
	// Turning tracking changes on only after startup ends.
	rlog.Info("MAIN: add onStartup, beforeAll, module and afterAll tasks")
	TasksQueue.ChangesDisable()

	CreateOnStartupTasks()
	CreateReloadAllTasks(true)

	KubeEventsHooks.EnableGlobalHooks(ModuleManager, KubeEventsManager)

	TasksQueue.ChangesEnable(true)

	if RegistryManager != nil {
		// Managers are go routines, that send events to their channels
		RegistryManager.SetErrorCallback(func() {
			MetricsStorage.SendCounterMetric("antiopa_registry_errors", 1.0, map[string]string{})
		})
		go RegistryManager.Run()
	}
	go ModuleManager.Run()
	go ScheduleManager.Run()

	// Metric add handler
	go MetricsStorage.Run()

	// Managers events handler adds task to the queue on every received event/
	go ManagersEventsHandler()

	// TasksRunner runs tasks from the queue.
	go TasksRunner()

	RunAntiopaMetrics()
}

func ManagersEventsHandler() {
	for {
		select {
		// Antiopa image has changed, deployment must be restarted.
		case newImageId := <-docker_registry_manager.ImageUpdated:
			rlog.Infof("EVENT ImageUpdated")
			err := kube.KubeUpdateDeployment(newImageId)
			if err == nil {
				rlog.Infof("KUBE deployment update successful, exiting ...")
				os.Exit(1)
			} else {
				rlog.Errorf("KUBE deployment update error: %s", err)
			}
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
					switch moduleChange.ChangeType {
					case module_manager.Enabled:
						// TODO этого события по сути нет. Нужно реализовать для вызова onStartup!
						rlog.Infof("EVENT ModulesChanged, type=Enabled")
						newTask := task.NewTask(task.ModuleRun, moduleChange.Name).
							WithOnStartupHooks(true)
						TasksQueue.Add(newTask)
						rlog.Infof("QUEUE add ModuleRun %s", newTask.Name)

						err := KubeEventsHooks.EnableModuleHooks(moduleChange.Name, ModuleManager, KubeEventsManager)
						if err != nil {
							rlog.Errorf("MAIN_LOOP module '%s' enabled: cannot enable hooks: %s", moduleChange.Name, err)
						}

					case module_manager.Changed:
						rlog.Infof("EVENT ModulesChanged, type=Changed")
						newTask := task.NewTask(task.ModuleRun, moduleChange.Name)
						TasksQueue.Add(newTask)
						rlog.Infof("QUEUE add ModuleRun %s", newTask.Name)

					case module_manager.Disabled:
						rlog.Infof("EVENT ModulesChanged, type=Disabled")
						newTask := task.NewTask(task.ModuleDelete, moduleChange.Name)
						TasksQueue.Add(newTask)
						rlog.Infof("QUEUE add ModuleDelete %s", newTask.Name)

						err := KubeEventsHooks.DisableModuleHooks(moduleChange.Name, ModuleManager, KubeEventsManager)
						if err != nil {
							rlog.Errorf("MAIN_LOOP module '%s' disabled: cannot disable hooks: %s", moduleChange.Name, err)
						}

					case module_manager.Purged:
						rlog.Infof("EVENT ModulesChanged, type=Purged")
						newTask := task.NewTask(task.ModulePurge, moduleChange.Name)
						TasksQueue.Add(newTask)
						rlog.Infof("QUEUE add ModulePurge %s", newTask.Name)

						err := KubeEventsHooks.DisableModuleHooks(moduleChange.Name, ModuleManager, KubeEventsManager)
						if err != nil {
							rlog.Errorf("MAIN_LOOP module '%s' purged: cannot disable hooks: %s", moduleChange.Name, err)
						}
					}
				}
				// As module list may have changed, hook schedule index must be re-created.
				ScheduledHooks = UpdateScheduleHooks(ScheduledHooks)
			// As global values may have changed, all modules must be restarted.
			case module_manager.GlobalChanged:
				rlog.Infof("EVENT GlobalChanged")
				TasksQueue.ChangesDisable()
				CreateReloadAllTasks(false)
				TasksQueue.ChangesEnable(true)
				// Re-creating schedule hook index
				ScheduledHooks = UpdateScheduleHooks(ScheduledHooks)
			case module_manager.AmbigousState:
				rlog.Infof("EVENT AmbigousState")
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
	for _, moduleName := range modulesState.EnabledModules {
		err = KubeEventsHooks.EnableModuleHooks(moduleName, ModuleManager, KubeEventsManager)
		if err != nil {
			return err
		}
	}

	// Disable kube events hooks for newly disabled modules
	for _, moduleName := range modulesState.ModulesToDisable {
		err = KubeEventsHooks.DisableModuleHooks(moduleName, ModuleManager, KubeEventsManager)
		if err != nil {
			return err
		}
	}

	return nil
}

// There is a one task handler per queue.
// Task handler may delay task processing by pushing delay to the queue.
// TODO пока только один обработчик, всё ок. Но лучше, чтобы очередь позволяла удалять только то, чему ранее был сделан peek.
// Т.е. кто взял в обработку задание, тот его и удалил из очереди. Сейчас Peek-нуть может одна го-рутина, другая добавит,
// первая Pop-нет задание — новое задание пропало, второй раз будет обработано одно и тоже.
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
					MetricsStorage.SendCounterMetric("antiopa_modules_discover_errors", 1.0, map[string]string{})
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
					MetricsStorage.SendCounterMetric("antiopa_module_run_errors", 1.0, map[string]string{"module": t.GetName()})
					t.IncrementFailureCount()
					rlog.Errorf("TASK_RUN %s '%s' failed. Will retry after delay. Failed count is %d. Error: %s", t.GetType(), t.GetName(), t.GetFailureCount(), err)
					TasksQueue.Push(task.NewTaskDelay(FailedModuleDelay))
					rlog.Infof("QUEUE push FailedModuleDelay")
				} else {
					TasksQueue.Pop()
				}
			case task.ModuleDelete:
				rlog.Infof("TASK_RUN ModuleDelete %s", t.GetName())
				err := ModuleManager.DeleteModule(t.GetName())
				if err != nil {
					MetricsStorage.SendCounterMetric("antiopa_module_delete_errors", 1.0, map[string]string{"module": t.GetName()})
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
						MetricsStorage.SendCounterMetric("antiopa_module_hook_allowed_errors", 1.0, map[string]string{"module": moduleLabel, "hook": hookLabel})
						TasksQueue.Pop()
					} else {
						MetricsStorage.SendCounterMetric("antiopa_module_hook_errors", 1.0, map[string]string{"module": moduleLabel, "hook": hookLabel})
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
						MetricsStorage.SendCounterMetric("antiopa_global_hook_allowed_errors", 1.0, map[string]string{"hook": hookLabel})
						TasksQueue.Pop()
					} else {
						MetricsStorage.SendCounterMetric("antiopa_global_hook_errors", 1.0, map[string]string{"hook": hookLabel})
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
				err := HelmClient.DeleteRelease(t.GetName())
				if err != nil {
					rlog.Errorf("TASK_RUN %s Helm delete '%s' failed. Error: %s", t.GetType(), t.GetName(), err)
				}
				TasksQueue.Pop()
			case task.ModuleManagerRetry:
				rlog.Infof("TASK_RUN ModuleManagerRetry")
				// TODO метрику нужно отсылать из module_manager. Cделать metric_storage глобальным!
				MetricsStorage.SendCounterMetric("antiopa_modules_discover_errors", 1.0, map[string]string{})
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

type ScheduleHook struct {
	Name     string
	Schedule []module_manager.ScheduleConfig
}

type ScheduledHooksStorage []*ScheduleHook

// GetCrontabs returns all schedules from the hook store.
func (s ScheduledHooksStorage) GetCrontabs() []string {
	resMap := map[string]bool{}
	for _, hook := range s {
		for _, schedule := range hook.Schedule {
			resMap[schedule.Crontab] = true
		}
	}

	res := make([]string, len(resMap))
	for k := range resMap {
		res = append(res, k)
	}
	return res
}

// GetHooksForSchedule returns hooks for specific schedule.
func (s ScheduledHooksStorage) GetHooksForSchedule(crontab string) []*ScheduleHook {
	res := []*ScheduleHook{}

	for _, hook := range s {
		newHook := &ScheduleHook{
			Name:     hook.Name,
			Schedule: []module_manager.ScheduleConfig{},
		}
		for _, schedule := range hook.Schedule {
			if schedule.Crontab == crontab {
				newHook.Schedule = append(newHook.Schedule, schedule)
			}
		}

		if len(newHook.Schedule) > 0 {
			res = append(res, newHook)
		}
	}

	return res
}

// AddHook adds hook to the hook schedule.
func (s *ScheduledHooksStorage) AddHook(hookName string, config []module_manager.ScheduleConfig) {
	for i, hook := range *s {
		if hook.Name == hookName {
			// Changes hook config and exit if the hook already exists.
			(*s)[i].Schedule = []module_manager.ScheduleConfig{}
			for _, item := range config {
				(*s)[i].Schedule = append((*s)[i].Schedule, item)
			}
			return
		}
	}

	newHook := &ScheduleHook{
		Name:     hookName,
		Schedule: []module_manager.ScheduleConfig{},
	}
	for _, item := range config {
		newHook.Schedule = append(newHook.Schedule, item)
	}
	*s = append(*s, newHook)

}

// RemoveHook removes hook from the hook storage.
func (s *ScheduledHooksStorage) RemoveHook(hookName string) {
	tmp := ScheduledHooksStorage{}
	for _, hook := range *s {
		if hook.Name == hookName {
			continue
		}
		tmp = append(tmp, hook)
	}

	*s = tmp
}

// UpdateScheduleHooks creates the new ScheduledHooks.
// Calculates the difference between the old and the new schedule,
// removes what was in the old but is missing in the new schedule.
func UpdateScheduleHooks(storage ScheduledHooksStorage) ScheduledHooksStorage {
	if ScheduleManager == nil {
		return nil
	}

	oldCrontabs := map[string]bool{}
	if storage != nil {
		for _, crontab := range storage.GetCrontabs() {
			oldCrontabs[crontab] = false
		}
	}

	newScheduledTasks := ScheduledHooksStorage{}

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

func RunAntiopaMetrics() {
	// Antiopa live ticks.
	go func() {
		for {
			MetricsStorage.SendCounterMetric("antiopa_live_ticks", 1.0, map[string]string{})
			time.Sleep(10 * time.Second)
		}
	}()

	go func() {
		for {
			queueLen := float64(TasksQueue.Length())
			MetricsStorage.SendGaugeMetric("antiopa_tasks_queue_length", queueLen, map[string]string{})
			time.Sleep(5 * time.Second)
		}
	}()
}

func InitHttpServer() {
	http.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		writer.Write([]byte(`<html>
    <head><title>Antiopa</title></head>
    <body>
    <h1>Antiopa</h1>
    <pre>go tool pprof goprofex http://ANTIOPA_IP:9115/debug/pprof/profile</pre>
    </body>
    </html>`))
	})
	http.Handle("/metrics", promhttp.Handler())

	http.HandleFunc("/queue", func(writer http.ResponseWriter, request *http.Request) {
		io.Copy(writer, TasksQueue.DumpReader())
	})

	go func() {
		rlog.Info("Listening on :9115")
		if err := http.ListenAndServe(":9115", nil); err != nil {
			rlog.Error("Error starting HTTP server: %s", err)
		}
	}()
}

func main() {
	// Setting flag.Parsed() for glog.
	flag.CommandLine.Parse([]string{})

	// Be a good parent - clean up behind the children processes.
	// Antiopa is PID1, no special config required.
	go executor.Reap()

	// Enables HTTP server for pprof and prometheus clients
	InitHttpServer()

 Init()

	// Runs managers and handlers.
	Run()

	// Blocks main() by waiting signals from OS.
	utils.WaitForProcessInterruption()
}
