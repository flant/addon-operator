package applykubeconfigvalues

import (
	"context"
	"log/slog"
	"runtime/trace"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"

	"github.com/flant/addon-operator/pkg/kube_config_manager"
	"github.com/flant/addon-operator/pkg/kube_config_manager/config"
	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/metric"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
)

type TaskConfig interface {
	GetModuleManager() *module_manager.ModuleManager
	GetMetricStorage() metric.Storage
	GetKubeConfigManager() *kube_config_manager.KubeConfigManager
}

func RegisterTaskHandler(svc TaskConfig) func(t sh_task.Task, logger *log.Logger) task.Task {
	return func(t sh_task.Task, logger *log.Logger) task.Task {
		cfg := &taskConfig{
			ShellTask:         t,
			ModuleManager:     svc.GetModuleManager(),
			MetricStorage:     svc.GetMetricStorage(),
			KubeConfigManager: svc.GetKubeConfigManager(),
		}

		return newApplyKubeConfigValues(cfg, logger.Named("apply-kube-config-values"))
	}
}

type taskConfig struct {
	ShellTask sh_task.Task

	ModuleManager     *module_manager.ModuleManager
	MetricStorage     metric.Storage
	KubeConfigManager *kube_config_manager.KubeConfigManager
}

type Task struct {
	shellTask sh_task.Task

	moduleManager     *module_manager.ModuleManager
	metricStorage     metric.Storage
	kubeConfigManager *kube_config_manager.KubeConfigManager

	logger *log.Logger
}

// newApplyKubeConfigValues creates a new task handler service
func newApplyKubeConfigValues(cfg *taskConfig, logger *log.Logger) *Task {
	service := &Task{
		shellTask: cfg.ShellTask,

		moduleManager:     cfg.ModuleManager,
		metricStorage:     cfg.MetricStorage,
		kubeConfigManager: cfg.KubeConfigManager,

		logger: logger,
	}

	return service
}

func (s *Task) Handle(ctx context.Context) queue.TaskResult {
	defer trace.StartRegion(ctx, "HandleApplyKubeConfigValues").End()

	var (
		handleErr error
		res       queue.TaskResult
		hm        = task.HookMetadataAccessor(s.shellTask)
	)

	s.kubeConfigManager.SafeReadConfig(func(config *config.KubeConfig) {
		handleErr = s.moduleManager.ApplyNewKubeConfigValues(config, hm.GlobalValuesChanged)
	})

	if handleErr != nil {
		res.Status = queue.Fail

		s.logger.Error("HandleApplyKubeConfigValues failed, requeue task to retry after delay.",
			slog.Int("count", s.shellTask.GetFailureCount()+1),
			log.Err(handleErr))

		s.metricStorage.CounterAdd("{PREFIX}modules_discover_errors_total", 1.0, map[string]string{})

		s.shellTask.UpdateFailureMessage(handleErr.Error())
		s.shellTask.WithQueuedAt(time.Now())

		return res
	}

	res.Status = queue.Success

	s.logger.Debug("HandleApplyKubeConfigValues success")

	return res
}
