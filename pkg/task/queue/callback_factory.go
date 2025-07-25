package queue

import (
	"log/slog"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/task"
	sh_task "github.com/flant/shell-operator/pkg/task"
)

// UniversalCompactionCallback создает универсальный callback для компакции очередей
func UniversalCompactionCallback(moduleManager *module_manager.ModuleManager, logger *log.Logger) func(compactedTasks []sh_task.Task, targetTask sh_task.Task) {
	return func(compactedTasks []sh_task.Task, targetTask sh_task.Task) {
		for _, compactedTask := range compactedTasks {
			thm := task.HookMetadataAccessor(compactedTask)
			if thm.IsSynchronization() {
				logger.Debug("Compacted synchronization task, marking as Done",
					slog.String("hook", thm.HookName),
					slog.String("binding", thm.Binding),
					slog.String("id", thm.KubernetesBindingId))

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
