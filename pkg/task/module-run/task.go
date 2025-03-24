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
	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/helm_resources_manager"
	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/module_manager/models/hooks"
	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
	"github.com/flant/addon-operator/pkg/task"
	"github.com/flant/addon-operator/pkg/task/helpers"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/shell-operator/pkg/hook/controller"
	htypes "github.com/flant/shell-operator/pkg/hook/types"
	"github.com/flant/shell-operator/pkg/metric"
	shell_operator "github.com/flant/shell-operator/pkg/shell-operator"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
	"github.com/flant/shell-operator/pkg/utils/measure"
)

type TaskConfig interface {
	GetEngine() *shell_operator.ShellOperator
	GetHelm() *helm.ClientFactory
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
			Helm:                 svc.GetHelm(),
			HelmResourcesManager: svc.GetHelmResourcesManager(),
			ModuleManager:        svc.GetModuleManager(),
			MetricStorage:        svc.GetMetricStorage(),
		}

		return newModuleRun(cfg, logger.Named("module-run"))
	}
}

type taskConfig struct {
	ShellTask         sh_task.Task
	IsOperatorStartup bool

	Engine               *shell_operator.ShellOperator
	Helm                 *helm.ClientFactory
	HelmResourcesManager helm_resources_manager.HelmResourcesManager
	ModuleManager        *module_manager.ModuleManager
	MetricStorage        metric.Storage
}

type Task struct {
	shellTask         sh_task.Task
	isOperatorStartup bool
	engine            *shell_operator.ShellOperator
	helm              *helm.ClientFactory
	// helmResourcesManager monitors absent resources created for modules.
	helmResourcesManager helm_resources_manager.HelmResourcesManager
	moduleManager        *module_manager.ModuleManager
	metricStorage        metric.Storage
	logger               *log.Logger

	// internals task.TaskInternals
}

// newModuleRun creates a new task handler service
func newModuleRun(cfg *taskConfig, logger *log.Logger) *Task {
	service := &Task{
		shellTask: cfg.ShellTask,

		isOperatorStartup: cfg.IsOperatorStartup,

		engine:               cfg.Engine,
		helm:                 cfg.Helm,
		helmResourcesManager: cfg.HelmResourcesManager,
		moduleManager:        cfg.ModuleManager,
		metricStorage:        cfg.MetricStorage,

		logger: logger,
	}

	return service
}

func (s *Task) Handle(_ context.Context) queue.TaskResult {
	defer trace.StartRegion(context.Background(), "ModuleRun").End()

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

	metricLabels := map[string]string{
		"module":     hm.ModuleName,
		"activation": taskLogLabels["event.type"],
	}

	defer measure.Duration(func(d time.Duration) {
		s.engine.MetricStorage.HistogramObserve("{PREFIX}module_run_seconds", d.Seconds(), metricLabels, nil)
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
			s.engine.MetricStorage.CounterAdd("{PREFIX}module_run_errors_total", 1.0, map[string]string{"module": hm.ModuleName})
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
			delete(newLabels, "task.id")
			newTask := sh_task.NewTask(task.ModuleRun).
				WithLogLabels(newLabels).
				WithQueueName(s.shellTask.GetQueueName()).
				WithMetadata(hm)

			res.AfterTasks = []sh_task.Task{newTask.WithQueuedAt(time.Now())}
			s.logTaskAdd("after", res.AfterTasks...)
		} else {
			s.logger.Info("ModuleRun success, module is ready")
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
				q := s.engine.TaskQueues.GetByName(tsk.GetQueueName())
				if q == nil {
					s.logger.Error("queue is not found while EnableKubernetesBindings task",
						slog.String("queue", tsk.GetQueueName()))
				} else {
					thm := task.HookMetadataAccessor(tsk)
					q.AddLast(tsk)
					baseModule.Synchronization().QueuedForBinding(thm)
				}
			}
			s.logTaskAdd("append", parallelSyncTasksToWait...)

			// Queue regular parallel tasks.
			for _, tsk := range parallelSyncTasks {
				q := s.engine.TaskQueues.GetByName(tsk.GetQueueName())
				if q == nil {
					s.logger.Error("queue is not found while EnableKubernetesBindings task",
						slog.String("queue", tsk.GetQueueName()))
				} else {
					q.AddLast(tsk)
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

	// Module start is done, module is ready to run hooks and helm chart.
	if baseModule.GetPhase() == modules.CanRunHelm {
		s.logger.Debug("ModuleRun phase", slog.String("phase", string(baseModule.GetPhase())))
		// run beforeHelm, helm, afterHelm
		valuesChanged, moduleRunErr = s.moduleManager.RunModule(baseModule.Name, s.shellTask.GetLogLabels())
	}

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
			if s.CreateAndStartQueue(hookBinding.Queue) {
				log.Debug("Queue started for module 'schedule'",
					slog.String("queue", hookBinding.Queue),
					slog.String("hook", hook.GetName()))
			}
		}
	}

	kubeEventsHooks := m.GetHooks(htypes.OnKubernetesEvent)
	for _, hook := range kubeEventsHooks {
		for _, hookBinding := range hook.GetHookConfig().OnKubernetesEvents {
			if s.CreateAndStartQueue(hookBinding.Queue) {
				log.Debug("Queue started for module 'kubernetes'",
					slog.String("queue", hookBinding.Queue),
					slog.String("hook", hook.GetName()))
			}
		}
	}
}

// CreateAndStartQueue creates a named queue and starts it.
// It returns false is queue is already created
func (s *Task) CreateAndStartQueue(_ string) bool {
	// TODO: uncomment when TaskHandler will be complete in service
	// if s.engine.TaskQueues.GetByName(queueName) != nil {
	// 	return false
	// }
	// s.engine.TaskQueues.NewNamedQueue(queueName, s.TaskHandler)
	// s.engine.TaskQueues.GetByName(queueName).Start(s.ctx)

	return true
}

// logTaskAdd prints info about queued tasks.
func (s *Task) logTaskAdd(action string, tasks ...sh_task.Task) {
	logger := s.logger.With(pkg.LogKeyTaskFlow, "add")
	for _, tsk := range tasks {
		logger.Info(helpers.TaskDescriptionForTaskFlowLog(tsk, action, "", ""))
	}
}
