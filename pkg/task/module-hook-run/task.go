package modulehookrun

import (
	"context"
	"log/slog"
	"runtime/trace"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"

	"github.com/flant/addon-operator/pkg"
	"github.com/flant/addon-operator/pkg/addon-operator/converge"
	"github.com/flant/addon-operator/pkg/helm_resources_manager"
	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/task"
	"github.com/flant/addon-operator/pkg/task/helpers"
	htypes "github.com/flant/shell-operator/pkg/hook/types"
	"github.com/flant/shell-operator/pkg/metric"
	shell_operator "github.com/flant/shell-operator/pkg/shell-operator"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
	"github.com/flant/shell-operator/pkg/utils/measure"
)

type TaskConfig interface {
	GetEngine() *shell_operator.ShellOperator
	GetHelmResourcesManager() helm_resources_manager.HelmResourcesManager
	GetModuleManager() *module_manager.ModuleManager
	GetMetricStorage() metric.Storage
}

func RegisterTaskHandler(svc TaskConfig) func(t sh_task.Task, logger *log.Logger) task.Task {
	return func(t sh_task.Task, logger *log.Logger) task.Task {
		cfg := &taskConfig{
			ShellTask:         t,
			IsOperatorStartup: helpers.IsOperatorStartupTask(t),

			Engine:               svc.GetEngine(),
			HelmResourcesManager: svc.GetHelmResourcesManager(),
			ModuleManager:        svc.GetModuleManager(),
			MetricStorage:        svc.GetMetricStorage(),
		}

		return newModuleHookRun(cfg, logger.Named("module-hook-run"))
	}
}

type taskConfig struct {
	ShellTask         sh_task.Task
	IsOperatorStartup bool

	Engine               *shell_operator.ShellOperator
	HelmResourcesManager helm_resources_manager.HelmResourcesManager
	ModuleManager        *module_manager.ModuleManager
	MetricStorage        metric.Storage
}

type Task struct {
	shellTask            sh_task.Task
	isOperatorStartup    bool
	engine               *shell_operator.ShellOperator
	helmResourcesManager helm_resources_manager.HelmResourcesManager
	moduleManager        *module_manager.ModuleManager
	metricStorage        metric.Storage
	logger               *log.Logger
}

// newModuleHookRun creates a new task handler service
func newModuleHookRun(cfg *taskConfig, logger *log.Logger) *Task {
	service := &Task{
		shellTask: cfg.ShellTask,

		isOperatorStartup: cfg.IsOperatorStartup,

		engine:               cfg.Engine,
		helmResourcesManager: cfg.HelmResourcesManager,
		moduleManager:        cfg.ModuleManager,
		metricStorage:        cfg.MetricStorage,

		logger: logger,
	}

	return service
}

func (s *Task) Handle(ctx context.Context) queue.TaskResult {
	defer trace.StartRegion(context.Background(), "ModuleHookRun").End()

	var res queue.TaskResult

	hm := task.HookMetadataAccessor(s.shellTask)
	baseModule := s.moduleManager.GetModule(hm.ModuleName)
	// TODO: check if module exists
	taskHook := baseModule.GetHookByName(hm.HookName)

	// Prevent hook running in parallel queue if module is disabled in "main" queue.
	if !s.moduleManager.IsModuleEnabled(baseModule.GetName()) {
		res.Status = queue.Success
		return res
	}

	err := taskHook.RateLimitWait(context.Background())
	if err != nil {
		// This could happen when the Context is
		// canceled, or the expected wait time exceeds the Context's Deadline.
		// The best we can do without proper context usage is to repeat the task.
		res.Status = queue.Repeat
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
		s.metricStorage.HistogramObserve("{PREFIX}module_hook_run_seconds", d.Seconds(), metricLabels, nil)
	})()

	shouldRunHook := true

	isSynchronization := hm.IsSynchronization()
	if isSynchronization {
		// Synchronization is not a part of v0 contract, skip hook execution.
		if taskHook.GetHookConfig().Version == "v0" {
			shouldRunHook = false
			res.Status = queue.Success
		}
		// Check for "executeOnSynchronization: false".
		if !hm.ExecuteOnSynchronization {
			shouldRunHook = false
			res.Status = queue.Success
		}
	}

	// Combine tasks in the queue and compact binding contexts for v1 hooks.
	if shouldRunHook && taskHook.GetHookConfig().Version == "v1" {
		combineResult := s.engine.CombineBindingContextForHook(
			s.engine.TaskQueues.GetByName(s.shellTask.GetQueueName()),
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

		beforeChecksum, afterChecksum, err := s.moduleManager.RunModuleHook(hm.ModuleName, hm.HookName, hm.BindingType, hm.BindingContext, s.shellTask.GetLogLabels())
		if err != nil {
			if hm.AllowFailure {
				allowed = 1.0
				s.logger.Info("Module hook failed, but allowed to fail.", log.Err(err))

				res.Status = queue.Success

				s.moduleManager.UpdateModuleHookStatusAndNotify(baseModule, hm.HookName, nil)
			} else {
				errors = 1.0

				s.logger.Error("Module hook failed, requeue task to retry after delay.",
					slog.Int("count", s.shellTask.GetFailureCount()+1),
					log.Err(err))

				s.shellTask.UpdateFailureMessage(err.Error())
				s.shellTask.WithQueuedAt(time.Now())

				res.Status = queue.Fail

				s.moduleManager.UpdateModuleHookStatusAndNotify(baseModule, hm.HookName, err)
			}
		} else {
			success = 1.0

			s.logger.Debug("Module hook success", slog.String("name", hm.HookName))

			res.Status = queue.Success
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
				hasTask := queueHasPendingModuleRunTask(s.engine.TaskQueues.GetMain(), hm.ModuleName)
				if !hasTask {
					newTask := sh_task.NewTask(task.ModuleRun).
						WithLogLabels(logLabels).
						WithQueueName("main").
						WithMetadata(task.HookMetadata{
							EventDescription: eventDescription,
							ModuleName:       hm.ModuleName,
						})

					newTask.SetProp("triggered-by", triggeredBy)

					s.engine.TaskQueues.GetMain().AddLast(newTask.WithQueuedAt(time.Now()))
					s.logTaskAdd("module values are changed, append", newTask)
				} else {
					s.logger.With(pkg.LogKeyTaskFlow, "noop").Info("module values are changed, ModuleRun task already queued")
				}
			}
		}

		s.metricStorage.CounterAdd("{PREFIX}module_hook_allowed_errors_total", allowed, metricLabels)
		s.metricStorage.CounterAdd("{PREFIX}module_hook_errors_total", errors, metricLabels)
		s.metricStorage.CounterAdd("{PREFIX}module_hook_success_total", success, metricLabels)
	}

	if isSynchronization && res.Status == queue.Success {
		baseModule.Synchronization().DoneForBinding(hm.KubernetesBindingId)
		// Unlock Kubernetes events for all monitors when Synchronization task is done.
		s.logger.Debug("Synchronization done, unlock Kubernetes events")

		for _, monitorID := range hm.MonitorIDs {
			taskHook.GetHookController().UnlockKubernetesEventsFor(monitorID)
		}
	}

	return res
}

// queueHasPendingModuleRunTask returns true if queue has pending tasks
// with the type "ModuleRun" related to the module "moduleName".
func queueHasPendingModuleRunTask(q *queue.TaskQueue, moduleName string) bool {
	if q == nil {
		return false
	}

	modules := modulesWithPendingModuleRun(q)
	_, has := modules[moduleName]

	return has
}

// modulesWithPendingModuleRun returns names of all modules in pending
// ModuleRun tasks. First task in queue considered not pending and is ignored.
func modulesWithPendingModuleRun(q *queue.TaskQueue) map[string]struct{} {
	if q == nil {
		return nil
	}

	modules := make(map[string]struct{})

	skipFirstTask := true

	q.Iterate(func(t sh_task.Task) {
		// Skip the first task in the queue as it can be executed already, i.e. "not pending".
		if skipFirstTask {
			skipFirstTask = false
			return
		}

		switch t.GetType() {
		case task.ModuleRun:
			hm := task.HookMetadataAccessor(t)
			modules[hm.ModuleName] = struct{}{}

		case task.ParallelModuleRun:
			hm := task.HookMetadataAccessor(t)
			for _, moduleName := range hm.ParallelRunMetadata.ListModules() {
				modules[moduleName] = struct{}{}
			}
		}
	})

	return modules
}

// logTaskAdd prints info about queued tasks.
func (s *Task) logTaskAdd(action string, tasks ...sh_task.Task) {
	logger := s.logger.With(pkg.LogKeyTaskFlow, "add")
	for _, tsk := range tasks {
		logger.Info(helpers.TaskDescriptionForTaskFlowLog(tsk, action, "", ""))
	}
}
