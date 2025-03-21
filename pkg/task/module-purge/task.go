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

type TaskConfig interface {
	GetHelm() *helm.ClientFactory
}

func RegisterTaskHandler(svc TaskConfig) func(t sh_task.Task, logger *log.Logger) task.Task {
	return func(t sh_task.Task, logger *log.Logger) task.Task {
		cfg := &taskConfig{
			ShellTask: t,
			Helm:      svc.GetHelm(),
		}

		return newModulePurge(cfg, logger.Named("module-purge"))
	}
}

type taskConfig struct {
	ShellTask sh_task.Task
	Helm      *helm.ClientFactory
}

type Task struct {
	shellTask sh_task.Task
	helm      *helm.ClientFactory
	logger    *log.Logger
}

// newModulePurge creates a new task handler service
func newModulePurge(cfg *taskConfig, logger *log.Logger) *Task {
	service := &Task{
		shellTask: cfg.ShellTask,

		helm: cfg.Helm,

		logger: logger,
	}

	return service
}

func (s *Task) Handle(ctx context.Context) queue.TaskResult {
	defer trace.StartRegion(ctx, "ModulePurge").End()

	var res queue.TaskResult

	s.logger.Debug("Module purge start")

	hm := task.HookMetadataAccessor(s.shellTask)
	err := s.helm.NewClient(s.logger.Named("helm-client"), s.shellTask.GetLogLabels()).DeleteRelease(hm.ModuleName)
	if err != nil {
		// Purge is for unknown modules, just print warning.
		s.logger.Warn("Module purge failed, no retry.", log.Err(err))
	} else {
		s.logger.Debug("Module purge success")
	}

	res.Status = queue.Success

	return res
}
