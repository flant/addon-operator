package globalhookwaitkubernetessynchronization

import (
	"context"
	"log/slog"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	"go.opentelemetry.io/otel"

	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/task"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
)

const (
	taskName = "global-hook-wait-kubernetes-synchronization"
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

func (s *Task) Handle(ctx context.Context) queue.TaskResult {
	_, span := otel.Tracer(taskName).Start(ctx, "handle")
	defer span.End()

	res := queue.TaskResult{
		Status: queue.Success,
	}

	// Add debug logging to understand the issue
	syncNeeded := s.moduleManager.GlobalSynchronizationNeeded()
	syncCompleted := s.moduleManager.GlobalSynchronizationState().IsCompleted()

	s.logger.Debug("GlobalHookWaitKubernetesSynchronization check",
		slog.Bool("syncNeeded", syncNeeded),
		slog.Bool("syncCompleted", syncCompleted))

	// Dump synchronization state for debugging
	s.moduleManager.GlobalSynchronizationState().DebugDumpState(s.logger)

	if syncNeeded && !syncCompleted {
		// dump state
		s.moduleManager.GlobalSynchronizationState().DebugDumpState(s.logger)
		s.shellTask.WithQueuedAt(time.Now())

		s.logger.Debug("GlobalHookWaitKubernetesSynchronization returning Repeat")
		res.Status = queue.Repeat
	} else {
		s.logger.Info("Synchronization done for all global hooks")
	}

	return res
}
