package moduleensurehooks

import (
	"context"
	"log/slog"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	"go.opentelemetry.io/otel"

	"github.com/flant/addon-operator/pkg"
	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/task"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
)

const (
	taskName = "module-ensure-hooks"
)

// TaskDependencies defines the interface for accessing necessary components.
type TaskDependencies interface {
	GetModuleManager() *module_manager.ModuleManager
}

// RegisterTaskHandler creates a factory function for ModuleEnsureHooks tasks.
func RegisterTaskHandler(svc TaskDependencies) func(t sh_task.Task, logger *log.Logger) task.Task {
	return func(t sh_task.Task, logger *log.Logger) task.Task {
		return NewTask(
			t,
			svc.GetModuleManager(),
			logger.Named("module-ensure-hooks"),
		)
	}
}

// Task applies a module's ConversionWebhook resources before its helm release.
type Task struct {
	shellTask sh_task.Task

	moduleManager *module_manager.ModuleManager

	logger *log.Logger
}

// NewTask creates a new task handler for ensuring module conversion webhooks.
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

// Handle renders and applies the module's ConversionWebhook resources.
func (s *Task) Handle(ctx context.Context) queue.TaskResult {
	_, span := otel.Tracer(taskName).Start(ctx, "handle")
	defer span.End()

	hm := task.HookMetadataAccessor(s.shellTask)

	res := queue.TaskResult{
		Status: queue.Success,
	}

	baseModule := s.moduleManager.GetModule(hm.ModuleName)

	s.logger.Debug("Module ensureHooks", slog.String(pkg.LogKeyName, hm.ModuleName))

	if err := s.moduleManager.EnsureConversionWebhooks(hm.ModuleName, s.shellTask.GetLogLabels()); err != nil {
		s.moduleManager.UpdateModuleLastErrorAndNotify(baseModule, err)

		s.logger.Error("ModuleEnsureHooks failed.", log.Err(err))

		s.shellTask.UpdateFailureMessage(err.Error())
		s.shellTask.WithQueuedAt(time.Now())

		res.Status = queue.Fail
	}

	return res
}
