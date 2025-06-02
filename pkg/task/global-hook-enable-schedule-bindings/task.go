package globalhookenableschedulebindings

import (
	"context"

	"github.com/deckhouse/deckhouse/pkg/log"
	"go.opentelemetry.io/otel"

	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/task"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
)

const (
	taskName = "global-hook-enable-schedule-bindings"
)

type TaskDependencies interface {
	GetModuleManager() *module_manager.ModuleManager
}

// Task represents a handler for enabling schedule bindings on global hooks
type Task struct {
	shellTask     sh_task.Task
	moduleManager *module_manager.ModuleManager
	logger        *log.Logger
}

// RegisterTaskHandler returns a function that creates a Task handler
func RegisterTaskHandler(config TaskDependencies) func(t sh_task.Task, logger *log.Logger) task.Task {
	return func(t sh_task.Task, logger *log.Logger) task.Task {
		return NewTask(
			t,
			config.GetModuleManager(),
			logger.Named("global-hook-enable-schedule-bindings"),
		)
	}
}

// NewTask creates a new Task instance.
func NewTask(shellTask sh_task.Task, moduleManager *module_manager.ModuleManager, logger *log.Logger) *Task {
	return &Task{
		shellTask:     shellTask,
		moduleManager: moduleManager,
		logger:        logger,
	}
}

func (s *Task) Handle(ctx context.Context) queue.TaskResult {
	_, span := otel.Tracer(taskName).Start(ctx, "handle")
	defer span.End()

	result := queue.TaskResult{}

	hm := task.HookMetadataAccessor(s.shellTask)

	globalHook := s.moduleManager.GetGlobalHook(hm.HookName)
	globalHook.GetHookController().EnableScheduleBindings()

	result.Status = queue.Success

	return result
}
