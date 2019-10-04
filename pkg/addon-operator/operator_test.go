package addon_operator

import (
	"testing"

	"github.com/stretchr/testify/assert"

	hook2 "github.com/flant/shell-operator/pkg/hook"
	"github.com/flant/shell-operator/pkg/metrics_storage"

	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/task"
)


// CreateOnStartupTasks fills a working queue with onStartup hooks.
// TaskRunner should run all hooks and clean a queue.
func Test_Operator_CreateOnStartupTasks_TaskRunner(t *testing.T) {
	MetricsStorage = metrics_storage.Init()
	var globalHook1 = &module_manager.GlobalHook{
		CommonHook: &module_manager.CommonHook{
			Name: "hook-global-1",
		},
		Config: &module_manager.GlobalHookConfig{
			HookConfig: hook2.HookConfig{
				OnStartup: &hook2.OnStartupConfig{
					Order: 10,
				},
			},
		},
	}

	var globalHook2 = &module_manager.GlobalHook{
		CommonHook: &module_manager.CommonHook{
			Name: "hook-global-2",
		},
		Config: &module_manager.GlobalHookConfig{
			HookConfig: hook2.HookConfig{
				OnStartup: &hook2.OnStartupConfig{
					Order: 10,
				},
			},
		},
	}

	var globalHooksMock = map[string]*module_manager.GlobalHook{
		"hook-global-1": globalHook1,
		"hook-global-2": globalHook2,
	}

	var hookRun = struct{
		hookGlobal1 bool
		hookGlobal2 bool
	}{}

	// Mock ModuleManager
	ModuleManager = module_manager.NewModuleManagerMock(module_manager.ModuleManagerMockFns{
		GetGlobalHooksInOrder: func(bindingType module_manager.BindingType) []string {
			res := []string{}
			for k := range globalHooksMock{
				res = append(res, k)
			}
			return res
		},
		GetGlobalHook: func(name string) (hook *module_manager.GlobalHook, e error) {
			return globalHooksMock[name], nil
		},
		RunGlobalHook: func(hookName string, binding module_manager.BindingType, bindingContext []module_manager.BindingContext) error {
			switch hookName {
			case "hook-global-1":
				hookRun.hookGlobal1 = true
			case "hook-global-2":
				hookRun.hookGlobal2 = true
			}
			return nil
		},
	})

	// Fill a queue with OnStartup global hooks
	TasksQueue = task.NewTasksQueue()
	TasksQueue.ChangesEnable(true)

	// Add StartupTasks
	CreateOnStartupTasks()

	expectedCount := len(ModuleManager.GetGlobalHooksInOrder(module_manager.OnStartup))
	assert.Equal(t, expectedCount, TasksQueue.Length(), "queue length is not equal to count of global 'OnStartup' hooks")

	// add stop task
	stopTask := task.NewTask(task.Stop, "stop runner")
	TasksQueue.Add(stopTask)

	TasksRunner()

	assert.True(t, hookRun.hookGlobal1)
	assert.True(t, hookRun.hookGlobal2)
	assert.Equalf(t, 0, TasksQueue.Length(), "%d tasks remain in queue after TasksRunner", TasksQueue.Length())
}
