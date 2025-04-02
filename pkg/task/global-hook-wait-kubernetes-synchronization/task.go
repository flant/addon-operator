package globalhookwaitkubernetessynchronization

import (
	"context"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"

	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/task"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
)

// TaskDependencies defines the interface for accessing necessary components
type TaskDependencies interface {
	GetModuleManager() *module_manager.ModuleManager
}

// RegisterTaskHandler creates a factory function for global hook wait kubernetes synchronization tasks
func RegisterTaskHandler(svc TaskDependencies) func(t sh_task.Task, logger *log.Logger) task.Task {
	return func(t sh_task.Task, logger *log.Logger) task.Task {
		return NewTask(
			t,
			svc.GetModuleManager(),
			logger.Named("global-hook-wait-kubernetes-synchronization"),
		)
	}
}

// Task handles waiting for kubernetes synchronization for global hooks
type Task struct {
	shellTask     sh_task.Task
	moduleManager *module_manager.ModuleManager
	logger        *log.Logger
}

// NewTask creates a new task handler for global hook wait kubernetes synchronization
func NewTask(
	shellTask sh_task.Task,
	moduleManager *module_manager.ModuleManager,
	logger *log.Logger,
) *Task {
	return &Task{
		shellTask:     shellTask,
		moduleManager: moduleManager,
		logger:        logger,
	}
}

func (s *Task) Handle(_ context.Context) queue.TaskResult {
	res := queue.TaskResult{
		Status: queue.Success,
	}

	if s.moduleManager.GlobalSynchronizationNeeded() && !s.moduleManager.GlobalSynchronizationState().IsCompleted() {
		// dump state
		s.moduleManager.GlobalSynchronizationState().DebugDumpState(s.logger)
		s.shellTask.WithQueuedAt(time.Now())

		res.Status = queue.Repeat
	} else {
		s.logger.Info("Synchronization done for all global hooks")
	}

	return res
}
