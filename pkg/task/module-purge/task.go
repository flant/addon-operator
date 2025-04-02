package modulepurge

import (
	"context"
	"runtime/trace"

	"github.com/deckhouse/deckhouse/pkg/log"

	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/task"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
)

// TaskDependencies defines the interface for accessing necessary components
type TaskDependencies interface {
	GetHelm() *helm.ClientFactory
}

// RegisterTaskHandler creates a factory function for ModulePurge tasks
func RegisterTaskHandler(svc TaskDependencies) func(t sh_task.Task, logger *log.Logger) task.Task {
	return func(t sh_task.Task, logger *log.Logger) task.Task {
		return NewTask(
			t,
			svc.GetHelm(),
			logger.Named("module-purge"),
		)
	}
}

// Task handles purging modules
type Task struct {
	shellTask sh_task.Task
	helm      *helm.ClientFactory
	logger    *log.Logger
}

// NewTask creates a new task handler for module purging
func NewTask(
	shellTask sh_task.Task,
	helm *helm.ClientFactory,
	logger *log.Logger,
) *Task {
	return &Task{
		shellTask: shellTask,
		helm:      helm,
		logger:    logger,
	}
}

func (s *Task) Handle(ctx context.Context) queue.TaskResult {
	defer trace.StartRegion(ctx, "ModulePurge").End()

	var res queue.TaskResult

	s.logger.Debug("Module purge start")

	hm := task.HookMetadataAccessor(s.shellTask)
	helmClientOptions := []helm.ClientOption{
		helm.WithLogLabels(s.shellTask.GetLogLabels()),
	}

	err := s.helm.NewClient(s.logger.Named("helm-client"), helmClientOptions...).DeleteRelease(hm.ModuleName)
	if err != nil {
		// Purge is for unknown modules, just print warning.
		s.logger.Warn("Module purge failed, no retry.", log.Err(err))
	} else {
		s.logger.Debug("Module purge success")
	}

	res.Status = queue.Success

	return res
}
