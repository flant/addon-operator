package service

import (
	"context"
	"log/slog"

	"github.com/deckhouse/deckhouse/pkg/log"

	"github.com/flant/addon-operator/pkg"
	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
	"github.com/flant/addon-operator/pkg/task"
	"github.com/flant/addon-operator/pkg/task/helpers"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
)

// logTaskStart prints info about task at start. Also prints event source info from task props.
func (s *TaskHandlerService) logTaskStart(tsk sh_task.Task, logger *log.Logger) {
	// Prevent excess messages for highly frequent tasks.
	if tsk.GetType() == task.GlobalHookWaitKubernetesSynchronization {
		return
	}

	if tsk.GetType() == task.ModuleRun {
		hm := task.HookMetadataAccessor(tsk)
		baseModule := s.moduleManager.GetModule(hm.ModuleName)

		if baseModule.GetPhase() == modules.WaitForSynchronization {
			return
		}
	}

	logger = logger.With(pkg.LogKeyTaskFlow, "start")

	if triggeredBy, ok := tsk.GetProp("triggered-by").([]slog.Attr); ok {
		for _, attr := range triggeredBy {
			logger = logger.With(attr)
		}
	}

	logger.Info(helpers.TaskDescriptionForTaskFlowLog(tsk, "start", s.taskPhase(tsk), ""))
}

// logTaskEnd prints info about task at the end. Info level used only for the ConvergeModules task.
func (s *TaskHandlerService) logTaskEnd(tsk sh_task.Task, result queue.TaskResult, logger *log.Logger) {
	logger = logger.With(pkg.LogKeyTaskFlow, "end")

	level := log.LevelDebug
	if tsk.GetType() == task.ConvergeModules {
		level = log.LevelInfo
	}

	logger.Log(context.TODO(), level.Level(), helpers.TaskDescriptionForTaskFlowLog(tsk, "end", s.taskPhase(tsk), string(result.Status)))
}

func (s *TaskHandlerService) taskPhase(tsk sh_task.Task) string {
	switch tsk.GetType() {
	case task.ConvergeModules:
		// return string(s.ConvergeState.Phase)
	case task.ModuleRun:
		hm := task.HookMetadataAccessor(tsk)
		mod := s.moduleManager.GetModule(hm.ModuleName)
		return string(mod.GetPhase())
	}
	return ""
}
