package addon_operator

import (
	"context"
	"log/slog"

	"github.com/gofrs/uuid/v5"

	"github.com/flant/addon-operator/pkg"
	"github.com/flant/addon-operator/pkg/module_manager/models/hooks"
	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
	"github.com/flant/addon-operator/pkg/task"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/shell-operator/pkg/hook/controller"
	htypes "github.com/flant/shell-operator/pkg/hook/types"
	"github.com/flant/shell-operator/pkg/kube_events_manager/types"
	sh_task "github.com/flant/shell-operator/pkg/task"
)

func (op *AddonOperator) RegisterManagerEventsHandlers() {
	// Register handler for schedule events
	op.engine.ManagerEventsHandler.WithScheduleEventHandler(func(ctx context.Context, crontab string) []sh_task.Task {
		logLabels := map[string]string{
			"event.id":        uuid.Must(uuid.NewV4()).String(),
			pkg.LogKeyBinding: string(htypes.Schedule),
		}
		logEntry := utils.EnrichLoggerWithLabels(op.Logger, logLabels)
		logEntry.Debug("Create tasks for 'schedule' event",
			slog.String("event", crontab))

		// Handle global hook schedule events
		return op.ModuleManager.HandleScheduleEvent(
			ctx,
			crontab,
			op.createGlobalHookTaskFactory(logLabels, htypes.Schedule, "Schedule", true),
			op.createModuleHookTaskFactory(logLabels, htypes.Schedule, "Schedule"),
		)
	})

	// Register handler for kubernetes events
	op.engine.ManagerEventsHandler.WithKubeEventHandler(func(ctx context.Context, kubeEvent types.KubeEvent) []sh_task.Task {
		logLabels := map[string]string{
			"event.id":        uuid.Must(uuid.NewV4()).String(),
			pkg.LogKeyBinding: string(htypes.OnKubernetesEvent),
		}
		logEntry := utils.EnrichLoggerWithLabels(op.Logger, logLabels)
		logEntry.Debug("Create tasks for 'kubernetes' event",
			slog.String("event", kubeEvent.String()))

		// Handle kubernetes events for global and module hooks
		tailTasks := op.ModuleManager.HandleKubeEvent(
			ctx,
			kubeEvent,
			op.createGlobalHookTaskFactory(logLabels, htypes.OnKubernetesEvent, "Kubernetes", true),
			op.createModuleHookTaskFactory(logLabels, htypes.OnKubernetesEvent, "Kubernetes"),
		)

		return tailTasks
	})
}

// createGlobalHookTaskFactory returns a factory function for creating tasks for global hooks
func (op *AddonOperator) createGlobalHookTaskFactory(
	logLabels map[string]string,
	bindingType htypes.BindingType,
	eventDescription string,
	reloadOnValuesChanges bool,
) func(globalHook *hooks.GlobalHook, info controller.BindingExecutionInfo) sh_task.Task {
	return func(globalHook *hooks.GlobalHook, info controller.BindingExecutionInfo) sh_task.Task {
		// For schedule events, check if we should allow handling
		if bindingType == htypes.Schedule && !op.allowHandleScheduleEvent(globalHook) {
			return nil
		}

		hookLabels := utils.MergeLabels(logLabels, map[string]string{
			pkg.LogKeyHook: globalHook.GetName(),
			"hook.type":    "global",
			"queue":        info.QueueName,
		})

		if len(info.BindingContext) > 0 {
			hookLabels["binding.name"] = info.BindingContext[0].Binding
			if bindingType == htypes.OnKubernetesEvent {
				hookLabels["watchEvent"] = string(info.BindingContext[0].WatchEvent)
			}
		}

		delete(hookLabels, "task.id")

		newTask := sh_task.NewTask(task.GlobalHookRun).
			WithLogLabels(hookLabels).
			WithQueueName(info.QueueName).
			WithMetadata(task.HookMetadata{
				EventDescription:         eventDescription,
				HookName:                 globalHook.GetName(),
				BindingType:              bindingType,
				BindingContext:           info.BindingContext,
				AllowFailure:             info.AllowFailure,
				Binding:                  info.Binding,
				ReloadAllOnValuesChanges: reloadOnValuesChanges,
			}).
			WithCompactionID(globalHook.GetName())

		return newTask
	}
}

// createModuleHookTaskFactory returns a factory function for creating tasks for module hooks
func (op *AddonOperator) createModuleHookTaskFactory(
	logLabels map[string]string,
	bindingType htypes.BindingType,
	eventDescription string,
) func(module *modules.BasicModule, moduleHook *hooks.ModuleHook, info controller.BindingExecutionInfo) sh_task.Task {
	return func(module *modules.BasicModule, moduleHook *hooks.ModuleHook, info controller.BindingExecutionInfo) sh_task.Task {
		// For schedule events, check if we should allow handling
		if bindingType == htypes.Schedule && !op.allowHandleScheduleEvent(moduleHook) {
			return nil
		}

		hookLabels := utils.MergeLabels(logLabels, map[string]string{
			"module":       module.GetName(),
			pkg.LogKeyHook: moduleHook.GetName(),
			"hook.type":    "module",
			"queue":        info.QueueName,
		})

		if len(info.BindingContext) > 0 {
			hookLabels["binding.name"] = info.BindingContext[0].Binding
			if bindingType == htypes.OnKubernetesEvent {
				hookLabels["watchEvent"] = string(info.BindingContext[0].WatchEvent)
			}
		}

		delete(hookLabels, "task.id")

		newTask := sh_task.NewTask(task.ModuleHookRun).
			WithLogLabels(hookLabels).
			WithQueueName(info.QueueName).
			WithMetadata(task.HookMetadata{
				EventDescription: eventDescription,
				ModuleName:       module.GetName(),
				HookName:         moduleHook.GetName(),
				Binding:          info.Binding,
				BindingType:      bindingType,
				BindingContext:   info.BindingContext,
				AllowFailure:     info.AllowFailure,
			}).
			WithCompactionID(moduleHook.GetName())

		return newTask
	}
}
