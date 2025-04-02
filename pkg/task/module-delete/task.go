package moduledelete

import (
	"context"
	"log/slog"
	"runtime/trace"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"

	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/task"
	htypes "github.com/flant/shell-operator/pkg/hook/types"
	"github.com/flant/shell-operator/pkg/metric"
	shell_operator "github.com/flant/shell-operator/pkg/shell-operator"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
)

type TaskConfig interface {
	GetEngine() *shell_operator.ShellOperator
	GetModuleManager() *module_manager.ModuleManager
	GetMetricStorage() metric.Storage
}

func RegisterTaskHandler(svc TaskConfig) func(t sh_task.Task, logger *log.Logger) task.Task {
	return func(t sh_task.Task, logger *log.Logger) task.Task {
		cfg := &taskConfig{
			ShellTask: t,

			Engine:        svc.GetEngine(),
			ModuleManager: svc.GetModuleManager(),
			MetricStorage: svc.GetMetricStorage(),
		}

		return newModuleDelete(cfg, logger.Named("module-delete"))
	}
}

type taskConfig struct {
	ShellTask sh_task.Task

	Engine        *shell_operator.ShellOperator
	ModuleManager *module_manager.ModuleManager
	MetricStorage metric.Storage
}

type Task struct {
	shellTask sh_task.Task

	engine        *shell_operator.ShellOperator
	moduleManager *module_manager.ModuleManager
	metricStorage metric.Storage

	logger *log.Logger
}

// newModuleDelete creates a new task handler service
func newModuleDelete(cfg *taskConfig, logger *log.Logger) *Task {
	service := &Task{
		shellTask: cfg.ShellTask,

		engine:        cfg.Engine,
		moduleManager: cfg.ModuleManager,
		metricStorage: cfg.MetricStorage,

		logger: logger,
	}

	return service
}

func (s *Task) Handle(ctx context.Context) queue.TaskResult {
	defer trace.StartRegion(ctx, "ModuleDelete").End()

	var res queue.TaskResult

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
		err = s.moduleManager.DeleteModule(hm.ModuleName, s.shellTask.GetLogLabels())
	}

	s.moduleManager.UpdateModuleLastErrorAndNotify(baseModule, err)

	if err != nil {
		s.metricStorage.CounterAdd("{PREFIX}module_delete_errors_total", 1.0, map[string]string{"module": hm.ModuleName})

		s.logger.Error("Module delete failed, requeue task to retry after delay.",
			slog.Int("count", s.shellTask.GetFailureCount()+1),
			log.Err(err))

		s.shellTask.UpdateFailureMessage(err.Error())
		s.shellTask.WithQueuedAt(time.Now())

		res.Status = queue.Fail
	} else {
		s.logger.Debug("Module delete success", slog.String("name", hm.ModuleName))

		res.Status = queue.Success
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
			drainNonMainQueue(s.engine.TaskQueues.GetByName(hookBinding.Queue))
		}
	}

	kubeEventsHooks := m.GetHooks(htypes.OnKubernetesEvent)
	for _, hook := range kubeEventsHooks {
		for _, hookBinding := range hook.GetHookConfig().OnKubernetesEvents {
			drainNonMainQueue(s.engine.TaskQueues.GetByName(hookBinding.Queue))
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

func drainNonMainQueue(q *queue.TaskQueue) {
	if q == nil || q.Name == "main" {
		return
	}

	// Remove all tasks.
	q.Filter(func(_ sh_task.Task) bool {
		return false
	})
}
