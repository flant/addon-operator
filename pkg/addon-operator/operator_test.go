package addon_operator

import (
	"flag"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golang/glog"
	"github.com/romana/rlog"
	"github.com/stretchr/testify/assert"
	"gopkg.in/satori/go.uuid.v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/flant/shell-operator/pkg/kube_events_manager"
	"github.com/flant/shell-operator/pkg/metrics_storage"
	"github.com/flant/shell-operator/pkg/schedule_manager"

	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/kube_config_manager"
	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/task"
)

type KubeEventsHooksControllerMock struct{}

func (obj *KubeEventsHooksControllerMock) EnableGlobalHooks(moduleManager module_manager.ModuleManager, eventsManager kube_events_manager.KubeEventsManager) error {
	return nil
}

func (obj *KubeEventsHooksControllerMock) EnableModuleHooks(moduleName string, moduleManager module_manager.ModuleManager, eventsManager kube_events_manager.KubeEventsManager) error {
	return nil
}

func (obj *KubeEventsHooksControllerMock) DisableModuleHooks(moduleName string, moduleManager module_manager.ModuleManager, eventsManager kube_events_manager.KubeEventsManager) error {
	return nil
}

func (obj *KubeEventsHooksControllerMock) HandleEvent(kubeEvent kube_events_manager.KubeEvent) (*struct{ Tasks []task.Task }, error) {
	return nil, nil
}

type KubeEventsManagerMock struct{}

func (kem *KubeEventsManagerMock) Run(eventTypes []kube_events_manager.OnKubernetesEventType, kind, namespace string, labelSelector *metav1.LabelSelector, objectName, jqFilter string, debug bool) (string, error) {
	return uuid.NewV4().String(), nil
}

func (kem *KubeEventsManagerMock) Stop(configId string) error {
	return nil
}

type ModuleManagerMock struct {
	BeforeHookErrorsCount    int
	TestModuleErrorsCount    int
	DeleteModuleErrorsCount  int
	ScheduledHookErrorsCount int
}

var _ module_manager.ModuleManager = &ModuleManagerMock{}

var mainTestGlobalHooksMap = map[module_manager.BindingType][]string{
	module_manager.OnStartup: {
		"hook_1__31", "hook_2__32",
	},
	module_manager.BeforeAll: {
		"before_hook_1__51", "before_hook_2__52",
	},
	module_manager.AfterAll: {
		"after_hook_1__201", "after_hook_2__202",
	},
}

var scheduledHooks = map[string]schedule_manager.ScheduleConfig{
	"scheduled_global_1": {
		Crontab:      "*/1 * * * *",
		AllowFailure: true,
	},
	"scheduled_global_2": {
		Crontab:      "*/1 * * * *",
		AllowFailure: true,
	},
	"scheduled_global_3": {
		Crontab:      "*/1 * * * *",
		AllowFailure: true,
	},
	"scheduled_module_1": {
		Crontab:      "*/2 * * * *",
		AllowFailure: false,
	},
}

var runOrder = []int{}

var globalT *testing.T

func (m *ModuleManagerMock) Init() error {
	fmt.Println("Init ModuleManagerMock")
	return nil
}

func (m *ModuleManagerMock) Run() {
	fmt.Println("ModuleManagerMock Run")
}

// Only for InitModuleHooks
func (m *ModuleManagerMock) GetModule(name string) (*module_manager.Module, error) {
	return nil, nil
}

func (m *ModuleManagerMock) GetModuleNamesInOrder() []string {
	return []string{"test_module_1__101", "test_module_2__102"}
}

func (m *ModuleManagerMock) DiscoverModulesState() (*module_manager.ModulesState, error) {
	return &module_manager.ModulesState{
		EnabledModules: []string{"test_module_1__101", "test_module_2__102"},
		ModulesToDisable: []string{"disabled_module_1__111", "disabled_2__112", "disabled_3.14__113"},
		ReleasedUnknownModules:	[]string{"unknown_module_1__121", "abandoned_1__122", "forgotten_3.14__123"},
	}, nil
}

func (m *ModuleManagerMock) GetGlobalHook(name string) (*module_manager.GlobalHook, error) {
	if _, has_hook := scheduledHooks[name]; has_hook {
		return &module_manager.GlobalHook{
			CommonHook: &module_manager.CommonHook{
				Name:           name,
				Path:           "/addon-operator/hooks/global_1",
				Bindings:       []module_manager.BindingType{module_manager.Schedule},
				OrderByBinding: map[module_manager.BindingType]float64{},
			},
			Config: &module_manager.GlobalHookConfig{
				HookConfig: module_manager.HookConfig{
					Schedule: []schedule_manager.ScheduleConfig{
						scheduledHooks[name],
					},
				},
			},
		}, nil
	} else {
		// Global hook run task handler requires Path field
		return &module_manager.GlobalHook{
			CommonHook: &module_manager.CommonHook{
				Name:           name,
				Path:           "/addon-operator/hooks/global_hook_1_1",
				Bindings:       []module_manager.BindingType{module_manager.BeforeAll},
				OrderByBinding: map[module_manager.BindingType]float64{},
			},
			Config: &module_manager.GlobalHookConfig{
				BeforeAll: 10,
			},
		}, nil
	}
}

func (m *ModuleManagerMock) GetModuleHook(name string) (*module_manager.ModuleHook, error) {
	if _, has_hook := scheduledHooks[name]; has_hook {
		return &module_manager.ModuleHook{
			CommonHook: &module_manager.CommonHook{
				Name:           name,
				Path:           "/addon-operator/modules/000_test_modu",
				Bindings:       []module_manager.BindingType{module_manager.Schedule},
				OrderByBinding: map[module_manager.BindingType]float64{},
			},
			Module: &module_manager.Module{
				Name:          "test_module",
				DirectoryName: "/addon-operator/modules/000_test_modue",
				Path:          "/addon-operator/modules/000_test_modu",
			},
			Config: &module_manager.ModuleHookConfig{
				HookConfig: module_manager.HookConfig{
					Schedule: []schedule_manager.ScheduleConfig{
						scheduledHooks[name],
					},
				},
			},
		}, nil
	}
	return nil, nil
}

func (m *ModuleManagerMock) GetGlobalHooksInOrder(bindingType module_manager.BindingType) []string {
	if bindingType == module_manager.Schedule {
		res := []string{}
		for k, _ := range scheduledHooks {
			if strings.Contains(k, "global") {
				res = append(res, k)
			}
		}
		return res
	}
	return mainTestGlobalHooksMap[bindingType]
}

func (m *ModuleManagerMock) GetModuleHooksInOrder(moduleName string, bindingType module_manager.BindingType) ([]string, error) {
	if bindingType == module_manager.Schedule {
		res := []string{}
		for k, _ := range scheduledHooks {
			if strings.Contains(k, "module") {
				res = append(res, k)
			}
		}
		return res, nil
	}
	return []string{"test_module_hook_1", "test_module_hook_2"}, nil
}

func (m *ModuleManagerMock) DeleteModule(moduleName string) error {
	addRunOrder(moduleName)
	fmt.Printf("ModuleManagerMock DeleteModule '%s'\n", moduleName)
	if strings.Contains(moduleName, "disabled_module_1") && m.DeleteModuleErrorsCount > 0 {
		m.DeleteModuleErrorsCount--
		return fmt.Errorf("fake module delete error: helm run error")
	}
	return nil
}

func (m *ModuleManagerMock) RunModule(moduleName string, onStartup bool) error {
	addRunOrder(moduleName)
	fmt.Printf("ModuleManagerMock RunModule '%s'\n", moduleName)
	if strings.Contains(moduleName, "test_module_2") && m.TestModuleErrorsCount > 0 {
		m.TestModuleErrorsCount--
		return fmt.Errorf("fake module error: /bin/bash not found")
	}
	return nil
}

func (m *ModuleManagerMock) RunGlobalHook(hookName string, binding module_manager.BindingType, bindingContext []module_manager.BindingContext) error {
	addRunOrder(hookName)
	fmt.Printf("Run global hook name '%s' binding '%s'\n", hookName, binding)
	if strings.Contains(hookName, "before_hook_1") && m.BeforeHookErrorsCount > 0 {
		m.BeforeHookErrorsCount--
		return fmt.Errorf("fake module error: /bin/bash not found")
	}
	return nil
}

func (m *ModuleManagerMock) RunModuleHook(hookName string, binding module_manager.BindingType, bindingContext []module_manager.BindingContext) error {
	addRunOrder(hookName)
	fmt.Printf("Run module hook name '%s' binding '%s'\n", hookName, binding)
	if strings.Contains(hookName, "scheduled_module_1") && m.ScheduledHookErrorsCount > 0 {
		m.ScheduledHookErrorsCount--
		return fmt.Errorf("fake module hook error: /bin/ash not found")
	}
	return nil
}

func (m *ModuleManagerMock) InitModuleHooks(module *module_manager.Module) error {
	return nil
}

func (m *ModuleManagerMock) Retry() {
	fmt.Println("ModuleManagerMock Retry")
}

func (m *ModuleManagerMock) WithDirectories(modulesDir string, globalHooksDir string, tempDir string) module_manager.ModuleManager {
	fmt.Println("WithDirectories")
	return m
}

func (m *ModuleManagerMock) WithKubeConfigManager(kubeConfigManager kube_config_manager.KubeConfigManager) module_manager.ModuleManager {
	fmt.Println("WithKubeConfigManager")
	return m
}


type MockHelmClient struct {
	helm.HelmClient
	DeleteReleaseErrorsCount int
}

func (h MockHelmClient) CommandEnv() []string {
	return []string{}
}

func (h MockHelmClient) DeleteRelease(name string) error {
	addRunOrder(name)
	fmt.Printf("HelmClient: DeleteRelease '%s'\n", name)
	if strings.Contains(name, "abandoned_2") && h.DeleteReleaseErrorsCount > 0 {
		h.DeleteReleaseErrorsCount--
		return fmt.Errorf("fake helm error: helm syntax error")
	}
	return nil
}

func addRunOrder(name string) {
	if !strings.Contains(name, "__") {
		return
	}
	order := strings.Split(name, "__")[1]
	orderI, err := strconv.Atoi(order)
	if err != nil {
		globalT.Fatalf("Cannot parse number from order '%s' from name '%s'", order, name)
	}
	runOrder = append(runOrder, orderI)
}

type QueueDumperTest struct {
}

func (q *QueueDumperTest) QueueChangeCallback() {
	headTask, _ := TasksQueue.Peek()
	if headTask != nil {
		fmt.Printf("head task now is '%s', len=%d\n", headTask.GetName(), TasksQueue.Length())
	}
}

// Тест заполнения очереди заданиями при запуске и прогон TaskRunner
// после прогона очередь должна быть пустой
func TestMain_TaskRunner_CreateOnStartupTasks(t *testing.T) {
	runOrder = []int{}

	// Mock ModuleManager
	ModuleManager = &ModuleManagerMock{}

	fmt.Println("Create queue")
	// Fill a queue
	TasksQueue = task.NewTasksQueue()
	// watcher for more verbosity of CreateStartupTasks and
	TasksQueue.AddWatcher(&QueueDumperTest{})
	TasksQueue.ChangesEnable(true)

	// Add StartupTasks
	CreateOnStartupTasks()

	expectedCount := len(ModuleManager.GetGlobalHooksInOrder(module_manager.OnStartup))
	assert.Equalf(t, expectedCount, TasksQueue.Length(), "queue length is not relevant to global hooks OnStartup", TasksQueue.Length())

	// add stop task
	stopTask := task.NewTask(task.Stop, "stop runner")
	TasksQueue.Add(stopTask)

	fmt.Println("Start task runner")
	TasksRunner()

	assert.Equalf(t, 0, TasksQueue.Length(), "%d tasks remain in queue after TasksRunner", TasksQueue.Length())
}

// Тест заполнения очереди через ModuleManager и его канал EventCh
// Проверяется, что очередь будет заполнена нужным количеством заданий
func TestMain_ModulesEventsHandler(t *testing.T) {
	module_manager.EventCh = make(chan module_manager.Event, 1)
	ManagersEventsHandlerStopCh = make(chan struct{}, 1)

	// Mock ModuleManager
	ModuleManager = &ModuleManagerMock{}
	KubeEventsManager = &KubeEventsManagerMock{}
	KubeEventsHooks = &KubeEventsHooksControllerMock{}

	fmt.Println("Create queue")
	// Fill a queue
	TasksQueue = task.NewTasksQueue()
	// watcher for more verbosity of CreateStartupTasks and
	TasksQueue.AddWatcher(&QueueDumperTest{})
	TasksQueue.ChangesEnable(true)

	assert.Equal(t, 0, TasksQueue.Length())

	go func(ch chan module_manager.Event) {
		ch <- module_manager.Event{
			Type: module_manager.ModulesChanged,
			ModulesChanges: []module_manager.ModuleChange{
				{
					Name:       "test_module_1",
					ChangeType: module_manager.Changed,
				},
				{
					Name:       "test_module_2",
					ChangeType: module_manager.Changed,
				},
			},
		}
		ch <- module_manager.Event{
			Type: module_manager.ModulesChanged,
			ModulesChanges: []module_manager.ModuleChange{
				{
					Name:       "test_module_2",
					ChangeType: module_manager.Changed,
				},
			},
		}
		ch <- module_manager.Event{
			Type: module_manager.ModulesChanged,
			ModulesChanges: []module_manager.ModuleChange{
				{
					Name:       "test_module_1",
					ChangeType: module_manager.Changed,
				},
			},
		}

		ch <- module_manager.Event{
			Type: module_manager.GlobalChanged,
		}
	}(module_manager.EventCh)

	go ManagersEventsHandler()

	time.Sleep(100 * time.Millisecond)
	ManagersEventsHandlerStopCh <- struct{}{}

	expectedCount := 4 // count of ModuleChange in previous go routine
	expectedCount += len(ModuleManager.GetGlobalHooksInOrder(module_manager.BeforeAll))
	expectedCount += 1 // DiscoverModulesState task

	assert.Equal(t, expectedCount, TasksQueue.Length())
}

func TestMain(m *testing.M) {

	MetricsStorage = metrics_storage.Init()

	os.Exit(m.Run())
}

// Тест совместной работы ManagersEventsHandler и TaskRunner.
// один модуль выдаёт ошибку, TaskRunner должен его перезапускать, не запуская другие модули
// проверяется, что модули запускаются по порядку (порядок в runOrder — суффикс имени "__число")
func TestMain_Run_With_InfiniteModuleError(t *testing.T) {
	// Настройки задержек при ошибках и пустой очереди, чтобы тест побыстрее завершался.
	QueueIsEmptyDelay = 50 * time.Millisecond
	FailedHookDelay = 50 * time.Millisecond
	FailedModuleDelay = 50 * time.Millisecond

	module_manager.EventCh = make(chan module_manager.Event, 1)
	ManagersEventsHandlerStopCh = make(chan struct{}, 1)

	runOrder = []int{}

	// Сделать моки для всего, что нужно для запуска Run
	KubeEventsManager = &KubeEventsManagerMock{}
	KubeEventsHooks = &KubeEventsHooksControllerMock{}

	helm.Client = MockHelmClient{
		DeleteReleaseErrorsCount: 0,
	}

	// Mock ModuleManager
	ModuleManager = &ModuleManagerMock{
		BeforeHookErrorsCount:   0,
		TestModuleErrorsCount:   10000,
		DeleteModuleErrorsCount: 0,
	}

	ScheduleManager = &MockScheduleManager{}

	// Создать очередь
	fmt.Println("Create queue")
	// Fill a queue
	TasksQueue = task.NewTasksQueue()
	// watcher for more verbosity of CreateStartupTasks and
	TasksQueue.AddWatcher(&QueueDumperTest{})
	TasksQueue.ChangesEnable(true)

	Run()

	time.Sleep(1000 * time.Millisecond)
	// Stop events handler
	ManagersEventsHandlerStopCh <- struct{}{}
	// stop tasks runner: add stop task
	stopTask := task.NewTask(task.Stop, "stop runner")
	TasksQueue.Push(stopTask)

	fmt.Println("wait for queueIsEmptyDelay")
	time.Sleep(100 * time.Millisecond)

	assert.True(t, TasksQueue.Length() > 0, "queue is empty with errored module %d", TasksQueue.Length())

	accum := 0
	for _, ord := range runOrder {
		assert.True(t, ord >= accum, "detect unordered execution: '%d' '%d'\n%+v", accum, ord, runOrder)
		accum = ord
	}

	fmt.Printf("runOrder: %+v", runOrder)
}

// Тест совместной работы ManagersEventsHandler и TaskRunner.
// Модули и хуки выдают ошибки, TaskRunner должен их перезапускать, не запуская следующие задания.
// Проверяется, что модули и хуки запускаются по порядку (порядок в runOrder — суффикс имени "__число")
func TestMain_Run_With_RecoverableErrors(t *testing.T) {
	// Настройки задержек при ошибках и пустой очереди, чтобы тест побыстрее завершался.
	QueueIsEmptyDelay = 50 * time.Millisecond
	FailedHookDelay = 50 * time.Millisecond
	FailedModuleDelay = 50 * time.Millisecond

	module_manager.EventCh = make(chan module_manager.Event, 1)
	ManagersEventsHandlerStopCh = make(chan struct{}, 1)

	runOrder = []int{}

	// Сделать моки для всего, что нужно для запуска Run

	helm.Client = MockHelmClient{
		DeleteReleaseErrorsCount: 3,
	}

	// Mock ModuleManager
	ModuleManager = &ModuleManagerMock{
		BeforeHookErrorsCount:   3,
		TestModuleErrorsCount:   6,
		DeleteModuleErrorsCount: 2,
	}

	ScheduleManager = &MockScheduleManager{}

	fmt.Println("Create queue")
	// Fill a queue
	TasksQueue = task.NewTasksQueue()
	// watcher for more verbosity of CreateStartupTasks and
	TasksQueue.AddWatcher(&QueueDumperTest{})
	TasksQueue.ChangesEnable(true)

	Run()

	time.Sleep(1000 * time.Millisecond)
	// Stop events handler
	ManagersEventsHandlerStopCh <- struct{}{}
	// stop tasks runner: add stop task
	stopTask := task.NewTask(task.Stop, "stop runner")
	TasksQueue.Add(stopTask)

	fmt.Println("wait for queueIsEmptyDelay")
	time.Sleep(100 * time.Millisecond)

	assert.Equalf(t, 0, TasksQueue.Length(), "%d tasks remain in queue after TasksRunner", TasksQueue.Length())

	accum := 0
	for _, ord := range runOrder {
		assert.True(t, ord >= accum, "detect unordered execution: '%d' '%d'\n%+v", accum, ord, runOrder)
		accum = ord
	}

	fmt.Printf("runOrder: %+v", runOrder)
}

type MockScheduleManager struct {
	schedule_manager.ScheduleManager
}

func (m *MockScheduleManager) Add(crontab string) (string, error) {
	fmt.Printf("MockScheduleManager: Add crontab '%s'\n", crontab)
	return crontab, nil
}

func (m *MockScheduleManager) Remove(entryId string) error {
	fmt.Printf("MockScheduleManager: Remove crontab '%s'\n", entryId)
	return nil
}

func (m *MockScheduleManager) Run() {
	fmt.Printf("MockScheduleManager: Run\n")
}

// Тесты scheduled_tasks
// Проинициализировать первый раз хуки по расписанию
// Забросить в scheduled канал несколько расписаний
// отключить модуль, забросить GLOBAL изменения, проверить, что хуки пересоздались и остался только глобальный
// забросить в канал расписания, проверить, что выполнится только глобальный хук
func TestMain_ScheduledTasks(t *testing.T) {

	// Настройки задержек при ошибках и пустой очереди, чтобы тест побыстрее завершался.
	QueueIsEmptyDelay = 50 * time.Millisecond
	FailedHookDelay = 50 * time.Millisecond
	FailedModuleDelay = 50 * time.Millisecond

	module_manager.EventCh = make(chan module_manager.Event, 1)
	ManagersEventsHandlerStopCh = make(chan struct{}, 1)

	runOrder = []int{}

	helm.Client = MockHelmClient{
		DeleteReleaseErrorsCount: 3,
	}

	// Mock ModuleManager
	ModuleManager = &ModuleManagerMock{
		BeforeHookErrorsCount:   3,
		TestModuleErrorsCount:   6,
		DeleteModuleErrorsCount: 2,
	}

	// Create ScheduleManager
	// Инициализация хуков по расписанию - карта scheduleId → []ScheduleHook
	ScheduleManager = &MockScheduleManager{}
	schedule_manager.ScheduleCh = make(chan string, 1)
	ScheduledHooks = UpdateScheduleHooks(nil)
	assert.Equal(t, 4, len(ScheduledHooks), "not enough scheduled hooks")
	assert.Equal(t, 3, len(ScheduledHooks.GetHooksForSchedule("*/1 * * * *")), "not enough global scheduled hooks")

	fmt.Println("Create queue")
	// Fill a queue
	TasksQueue = task.NewTasksQueue()
	// watcher for more verbosity of CreateStartupTasks and
	TasksQueue.AddWatcher(&QueueDumperTest{})
	TasksQueue.ChangesEnable(true)

	stepCh := make(chan struct{})

	// обработчик событий от менеджеров — события превращаются в таски и
	// добавляются в очередь
	go ManagersEventsHandler()

	// TasksRunner запускает задания из очереди
	go TasksRunner()

	// EmitScheduleEvents
	go func() {
		// подождать завершения init тасков
		//time.Sleep(300 * time.Millisecond)
		schedule_manager.ScheduleCh <- "*/1 * * * *"
		time.Sleep(300 * time.Millisecond)
		schedule_manager.ScheduleCh <- "*/2 * * * *"

		// удалить хук
		delete(scheduledHooks, "scheduled_global_1")

		// GlobalChanged должен привести к пересозданию хранилища хуков по расписанию
		time.Sleep(300 * time.Millisecond)
		module_manager.EventCh <- module_manager.Event{
			Type: module_manager.GlobalChanged,
		}
		time.Sleep(100 * time.Millisecond)
		stepCh <- struct{}{}
	}()
	<-stepCh

	// проверка хуков
	assert.Equalf(t, 3, len(ScheduledHooks), "bad scheduled hooks count after GlobalChanged: %+v", ScheduledHooks)

	// повторная отправка всех расписаний, в том числе удалённого
	go func() {
		time.Sleep(300 * time.Millisecond)
		schedule_manager.ScheduleCh <- "*/1 * * * *"
		time.Sleep(300 * time.Millisecond)
		schedule_manager.ScheduleCh <- "*/2 * * * *"
		stepCh <- struct{}{}
	}()
	<-stepCh

	time.Sleep(1000 * time.Millisecond)

	// Stop events handler
	ManagersEventsHandlerStopCh <- struct{}{}

	// stop tasks runner: add stop task
	stopTask := task.NewTask(task.Stop, "stop runner")
	TasksQueue.Add(stopTask)

	fmt.Println("wait for queueIsEmptyDelay")
	time.Sleep(100 * time.Millisecond)

	assert.Equalf(t, 0, TasksQueue.Length(), "%d tasks remain in queue after TasksRunner", TasksQueue.Length())

	// TODO надо этот order переделать, чтобы были не чиселки, а лог выполнения модулей/хуков
	accum := 0
	for _, ord := range runOrder {
		assert.True(t, ord >= accum, "detect unordered execution: '%d' '%d'\n%+v", accum, ord, runOrder)
		accum = ord
	}

	fmt.Printf("runOrder: %+v", runOrder)
}

// Тесты запускаются уже с flag.Parsed(), поэтому glog ничего не пишет
func TestGlog(t *testing.T) {
	t.SkipNow()
	os.Setenv("RLOG_LOG_LEVEL", "DEBUG")
	rlog.UpdateEnv()

	rlog.Info("start TestGlog")
	glog.Warning("test warngin from glog")

	flag.Set("", "")
	//flag.CommandLine.Parse([]string{})
	glog.Warning("test warngin from glog after Parse")
	rlog.Info("stop TestGlog")

	time.Sleep(1 * time.Second)

	t.Error("Error call to get stdout")
}

// Dump global hooks and modules and modules hooks info
func TestDumpModuleManagerInfo(t *testing.T) {
	t.SkipNow()
	rlog.Debugf("=== START ModuleManager Dump ===")
	bindings := []module_manager.BindingType{module_manager.OnStartup, module_manager.BeforeHelm, module_manager.AfterHelm,
		module_manager.AfterDeleteHelm, module_manager.BeforeAll, module_manager.AfterAll, module_manager.Schedule, module_manager.KubeEvents,
	}
	rlog.Debugf("  GlobalHooks")

	for _, binding := range bindings {
		ghNames := ModuleManager.GetGlobalHooksInOrder(binding)
		if len(ghNames) > 0 {
			rlog.Debugf("  %s:", binding)
			for idx, ghName := range ghNames {
				gh, _ := ModuleManager.GetGlobalHook(ghName)
				rlog.Debugf("%d. %s %s %s safe: %s, bind: %+v", idx, ghName, gh.Name, path.Base(gh.Path), gh.SafeName())
			}
		}
	}

	rlog.Debugf("  Modules and module hooks")
	mNames := ModuleManager.GetModuleNamesInOrder()
	for idx, mName := range mNames {
		rlog.Debugf("%d. %s", idx, mName)
		for _, binding := range bindings {
			mhNames, _ := ModuleManager.GetModuleHooksInOrder(mName, binding)
			if len(mhNames) > 0 {
				rlog.Debugf("  %s:", binding)
				for _, mhName := range mhNames {
					mh, _ := ModuleManager.GetModuleHook(mhName)
					rlog.Debugf("    %s '%s' safe:'%s' %s module: '%s' safe:'%s'", mhName, mh.Name, mh.SafeName(), path.Base(mh.Path), mh.Module.Name, mh.Module.SafeName())
				}
			}
		}
	}
	rlog.Debugf("=== END ModuleManager Dump ===")
}
