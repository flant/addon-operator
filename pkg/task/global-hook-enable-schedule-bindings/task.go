package globalhookenableschedulebindings

import (
	"context"

	"github.com/deckhouse/deckhouse/pkg/log"

	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/task"
	"github.com/flant/addon-operator/pkg/utils"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
)

type TaskConfig interface {
	GetModuleManager() *module_manager.ModuleManager
	GetLogger() *log.Logger
}

func RegisterTaskHandler(svc TaskConfig) func(t sh_task.Task) task.Task {
	return func(t sh_task.Task) task.Task {
		cfg := &taskConfig{
			ShellTask:     t,
			ModuleManager: svc.GetModuleManager(),
		}

		return newGlobalHookEnableScheduleBindings(cfg, svc.GetLogger().Named("global-hook-enable-schedule-bindings"))
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

// newGlobalHookEnableScheduleBindings creates a new task handler service
func newGlobalHookEnableScheduleBindings(cfg *taskConfig, logger *log.Logger) *Task {
	service := &Task{
		shellTask: cfg.ShellTask,

		moduleManager: cfg.ModuleManager,

		logger: logger,
	}

	return service
}

func (s *Task) Handle(_ context.Context) queue.TaskResult {
	result := queue.TaskResult{}

	taskLogLabels := s.shellTask.GetLogLabels()
	s.logger = utils.EnrichLoggerWithLabels(s.logger, taskLogLabels)

	hm := task.HookMetadataAccessor(s.shellTask)

	globalHook := s.moduleManager.GetGlobalHook(hm.HookName)
	globalHook.GetHookController().EnableScheduleBindings()

	result.Status = queue.Success

	return result
}
