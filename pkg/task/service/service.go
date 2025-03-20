package service

import (
	"context"
	"log/slog"

	"github.com/deckhouse/deckhouse/pkg/log"

	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/helm_resources_manager"
	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/task"
	globalhookenablekubernetesbindings "github.com/flant/addon-operator/pkg/task/global-hook-enable-kubernetes-bindings"
	globalhookenableschedulebindings "github.com/flant/addon-operator/pkg/task/global-hook-enable-schedule-bindings"
	globalhookrun "github.com/flant/addon-operator/pkg/task/global-hook-run"
	globalhookwaitkubernetessynchronization "github.com/flant/addon-operator/pkg/task/global-hook-wait-kubernetes-synchronization"
	"github.com/flant/shell-operator/pkg/metric"
	shell_operator "github.com/flant/shell-operator/pkg/shell-operator"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
)

type TaskHandlerServiceConfig struct {
	Engine               *shell_operator.ShellOperator
	Helm                 *helm.ClientFactory
	HelmResourcesManager helm_resources_manager.HelmResourcesManager
	ModuleManager        *module_manager.ModuleManager
	MetricStorage        metric.Storage
}

type TaskHandlerService struct {
	engine *shell_operator.ShellOperator

	helm *helm.ClientFactory

	// helmResourcesManager monitors absent resources created for modules.
	helmResourcesManager helm_resources_manager.HelmResourcesManager

	moduleManager *module_manager.ModuleManager

	metricStorage metric.Storage

	taskFactory map[sh_task.TaskType]func(t sh_task.Task) task.Task

	logger *log.Logger
}

// NewTaskHandlerService creates a new task handler service
func NewTaskHandlerService(config *TaskHandlerServiceConfig, logger *log.Logger) *TaskHandlerService {
	svc := &TaskHandlerService{
		engine:               config.Engine,
		helm:                 config.Helm,
		helmResourcesManager: config.HelmResourcesManager,
		moduleManager:        config.ModuleManager,
		metricStorage:        config.MetricStorage,
		logger:               logger,
	}

	svc.initFactory()

	return svc
}

func (s *TaskHandlerService) Handle(ctx context.Context, t sh_task.Task) queue.TaskResult {
	transformTask, ok := s.taskFactory[t.GetType()]
	if !ok {
		s.logger.Warn("TaskHandlerService: unknown task type", slog.String("task_type", string(t.GetType())))

		return queue.TaskResult{}
	}

	return transformTask(t).Handle(ctx)
}

func (s *TaskHandlerService) initFactory() {
	s.taskFactory = map[sh_task.TaskType]func(t sh_task.Task) task.Task{
		task.GlobalHookRun:                           globalhookrun.RegisterTaskHandler(s),
		task.GlobalHookEnableScheduleBindings:        globalhookenableschedulebindings.RegisterTaskHandler(s),
		task.GlobalHookEnableKubernetesBindings:      globalhookenablekubernetesbindings.RegisterTaskHandler(s),
		task.GlobalHookWaitKubernetesSynchronization: globalhookwaitkubernetessynchronization.RegisterTaskHandler(s),
	}
}

// GetEngine returns the shell operator engine
func (s *TaskHandlerService) GetEngine() *shell_operator.ShellOperator {
	return s.engine
}

// GetHelm returns the helm client factory
func (s *TaskHandlerService) GetHelm() *helm.ClientFactory {
	return s.helm
}

// GetHelmResourcesManager returns the helm resources manager
func (s *TaskHandlerService) GetHelmResourcesManager() helm_resources_manager.HelmResourcesManager {
	return s.helmResourcesManager
}

// GetModuleManager returns the module manager
func (s *TaskHandlerService) GetModuleManager() *module_manager.ModuleManager {
	return s.moduleManager
}

// GetMetricStorage returns the metric storage
func (s *TaskHandlerService) GetMetricStorage() metric.Storage {
	return s.metricStorage
}

// GetTaskFactory returns the task factory
func (s *TaskHandlerService) GetTaskFactory() map[sh_task.TaskType]func(t sh_task.Task) task.Task {
	return s.taskFactory
}

// GetLogger returns the logger
func (s *TaskHandlerService) GetLogger() *log.Logger {
	return s.logger
}
