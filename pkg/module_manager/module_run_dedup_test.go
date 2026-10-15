package module_manager

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/flant/addon-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/metric"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
)

// Test_queueHasPendingModuleRunTask pins the guard that keeps PushRunModuleTask from stacking
// duplicate moduleRun tasks for one module. Every caller pushes with doModuleStartup=false, so a
// guard that only answers for pending tasks carrying the startup flag never fires for them: the
// main queue then fills with copies of the same task and the module is re-released once per copy.
func Test_queueHasPendingModuleRunTask(t *testing.T) {
	const moduleName = "console"

	type pending struct {
		module    string
		doStartup bool
	}

	// The first task in the queue may already be running and is therefore not pending, so the queue
	// is built with a leading task that is never the one under test.
	newQueue := func(t *testing.T, tasks []pending) *queue.TaskQueue {
		t.Helper()

		metricStorage := metric.NewStorageMock(t)
		metricStorage.HistogramObserveMock.Set(func(_ string, _ float64, _ map[string]string, _ []float64) {})
		metricStorage.GaugeSetMock.Optional().Set(func(_ string, _ float64, _ map[string]string) {})

		q := queue.NewTasksQueue("main", metricStorage)
		q.AddLast(&sh_task.BaseTask{Type: task.ConvergeModules, Id: "running"})

		for i, p := range tasks {
			t := sh_task.NewTask(task.ModuleRun).WithMetadata(task.HookMetadata{
				ModuleName:      p.module,
				DoModuleStartup: p.doStartup,
			})
			t.Id = string(rune('a' + i))
			q.AddLast(t)
		}

		return q
	}

	tests := []struct {
		name      string
		pending   []pending
		doStartup bool
		want      bool
	}{
		{
			name:      "nothing pending",
			doStartup: false,
			want:      false,
		},
		{
			// the regression: identical pushes must collapse into one task
			name:      "pending task without startup covers a push without startup",
			pending:   []pending{{module: moduleName}},
			doStartup: false,
			want:      true,
		},
		{
			name:      "pending task with startup covers a push without startup",
			pending:   []pending{{module: moduleName, doStartup: true}},
			doStartup: false,
			want:      true,
		},
		{
			name:      "pending task with startup covers a push with startup",
			pending:   []pending{{module: moduleName, doStartup: true}},
			doStartup: true,
			want:      true,
		},
		{
			// the pending task does strictly less work, so dropping this push loses the startup
			name:      "pending task without startup does not cover a push with startup",
			pending:   []pending{{module: moduleName}},
			doStartup: true,
			want:      false,
		},
		{
			// the startup is already queued even though a later task does not carry it
			name:      "startup anywhere among the pending tasks counts",
			pending:   []pending{{module: moduleName, doStartup: true}, {module: moduleName}},
			doStartup: true,
			want:      true,
		},
		{
			name:      "another module does not count",
			pending:   []pending{{module: "observability"}},
			doStartup: false,
			want:      false,
		},
		{
			// the only task for the module is the one at the head, which may already be running
			name:      "a task that is already running is not pending",
			pending:   nil,
			doStartup: false,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := queueHasPendingModuleRunTask(newQueue(t, tt.pending), moduleName, tt.doStartup)
			assert.Equal(t, tt.want, got)
		})
	}

	t.Run("nil queue", func(t *testing.T) {
		assert.False(t, queueHasPendingModuleRunTask(nil, moduleName, false))
	})
}
