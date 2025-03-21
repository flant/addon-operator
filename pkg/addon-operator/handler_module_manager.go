package addon_operator

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/gofrs/uuid/v5"

	"github.com/flant/addon-operator/pkg"
	"github.com/flant/addon-operator/pkg/addon-operator/converge"
	hrmtypes "github.com/flant/addon-operator/pkg/helm_resources_manager/types"
	"github.com/flant/addon-operator/pkg/kube_config_manager/config"
	"github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders"
	dynamic_extender "github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders/dynamically_enabled"
	"github.com/flant/addon-operator/pkg/task"
	"github.com/flant/addon-operator/pkg/utils"
	sh_task "github.com/flant/shell-operator/pkg/task"
)

func (op *AddonOperator) StartModuleManagerEventHandler() {
	go func() {
		logEntry := op.Logger.With("operator.component", "handleManagerEvents")
		for {
			select {
			case schedulerEvent := <-op.ModuleManager.SchedulerEventCh():
				op.handleSchedulerEvent(schedulerEvent, logEntry)
			case helmReleaseEvent := <-op.HelmResourcesManager.Ch():
				op.handleHelmReleaseStatusEvent(helmReleaseEvent, logEntry)
			}
		}
	}()
}

// handleSchedulerEvent processes events from the ModuleManager's scheduler
func (op *AddonOperator) handleSchedulerEvent(schedulerEvent extenders.ExtenderEvent, logEntry *log.Logger) {
	switch event := schedulerEvent.EncapsulatedEvent.(type) {
	case dynamic_extender.DynamicExtenderEvent:
		op.handleDynamicExtenderEvent(event, logEntry)
	case config.KubeConfigEvent:
		op.handleKubeConfigEvent(event, logEntry)
	}
}

// handleDynamicExtenderEvent processes events from the dynamic extender
func (op *AddonOperator) handleDynamicExtenderEvent(_ dynamic_extender.DynamicExtenderEvent, logEntry *log.Logger) {
	logLabels := map[string]string{
		"event.id":     uuid.Must(uuid.NewV4()).String(),
		"type":         "ModuleScheduler event",
		"event_source": "DymicallyEnabledExtenderChanged",
	}
	eventLogEntry := utils.EnrichLoggerWithLabels(logEntry, logLabels)

	// Don't process until global hooks have been executed
	if op.globalHooksNotExecutedYet() {
		eventLogEntry.Info("Global hook dynamic modification detected, ignore until starting first converge")
		return
	}

	graphStateChanged := op.ModuleManager.RecalculateGraph(logLabels)

	if graphStateChanged {
		convergeTask := converge.NewConvergeModulesTask(
			"ReloadAll-After-GlobalHookDynamicUpdate",
			converge.ReloadAllModules,
			logLabels,
		)

		op.ConvergeState.PhaseLock.Lock()
		defer op.ConvergeState.PhaseLock.Unlock()

		// If converge has already begun - restart it immediately
		if op.engine.TaskQueues.GetMain().Length() > 0 &&
			RemoveCurrentConvergeTasks(op.getConvergeQueues(), logLabels, op.Logger) &&
			op.ConvergeState.Phase != converge.StandBy {
			logEntry.Info("ConvergeModules: global hook dynamic modification detected, restart current converge process",
				slog.String("phase", string(op.ConvergeState.Phase)))
			op.engine.TaskQueues.GetMain().AddFirst(convergeTask)
			op.logTaskAdd(eventLogEntry, "DynamicExtender is updated, put first", convergeTask)
		} else {
			// If converge hasn't started - queue task normally
			logEntry.Info("ConvergeModules: global hook dynamic modification detected, rerun all modules required")
			op.engine.TaskQueues.GetMain().AddLast(convergeTask)
		}

		// Reset converge state as ConvergeModules may be in progress
		op.ConvergeState.Phase = converge.StandBy
	}
}

// handleKubeConfigEvent processes KubeConfig changes
func (op *AddonOperator) handleKubeConfigEvent(event config.KubeConfigEvent, logEntry *log.Logger) {
	logLabels := map[string]string{
		"event.id":     uuid.Must(uuid.NewV4()).String(),
		"type":         "ModuleScheduler event",
		"event_source": "KubeConfigExtenderChanged",
	}
	eventLogEntry := utils.EnrichLoggerWithLabels(logEntry, logLabels)

	switch event.Type {
	case config.KubeConfigInvalid:
		op.ModuleManager.SetKubeConfigValid(false)
		eventLogEntry.Info("KubeConfig become invalid")

	case config.KubeConfigChanged:
		op.handleKubeConfigChanged(event, logLabels, logEntry, eventLogEntry)
	}
}

// handleKubeConfigChanged processes the KubeConfigChanged event type
func (op *AddonOperator) handleKubeConfigChanged(
	event config.KubeConfigEvent,
	logLabels map[string]string,
	logEntry *log.Logger,
	eventLogEntry *log.Logger,
) {
	eventLogEntry.Debug("ModuleManagerEventHandler-KubeConfigChanged",
		slog.Bool("globalSectionChanged", event.GlobalSectionChanged),
		slog.Any("moduleValuesChanged", event.ModuleValuesChanged),
		slog.Any("moduleEnabledStateChanged", event.ModuleEnabledStateChanged))

	if !op.ModuleManager.GetKubeConfigValid() {
		eventLogEntry.Info("KubeConfig become valid")
	}
	// Config is valid now, update ModuleManager state
	op.ModuleManager.SetKubeConfigValid(true)

	var kubeConfigTask sh_task.Task

	// Create task to apply KubeConfig values changes if needed
	if event.GlobalSectionChanged || len(event.ModuleValuesChanged) > 0 {
		kubeConfigTask = converge.NewApplyKubeConfigValuesTask(
			"Apply-Kube-Config-Values-Changes",
			logLabels,
			event.GlobalSectionChanged,
		)
	}

	// Handle the case when global hooks haven't been run yet
	if op.globalHooksNotExecutedYet() {
		if kubeConfigTask != nil {
			op.engine.TaskQueues.GetMain().AddFirst(kubeConfigTask)
			// Cancel delay in case the head task is stuck in the error loop
			op.engine.TaskQueues.GetMain().CancelTaskDelay()
			op.logTaskAdd(eventLogEntry, "KubeConfigExtender is updated, put first", kubeConfigTask)
		}
		eventLogEntry.Info("Kube config modification detected, ignore until starting first converge")
		return
	}

	graphStateChanged := op.ModuleManager.RecalculateGraph(logLabels)

	if event.GlobalSectionChanged || graphStateChanged {
		op.handleGlobalOrGraphStateChange(kubeConfigTask, logLabels, logEntry, eventLogEntry)
	} else {
		op.handleModuleValuesChange(event, kubeConfigTask, logEntry)
	}
}

// handleGlobalOrGraphStateChange handles changes that require a full converge
func (op *AddonOperator) handleGlobalOrGraphStateChange(
	kubeConfigTask sh_task.Task,
	logLabels map[string]string,
	logEntry *log.Logger,
	eventLogEntry *log.Logger,
) {
	// Prepare convergeModules task
	op.ConvergeState.PhaseLock.Lock()
	defer op.ConvergeState.PhaseLock.Unlock()

	convergeTask := converge.NewConvergeModulesTask(
		"ReloadAll-After-KubeConfigChange",
		converge.ReloadAllModules,
		logLabels,
	)

	mainQueue := op.engine.TaskQueues.GetMain()

	// If main queue isn't empty and there was another convergeModules task
	if mainQueue.Length() > 0 && RemoveCurrentConvergeTasks(op.getConvergeQueues(), logLabels, op.Logger) {
		logEntry.Info("ConvergeModules: kube config modification detected, restart current converge process",
			slog.String("phase", string(op.ConvergeState.Phase)))

		// Put ApplyKubeConfig->NewConvergeModulesTask sequence at the beginning of the main queue
		if kubeConfigTask != nil {
			mainQueue.AddFirst(kubeConfigTask)
			mainQueue.AddAfter(kubeConfigTask.GetId(), convergeTask)
			op.logTaskAdd(eventLogEntry, "KubeConfig is changed, put after AppplyNewKubeConfig", convergeTask)
		} else {
			// Otherwise, put just NewConvergeModulesTask
			mainQueue.AddFirst(convergeTask)
			op.logTaskAdd(eventLogEntry, "KubeConfig is changed, put first", convergeTask)
		}
	} else {
		// If main queue is empty - put tasks at the appropriate positions
		if kubeConfigTask != nil {
			mainQueue.AddFirst(kubeConfigTask)
		}
		logEntry.Info("ConvergeModules: kube config modification detected, rerun all modules required")
		mainQueue.AddLast(convergeTask)
	}

	// ConvergeModules may be in progress, reset converge state
	op.ConvergeState.Phase = converge.StandBy
}

// handleModuleValuesChange handles changes that only affect specific modules
func (op *AddonOperator) handleModuleValuesChange(
	event config.KubeConfigEvent,
	kubeConfigTask sh_task.Task,
	logEntry *log.Logger,
) {
	if kubeConfigTask == nil {
		return
	}

	modulesToRerun := []string{}
	for _, moduleName := range event.ModuleValuesChanged {
		if op.ModuleManager.IsModuleEnabled(moduleName) {
			modulesToRerun = append(modulesToRerun, moduleName)
		}
	}

	// Append ModuleRun tasks if ModuleRun is not queued already
	reloadTasks := op.CreateReloadModulesTasks(modulesToRerun, kubeConfigTask.GetLogLabels(), "KubeConfig-Changed-Modules")
	mainQueue := op.engine.TaskQueues.GetMain()

	mainQueue.AddFirst(kubeConfigTask)
	if len(reloadTasks) > 0 {
		for i := len(reloadTasks) - 1; i >= 0; i-- {
			mainQueue.AddAfter(kubeConfigTask.GetId(), reloadTasks[i])
		}
		logEntry.Info("ConvergeModules: kube config modification detected, append tasks to rerun modules",
			slog.Int("count", len(reloadTasks)),
			slog.Any("modules", modulesToRerun))
		op.logTaskAdd(logEntry, "tail", reloadTasks...)
	}
}

// handleHelmReleaseStatusEvent processes Helm release status events
func (op *AddonOperator) handleHelmReleaseStatusEvent(event hrmtypes.ReleaseStatusEvent, logEntry *log.Logger) {
	logLabels := map[string]string{
		"event.id": uuid.Must(uuid.NewV4()).String(),
		"module":   event.ModuleName,
	}
	eventLogEntry := utils.EnrichLoggerWithLabels(logEntry, logLabels)

	// Do not add ModuleRun task if it is already queued
	hasTask := QueueHasPendingModuleRunTask(op.engine.TaskQueues.GetMain(), event.ModuleName)
	eventDescription, additionalDescription := op.getHelmEventDescriptions(event)

	// Add metrics based on the event type
	op.recordHelmEventMetrics(event)

	if !hasTask {
		newTask := sh_task.NewTask(task.ModuleRun).
			WithLogLabels(logLabels).
			WithQueueName("main").
			WithMetadata(task.HookMetadata{
				EventDescription: eventDescription,
				ModuleName:       event.ModuleName,
			})
		op.engine.TaskQueues.GetMain().AddLast(newTask.WithQueuedAt(time.Now()))
		op.logTaskAdd(logEntry, fmt.Sprintf("detected %s, append", additionalDescription), newTask)
	} else {
		eventLogEntry.With(pkg.LogKeyTaskFlow, "noop").Info("Detected event, ModuleRun task already queued",
			slog.String("description", additionalDescription))
	}
}

// getHelmEventDescriptions returns appropriate description strings for a helm event
func (op *AddonOperator) getHelmEventDescriptions(event hrmtypes.ReleaseStatusEvent) (string, string) {
	if event.UnexpectedStatus {
		return "HelmReleaseUnexpectedStatus", "unexpected helm release status"
	}
	return "AbsentHelmResourcesDetected", fmt.Sprintf("%d absent module resources", len(event.Absent))
}

// recordHelmEventMetrics records metrics based on helm event type
func (op *AddonOperator) recordHelmEventMetrics(event hrmtypes.ReleaseStatusEvent) {
	if event.UnexpectedStatus {
		op.engine.MetricStorage.CounterAdd("{PREFIX}modules_helm_release_redeployed_total", 1.0,
			map[string]string{"module": event.ModuleName})
	} else {
		// Record metrics for each missing resource
		for _, manifest := range event.Absent {
			op.engine.MetricStorage.CounterAdd("{PREFIX}modules_absent_resources_total", 1.0,
				map[string]string{
					"module": event.ModuleName,
					"resource": fmt.Sprintf("%s/%s/%s",
						manifest.Namespace(""),
						manifest.Kind(),
						manifest.Name()),
				})
		}
	}
}
