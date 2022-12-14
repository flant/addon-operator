package addon_operator

import (
	"testing"

	"github.com/flant/addon-operator/pkg/task"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
	"github.com/stretchr/testify/require"
)

func Test_QueueHasPendingModuleRunTask(t *testing.T) {
	tests := []struct {
		name   string
		result bool
		queue  func() *queue.TaskQueue
	}{
		{
			name:   "Normal",
			result: true,
			queue: func() *queue.TaskQueue {
				q := queue.NewTasksQueue()

				Task := &sh_task.BaseTask{Type: task.ModuleRun, Id: "unknown"}
				q.AddLast(Task.WithMetadata(task.HookMetadata{ModuleName: "unknown"}))

				Task = &sh_task.BaseTask{Type: task.ModuleRun, Id: "unknown"}
				q.AddLast(Task.WithMetadata(task.HookMetadata{ModuleName: "unknown"}))

				Task = &sh_task.BaseTask{Type: task.ModuleRun, Id: "test"}
				q.AddLast(Task.WithMetadata(task.HookMetadata{ModuleName: "test"}))
				return q
			},
		},
		{
			name:   "First task",
			result: false,
			queue: func() *queue.TaskQueue {
				q := queue.NewTasksQueue()

				Task := &sh_task.BaseTask{Type: task.ModuleRun, Id: "test"}
				q.AddLast(Task.WithMetadata(task.HookMetadata{ModuleName: "test"}))

				Task = &sh_task.BaseTask{Type: task.GlobalHookRun, Id: "unknown"}
				q.AddLast(Task.WithMetadata(task.HookMetadata{ModuleName: "unknown"}))

				Task = &sh_task.BaseTask{Type: task.ModuleRun, Id: "unknown"}
				q.AddLast(Task.WithMetadata(task.HookMetadata{ModuleName: "unknown"}))
				return q
			},
		},
		{
			name:   "No module run",
			result: false,
			queue: func() *queue.TaskQueue {
				q := queue.NewTasksQueue()

				Task := &sh_task.BaseTask{Type: task.ModuleRun, Id: "unknown"}
				q.AddLast(Task.WithMetadata(task.HookMetadata{ModuleName: "unknown"}))

				Task = &sh_task.BaseTask{Type: task.ModuleHookRun, Id: "test"}
				q.AddLast(Task.WithMetadata(task.HookMetadata{ModuleName: "test"}))

				Task = &sh_task.BaseTask{Type: task.ModuleRun, Id: "unknown"}
				q.AddLast(Task.WithMetadata(task.HookMetadata{ModuleName: "unknown"}))
				return q
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := QueueHasPendingModuleRunTask(tt.queue(), "test")
			require.Equal(t, tt.result, result, "QueueHasPendingModuleRunTask should run correctly")
		})
	}
}

func Test_RemoveAdjacentConvergeModules(t *testing.T) {
	tests := []struct {
		name    string
		afterID string
		in      []sh_task.BaseTask
		expect  []sh_task.BaseTask
	}{
		{
			name:    "No adjacent ConvergeModules",
			afterID: "1",
			in: []sh_task.BaseTask{
				{Type: task.ConvergeModules, Id: "1"},
				{Type: task.ModuleRun, Id: "2"},
				{Type: task.GlobalHookRun, Id: "3"},
			},
			expect: []sh_task.BaseTask{
				{Type: task.ConvergeModules, Id: "1"},
				{Type: task.ModuleRun, Id: "2"},
				{Type: task.GlobalHookRun, Id: "3"},
			},
		},
		{
			name:    "No adjacent ConvergeModules, preceding tasks present",
			afterID: "1",
			in: []sh_task.BaseTask{
				{Type: task.ConvergeModules, Id: "-1"},
				{Type: task.ConvergeModules, Id: "0"},
				{Type: task.ConvergeModules, Id: "1"},
				{Type: task.ModuleRun, Id: "2"},
				{Type: task.GlobalHookRun, Id: "3"},
			},
			expect: []sh_task.BaseTask{
				{Type: task.ConvergeModules, Id: "-1"},
				{Type: task.ConvergeModules, Id: "0"},
				{Type: task.ConvergeModules, Id: "1"},
				{Type: task.ModuleRun, Id: "2"},
				{Type: task.GlobalHookRun, Id: "3"},
			},
		},
		{
			name:    "Single adjacent ConvergeModules task",
			afterID: "1",
			in: []sh_task.BaseTask{
				{Type: task.ConvergeModules, Id: "1"},
				{Type: task.ConvergeModules, Id: "2"},
				{Type: task.ModuleRun, Id: "3"},
				{Type: task.GlobalHookRun, Id: "4"},
			},
			expect: []sh_task.BaseTask{
				{Type: task.ConvergeModules, Id: "1"},
				{Type: task.ModuleRun, Id: "3"},
				{Type: task.GlobalHookRun, Id: "4"},
			},
		},
		{
			name:    "Multiple adjacent ConvergeModules tasks",
			afterID: "1",
			in: []sh_task.BaseTask{
				{Type: task.ConvergeModules, Id: "1"},
				{Type: task.ConvergeModules, Id: "2"},
				{Type: task.ConvergeModules, Id: "3"},
				{Type: task.ConvergeModules, Id: "4"},
				{Type: task.ModuleRun, Id: "5"},
				{Type: task.GlobalHookRun, Id: "6"},
			},
			expect: []sh_task.BaseTask{
				{Type: task.ConvergeModules, Id: "1"},
				{Type: task.ModuleRun, Id: "5"},
				{Type: task.GlobalHookRun, Id: "6"},
			},
		},
		{
			name:    "Interleaving ConvergeModules tasks",
			afterID: "1",
			in: []sh_task.BaseTask{
				{Type: task.ConvergeModules, Id: "1"},
				{Type: task.ConvergeModules, Id: "2"},
				{Type: task.ModuleRun, Id: "3"},
				{Type: task.ConvergeModules, Id: "4"},
				{Type: task.ConvergeModules, Id: "5"},
				{Type: task.GlobalHookRun, Id: "6"},
			},
			expect: []sh_task.BaseTask{
				{Type: task.ConvergeModules, Id: "1"},
				{Type: task.ModuleRun, Id: "3"},
				{Type: task.ConvergeModules, Id: "4"},
				{Type: task.ConvergeModules, Id: "5"},
				{Type: task.GlobalHookRun, Id: "6"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := queue.NewTasksQueue()
			for _, tsk := range tt.in {
				tmpTsk := tsk
				q.AddLast(&tmpTsk)
			}
			require.Equal(t, len(tt.in), q.Length(), "Should add all tasks to the queue.")

			RemoveAdjacentConvergeModules(q, tt.afterID)

			// Check tasks after remove.
			require.Equal(t, len(tt.expect), q.Length(), "queue length should match length of expected tasks")
			i := 0
			q.Iterate(func(tsk sh_task.Task) {
				require.Equal(t, tt.expect[i].Id, tsk.GetId(), "ID should match for task %d %+v", i, tsk)
				require.Equal(t, tt.expect[i].Type, tsk.GetType(), "Type should match for task %d %+v", i, tsk)
				i++
			})
		})
	}
}

func Test_ModulesWithPendingModuleRun(t *testing.T) {
	moduleRunTask := func(id string, moduleName string) *sh_task.BaseTask {
		tsk := &sh_task.BaseTask{Type: task.ModuleRun, Id: id}
		return tsk.WithMetadata(task.HookMetadata{
			ModuleName: moduleName,
		})
	}

	tests := []struct {
		name    string
		afterID string
		in      []*sh_task.BaseTask
		expect  map[string]struct{}
	}{
		{
			name:    "No ModuleRun tasks",
			afterID: "1",
			in: []*sh_task.BaseTask{
				{Type: task.ConvergeModules, Id: "1"},
				{Type: task.ModuleHookRun, Id: "2"},
				{Type: task.GlobalHookRun, Id: "3"},
			},
			expect: map[string]struct{}{},
		},
		{
			name:    "First task is ModuleRun",
			afterID: "1",
			in: []*sh_task.BaseTask{
				moduleRunTask("1", "module_1"),
				{Type: task.ModuleHookRun, Id: "2"},
				{Type: task.GlobalHookRun, Id: "3"},
			},
			expect: map[string]struct{}{},
		},
		{
			name:    "One pending ModuleRun",
			afterID: "1",
			in: []*sh_task.BaseTask{
				{Type: task.GlobalHookRun, Id: "1"},
				moduleRunTask("2", "module_1"),
				{Type: task.GlobalHookRun, Id: "3"},
			},
			expect: map[string]struct{}{
				"module_1": {},
			},
		},
		{
			name:    "Multiple ModuleRun tasks",
			afterID: "1",
			in: []*sh_task.BaseTask{
				{Type: task.GlobalHookRun, Id: "1"},
				moduleRunTask("2", "module_1"),
				{Type: task.ConvergeModules, Id: "3"},
				moduleRunTask("4", "module_2"),
				{Type: task.GlobalHookRun, Id: "5"},
			},
			expect: map[string]struct{}{
				"module_1": {},
				"module_2": {},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := queue.NewTasksQueue()
			for _, tsk := range tt.in {
				q.AddLast(tsk)
			}
			require.Equal(t, len(tt.in), q.Length(), "Should add all tasks to the queue.")

			actual := ModulesWithPendingModuleRun(q)

			// Check tasks after remove.
			require.Equal(t, len(tt.expect), len(actual), "Should match length of expected modules")
			require.Equal(t, tt.expect, actual, "Should match expected modules")
		})
	}
}

func Test_RemoveCurrentConvergeTasks(t *testing.T) {
	const currentTaskID = "1"
	tests := []struct {
		name          string
		initialTasks  []sh_task.BaseTask
		expectTasks   []sh_task.BaseTask
		expectRemoved bool
	}{
		{
			name: "No Converge tasks",
			initialTasks: []sh_task.BaseTask{
				{Type: task.ConvergeModules, Id: currentTaskID},
				{Type: task.ModuleHookRun, Id: "2"},
				{Type: task.GlobalHookRun, Id: "3"},
			},
			expectTasks: []sh_task.BaseTask{
				{Type: task.ConvergeModules, Id: "1"},
				{Type: task.ModuleHookRun, Id: "2"},
				{Type: task.GlobalHookRun, Id: "3"},
			},
		},
		{
			name: "No Converge in progress, preceding tasks present",
			initialTasks: []sh_task.BaseTask{
				{Type: task.ConvergeModules, Id: "-1"},
				{Type: task.ConvergeModules, Id: "0"},
				{Type: task.ConvergeModules, Id: currentTaskID},
				{Type: task.ModuleHookRun, Id: "2"},
				{Type: task.GlobalHookRun, Id: "3"},
			},
			expectTasks: []sh_task.BaseTask{
				{Type: task.ConvergeModules, Id: "-1"},
				{Type: task.ConvergeModules, Id: "0"},
				{Type: task.ConvergeModules, Id: currentTaskID},
				{Type: task.ModuleHookRun, Id: "2"},
				{Type: task.GlobalHookRun, Id: "3"},
			},
		},
		{
			name: "Single adjacent ConvergeModules task with more Converge tasks",
			initialTasks: []sh_task.BaseTask{
				{Type: task.ConvergeModules, Id: currentTaskID},
				{Type: task.ConvergeModules, Id: "2"},
				{Type: task.ModuleRun, Id: "3"},
				{Type: task.ModuleDelete, Id: "4"},
			},
			expectTasks: []sh_task.BaseTask{
				{Type: task.ConvergeModules, Id: currentTaskID},
				{Type: task.ModuleRun, Id: "3"},
				{Type: task.ModuleDelete, Id: "4"},
			},
			expectRemoved: true,
		},
		{
			name: "Converge in progress",
			initialTasks: []sh_task.BaseTask{
				{Type: task.ConvergeModules, Id: currentTaskID},
				{Type: task.ModuleDelete, Id: "2"},
				{Type: task.ModuleDelete, Id: "3"},
				{Type: task.ModuleRun, Id: "4", Metadata: task.HookMetadata{IsReloadAll: true}},
				{Type: task.ModuleRun, Id: "5", Metadata: task.HookMetadata{IsReloadAll: true}},
				{Type: task.ModuleRun, Id: "6", Metadata: task.HookMetadata{IsReloadAll: true}},
				{Type: task.ConvergeModules, Id: "7"},
				{Type: task.ConvergeModules, Id: "8"},
				{Type: task.ConvergeModules, Id: "9"},
				{Type: task.ModuleRun, Id: "11"},
				{Type: task.GlobalHookRun, Id: "10"},
			},
			expectTasks: []sh_task.BaseTask{
				{Type: task.ConvergeModules, Id: currentTaskID},
				{Type: task.ConvergeModules, Id: "8"},
				{Type: task.ConvergeModules, Id: "9"},
				{Type: task.ModuleRun, Id: "11"},
				{Type: task.GlobalHookRun, Id: "10"},
			},
			expectRemoved: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Fill queue from the test case.
			q := queue.NewTasksQueue()
			for _, tsk := range tt.initialTasks {
				tmpTsk := tsk
				// Set metadata to prevent "possible bug" errors.
				if tmpTsk.Metadata == nil {
					tmpTsk.Metadata = task.HookMetadata{}
				}
				q.AddLast(&tmpTsk)
			}
			require.Equal(t, len(tt.initialTasks), q.Length(), "Should add all tasks to the queue.")

			// Try to clean the queue.
			removed := RemoveCurrentConvergeTasks(q, currentTaskID)

			// Check result.
			if tt.expectRemoved {
				require.True(t, removed, "Should remove tasks from the queue")
			} else {
				require.False(t, removed, "Should not remove tasks from the queue")
			}

			// Check tasks in queue after remove.
			require.Equal(t, len(tt.expectTasks), q.Length(), "queue length should match length of expected tasks")
			i := 0
			q.Iterate(func(tsk sh_task.Task) {
				require.Equal(t, tt.expectTasks[i].Id, tsk.GetId(), "ID should match for task %d %+v", i, tsk)
				require.Equal(t, tt.expectTasks[i].Type, tsk.GetType(), "Type should match for task %d %+v", i, tsk)
				i++
			})
		})
	}
}

func Test_ConvergeModulesInQueue(t *testing.T) {
	tests := []struct {
		name   string
		result int
		queue  func() *queue.TaskQueue
	}{
		{
			name:   "Non converge ModuleRun tasks",
			result: 0,
			queue: func() *queue.TaskQueue {
				q := queue.NewTasksQueue()

				Task := &sh_task.BaseTask{Type: task.ModuleRun, Id: "unknown"}
				q.AddLast(Task.WithMetadata(task.HookMetadata{ModuleName: "unknown"}))

				Task = &sh_task.BaseTask{Type: task.ModuleRun, Id: "unknown"}
				q.AddLast(Task.WithMetadata(task.HookMetadata{ModuleName: "unknown"}))

				Task = &sh_task.BaseTask{Type: task.ModuleRun, Id: "test"}
				q.AddLast(Task.WithMetadata(task.HookMetadata{ModuleName: "test"}))
				return q
			},
		},
		{
			name:   "Converge ModuleRun tasks",
			result: 3,
			queue: func() *queue.TaskQueue {
				q := queue.NewTasksQueue()

				Task := &sh_task.BaseTask{Type: task.GlobalHookRun, Id: "unknown"}
				q.AddLast(Task.WithMetadata(task.HookMetadata{ModuleName: "unknown"}))

				Task = &sh_task.BaseTask{Type: task.ModuleRun, Id: "test"}
				q.AddLast(Task.WithMetadata(task.HookMetadata{ModuleName: "test", IsReloadAll: true}))

				Task = &sh_task.BaseTask{Type: task.ModuleRun, Id: "test2"}
				q.AddLast(Task.WithMetadata(task.HookMetadata{ModuleName: "test2", IsReloadAll: true}))

				Task = &sh_task.BaseTask{Type: task.ModuleRun, Id: "test3"}
				q.AddLast(Task.WithMetadata(task.HookMetadata{ModuleName: "test3", IsReloadAll: true}))

				Task = &sh_task.BaseTask{Type: task.ConvergeModules, Id: "unknown-converge"}
				// Prevent "Possible bug: metadata is nil"
				q.AddLast(Task.WithMetadata(task.HookMetadata{}))
				return q
			},
		},
		{
			name:   "Converge ModuleRun and ModuleDelete tasks",
			result: 3,
			queue: func() *queue.TaskQueue {
				q := queue.NewTasksQueue()

				Task := &sh_task.BaseTask{Type: task.GlobalHookRun, Id: "unknown"}
				q.AddLast(Task.WithMetadata(task.HookMetadata{ModuleName: "unknown"}))

				Task = &sh_task.BaseTask{Type: task.ModuleDelete, Id: "test"}
				q.AddLast(Task.WithMetadata(task.HookMetadata{ModuleName: "test"}))

				Task = &sh_task.BaseTask{Type: task.ModuleRun, Id: "test2"}
				q.AddLast(Task.WithMetadata(task.HookMetadata{ModuleName: "test2", IsReloadAll: true}))

				Task = &sh_task.BaseTask{Type: task.ModuleRun, Id: "test3"}
				q.AddLast(Task.WithMetadata(task.HookMetadata{ModuleName: "test3", IsReloadAll: true}))

				Task = &sh_task.BaseTask{Type: task.ConvergeModules, Id: "unknown-converge"}
				// Prevent "Possible bug: metadata is nil"
				q.AddLast(Task.WithMetadata(task.HookMetadata{}))
				return q
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvergeModulesInQueue(tt.queue())
			require.Equal(t, tt.result, result, "ConvergeModulesInQueue should run correctly")
		})
	}
}
