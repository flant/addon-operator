package modulerun

import (
	"context"
	"log/slog"
	"runtime/trace"
	"strings"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/gofrs/uuid/v5"

	"github.com/flant/addon-operator/pkg"
	"github.com/flant/addon-operator/pkg/addon-operator/converge"
	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/module_manager/models/hooks"
	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
	"github.com/flant/addon-operator/pkg/task"
	"github.com/flant/addon-operator/pkg/task/helpers"
	taskqueue "github.com/flant/addon-operator/pkg/task/queue"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/shell-operator/pkg/hook/controller"
	htypes "github.com/flant/shell-operator/pkg/hook/types"
	"github.com/flant/shell-operator/pkg/metric"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
	"github.com/flant/shell-operator/pkg/utils/measure"
)

// TaskDependencies defines the interface for accessing necessary components
type TaskDependencies interface {
	GetModuleManager() *module_manager.ModuleManager
	GetMetricStorage() metric.Storage
	GetQueueService() *taskqueue.Service
}

// RegisterTaskHandler creates a factory function for ModuleRun tasks
func RegisterTaskHandler(svc TaskDependencies) func(t sh_task.Task, logger *log.Logger) task.Task {
	return func(t sh_task.Task, logger *log.Logger) task.Task {
		return NewTask(
			t,
			helpers.IsOperatorStartupTask(t),
			svc.GetModuleManager(),
			svc.GetMetricStorage(),
			svc.GetQueueService(),
			logger.Named("module-run"),
		)
	}
}

// Task handles module execution
type Task struct {
	shellTask         sh_task.Task
	isOperatorStartup bool
	moduleManager     *module_manager.ModuleManager
	metricStorage     metric.Storage
	queueService      *taskqueue.Service
	logger            *log.Logger
}

// NewTask creates a new task handler for running modules
func NewTask(
	shellTask sh_task.Task,
	isOperatorStartup bool,
	moduleManager *module_manager.ModuleManager,
	metricStorage metric.Storage,
	queueService *taskqueue.Service,
	logger *log.Logger,
) *Task {
	return &Task{
		shellTask:         shellTask,
		isOperatorStartup: isOperatorStartup,
		moduleManager:     moduleManager,
		metricStorage:     metricStorage,
		queueService:      queueService,
		logger:            logger,
	}
}

// Handle starts a module by executing module hooks and installing a Helm chart.
//
// Execution sequence:
// - Run onStartup hooks.
// - Queue kubernetes hooks as Synchronization tasks.
// - Wait until Synchronization tasks are done to fill all snapshots.
// - Enable kubernetes events.
// - Enable schedule events.
// - Run beforeHelm hooks
// - Run Helm to install or upgrade a module's chart.
// - Run afterHelm hooks.
//
// ModuleRun is restarted if hook or chart is failed.
// After first Handle success, no onStartup and kubernetes.Synchronization tasks will run.
func (s *Task) Handle(ctx context.Context) queue.TaskResult {
	defer trace.StartRegion(ctx, "ModuleRun").End()

	var res queue.TaskResult

	taskLogLabels := s.shellTask.GetLogLabels()
	s.logger = utils.EnrichLoggerWithLabels(s.logger, taskLogLabels)

	hm := task.HookMetadataAccessor(s.shellTask)
	baseModule := s.moduleManager.GetModule(hm.ModuleName)

	// Break error loop when module becomes disabled.
	if !s.moduleManager.IsModuleEnabled(baseModule.GetName()) {
		res.Status = queue.Success
		return res
	}

	eventType := ""
	if s.isOperatorStartup {
		eventType = converge.OperatorStartup.String()
	}

	metricLabels := map[string]string{
		pkg.MetricKeyModule:     hm.ModuleName,
		pkg.MetricKeyActivation: eventType,
	}

	defer measure.Duration(func(d time.Duration) {
		s.metricStorage.HistogramObserve("{PREFIX}module_run_seconds", d.Seconds(), metricLabels, nil)
	})()

	var moduleRunErr error
	valuesChanged := false

	defer func(res *queue.TaskResult, valuesChanged *bool) {
		s.moduleManager.UpdateModuleLastErrorAndNotify(baseModule, moduleRunErr)
		if moduleRunErr != nil {
			res.Status = queue.Fail

			s.logger.Error("ModuleRun failed. Requeue task to retry after delay.",
				slog.String("phase", string(baseModule.GetPhase())),
				slog.Int("count", s.shellTask.GetFailureCount()+1),
				log.Err(moduleRunErr))

			s.metricStorage.CounterAdd("{PREFIX}module_run_errors_total", 1.0, map[string]string{"module": hm.ModuleName})
			s.shellTask.UpdateFailureMessage(moduleRunErr.Error())
			s.shellTask.WithQueuedAt(time.Now())
		}

		if res.Status != queue.Success {
			return
		}

		if *valuesChanged {
			s.logger.Info("ModuleRun success, values changed, restart module")

			// One of afterHelm hooks changes values, run ModuleRun again: copy task, but disable startup hooks.
			hm.DoModuleStartup = false
			hm.EventDescription = "AfterHelm-Hooks-Change-Values"
			newLabels := utils.MergeLabels(s.shellTask.GetLogLabels())

			delete(newLabels, pkg.LogKeyTaskID)

			newTask := sh_task.NewTask(task.ModuleRun).
				WithLogLabels(newLabels).
				WithQueueName(s.shellTask.GetQueueName()).
				WithMetadata(hm)

			res.AfterTasks = []sh_task.Task{newTask.WithQueuedAt(time.Now())}
			s.logTaskAdd("after", res.AfterTasks...)
		} else {
			s.logger.Info("ModuleRun success, module is ready")
			// if values not changed we do not need to make another task
			// so we think that module made all the things what it can
			s.moduleManager.SetModulePhaseAndNotify(baseModule, modules.Ready)
		}
	}(&res, &valuesChanged)

	// First module run on operator startup or when module is enabled.
	if baseModule.GetPhase() == modules.Startup {
		// Register module hooks on every enable.
		moduleRunErr = s.moduleManager.RegisterModuleHooks(baseModule, taskLogLabels)
		if moduleRunErr == nil {
			if hm.DoModuleStartup {
				s.logger.Debug("ModuleRun phase",
					slog.String("phase", string(baseModule.GetPhase())))

				treg := trace.StartRegion(context.Background(), "ModuleRun-OnStartup")

				// Start queues for module hooks.
				s.CreateAndStartQueuesForModuleHooks(baseModule.GetName())

				// Run onStartup hooks.
				moduleRunErr = s.moduleManager.RunModuleHooks(baseModule, htypes.OnStartup, s.shellTask.GetLogLabels())
				if moduleRunErr == nil {
					s.moduleManager.SetModulePhaseAndNotify(baseModule, modules.OnStartupDone)
				}
				treg.End()
			} else {
				s.moduleManager.SetModulePhaseAndNotify(baseModule, modules.OnStartupDone)
			}

			res.Status = queue.Repeat

			return res
		}
	}

	if baseModule.GetPhase() == modules.OnStartupDone {
		s.logger.Debug("ModuleRun phase", slog.String("phase", string(baseModule.GetPhase())))
		if baseModule.HasKubernetesHooks() {
			s.moduleManager.SetModulePhaseAndNotify(baseModule, modules.QueueSynchronizationTasks)
		} else {
			// Skip Synchronization process if there are no kubernetes hooks.
			s.moduleManager.SetModulePhaseAndNotify(baseModule, modules.EnableScheduleBindings)
		}

		res.Status = queue.Repeat

		return res
	}

	// Note: All hooks should be queued to fill snapshots before proceed to beforeHelm hooks.
	if baseModule.GetPhase() == modules.QueueSynchronizationTasks {
		s.logger.Debug("ModuleRun phase", slog.String("phase", string(baseModule.GetPhase())))

		// ModuleHookRun.Synchronization tasks for bindings with the "main" queue.
		mainSyncTasks := make([]sh_task.Task, 0)
		// ModuleHookRun.Synchronization tasks to add in parallel queues.
		parallelSyncTasks := make([]sh_task.Task, 0)
		// Wait for these ModuleHookRun.Synchronization tasks from parallel queues.
		parallelSyncTasksToWait := make([]sh_task.Task, 0)

		// Start monitors for each kubernetes binding in each module hook.
		err := s.moduleManager.HandleModuleEnableKubernetesBindings(hm.ModuleName, func(hook *hooks.ModuleHook, info controller.BindingExecutionInfo) {
			queueName := info.QueueName
			if queueName == "main" && strings.HasPrefix(s.shellTask.GetQueueName(), app.ParallelQueuePrefix) {
				// override main queue with parallel queue
				queueName = s.shellTask.GetQueueName()
			}

			taskLogLabels := utils.MergeLabels(s.shellTask.GetLogLabels(), map[string]string{
				"binding":   string(htypes.OnKubernetesEvent) + "Synchronization",
				"module":    hm.ModuleName,
				"hook":      hook.GetName(),
				"hook.type": "module",
				"queue":     queueName,
			})

			if len(info.BindingContext) > 0 {
				taskLogLabels["binding.name"] = info.BindingContext[0].Binding
			}

			delete(taskLogLabels, "task.id")

			kubernetesBindingID := uuid.Must(uuid.NewV4()).String()
			parallelRunMetadata := &task.ParallelRunMetadata{}
			if hm.ParallelRunMetadata != nil && len(hm.ParallelRunMetadata.ChannelId) != 0 {
				parallelRunMetadata.ChannelId = hm.ParallelRunMetadata.ChannelId
			}

			taskMeta := task.HookMetadata{
				EventDescription:         hm.EventDescription,
				ModuleName:               hm.ModuleName,
				HookName:                 hook.GetName(),
				BindingType:              htypes.OnKubernetesEvent,
				BindingContext:           info.BindingContext,
				AllowFailure:             info.AllowFailure,
				KubernetesBindingId:      kubernetesBindingID,
				WaitForSynchronization:   info.KubernetesBinding.WaitForSynchronization,
				MonitorIDs:               []string{info.KubernetesBinding.Monitor.Metadata.MonitorId},
				ExecuteOnSynchronization: info.KubernetesBinding.ExecuteHookOnSynchronization,
				ParallelRunMetadata:      parallelRunMetadata,
			}

			newTask := sh_task.NewTask(task.ModuleHookRun).
				WithLogLabels(taskLogLabels).
				WithQueueName(queueName).
				WithMetadata(taskMeta)
			newTask.WithQueuedAt(time.Now())

			if queueName == s.shellTask.GetQueueName() {
				// Ignore "waitForSynchronization: false" for hooks in the main queue.
				// There is no way to not wait for these hooks.
				mainSyncTasks = append(mainSyncTasks, newTask)
			} else {
				// Do not wait for parallel hooks on "waitForSynchronization: false".
				if info.KubernetesBinding.WaitForSynchronization {
					parallelSyncTasksToWait = append(parallelSyncTasksToWait, newTask)
				} else {
					parallelSyncTasks = append(parallelSyncTasks, newTask)
				}
			}
		})
		if err != nil {
			// Fail to enable bindings: cannot start Kubernetes monitors.
			moduleRunErr = err
		} else {
			// Queue parallel tasks that should be waited.
			for _, tsk := range parallelSyncTasksToWait {
				if err := s.queueService.AddLastTaskToQueue(tsk.GetQueueName(), tsk); err != nil {
					s.logger.Error("queue is not found while EnableKubernetesBindings task",
						slog.String("queue", tsk.GetQueueName()))

					continue
				}

				thm := task.HookMetadataAccessor(tsk)
				baseModule.Synchronization().QueuedForBinding(thm)
			}

			s.logTaskAdd("append", parallelSyncTasksToWait...)

			// Queue regular parallel tasks.
			for _, tsk := range parallelSyncTasks {
				if err := s.queueService.AddLastTaskToQueue(tsk.GetQueueName(), tsk); err != nil {
					s.logger.Error("queue is not found while EnableKubernetesBindings task",
						slog.String("queue", tsk.GetQueueName()))
				}
			}

			s.logTaskAdd("append", parallelSyncTasks...)

			if len(parallelSyncTasksToWait) == 0 {
				// Skip waiting tasks in parallel queues, proceed to schedule bindings.
				s.moduleManager.SetModulePhaseAndNotify(baseModule, modules.EnableScheduleBindings)
			} else {
				// There are tasks to wait.
				s.moduleManager.SetModulePhaseAndNotify(baseModule, modules.WaitForSynchronization)
				s.logger.With("module.state", "wait-for-synchronization").
					Debug("ModuleRun wait for Synchronization")
			}

			// Put Synchronization tasks for kubernetes hooks before ModuleRun task.
			if len(mainSyncTasks) > 0 {
				res.HeadTasks = mainSyncTasks
				res.Status = queue.Keep
				s.logTaskAdd("head", mainSyncTasks...)
				return res
			}
		}

		res.Status = queue.Repeat

		return res
	}

	// Repeat ModuleRun if there are running Synchronization tasks to wait.
	if baseModule.GetPhase() == modules.WaitForSynchronization {
		if baseModule.Synchronization().IsCompleted() {
			// Proceed with the next phase.
			s.moduleManager.SetModulePhaseAndNotify(baseModule, modules.EnableScheduleBindings)
			s.logger.Info("Synchronization done for module hooks")
		} else {
			// Debug messages every fifth second: print Synchronization state.
			if time.Now().UnixNano()%5000000000 == 0 {
				s.logger.Debug("ModuleRun wait Synchronization state",
					slog.Bool("moduleStartup", hm.DoModuleStartup),
					slog.Bool("syncNeeded", baseModule.SynchronizationNeeded()),
					slog.Bool("syncQueued", baseModule.Synchronization().HasQueued()),
					slog.Bool("syncDone", baseModule.Synchronization().IsCompleted()))
				baseModule.Synchronization().DebugDumpState(s.logger)
			}
			s.logger.Debug("Synchronization not completed, keep ModuleRun task in repeat mode")
			s.shellTask.WithQueuedAt(time.Now())
		}

		res.Status = queue.Repeat

		return res
	}

	// Enable schedule events once at module start.
	if baseModule.GetPhase() == modules.EnableScheduleBindings {
		s.logger.Debug("ModuleRun phase", slog.String("phase", string(baseModule.GetPhase())))

		s.moduleManager.EnableModuleScheduleBindings(hm.ModuleName)
		s.moduleManager.SetModulePhaseAndNotify(baseModule, modules.CanRunHelm)

		res.Status = queue.Repeat

		return res
	}

	// If the module is already in Ready state and we receive another ModuleRun task,
	// reset it to CanRunHelm state to allow the module's Helm chart to be reapplied
	if baseModule.GetPhase() == modules.Ready {
		s.moduleManager.SetModulePhaseAndNotify(baseModule, modules.CanRunHelm)
	}

	// Module start is done, module is ready to run hooks and helm chart.
	if baseModule.GetPhase() == modules.CanRunHelm {
		s.logger.Debug("ModuleRun phase", slog.String("phase", string(baseModule.GetPhase())))
		// run beforeHelm, helm, afterHelm
		valuesChanged, moduleRunErr = s.moduleManager.RunModule(baseModule.Name, s.shellTask.GetLogLabels())
	}

	// this is task success, but not guarantee that module in Ready phase
	res.Status = queue.Success

	return res
}

// CreateAndStartQueuesForModuleHooks creates queues for registered module hooks.
// It is safe to run this method multiple times, as it checks
// for existing queues.
func (s *Task) CreateAndStartQueuesForModuleHooks(moduleName string) {
	m := s.moduleManager.GetModule(moduleName)
	if m == nil {
		return
	}

	scheduleHooks := m.GetHooks(htypes.Schedule)
	for _, hook := range scheduleHooks {
		for _, hookBinding := range hook.GetHookConfig().Schedules {
			if !s.queueService.IsQueueExists(hookBinding.Queue) {
				s.queueService.CreateAndStartQueue(hookBinding.Queue)

				log.Debug("Queue started for module 'schedule'",
					slog.String("queue", hookBinding.Queue),
					slog.String("hook", hook.GetName()))
			}
		}
	}

	kubeEventsHooks := m.GetHooks(htypes.OnKubernetesEvent)
	for _, hook := range kubeEventsHooks {
		for _, hookBinding := range hook.GetHookConfig().OnKubernetesEvents {
			if !s.queueService.IsQueueExists(hookBinding.Queue) {
				s.queueService.CreateAndStartQueue(hookBinding.Queue)

				log.Debug("Queue started for module 'kubernetes'",
					slog.String("queue", hookBinding.Queue),
					slog.String("hook", hook.GetName()))
			}
		}
	}
}

// logTaskAdd prints info about queued tasks.
func (s *Task) logTaskAdd(action string, tasks ...sh_task.Task) {
	logger := s.logger.With(pkg.LogKeyTaskFlow, "add")
	for _, tsk := range tasks {
		logger.Info(helpers.TaskDescriptionForTaskFlowLog(tsk, action, "", ""))
	}
}
