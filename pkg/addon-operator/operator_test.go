package addon_operator

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/flant/kube-client/fake"
	. "github.com/flant/shell-operator/pkg/hook/types"
	shell_operator "github.com/flant/shell-operator/pkg/shell-operator"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
	file_utils "github.com/flant/shell-operator/pkg/utils/file"
	. "github.com/onsi/gomega"
	log "github.com/sirupsen/logrus"
	logrus_test "github.com/sirupsen/logrus/hooks/test"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8types "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"

	mockhelm "github.com/flant/addon-operator/pkg/helm/test/mock"
	mockhelmresmgr "github.com/flant/addon-operator/pkg/helm_resources_manager/test/mock"
	. "github.com/flant/addon-operator/pkg/hook/types"
	"github.com/flant/addon-operator/pkg/kube_config_manager"
	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/task"
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
	if exists, _ := file_utils.DirExists(modulesDir); !exists {
		modulesDir = ""
	}
	globalHooksDir := filepath.Join(rootDir, "global-hooks")
	if exists, _ := file_utils.DirExists(globalHooksDir); !exists {
		globalHooksDir = ""
	}
	if globalHooksDir == "" {
		globalHooksDir = filepath.Join(rootDir, "global")
		if exists, _ := file_utils.DirExists(globalHooksDir); !exists {
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
	op := NewAddonOperator()
	op.WithContext(context.Background())
	op.KubeClient = kubeClient
	// Mock helm client for ModuleManager
	result.helmClient = &mockhelm.Client{}
	op.Helm = mockhelm.NewClientFactory(result.helmClient)
	// Mock helm resources manager to execute module actions: run, delete.
	result.helmResourcesManager = &mockhelmresmgr.MockHelmResourcesManager{}
	op.HelmResourcesManager = result.helmResourcesManager

	shell_operator.SetupEventManagers(op.ShellOperator)

	op.KubeConfigManager = kube_config_manager.NewKubeConfigManager()
	op.KubeConfigManager.WithKubeClient(op.KubeClient)
	op.KubeConfigManager.WithContext(op.ctx)
	op.KubeConfigManager.WithNamespace(result.cmNamespace)
	op.KubeConfigManager.WithConfigMapName(result.cmName)

	op.ModuleManager = module_manager.NewModuleManager()
	op.ModuleManager.WithContext(op.ctx)
	op.ModuleManager.WithDirectories(modulesDir, globalHooksDir, t.TempDir())
	op.ModuleManager.WithKubeConfigManager(op.KubeConfigManager)
	op.ModuleManager.WithHelm(op.Helm)
	op.ModuleManager.WithScheduleManager(op.ScheduleManager)
	op.ModuleManager.WithKubeEventManager(op.KubeEventsManager)
	op.ModuleManager.WithHelmResourcesManager(op.HelmResourcesManager)

	err = op.InitModuleManager()
	g.Expect(err).ShouldNot(HaveOccurred(), "Should init ModuleManager")

	return op, result
}

func convergeDone(op *AddonOperator) func(g Gomega) bool {
	return func(g Gomega) bool {
		if op.IsStartupConvergeDone() {
			return true
		}
		mainQueue := op.TaskQueues.GetMain()
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
	log.SetLevel(log.ErrorLevel)

	op, _ := assembleTestAddonOperator(t, "startup_tasks")

	op.BootstrapMainQueue(op.TaskQueues)

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
	op.TaskQueues.GetMain().Iterate(func(tsk sh_task.Task) {
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
	log.SetLevel(log.ErrorLevel)

	op, res := assembleTestAddonOperator(t, "converge__main_queue_only")
	op.BootstrapMainQueue(op.TaskQueues)

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
	op.TaskQueues.GetMain().WithHandler(func(tsk sh_task.Task) queue.TaskResult {
		// Put task info to history.
		hm := task.HookMetadataAccessor(tsk)
		phase := ""
		switch tsk.GetType() {
		case task.ConvergeModules:
			phase = string(op.ConvergeState.Phase)
		case task.ModuleRun:
			phase = string(op.ModuleManager.GetModule(hm.ModuleName).State.Phase)
		}
		taskHandleHistory = append(taskHandleHistory, taskInfo{
			taskType:         tsk.GetType(),
			bindingType:      hm.BindingType,
			moduleName:       hm.ModuleName,
			hookName:         hm.HookName,
			spawnerTaskPhase: phase,
		})

		// Handle it.
		return op.TaskHandler(tsk)
	})

	op.TaskQueues.StartMain()

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
		{task.DiscoverHelmReleases, "", "", ""},
		{task.ModulePurge, "", moduleToPurge, ""},

		// ConvergeModules runs after global Synchronization and emerges BeforeAll tasks.
		{task.ConvergeModules, "", "", string(StandBy)},
		{task.GlobalHookRun, BeforeAll, "hook02", ""},
		{task.GlobalHookRun, BeforeAll, "hook01", ""},

		{task.ConvergeModules, "", "", string(WaitBeforeAll)},

		// ConvergeModules adds ModuleDelete and ModuleRun tasks.
		{task.ModuleDelete, "", "module-beta", ""},

		{task.ModuleRun, "", "module-alpha", string(module_manager.Startup)},

		// Only one hook with kubernetes binding.
		{task.ModuleHookRun, OnKubernetesEvent, "module-alpha/hook01", ""},
		//{task.ModuleHookRun, OnKubernetesEvent, "module-alpha/hook02", ""},

		// Skip waiting tasks in parallel queues, proceed to schedule bindings.
		{task.ModuleRun, "", "module-alpha", string(module_manager.EnableScheduleBindings)},

		// ConvergeModules emerges afterAll tasks
		{task.ConvergeModules, "", "", string(WaitDeleteAndRunModules)},
		{task.GlobalHookRun, AfterAll, "hook03", ""},

		{task.ConvergeModules, "", "", string(WaitAfterAll)},
	}

	for i, historyInfo := range taskHandleHistory {
		if i >= len(historyExpects) {
			break
		}
		expect := historyExpects[i]
		g.Expect(historyInfo.taskType).To(Equal(expect.taskType), "task type should match for history entry %d, got %+v %+v", i, historyInfo)
		g.Expect(historyInfo.bindingType).To(Equal(expect.bindingType), "binding should match for history entry %d, got %+v %+v", i, historyInfo)
		g.Expect(historyInfo.spawnerTaskPhase).To(Equal(expect.convergePhase), "converge phase should match for history entry %d, got %+v %+v", i, historyInfo)

		switch historyInfo.taskType {
		case task.ModuleRun, task.ModulePurge, task.ModuleDelete:
			g.Expect(historyInfo.moduleName).To(ContainSubstring(expect.namePrefix), "module name should match for history entry %d, got %+v %+v", i, historyInfo)
		case task.GlobalHookRun, task.GlobalHookEnableScheduleBindings, task.GlobalHookEnableKubernetesBindings:
			g.Expect(historyInfo.hookName).To(HavePrefix(expect.namePrefix), "hook name should match for history entry %d, got %+v %+v", i, historyInfo)
		case task.ModuleHookRun:
			parts := strings.Split(expect.namePrefix, "/")
			g.Expect(historyInfo.moduleName).To(ContainSubstring(parts[0]), "module name should match for history entry %d, got %+v %+v", i, historyInfo)
			g.Expect(historyInfo.hookName).To(ContainSubstring("/"+parts[1]), "hook name should match for history entry %d, got %+v %+v", i, historyInfo)
		}
	}
}

// This test case checks tasks sequence in the 'main' queue when
// global section is changed during converge.
func Test_HandleConvergeModules_global_changed_during_converge(t *testing.T) {
	g := NewWithT(t)
	// Mute messages about registration and tasks queueing.
	log.SetLevel(log.ErrorLevel)

	op, res := assembleTestAddonOperator(t, "converge__main_queue_only")

	// Prefill main queue and start required managers.
	op.BootstrapMainQueue(op.TaskQueues)

	op.KubeConfigManager.Start()
	op.ModuleManager.Start()
	op.StartModuleManagerEventHandler()

	// Define task handler to gather task execution history.
	type taskInfo struct {
		taskType         sh_task.TaskType
		bindingType      BindingType
		moduleName       string
		hookName         string
		spawnerTaskPhase string
		convergeEvent    ConvergeEvent
	}

	canChangeConfigMap := make(chan struct{})
	canHandleTasks := make(chan struct{})
	triggerPause := true

	historyMu := new(sync.Mutex)
	taskHandleHistory := make([]taskInfo, 0)
	op.TaskQueues.GetMain().WithHandler(func(tsk sh_task.Task) queue.TaskResult {
		// Put task info to history.
		hm := task.HookMetadataAccessor(tsk)
		phase := ""
		var convergeEvent ConvergeEvent
		switch tsk.GetType() {
		case task.ConvergeModules:
			phase = string(op.ConvergeState.Phase)
			convergeEvent = tsk.GetProp(ConvergeEventProp).(ConvergeEvent)
		case task.ModuleRun:
			if triggerPause {
				close(canChangeConfigMap)
				<-canHandleTasks
				triggerPause = false
			}
			phase = string(op.ModuleManager.GetModule(hm.ModuleName).State.Phase)
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
		return op.TaskHandler(tsk)
	})

	// Start 'main' queue and wait for first converge.
	op.TaskQueues.StartMain()

	// Emulate changing ConfigMap during converge.
	go func() {
		<-canChangeConfigMap
		// Trigger global changes via KubeConfigManager.
		globalValuesChangePatch := `[{"op": "add", 
"path": "/data/global",
"value": "param: newValue"}]`

		cmPatched, err := op.KubeClient.CoreV1().ConfigMaps(res.cmNamespace).Patch(context.TODO(),
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
		//if i < ignoreTasksCount {
		//	continue
		//}
		if tsk.taskType != task.ConvergeModules {
			continue
		}
		if tsk.convergeEvent == KubeConfigChanged {
			g.Expect(len(taskHandleHistory) > i+1).Should(BeTrue(), "history should not ends on KubeConfigChanged")
			next := taskHandleHistory[i+1]
			g.Expect(next.convergeEvent).Should(Equal(ReloadAllModules))
			g.Expect(next.spawnerTaskPhase).Should(Equal(string(StandBy)))
			hasReloadAllInStandby = true
			break
		}
	}

	g.Expect(hasReloadAllInStandby).To(BeTrue(), "Should have ReloadAllModules right after KubeConfigChanged")
}

// This test case checks tasks sequence in the 'main' queue after changing
// global section in the config map.
func Test_HandleConvergeModules_global_changed(t *testing.T) {
	g := NewWithT(t)
	// Mute messages about registration and tasks queueing.
	log.SetLevel(log.ErrorLevel)

	op, res := assembleTestAddonOperator(t, "converge__main_queue_only")

	op.BootstrapMainQueue(op.TaskQueues)

	op.KubeConfigManager.Start()
	op.ModuleManager.Start()
	op.StartModuleManagerEventHandler()

	type taskInfo struct {
		taskType         sh_task.TaskType
		bindingType      BindingType
		moduleName       string
		hookName         string
		spawnerTaskPhase string
		convergeEvent    ConvergeEvent
	}

	historyMu := new(sync.Mutex)
	taskHandleHistory := make([]taskInfo, 0)
	op.TaskQueues.GetMain().WithHandler(func(tsk sh_task.Task) queue.TaskResult {
		// Put task info to history.
		hm := task.HookMetadataAccessor(tsk)
		phase := ""
		var convergeEvent ConvergeEvent
		switch tsk.GetType() {
		case task.ConvergeModules:
			phase = string(op.ConvergeState.Phase)
			convergeEvent = tsk.GetProp(ConvergeEventProp).(ConvergeEvent)
		case task.ModuleRun:
			phase = string(op.ModuleManager.GetModule(hm.ModuleName).State.Phase)
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
		return op.TaskHandler(tsk)
	})

	op.TaskQueues.StartMain()

	g.Eventually(convergeDone(op), "30s", "200ms").Should(BeTrue())

	log.Infof("Converge done, got %d tasks in history", len(taskHandleHistory))

	// Save current history length to ignore first converge tasks later.
	ignoreTasksCount := len(taskHandleHistory)

	// Trigger global changes via KubeConfigManager.
	globalValuesChangePatch := `[{"op": "add", 
"path": "/data/global",
"value": "param: newValue"}]`

	cmPatched, err := op.KubeClient.CoreV1().ConfigMaps(res.cmNamespace).Patch(context.TODO(),
		res.cmName,
		k8types.JSONPatchType,
		[]byte(globalValuesChangePatch),
		metav1.PatchOptions{},
	)
	g.Expect(err).ShouldNot(HaveOccurred(), "ConfigMap should be patched")
	g.Expect(cmPatched).ShouldNot(BeNil())
	g.Expect(cmPatched.Data).Should(HaveKey("global"))
	g.Expect(cmPatched.Data["global"]).Should(Equal("param: newValue"))

	log.Infof("ConfigMap patched, got %d tasks in history", len(taskHandleHistory))

	// Expect ConvergeModules appears in queue.
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
			if tsk.convergeEvent == KubeConfigChanged {
				return true
			}
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
			if tsk.convergeEvent == ReloadAllModules {
				return true
			}
		}
		return false
	}, "30s", "200ms").Should(BeTrue(), "Should queue ReloadAllModules task after changing global section in ConfigMap")
}

// Test task flow logging:
//   - ensure no messages about WaitForSynchronization
//   - log_task__wait_for_synchronization contains a global hook and a module hook
//     that use separate queue to execute and require waiting for Synchronization
func Test_Operator_logTask(t *testing.T) {
	g := NewWithT(t)

	// Catch all info messages.
	log.SetLevel(log.InfoLevel)
	log.SetOutput(io.Discard)
	logHook := new(logrus_test.Hook)
	log.AddHook(logHook)

	op, _ := assembleTestAddonOperator(t, "log_task__wait_for_synchronization")
	op.BootstrapMainQueue(op.TaskQueues)
	op.TaskQueues.StartMain()
	op.CreateAndStartQueuesForGlobalHooks()

	// Wait until converge is done.
	g.Eventually(convergeDone(op), "30s", "200ms").Should(BeTrue())

	g.Expect(len(logHook.Entries) > 0).Should(BeTrue())

	hasWaitForSynchronizationMessages := false
	for _, entry := range logHook.Entries {
		if strings.Contains(entry.Message, "WaitForSynchronization") && entry.Level < log.DebugLevel {
			hasWaitForSynchronizationMessages = true
		}
	}
	logHook.Reset()

	g.Expect(hasWaitForSynchronizationMessages).Should(BeFalse(), "should not log messages about WaitForSynchronization")
}
