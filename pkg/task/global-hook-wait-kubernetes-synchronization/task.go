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

type TaskConfig interface {
	GetModuleManager() *module_manager.ModuleManager
}

func RegisterTaskHandler(svc TaskConfig) func(t sh_task.Task, logger *log.Logger) task.Task {
	return func(t sh_task.Task, logger *log.Logger) task.Task {
		cfg := &taskConfig{
			ShellTask: t,

			ModuleManager: svc.GetModuleManager(),
		}

		return newGlobalHookWaitKubernetesSynchronization(cfg, logger.Named("global-hook-wait-kubernetes-synchronization"))
	}
}

type taskConfig struct {
	ShellTask     sh_task.Task
	ModuleManager *module_manager.ModuleManager
}

type Task struct {
	shellTask     sh_task.Task
	moduleManager *module_manager.ModuleManager
	logger        *log.Logger
}

// newGlobalHookWaitKubernetesSynchronization creates a new task handler service
func newGlobalHookWaitKubernetesSynchronization(cfg *taskConfig, logger *log.Logger) *Task {
	service := &Task{
		shellTask: cfg.ShellTask,

		moduleManager: cfg.ModuleManager,

		logger: logger,
	}

	return service
}

func (s *Task) Handle(ctx context.Context) queue.TaskResult {
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
