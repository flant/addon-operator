package modulehookrun

import (
	"context"
	"log/slog"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	metricsstorage "github.com/deckhouse/deckhouse/pkg/metrics-storage"
	"go.opentelemetry.io/otel"

	"github.com/flant/addon-operator/pkg"
	"github.com/flant/addon-operator/pkg/addon-operator/converge"
	"github.com/flant/addon-operator/pkg/helm_resources_manager"
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
	taskName = "module-hook-run"
)

// TaskDependencies defines the interface for accessing necessary components
type TaskDependencies interface {
	GetHelmResourcesManager() helm_resources_manager.HelmResourcesManager
	GetModuleManager() *module_manager.ModuleManager
	GetMetricStorage() metricsstorage.Storage
	GetQueueService() *taskqueue.Service
}

// RegisterTaskHandler creates a factory function for ModuleHookRun tasks
func RegisterTaskHandler(svc TaskDependencies) func(t sh_task.Task, logger *log.Logger) task.Task {
	return func(t sh_task.Task, logger *log.Logger) task.Task {
		return NewTask(
			t,
			helpers.IsOperatorStartupTask(t),
			svc.GetHelmResourcesManager(),
			svc.GetModuleManager(),
			svc.GetMetricStorage(),
			svc.GetQueueService(),
			logger.Named("module-hook-run"),
		)
	}
}

// Task handles running module hooks
type Task struct {
	shellTask         sh_task.Task
	isOperatorStartup bool

	helmResourcesManager helm_resources_manager.HelmResourcesManager
	moduleManager        *module_manager.ModuleManager
	metricStorage        metricsstorage.Storage
	queueService         *taskqueue.Service

	logger *log.Logger
}

// NewTask creates a new task handler for module hook runs
func NewTask(
	shellTask sh_task.Task,
	isOperatorStartup bool,
	helmResourcesManager helm_resources_manager.HelmResourcesManager,
	moduleManager *module_manager.ModuleManager,
	metricStorage metricsstorage.Storage,
	queueService *taskqueue.Service,
	logger *log.Logger,
) *Task {
	return &Task{
		shellTask:            shellTask,
		isOperatorStartup:    isOperatorStartup,
		helmResourcesManager: helmResourcesManager,
		moduleManager:        moduleManager,
		metricStorage:        metricStorage,
		queueService:         queueService,
		logger:               logger,
	}
}

func (s *Task) Handle(ctx context.Context) sh_task.Result {
	ctx, span := otel.Tracer(taskName).Start(ctx, "handle")
	defer span.End()

	var res sh_task.Result

	hm := task.HookMetadataAccessor(s.shellTask)
	baseModule := s.moduleManager.GetModule(hm.ModuleName)
	// TODO: check if module exists
	taskHook := baseModule.GetHookByName(hm.HookName)

	// Prevent hook running in parallel queue if module is disabled in "main" queue.
	if !s.moduleManager.IsModuleEnabled(baseModule.GetName()) {
		res.Status = sh_task.Success
		return res
	}

	err := taskHook.RateLimitWait(context.Background())
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
		pkg.MetricKeyModule:     hm.ModuleName,
		pkg.MetricKeyHook:       hm.HookName,
		pkg.MetricKeyBinding:    string(hm.BindingType),
		pkg.MetricKeyQueue:      s.shellTask.GetQueueName(),
		pkg.MetricKeyActivation: eventType,
	}

	defer measure.Duration(func(d time.Duration) {
		s.metricStorage.HistogramObserve(metrics.ModuleHookRunSeconds, d.Seconds(), metricLabels, nil)
	})()

	shouldRunHook := true

	isSynchronization := hm.IsSynchronization()
	if isSynchronization {
		// Synchronization is not a part of v0 contract, skip hook execution.
		if taskHook.GetHookConfig().Version == "v0" {
			shouldRunHook = false
			res.Status = sh_task.Success
		}
		// Check for "executeOnSynchronization: false".
		if !hm.ExecuteOnSynchronization {
			shouldRunHook = false
			res.Status = sh_task.Success
		}
	}

	// Combine tasks in the queue and compact binding contexts for v1 hooks.
	if shouldRunHook && taskHook.GetHookConfig().Version == "v1" {
		combineResult := s.queueService.CombineBindingContextForHook(
			s.shellTask.GetQueueName(),
			s.shellTask,
			func(tsk sh_task.Task) bool {
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
				// Task 'tsk' will be combined, so remove it from the SynchronizationState.
				if thm.IsSynchronization() {
					s.logger.Debug("Synchronization task is combined, mark it as Done",
						slog.String("name", thm.HookName),
						slog.String("binding", thm.Binding),
						slog.String("id", thm.KubernetesBindingId))

					baseModule.Synchronization().DoneForBinding(thm.KubernetesBindingId)
				}
				return false // do not stop combine process on this task
			})

		if combineResult != nil {
			hm.BindingContext = combineResult.BindingContexts
			// Extra monitor IDs can be returned if several Synchronization binding contexts are combined.
			if len(combineResult.MonitorIDs) > 0 {
				hm.MonitorIDs = append(hm.MonitorIDs, combineResult.MonitorIDs...)
			}

			s.logger.Debug("Got monitorIDs", slog.Any("monitorIDs", hm.MonitorIDs))

			s.shellTask.UpdateMetadata(hm)
		}
	}

	if shouldRunHook {
		// Module hook can recreate helm objects, so pause resources monitor.
		// Parallel hooks can interfere, so pause-resume only for hooks in the main queue.
		// FIXME pause-resume for parallel hooks
		if s.shellTask.GetQueueName() == "main" {
			s.helmResourcesManager.PauseMonitor(hm.ModuleName)
			defer s.helmResourcesManager.ResumeMonitor(hm.ModuleName)
		}

		errors := 0.0
		success := 0.0
		allowed := 0.0

		beforeChecksum, afterChecksum, err := s.moduleManager.RunModuleHook(ctx, hm.ModuleName, hm.HookName, hm.BindingType, hm.BindingContext, s.shellTask.GetLogLabels())
		if err != nil {
			if hm.AllowFailure {
				allowed = 1.0
				s.logger.Info("Module hook failed, but allowed to fail.", log.Err(err))

				res.Status = sh_task.Success

				s.moduleManager.UpdateModuleHookStatusAndNotify(baseModule, hm.HookName, nil)
			} else {
				errors = 1.0

				s.logger.Error("Module hook failed, requeue task to retry after delay.",
					slog.Int("count", s.shellTask.GetFailureCount()+1),
					log.Err(err))

				s.shellTask.UpdateFailureMessage(err.Error())
				s.shellTask.WithQueuedAt(time.Now())

				res.Status = sh_task.Fail

				s.moduleManager.UpdateModuleHookStatusAndNotify(baseModule, hm.HookName, err)
			}
		} else {
			success = 1.0

			s.logger.Debug("Module hook success", slog.String("name", hm.HookName))

			res.Status = sh_task.Success
			s.moduleManager.UpdateModuleHookStatusAndNotify(baseModule, hm.HookName, nil)

			// Handle module values change.
			reloadModule := false
			eventDescription := ""
			switch hm.BindingType {
			case htypes.Schedule:
				if beforeChecksum != afterChecksum {
					s.logger.Info("Module hook changed values, will restart ModuleRun.")

					reloadModule = true
					eventDescription = "Schedule-Change-ModuleValues"
				}
			case htypes.OnKubernetesEvent:
				// Do not reload module on changes during Synchronization.
				if beforeChecksum != afterChecksum {
					if hm.IsSynchronization() {
						s.logger.Info("Module hook changed values, but restart ModuleRun is ignored for the Synchronization task.")
					} else {
						s.logger.Info("Module hook changed values, will restart ModuleRun.")

						reloadModule = true
						eventDescription = "Kubernetes-Change-ModuleValues"
					}
				}
			}
			if reloadModule {
				// relabel
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

				// Do not add ModuleRun task if it is already queued.
				hasTask := s.queueService.MainQueueHasPendingModuleRunTask(hm.ModuleName)
				if !hasTask {
					newTask := sh_task.NewTask(task.ModuleRun).
						WithLogLabels(logLabels).
						WithQueueName("main").
						WithMetadata(task.HookMetadata{
							EventDescription: eventDescription,
							ModuleName:       hm.ModuleName,
						})

					newTask.SetProp("triggered-by", triggeredBy)

					_ = s.queueService.AddLastTaskToMain(newTask.WithQueuedAt(time.Now()))

					s.logTaskAdd("module values are changed, append", newTask)
				} else {
					s.logger.With(pkg.LogKeyTaskFlow, "noop").Info("module values are changed, ModuleRun task already queued")
				}
			}
		}

		s.metricStorage.CounterAdd(metrics.ModuleHookAllowedErrorsTotal, allowed, metricLabels)
		s.metricStorage.CounterAdd(metrics.ModuleHookErrorsTotal, errors, metricLabels)
		s.metricStorage.CounterAdd(metrics.ModuleHookSuccessTotal, success, metricLabels)
	}

	if isSynchronization && res.Status == sh_task.Success {
		baseModule.Synchronization().DoneForBinding(hm.KubernetesBindingId)
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
