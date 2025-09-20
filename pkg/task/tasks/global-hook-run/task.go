package globalhookrun

import (
	"context"
	"log/slog"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	metricsstorage "github.com/deckhouse/deckhouse/pkg/metrics-storage"
	"go.opentelemetry.io/otel"

	"github.com/flant/addon-operator/pkg"
	"github.com/flant/addon-operator/pkg/addon-operator/converge"
	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/helm/helm3lib"
	"github.com/flant/addon-operator/pkg/helm_resources_manager"
	hookTypes "github.com/flant/addon-operator/pkg/hook/types"
	"github.com/flant/addon-operator/pkg/metrics"
	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/task"
	"github.com/flant/addon-operator/pkg/task/helpers"
	taskqueue "github.com/flant/addon-operator/pkg/task/queue"
	htypes "github.com/flant/shell-operator/pkg/hook/types"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/utils/measure"
)

const (
	taskName = "global-hook-run"
)

// TaskDependencies defines the services required for global hook task execution
type TaskDependencies interface {
	GetHelm() *helm.ClientFactory
	GetHelmResourcesManager() helm_resources_manager.HelmResourcesManager
	GetModuleManager() *module_manager.ModuleManager
	GetMetricStorage() metricsstorage.Storage
	GetQueueService() *taskqueue.Service
}

// RegisterTaskHandler returns a function that creates a task handler for global hook execution
func RegisterTaskHandler(svc TaskDependencies) func(t sh_task.Task, logger *log.Logger) task.Task {
	return func(t sh_task.Task, logger *log.Logger) task.Task {
		return NewTask(t, helpers.IsOperatorStartupTask(t), svc, logger.Named("global-hook-run"))
	}
}

// Task handles the execution of global hook tasks
type Task struct {
	shellTask            sh_task.Task
	isOperatorStartup    bool
	helm                 *helm.ClientFactory
	helmResourcesManager helm_resources_manager.HelmResourcesManager
	moduleManager        *module_manager.ModuleManager
	metricStorage        metricsstorage.Storage
	queueService         *taskqueue.Service
	logger               *log.Logger
}

// NewTask creates a new Task for handling global hook execution
func NewTask(shellTask sh_task.Task, isOperatorStartup bool, svc TaskDependencies, logger *log.Logger) *Task {
	return &Task{
		shellTask:            shellTask,
		isOperatorStartup:    isOperatorStartup,
		helm:                 svc.GetHelm(),
		helmResourcesManager: svc.GetHelmResourcesManager(),
		moduleManager:        svc.GetModuleManager(),
		metricStorage:        svc.GetMetricStorage(),
		queueService:         svc.GetQueueService(),
		logger:               logger,
	}
}

func (s *Task) Handle(ctx context.Context) sh_task.TaskResult {
	ctx, span := otel.Tracer(taskName).Start(ctx, "handle")
	defer span.End()

	var res sh_task.TaskResult

	hm := task.HookMetadataAccessor(s.shellTask)
	taskHook := s.moduleManager.GetGlobalHook(hm.HookName)

	err := taskHook.RateLimitWait(ctx)
	if err != nil {
		// This could happen when the Context is
		// canceled, or the expected wait time exceeds the Context's Deadline.
		// The best we can do without proper context usage is to repeat the task.
		res.Status = sh_task.Repeat
		return res
	}

	eventType := ""
	if s.isOperatorStartup {
		eventType = converge.OperatorStartup.String()
	}

	metricLabels := map[string]string{
		pkg.MetricKeyHook:       hm.HookName,
		pkg.MetricKeyBinding:    string(hm.BindingType),
		pkg.MetricKeyQueue:      s.shellTask.GetQueueName(),
		pkg.MetricKeyActivation: eventType,
	}

	defer measure.Duration(func(d time.Duration) {
		s.metricStorage.HistogramObserve(metrics.GlobalHookRunSeconds, d.Seconds(), metricLabels, nil)
	})()

	isSynchronization := hm.IsSynchronization()
	shouldRunHook := true
	if isSynchronization {
		// Synchronization is not a part of v0 contract, skip hook execution.
		if taskHook.GetHookConfig().Version == "v0" {
			s.logger.Info("Execute on Synchronization ignored for v0 hooks")
			shouldRunHook = false
			res.Status = sh_task.Success
		}
		// Check for "executeOnSynchronization: false".
		if !hm.ExecuteOnSynchronization {
			s.logger.Info("Execute on Synchronization disabled in hook config: ExecuteOnSynchronization=false")
			shouldRunHook = false
			res.Status = sh_task.Success
		}
	}

	if shouldRunHook && taskHook.GetHookConfig().Version == "v1" {
		// Combine binding contexts in the queue.
		combineResult := s.queueService.CombineBindingContextForHook(s.shellTask.GetQueueName(), s.shellTask, func(tsk sh_task.Task) bool {
			thm := task.HookMetadataAccessor(tsk)

			// Stop combine process when Synchronization tasks have different
			// values in WaitForSynchronization or ExecuteOnSynchronization fields.
			if hm.KubernetesBindingId != "" && thm.KubernetesBindingId != "" {
				if hm.WaitForSynchronization != thm.WaitForSynchronization {
					return true
				}

				if hm.ExecuteOnSynchronization != thm.ExecuteOnSynchronization {
					return true
				}
			}

			// Task 'tsk' will be combined, so remove it from the GlobalSynchronizationState.
			if thm.IsSynchronization() {
				s.logger.Debug("Synchronization task is combined, mark it as Done",
					slog.String("name", thm.HookName),
					slog.String(pkg.LogKeyBinding, thm.Binding),
					slog.String("id", thm.KubernetesBindingId))

				s.moduleManager.GlobalSynchronizationState().DoneForBinding(thm.KubernetesBindingId)
			}

			return false // Combine tsk.
		})

		if combineResult != nil {
			hm.BindingContext = combineResult.BindingContexts
			// Extra monitor IDs can be returned if several Synchronization binding contexts are combined.
			if len(combineResult.MonitorIDs) > 0 {
				s.logger.Debug("Task monitorID. Combined monitorIDs.",
					slog.Any("monitorIDs", hm.MonitorIDs),
					slog.Any("combinedMonitorIDs", combineResult.MonitorIDs))

				hm.MonitorIDs = combineResult.MonitorIDs
			}

			s.logger.Debug("Got monitorIDs",
				slog.Any("monitorIDs", hm.MonitorIDs))

			s.shellTask.UpdateMetadata(hm)
		}
	}

	// TODO create metadata flag that indicate whether to add reload all task on values changes
	// op.HelmResourcesManager.PauseMonitors()

	if shouldRunHook {
		s.logger.Debug("Global hook run")

		errors := 0.0
		success := 0.0
		allowed := 0.0

		// Save a checksum of *Enabled values.
		// Run Global hook.
		beforeChecksum, afterChecksum, err := s.moduleManager.RunGlobalHook(ctx, hm.HookName, hm.BindingType, hm.BindingContext, s.shellTask.GetLogLabels())
		if err != nil {
			if hm.AllowFailure {
				allowed = 1.0

				s.logger.Info("Global hook failed, but allowed to fail.", log.Err(err))

				res.Status = sh_task.Success
			} else {
				errors = 1.0

				s.logger.Error("Global hook failed, requeue task to retry after delay.",
					slog.Int("count", s.shellTask.GetFailureCount()+1),
					log.Err(err))

				s.shellTask.UpdateFailureMessage(err.Error())
				s.shellTask.WithQueuedAt(time.Now())

				res.Status = sh_task.Fail
			}
		} else {
			// Calculate new checksum of *Enabled values.
			success = 1.0

			s.logger.Debug("GlobalHookRun success",
				slog.String("beforeChecksum", beforeChecksum),
				slog.String("afterChecksum", afterChecksum),
				slog.String("savedChecksum", hm.ValuesChecksum))

			res.Status = sh_task.Success

			reloadAll := false
			eventDescription := ""

			switch hm.BindingType {
			case htypes.Schedule:
				if beforeChecksum != afterChecksum {
					s.logger.Info("Global hook changed values, will run ReloadAll.")

					reloadAll = true
					eventDescription = "Schedule-Change-GlobalValues"
				}
			case htypes.OnKubernetesEvent:
				if beforeChecksum != afterChecksum {
					if hm.ReloadAllOnValuesChanges {
						s.logger.Info("Global hook changed values, will run ReloadAll.")

						reloadAll = true
						eventDescription = "Kubernetes-Change-GlobalValues"
					} else {
						s.logger.Info("Global hook changed values, but ReloadAll ignored for the Synchronization task.")
					}
				}
			case hookTypes.AfterAll:
				if !hm.LastAfterAllHook && afterChecksum != beforeChecksum {
					s.logger.Info("Global hook changed values, but ReloadAll ignored: more AfterAll hooks to execute.")
				}

				// values are changed when afterAll hooks are executed
				if hm.LastAfterAllHook && afterChecksum != hm.ValuesChecksum {
					s.logger.Info("Global values changed by AfterAll hooks, will run ReloadAll.")

					reloadAll = true
					eventDescription = "AfterAll-Hooks-Change-GlobalValues"
				}
			}
			// Queue ReloadAllModules task
			if reloadAll {
				// if helm3lib is in use - reinit helm action configuration to update helm capabilities (newly available apiVersions and resoruce kinds)
				if s.helm.ClientType == helm.Helm3Lib {
					if err := helm3lib.ReinitActionConfig(s.logger.Named("helm3-client")); err != nil {
						s.logger.Error("Couldn't reinitialize helm3lib action configuration", log.Err(err))

						s.shellTask.UpdateFailureMessage(err.Error())
						s.shellTask.WithQueuedAt(time.Now())

						res.Status = sh_task.Fail

						return res
					}
				}

				// Stop and remove all resource monitors to prevent excessive ModuleRun tasks
				s.helmResourcesManager.StopMonitors()
				logLabels := s.shellTask.GetLogLabels()

				// Save event source info to add it as props to the task and use in logger later.
				triggeredBy := []slog.Attr{
					slog.String("event.triggered-by.hook", logLabels[pkg.LogKeyHook]),
					slog.String("event.triggered-by.binding", logLabels[pkg.LogKeyBinding]),
					slog.String("event.triggered-by.binding.name", logLabels[pkg.LogKeyBindingName]),
					slog.String("event.triggered-by.watchEvent", logLabels[pkg.LogKeyWatchEvent]),
				}

				delete(logLabels, pkg.LogKeyHook)
				delete(logLabels, pkg.LogKeyHookType)
				delete(logLabels, pkg.LogKeyBinding)
				delete(logLabels, pkg.LogKeyBindingName)
				delete(logLabels, pkg.LogKeyWatchEvent)

				// Reload all using "ConvergeModules" task.
				newTask := converge.NewConvergeModulesTask(eventDescription, converge.GlobalValuesChanged, logLabels)
				newTask.SetProp("triggered-by", triggeredBy)

				err := s.queueService.AddLastTaskToMain(newTask)
				if err != nil {
					s.logger.Error("failed to add ReloadAll task to the main queue", log.Err(err))
				}

				s.logTaskAdd("global values are changed, append", newTask)
			}
			// TODO rethink helm monitors pause-resume. It is not working well with parallel hooks without locks. But locks will destroy parallelization.
			// else {
			//	op.HelmResourcesManager.ResumeMonitors()
			//}
		}

		s.metricStorage.CounterAdd(metrics.GlobalHookAllowedErrorsTotal, allowed, metricLabels)
		s.metricStorage.CounterAdd(metrics.GlobalHookErrorsTotal, errors, metricLabels)
		s.metricStorage.CounterAdd(metrics.GlobalHookSuccessTotal, success, metricLabels)
	}

	if isSynchronization && res.Status == sh_task.Success {
		s.moduleManager.GlobalSynchronizationState().DoneForBinding(hm.KubernetesBindingId)

		// Unlock Kubernetes events for all monitors when Synchronization task is done.
		s.logger.Debug("Synchronization done, unlock Kubernetes events")
		for _, monitorID := range hm.MonitorIDs {
			taskHook.GetHookController().UnlockKubernetesEventsFor(monitorID)
		}
	}

	return res
}

// logTaskAdd prints info about queued tasks.
func (s *Task) logTaskAdd(action string, tasks ...sh_task.Task) {
	logger := s.logger.With(pkg.LogKeyTaskFlow, "add")
	for _, tsk := range tasks {
		logger.Info(helpers.TaskDescriptionForTaskFlowLog(tsk, action, "", ""))
	}
}
