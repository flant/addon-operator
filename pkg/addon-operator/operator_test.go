package addon_operator

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/deckhouse/deckhouse/pkg/log"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8types "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"

	"github.com/flant/addon-operator/pkg/addon-operator/converge"
	mockhelm "github.com/flant/addon-operator/pkg/helm/test/mock"
	mockhelmresmgr "github.com/flant/addon-operator/pkg/helm_resources_manager/test/mock"
	. "github.com/flant/addon-operator/pkg/hook/types"
	"github.com/flant/addon-operator/pkg/kube_config_manager"
	"github.com/flant/addon-operator/pkg/kube_config_manager/backend/configmap"
	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
	"github.com/flant/addon-operator/pkg/task"
	taskqueue "github.com/flant/addon-operator/pkg/task/queue"
	taskservice "github.com/flant/addon-operator/pkg/task/service"
	"github.com/flant/kube-client/fake"
	. "github.com/flant/shell-operator/pkg/hook/types"
	metricstorage "github.com/flant/shell-operator/pkg/metric_storage"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
	file_utils "github.com/flant/shell-operator/pkg/utils/file"
)

type assembleResult struct {
	helmClient           *mockhelm.Client
	helmResourcesManager *mockhelmresmgr.MockHelmResourcesManager
	cmName               string
	cmNamespace          string
}

func assembleTestAddonOperator(t *testing.T, configPath string) (*AddonOperator, *assembleResult) {
	g := NewWithT(t)

	const defaultNamespace = "default"
	const defaultName = "addon-operator"

	result := new(assembleResult)

	// Check content in configPath.
	rootDir := filepath.Join("testdata", configPath)
	g.Expect(rootDir).Should(BeADirectory())

	modulesDir := filepath.Join(rootDir, "modules")
	if exists := file_utils.DirExists(modulesDir); !exists {
		modulesDir = ""
	}
	globalHooksDir := filepath.Join(rootDir, "global-hooks")
	if exists := file_utils.DirExists(globalHooksDir); !exists {
		globalHooksDir = ""
	}
	if globalHooksDir == "" {
		globalHooksDir = filepath.Join(rootDir, "global")
		if exists := file_utils.DirExists(globalHooksDir); !exists {
			globalHooksDir = ""
		}
	}

	// Load config values from config_map.yaml.
	cmFilePath := filepath.Join(rootDir, "config_map.yaml")
	cmExists, _ := file_utils.FileExists(cmFilePath)

	var cmObj *v1.ConfigMap
	if cmExists {
		cmDataBytes, err := os.ReadFile(cmFilePath)
		g.Expect(err).ShouldNot(HaveOccurred(), "Should read config map file '%s'", cmFilePath)

		cmObj = new(v1.ConfigMap)
		err = yaml.Unmarshal(cmDataBytes, &cmObj)
		g.Expect(err).ShouldNot(HaveOccurred(), "Should parse YAML in %s", cmFilePath)
		if cmObj.Namespace == "" {
			cmObj.SetNamespace(defaultNamespace)
		}
	} else {
		cmObj = &v1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      defaultName,
				Namespace: defaultNamespace,
			},
			Data: nil,
		}
	}
	result.cmName = cmObj.Name
	result.cmNamespace = cmObj.Namespace

	// Create ConfigMap.
	kubeClient := fake.NewFakeCluster(fake.ClusterVersionV119).Client
	_, err := kubeClient.CoreV1().ConfigMaps(result.cmNamespace).Create(context.TODO(), cmObj, metav1.CreateOptions{})
	g.Expect(err).ShouldNot(HaveOccurred(), "Should create ConfigMap/%s", result.cmName)

	// Assemble AddonOperator.
	op := NewAddonOperator(context.Background(), WithLogger(log.NewNop()))
	op.engine.KubeClient = kubeClient
	// Mock helm client for ModuleManager
	result.helmClient = &mockhelm.Client{}
	op.Helm = mockhelm.NewClientFactory(result.helmClient)
	// Mock helm resources manager to execute module actions: run, delete.
	result.helmResourcesManager = &mockhelmresmgr.MockHelmResourcesManager{}
	op.HelmResourcesManager = result.helmResourcesManager

	op.engine.SetupEventManagers()

	bk := configmap.New(op.engine.KubeClient, result.cmNamespace, result.cmName, log.NewNop())
	manager := kube_config_manager.NewKubeConfigManager(op.ctx, bk, op.runtimeConfig, log.NewNop())
	op.KubeConfigManager = manager

	dirs := module_manager.DirectoryConfig{
		ModulesDir:     modulesDir,
		GlobalHooksDir: globalHooksDir,
		TempDir:        t.TempDir(),
	}

	deps := module_manager.ModuleManagerDependencies{
		KubeObjectPatcher:    nil,
		KubeEventsManager:    op.engine.KubeEventsManager,
		KubeConfigManager:    manager,
		ScheduleManager:      op.engine.ScheduleManager,
		Helm:                 op.Helm,
		HelmResourcesManager: op.HelmResourcesManager,
		MetricStorage:        metricstorage.NewMetricStorage(op.ctx, "addon_operator_", false, log.NewNop()),
		HookMetricStorage:    metricstorage.NewMetricStorage(op.ctx, "addon_operator_", false, log.NewNop()),
	}
	cfg := module_manager.ModuleManagerConfig{
		DirectoryConfig: dirs,
		Dependencies:    deps,
	}
	op.ModuleManager = module_manager.NewModuleManager(op.ctx, &cfg, log.NewNop())

	err = op.InitModuleManager()
	g.Expect(err).ShouldNot(HaveOccurred(), "Should init ModuleManager")
	_ = op.ModuleManager.RecalculateGraph(map[string]string{})

	op.TaskService = taskservice.NewTaskHandlerService(&taskservice.TaskHandlerServiceConfig{
		Engine:               op.engine,
		ParallelTaskChannels: op.parallelTaskChannels,
		Helm:                 op.Helm,
		HelmResourcesManager: op.HelmResourcesManager,
		ModuleManager:        op.ModuleManager,
		MetricStorage:        op.engine.MetricStorage,
		KubeConfigManager:    op.KubeConfigManager,
		QueueService: taskqueue.NewService(op.ctx, &taskqueue.ServiceConfig{
			Engine: op.engine,
			Handle: op.TaskHandler,
		}, log.NewNop()),
		ConvergeState:  op.ConvergeState,
		CRDExtraLabels: map[string]string{},
	}, log.NewNop())

	return op, result
}

func convergeDone(op *AddonOperator) func(g Gomega) bool {
	return func(g Gomega) bool {
		if op.IsStartupConvergeDone() {
			return true
		}
		mainQueue := op.engine.TaskQueues.GetMain()
		g.Expect(func() bool {
			if mainQueue.IsEmpty() {
				return true
			}
			return mainQueue.GetFirst().GetFailureCount() >= 2
		}).Should(BeTrue(), "Error loop detected.")
		return false
	}
}

// CreateOnStartupTasks fills a working queue with onStartup hooks.
// TaskRunner should run all hooks and clean the queue.
func Test_Operator_startup_tasks(t *testing.T) {
	g := NewWithT(t)
	log.SetDefaultLevel(log.LevelError)

	op, _ := assembleTestAddonOperator(t, "startup_tasks")

	op.BootstrapMainQueue(op.engine.TaskQueues)

	expectTasks := []struct {
		taskType    sh_task.TaskType
		bindingType BindingType
		hookPrefix  string
	}{
		// OnStartup in specified order.
		// onStartup: 1
		{task.GlobalHookRun, OnStartup, "hook02"},
		// onStartup: 10
		{task.GlobalHookRun, OnStartup, "hook03"},
		// onStartup: 20
		{task.GlobalHookRun, OnStartup, "hook01"},
		// EnableSchedule in alphabet order.
		{task.GlobalHookEnableScheduleBindings, "", "hook02"},
		{task.GlobalHookEnableScheduleBindings, "", "hook03"},
		// Synchronization for kubernetes bindings in alphabet order.
		{task.GlobalHookEnableKubernetesBindings, "", "hook01"},
		{task.GlobalHookEnableKubernetesBindings, "", "hook03"},
		{task.GlobalHookWaitKubernetesSynchronization, "", ""},
	}

	i := 0
	op.engine.TaskQueues.GetMain().Iterate(func(tsk sh_task.Task) {
		// Stop checking if no expects left.
		if i >= len(expectTasks) {
			return
		}

		expect := expectTasks[i]
		hm := task.HookMetadataAccessor(tsk)
		g.Expect(tsk.GetType()).To(Equal(expect.taskType), "task type should match for task %d, got %+v %+v", i, tsk, hm)
		g.Expect(hm.BindingType).To(Equal(expect.bindingType), "binding should match for task %d, got %+v %+v", i, tsk, hm)
		g.Expect(hm.HookName).To(HavePrefix(expect.hookPrefix), "hook name should match for task %d, got %+v %+v", i, tsk, hm)
		i++
	})
}

// This test case checks tasks sequence in the 'main' queue during first converge.
// It loads all global hooks and modules, setup wrapper for TaskHandler, and check
// tasks sequence until converge is done.
func Test_Operator_ConvergeModules_main_queue_only(t *testing.T) {
	g := NewWithT(t)
	// Mute messages about registration and tasks queueing.
	log.SetDefaultLevel(log.LevelError)

	op, res := assembleTestAddonOperator(t, "converge__main_queue_only")

	op.BootstrapMainQueue(op.engine.TaskQueues)

	// Fill mocked helm with two releases: one to purge and one to disable during converge process.
	moduleToPurge := "moduleToPurge"
	moduleToDelete := "module-beta"

	res.helmClient.ReleaseNames = []string{moduleToPurge, moduleToDelete}

	type taskInfo struct {
		taskType         sh_task.TaskType
		bindingType      BindingType
		moduleName       string
		hookName         string
		spawnerTaskPhase string
	}

	taskHandleHistory := make([]taskInfo, 0)
	op.engine.TaskQueues.GetMain().WithHandler(func(ctx context.Context, tsk sh_task.Task) queue.TaskResult {
		// Put task info to history.
		hm := task.HookMetadataAccessor(tsk)
		phase := ""
		switch tsk.GetType() {
		case task.ConvergeModules:
			phase = string(op.ConvergeState.Phase)
		case task.ModuleRun:
			phase = string(op.ModuleManager.GetModule(hm.ModuleName).GetPhase())
		}
		taskHandleHistory = append(taskHandleHistory, taskInfo{
			taskType:         tsk.GetType(),
			bindingType:      hm.BindingType,
			moduleName:       hm.ModuleName,
			hookName:         hm.HookName,
			spawnerTaskPhase: phase,
		})

		// Handle it.
		return op.TaskHandler(ctx, tsk)
	})

	op.engine.TaskQueues.StartMain(op.ctx)

	// Wait until converge is done.
	g.Eventually(convergeDone(op), "30s", "200ms").Should(BeTrue())

	// Match history with expected tasks.
	historyExpects := []struct {
		taskType      sh_task.TaskType
		bindingType   BindingType
		namePrefix    string
		convergePhase string
	}{
		// OnStartup in specified order.
		// onStartup: 1
		{task.GlobalHookRun, OnStartup, "hook02", ""},
		// onStartup: 10
		{task.GlobalHookRun, OnStartup, "hook03", ""},
		// onStartup: 20
		{task.GlobalHookRun, OnStartup, "hook01", ""},
		// EnableSchedule in alphabet order.
		{task.GlobalHookEnableScheduleBindings, "", "hook02", ""},
		{task.GlobalHookEnableScheduleBindings, "", "hook03", ""},
		// Synchronization for kubernetes bindings in alphabet order.
		{task.GlobalHookEnableKubernetesBindings, "", "hook01", ""},
		{task.GlobalHookRun, OnKubernetesEvent, "hook01", ""},

		// hook03 has executeForSynchronization false, but GlobalHookRun is present to properly enable events.
		{task.GlobalHookEnableKubernetesBindings, "", "hook03", ""},
		{task.GlobalHookRun, OnKubernetesEvent, "hook03", ""},

		// There are no parallel queues, so wait task runs without repeating.
		{task.GlobalHookWaitKubernetesSynchronization, "", "", ""},

		// TODO DiscoverHelmReleases can add ModulePurge tasks.
		// {task.DiscoverHelmReleases, "", "", ""},
		//  {task.ModulePurge, "", moduleToPurge, ""},

		// ConvergeModules runs after global Synchronization and emerges BeforeAll tasks.
		{task.ConvergeModules, "", "", string(converge.StandBy)},
		{task.GlobalHookRun, BeforeAll, "hook02", ""},
		{task.GlobalHookRun, BeforeAll, "hook01", ""},

		{task.ConvergeModules, "", "", string(converge.WaitBeforeAll)},

		// ConvergeModules adds ModuleDelete and ModuleRun tasks.
		{task.ModuleRun, "", "module-alpha", string(modules.Startup)},
		{task.ModuleRun, "", "module-alpha", string(modules.OnStartupDone)},
		{task.ModuleRun, "", "module-alpha", string(modules.QueueSynchronizationTasks)},
		// {task.ModuleDelete, "", "module-beta", ""},

		// Only one hook with kubernetes binding.
		{task.ModuleHookRun, OnKubernetesEvent, "module-alpha/hook01", ""},
		// {task.ModuleHookRun, OnKubernetesEvent, "module-alpha/hook02", ""},

		// Skip waiting tasks in parallel queues, proceed to schedule bindings.
		{task.ModuleRun, "", "module-alpha", string(modules.EnableScheduleBindings)},
		{task.ModuleRun, "", "module-alpha", string(modules.CanRunHelm)},

		// ConvergeModules emerges afterAll tasks
		{task.ConvergeModules, "", "", string(converge.WaitDeleteAndRunModules)},
		{task.GlobalHookRun, AfterAll, "hook03", ""},

		{task.ConvergeModules, "", "", string(converge.WaitAfterAll)},
	}

	for i, historyInfo := range taskHandleHistory {
		if i >= len(historyExpects) {
			break
		}
		expect := historyExpects[i]
		g.Expect(historyInfo.taskType).To(Equal(expect.taskType), "task type should match for history entry %d, got %+v", i, historyInfo)
		g.Expect(historyInfo.bindingType).To(Equal(expect.bindingType), "binding should match for history entry %d, got %+v", i, historyInfo)
		g.Expect(historyInfo.spawnerTaskPhase).To(Equal(expect.convergePhase), "converge phase should match for history entry %d, got %+v", i, historyInfo)

		switch historyInfo.taskType {
		case task.ModuleRun, task.ModulePurge, task.ModuleDelete:
			g.Expect(historyInfo.moduleName).To(ContainSubstring(expect.namePrefix), "module name should match for history entry %d, got %+v", i, historyInfo)
		case task.GlobalHookRun, task.GlobalHookEnableScheduleBindings, task.GlobalHookEnableKubernetesBindings:
			g.Expect(historyInfo.hookName).To(HavePrefix(expect.namePrefix), "hook name should match for history entry %d, got %+v", i, historyInfo)
		case task.ModuleHookRun:
			parts := strings.Split(expect.namePrefix, "/")
			g.Expect(historyInfo.moduleName).To(ContainSubstring(parts[0]), "module name should match for history entry %d, got %+v", i, historyInfo)
			g.Expect(historyInfo.hookName).To(ContainSubstring("/"+parts[1]), "hook name should match for history entry %d, got %+v", i, historyInfo)
		}
	}
}

// This test case checks tasks sequence in the 'main' queue when
// global section is changed during converge.
func Test_HandleConvergeModules_global_changed_during_converge(t *testing.T) {
	g := NewWithT(t)
	// Mute messages about registration and tasks queueing.
	log.SetDefaultLevel(log.LevelError)

	op, res := assembleTestAddonOperator(t, "converge__main_queue_only")

	// Prefill main queue and start required managers.
	op.BootstrapMainQueue(op.engine.TaskQueues)

	op.KubeConfigManager.Start()
	op.ModuleManager.Start()
	op.StartModuleManagerEventHandler()

	// Define task handler to gather task execution history.
	type taskInfo struct {
		id               string
		taskType         sh_task.TaskType
		bindingType      BindingType
		moduleName       string
		hookName         string
		spawnerTaskPhase string
		convergeEvent    converge.ConvergeEvent
	}

	canChangeConfigMap := make(chan struct{})
	canHandleTasks := make(chan struct{})
	triggerPause := true

	historyMu := new(sync.Mutex)
	taskHandleHistory := make([]taskInfo, 0)
	op.engine.TaskQueues.GetMain().WithHandler(func(ctx context.Context, tsk sh_task.Task) queue.TaskResult {
		// Put task info to history.
		hm := task.HookMetadataAccessor(tsk)
		phase := ""
		var convergeEvent converge.ConvergeEvent
		switch tsk.GetType() {
		case task.ConvergeModules:
			phase = string(op.ConvergeState.Phase)
			convergeEvent = tsk.GetProp(converge.ConvergeEventProp).(converge.ConvergeEvent)
		case task.ModuleRun:
			if triggerPause {
				close(canChangeConfigMap)
				<-canHandleTasks
				triggerPause = false
			}
			phase = string(op.ModuleManager.GetModule(hm.ModuleName).GetPhase())
		}
		historyMu.Lock()
		taskHandleHistory = append(taskHandleHistory, taskInfo{
			id:               tsk.GetId(),
			taskType:         tsk.GetType(),
			bindingType:      hm.BindingType,
			moduleName:       hm.ModuleName,
			hookName:         hm.HookName,
			spawnerTaskPhase: phase,
			convergeEvent:    convergeEvent,
		})
		historyMu.Unlock()

		// Handle it.
		return op.TaskHandler(ctx, tsk)
	})

	// Start 'main' queue and wait for first converge.
	op.engine.TaskQueues.StartMain(op.ctx)

	// Emulate changing ConfigMap during converge.
	go func() {
		<-canChangeConfigMap
		// Trigger global changes via KubeConfigManager.
		globalValuesChangePatch := `[{"op": "add", 
"path": "/data/global",
"value": "param: newValue"}]`

		cmPatched, err := op.engine.KubeClient.CoreV1().ConfigMaps(res.cmNamespace).Patch(context.TODO(),
			res.cmName,
			k8types.JSONPatchType,
			[]byte(globalValuesChangePatch),
			metav1.PatchOptions{},
		)
		g.Expect(err).ShouldNot(HaveOccurred(), "ConfigMap should be patched")
		g.Expect(cmPatched).ShouldNot(BeNil())
		g.Expect(cmPatched.Data).Should(HaveKey("global"))
		g.Expect(cmPatched.Data["global"]).Should(Equal("param: newValue"))
		close(canHandleTasks)
	}()

	g.Eventually(convergeDone(op), "30s", "200ms").Should(BeTrue())

	hasReloadAllInStandby := false
	for i, tsk := range taskHandleHistory {
		// if i < ignoreTasksCount {
		//	continue
		//}
		if tsk.taskType != task.ApplyKubeConfigValues {
			continue
		}

		g.Expect(len(taskHandleHistory) > i+1).Should(BeTrue(), "history should not end on ApplyKubeConfigValues")
		next := taskHandleHistory[i+1]
		g.Expect(next.convergeEvent).Should(Equal(converge.ReloadAllModules))
		g.Expect(next.spawnerTaskPhase).Should(Equal(string(converge.StandBy)))
		hasReloadAllInStandby = true
		break
	}

	g.Expect(hasReloadAllInStandby).To(BeTrue(), "Should have ReloadAllModules right after ApplyKubeConfigValues")
}

// This test case checks tasks sequence in the 'main' queue after changing
// global section in the config map.
func Test_HandleConvergeModules_global_changed(t *testing.T) {
	g := NewWithT(t)
	// Mute messages about registration and tasks queueing.
	log.SetDefaultLevel(log.LevelError)

	op, res := assembleTestAddonOperator(t, "converge__main_queue_only")

	op.BootstrapMainQueue(op.engine.TaskQueues)

	op.KubeConfigManager.Start()
	op.ModuleManager.Start()
	op.StartModuleManagerEventHandler()

	type taskInfo struct {
		taskType         sh_task.TaskType
		bindingType      BindingType
		moduleName       string
		hookName         string
		spawnerTaskPhase string
		convergeEvent    converge.ConvergeEvent
	}

	historyMu := new(sync.Mutex)
	taskHandleHistory := make([]taskInfo, 0)
	op.engine.TaskQueues.GetMain().WithHandler(func(ctx context.Context, tsk sh_task.Task) queue.TaskResult {
		// Put task info to history.
		hm := task.HookMetadataAccessor(tsk)
		phase := ""
		var convergeEvent converge.ConvergeEvent
		switch tsk.GetType() {
		case task.ApplyKubeConfigValues:
			phase = string(op.ConvergeState.Phase)
		case task.ConvergeModules:
			phase = string(op.ConvergeState.Phase)
			convergeEvent = tsk.GetProp(converge.ConvergeEventProp).(converge.ConvergeEvent)
		case task.ModuleRun:
			phase = string(op.ModuleManager.GetModule(hm.ModuleName).GetPhase())
		}
		historyMu.Lock()
		taskHandleHistory = append(taskHandleHistory, taskInfo{
			taskType:         tsk.GetType(),
			bindingType:      hm.BindingType,
			moduleName:       hm.ModuleName,
			hookName:         hm.HookName,
			spawnerTaskPhase: phase,
			convergeEvent:    convergeEvent,
		})
		historyMu.Unlock()

		// Handle it.
		return op.TaskHandler(ctx, tsk)
	})

	op.engine.TaskQueues.StartMain(op.ctx)

	g.Eventually(convergeDone(op), "30s", "200ms").Should(BeTrue())

	log.Info("Converge done, got tasks in history",
		slog.Int("count", len(taskHandleHistory)))

	// Save current history length to ignore first converge tasks later.
	ignoreTasksCount := len(taskHandleHistory)

	// Trigger global changes via KubeConfigManager.
	globalValuesChangePatch := `[{"op": "add", 
"path": "/data/global",
"value": "param: newValue"}]`

	cmPatched, err := op.engine.KubeClient.CoreV1().ConfigMaps(res.cmNamespace).Patch(context.TODO(),
		res.cmName,
		k8types.JSONPatchType,
		[]byte(globalValuesChangePatch),
		metav1.PatchOptions{},
	)
	g.Expect(err).ShouldNot(HaveOccurred(), "ConfigMap should be patched")
	g.Expect(cmPatched).ShouldNot(BeNil())
	g.Expect(cmPatched.Data).Should(HaveKey("global"))
	g.Expect(cmPatched.Data["global"]).Should(Equal("param: newValue"))

	log.Info("ConfigMap patched, got tasks in history",
		slog.Int("count", len(taskHandleHistory)))

	// Expect ConvergeModules appears in queue.
	g.Eventually(func() bool {
		historyMu.Lock()
		defer historyMu.Unlock()
		for i, tsk := range taskHandleHistory {
			if i < ignoreTasksCount {
				continue
			}
			if tsk.taskType == task.ApplyKubeConfigValues {
				return true
			}
			continue
		}
		return false
	}, "30s", "200ms").Should(BeTrue(), "Should queue ConvergeModules task after changing global section in ConfigMap")

	// Expect ConvergeModules/ReloadAllModules appears in queue.
	g.Eventually(func() bool {
		historyMu.Lock()
		defer historyMu.Unlock()
		for i, tsk := range taskHandleHistory {
			if i < ignoreTasksCount {
				continue
			}
			if tsk.taskType != task.ConvergeModules {
				continue
			}
			if tsk.convergeEvent == converge.ReloadAllModules {
				return true
			}
		}
		return false
	}, "30s", "200ms").Should(BeTrue(), "Should queue ReloadAllModules task after changing global section in ConfigMap")
}

// TODO: check test
// Test task flow logging:
//   - ensure no messages about WaitForSynchronization
//   - log_task__wait_for_synchronization contains a global hook and a module hook
//     that use separate queue to execute and require waiting for Synchronization
// func Test_Operator_logTask(t *testing.T) {
// 	g := NewWithT(t)

// 	// Catch all info messages.
// 	log.SetDefaultLevel(log.LevelError)
// 	log.SetOutput(io.Discard)
// 	logHook := new(logrus_test.Hook)
// 	log.AddHook(logHook)

// 	op, _ := assembleTestAddonOperator(t, "log_task__wait_for_synchronization")
// 	op.BootstrapMainQueue(op.engine.TaskQueues)
// 	op.engine.TaskQueues.StartMain()
// 	op.CreateAndStartQueuesForGlobalHooks()

// 	// Wait until converge is done.
// 	g.Eventually(convergeDone(op), "30s", "200ms").Should(BeTrue())

// 	g.Expect(len(logHook.Entries) > 0).Should(BeTrue())

// 	hasWaitForSynchronizationMessages := false
// 	for _, entry := range logHook.Entries {
// 		if strings.Contains(entry.Message, "WaitForSynchronization") && entry.Level < log.Level {
// 			hasWaitForSynchronizationMessages = true
// 		}
// 	}
// 	logHook.Reset()

// 	g.Expect(hasWaitForSynchronizationMessages).Should(BeFalse(), "should not log messages about WaitForSynchronization")
// }
