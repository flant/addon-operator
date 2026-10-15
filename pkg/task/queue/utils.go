package queue

import (
	"log/slog"

	"github.com/deckhouse/deckhouse/pkg/log"

	"github.com/flant/addon-operator/pkg"
	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
	"github.com/flant/addon-operator/pkg/task"
	sh_task "github.com/flant/shell-operator/pkg/task"
)

var MergeTasks = []sh_task.TaskType{task.ModuleHookRun}

type ModuleManager interface {
	GlobalSynchronizationState() *modules.SynchronizationState
	GetModule(moduleName string) *modules.BasicModule
}

type Callback func(compactedTasks []sh_task.Task, targetTask sh_task.Task)

func CompactionCallback(moduleManager ModuleManager, logger *log.Logger) Callback {
	return func(compactedTasks []sh_task.Task, _ sh_task.Task) {
		for _, compactedTask := range compactedTasks {
			thm := task.HookMetadataAccessor(compactedTask)
			if thm.IsSynchronization() {
				logger.Debug("Compacted synchronization task, marking as Done",
					slog.String(pkg.LogKeyHook, thm.HookName),
					slog.String(pkg.LogKeyBinding, thm.Binding),
					slog.String(pkg.LogKeyID, thm.KubernetesBindingId))

				if thm.ModuleName == "" {
					if moduleManager != nil && moduleManager.GlobalSynchronizationState() != nil {
						moduleManager.GlobalSynchronizationState().DoneForBinding(thm.KubernetesBindingId)
					}
				} else {
					if moduleManager != nil {
						baseModule := moduleManager.GetModule(thm.ModuleName)
						if baseModule != nil && baseModule.Synchronization() != nil {
							baseModule.Synchronization().DoneForBinding(thm.KubernetesBindingId)
						}
					}
				}
			}
		}
	}
}
