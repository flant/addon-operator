package moduledelete

import (
	"context"
	"log/slog"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	metricsstorage "github.com/deckhouse/deckhouse/pkg/metrics-storage"
	"go.opentelemetry.io/otel"

	"github.com/flant/addon-operator/pkg/metrics"
	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/task"
	taskqueue "github.com/flant/addon-operator/pkg/task/queue"
	htypes "github.com/flant/shell-operator/pkg/hook/types"
	sh_task "github.com/flant/shell-operator/pkg/task"
)

const (
	taskName = "module-delete"
)

// TaskDependencies defines the interface for accessing necessary components
type TaskDependencies interface {
	GetModuleManager() *module_manager.ModuleManager
	GetMetricStorage() metricsstorage.Storage
	GetQueueService() *taskqueue.Service
}

// RegisterTaskHandler creates a factory function for ModuleDelete tasks
func RegisterTaskHandler(svc TaskDependencies) func(t sh_task.Task, logger *log.Logger) task.Task {
	return func(t sh_task.Task, logger *log.Logger) task.Task {
		return NewTask(
			t,
			svc.GetModuleManager(),
			svc.GetMetricStorage(),
			svc.GetQueueService(),
			logger.Named("module-delete"),
		)
	}
}

// Task handles module deletion
type Task struct {
	shellTask     sh_task.Task
	moduleManager *module_manager.ModuleManager
	metricStorage metricsstorage.Storage
	queueService  *taskqueue.Service
	logger        *log.Logger
}

// NewTask creates a new task handler for module deletion
func NewTask(
	shellTask sh_task.Task,
	moduleManager *module_manager.ModuleManager,
	metricStorage metricsstorage.Storage,
	queueService *taskqueue.Service,
	logger *log.Logger,
) *Task {
	return &Task{
		shellTask:     shellTask,
		moduleManager: moduleManager,
		metricStorage: metricStorage,
		queueService:  queueService,
		logger:        logger,
	}
}

func (s *Task) Handle(ctx context.Context) sh_task.Result {
	ctx, span := otel.Tracer(taskName).Start(ctx, "handle")
	defer span.End()

	var res sh_task.Result

	hm := task.HookMetadataAccessor(s.shellTask)

	baseModule := s.moduleManager.GetModule(hm.ModuleName)

	s.logger.Debug("Module delete", slog.String("name", hm.ModuleName))

	// Register module hooks to run afterHelmDelete hooks on startup.
	// It's a noop if registration is done before.
	// TODO: add filter to register only afterHelmDelete hooks
	err := s.moduleManager.RegisterModuleHooks(baseModule, s.shellTask.GetLogLabels())

	// TODO disable events and drain queues here or earlier during ConvergeModules.RunBeforeAll phase?
	if err == nil {
		// Disable events
		// op.ModuleManager.DisableModuleHooks(hm.ModuleName)
		// Remove all hooks from parallel queues.
		s.drainModuleQueues(hm.ModuleName)
		err = s.moduleManager.DeleteModule(ctx, hm.ModuleName, s.shellTask.GetLogLabels())
	}

	s.moduleManager.UpdateModuleLastErrorAndNotify(baseModule, err)

	if err != nil {
		s.metricStorage.CounterAdd(metrics.ModuleDeleteErrorsTotal, 1.0, map[string]string{"module": hm.ModuleName})

		s.logger.Error("Module delete failed, requeue task to retry after delay.",
			slog.Int("count", s.shellTask.GetFailureCount()+1),
			log.Err(err))

		s.shellTask.UpdateFailureMessage(err.Error())
		s.shellTask.WithQueuedAt(time.Now())

		res.Status = sh_task.Fail
	} else {
		s.logger.Debug("Module delete success", slog.String("name", hm.ModuleName))

		res.Status = sh_task.Success
	}

	return res
}

func (s *Task) drainModuleQueues(modName string) {
	m := s.moduleManager.GetModule(modName)
	if m == nil {
		s.logger.Warn("Module is absent when we try to drain its queue", slog.String("module", modName))
		return
	}

	scheduleHooks := m.GetHooks(htypes.Schedule)
	for _, hook := range scheduleHooks {
		for _, hookBinding := range hook.GetHookConfig().Schedules {
			s.queueService.DrainNonMainQueue(hookBinding.Queue)
		}
	}

	kubeEventsHooks := m.GetHooks(htypes.OnKubernetesEvent)
	for _, hook := range kubeEventsHooks {
		for _, hookBinding := range hook.GetHookConfig().OnKubernetesEvents {
			s.queueService.DrainNonMainQueue(hookBinding.Queue)
		}
	}

	// TODO: duplication here?
	// for _, hookName := range op.ModuleManager.GetModuleHookNames(modName) {
	//	h := op.ModuleManager.GetModuleHook(hookName)
	//	for _, hookBinding := range h.Get.Schedules {
	//		DrainNonMainQueue(op.engine.TaskQueues.GetByName(hookBinding.Queue))
	//	}
	//	for _, hookBinding := range h.Config.OnKubernetesEvents {
	//		DrainNonMainQueue(op.engine.TaskQueues.GetByName(hookBinding.Queue))
	//	}
	//}
}
