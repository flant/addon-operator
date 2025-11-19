package applykubeconfigvalues

import (
	"context"
	"log/slog"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	metricsstorage "github.com/deckhouse/deckhouse/pkg/metrics-storage"
	"go.opentelemetry.io/otel"

	"github.com/flant/addon-operator/pkg/kube_config_manager"
	"github.com/flant/addon-operator/pkg/kube_config_manager/config"
	"github.com/flant/addon-operator/pkg/metrics"
	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/task"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
)

const (
	taskName = "apply-kube-config-values"
)

// TaskDependencies defines the interface for accessing necessary components
type TaskDependencies interface {
	GetModuleManager() *module_manager.ModuleManager
	GetMetricStorage() metricsstorage.Storage
	GetKubeConfigManager() *kube_config_manager.KubeConfigManager
}

// RegisterTaskHandler creates a factory function for ApplyKubeConfigValues tasks
func RegisterTaskHandler(svc TaskDependencies) func(t sh_task.Task, logger *log.Logger) task.Task {
	return func(t sh_task.Task, logger *log.Logger) task.Task {
		return NewTask(
			t,
			svc.GetModuleManager(),
			svc.GetMetricStorage(),
			svc.GetKubeConfigManager(),
			logger.Named("apply-kube-config-values"),
		)
	}
}

// Task handles applying Kubernetes configuration values
type Task struct {
	shellTask         sh_task.Task
	moduleManager     *module_manager.ModuleManager
	metricStorage     metricsstorage.Storage
	kubeConfigManager *kube_config_manager.KubeConfigManager
	logger            *log.Logger
}

// NewTask creates a new task handler for applying Kubernetes config values
func NewTask(
	shellTask sh_task.Task,
	moduleManager *module_manager.ModuleManager,
	metricStorage metricsstorage.Storage,
	kubeConfigManager *kube_config_manager.KubeConfigManager,
	logger *log.Logger,
) *Task {
	return &Task{
		shellTask:         shellTask,
		moduleManager:     moduleManager,
		metricStorage:     metricStorage,
		kubeConfigManager: kubeConfigManager,
		logger:            logger,
	}
}

func (s *Task) Handle(ctx context.Context) queue.TaskResult {
	_, span := otel.Tracer(taskName).Start(ctx, "handle")
	defer span.End()

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

		s.metricStorage.CounterAdd(metrics.ModulesDiscoverErrorsTotal, 1.0, map[string]string{})

		s.shellTask.UpdateFailureMessage(handleErr.Error())
		s.shellTask.WithQueuedAt(time.Now())

		return res
	}

	res.Status = queue.Success

	s.logger.Debug("HandleApplyKubeConfigValues success")

	return res
}
