package queue

import (
	"time"

	"github.com/flant/addon-operator/pkg/addon-operator/converge"
	"github.com/flant/addon-operator/pkg/task"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
)

func NewManager(queues *queue.TaskQueueSet) *Manager {
	return &Manager{queues: queues}
}

type Manager struct {
	queues *queue.TaskQueueSet
}

func (m *Manager) PurgeModule(moduleName string) {
	q := m.queues.GetMain()
	q.AddLast(newPurgeTask(moduleName))
	q.AddLast(converge.NewConvergeModulesTask("ReloadAll-After-Module-Purge", converge.ReloadAllModules, nil))
}

func newPurgeTask(moduleName string) sh_task.Task {
	return sh_task.NewTask(task.ModulePurge).
		WithLogLabels(map[string]string{"module": moduleName}).
		WithQueueName("main").
		WithMetadata(task.HookMetadata{ModuleName: moduleName}).
		WithQueuedAt(time.Now())
}
