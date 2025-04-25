package service

import (
	"context"
	"log/slog"
	"sync"

	"github.com/deckhouse/deckhouse/pkg/log"

	"github.com/flant/addon-operator/pkg/addon-operator/converge"
	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/helm_resources_manager"
	"github.com/flant/addon-operator/pkg/kube_config_manager"
	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/task"
	applykubeconfigvalues "github.com/flant/addon-operator/pkg/task/apply-kube-config-values"
	convergemodules "github.com/flant/addon-operator/pkg/task/converge-modules"
	discoverhelmrelease "github.com/flant/addon-operator/pkg/task/discover-helm-release"
	globalhookenablekubernetesbindings "github.com/flant/addon-operator/pkg/task/global-hook-enable-kubernetes-bindings"
	globalhookenableschedulebindings "github.com/flant/addon-operator/pkg/task/global-hook-enable-schedule-bindings"
	globalhookrun "github.com/flant/addon-operator/pkg/task/global-hook-run"
	globalhookwaitkubernetessynchronization "github.com/flant/addon-operator/pkg/task/global-hook-wait-kubernetes-synchronization"
	moduledelete "github.com/flant/addon-operator/pkg/task/module-delete"
	moduleensurecrds "github.com/flant/addon-operator/pkg/task/module-ensure-crds"
	modulehookrun "github.com/flant/addon-operator/pkg/task/module-hook-run"
	modulepurge "github.com/flant/addon-operator/pkg/task/module-purge"
	modulerun "github.com/flant/addon-operator/pkg/task/module-run"
	paralleltask "github.com/flant/addon-operator/pkg/task/parallel"
	parallelmodulerun "github.com/flant/addon-operator/pkg/task/parallel-module-run"
	taskqueue "github.com/flant/addon-operator/pkg/task/queue"
	"github.com/flant/addon-operator/pkg/utils"
	klient "github.com/flant/kube-client/client"
	"github.com/flant/shell-operator/pkg/metric"
	shell_operator "github.com/flant/shell-operator/pkg/shell-operator"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
)

type TaskHandlerServiceConfig struct {
	Engine               *shell_operator.ShellOperator
	ParallelTaskChannels *paralleltask.TaskChannels
	Helm                 *helm.ClientFactory
	HelmResourcesManager helm_resources_manager.HelmResourcesManager
	ModuleManager        *module_manager.ModuleManager
	MetricStorage        metric.Storage
	KubeConfigManager    *kube_config_manager.KubeConfigManager
	ConvergeState        *converge.ConvergeState
	CRDExtraLabels       map[string]string
}

type TaskHandlerService struct {
	ctx context.Context

	kubeClient *klient.Client

	// a map of channels to communicate with parallel queues and its lock
	parallelTaskChannels *paralleltask.TaskChannels

	helm *helm.ClientFactory

	// helmResourcesManager monitors absent resources created for modules.
	helmResourcesManager helm_resources_manager.HelmResourcesManager

	moduleManager *module_manager.ModuleManager

	metricStorage     metric.Storage
	kubeConfigManager *kube_config_manager.KubeConfigManager

	queueService *taskqueue.Service

	convergeMu    sync.Mutex
	convergeState *converge.ConvergeState

	// crdExtraLabels contains labels for processing CRD files
	// like heritage=addon-operator
	crdExtraLabels map[string]string

	taskFactory map[sh_task.TaskType]func(t sh_task.Task, logger *log.Logger) task.Task

	logger *log.Logger
}

// NewTaskHandlerService creates a new task handler service
func NewTaskHandlerService(ctx context.Context, config *TaskHandlerServiceConfig, logger *log.Logger) *TaskHandlerService {
	svc := &TaskHandlerService{
		ctx:                  ctx,
		kubeClient:           config.Engine.KubeClient,
		parallelTaskChannels: config.ParallelTaskChannels,
		helm:                 config.Helm,
		helmResourcesManager: config.HelmResourcesManager,
		moduleManager:        config.ModuleManager,
		metricStorage:        config.MetricStorage,
		kubeConfigManager:    config.KubeConfigManager,
		convergeState:        config.ConvergeState,
		crdExtraLabels:       config.CRDExtraLabels,
		logger:               logger,
	}

	svc.queueService = taskqueue.NewService(ctx, &taskqueue.ServiceConfig{
		Engine: config.Engine,
		Handle: svc.Handle,
	}, logger.Named("task-queue-service"))

	svc.initFactory()

	return svc
}

// TaskHandler handles tasks in queue.
func (s *TaskHandlerService) Handle(ctx context.Context, t sh_task.Task) queue.TaskResult {
	taskLogLabels := t.GetLogLabels()
	logger := utils.EnrichLoggerWithLabels(s.logger, taskLogLabels)

	s.logTaskStart(t, logger)
	s.UpdateWaitInQueueMetric(t)

	transformTask, ok := s.taskFactory[t.GetType()]
	if !ok {
		s.logger.Error("TaskHandlerService: unknown task type", slog.String("task_type", string(t.GetType())))

		return queue.TaskResult{}
	}

	res := transformTask(t, logger).Handle(ctx)

	if res.Status == queue.Success {
		origAfterHandle := res.AfterHandle

		res.AfterHandle = func() {
			s.CheckConvergeStatus(t)
			if origAfterHandle != nil {
				origAfterHandle()
			}
		}
	}

	s.logTaskEnd(t, res, logger)

	return res
}

// ParallelHandle handles limited types of tasks in parallel queues.
func (s *TaskHandlerService) ParallelHandle(ctx context.Context, t sh_task.Task) queue.TaskResult {
	taskLogLabels := t.GetLogLabels()
	logger := utils.EnrichLoggerWithLabels(s.logger, taskLogLabels)

	s.logTaskStart(t, logger)
	s.UpdateWaitInQueueMetric(t)

	var transformTask func(t sh_task.Task, logger *log.Logger) task.Task

	switch t.GetType() {
	case task.ModuleRun, task.ModuleHookRun:
		var ok bool

		transformTask, ok = s.taskFactory[t.GetType()]
		if !ok {
			s.logger.Error("TaskHandlerService: unknown task type", slog.String("task_type", string(t.GetType())))

			return queue.TaskResult{}
		}
	}

	var res queue.TaskResult

	if transformTask != nil {
		res = transformTask(t, logger).Handle(ctx)
	}

	s.logTaskEnd(t, res, logger)

	hm := task.HookMetadataAccessor(t)
	if hm.ParallelRunMetadata == nil || len(hm.ParallelRunMetadata.ChannelId) == 0 {
		s.logger.Warn("Parallel task had no communication channel set")
	}

	if parallelChannel, ok := s.parallelTaskChannels.Get(hm.ParallelRunMetadata.ChannelId); ok {
		if res.Status == queue.Fail {
			parallelChannel.SendFailure(hm.ModuleName, t.GetFailureMessage())
		}

		if res.Status == queue.Success && t.GetType() == task.ModuleRun && len(res.AfterTasks) == 0 {
			parallelChannel.SendSuccess(hm.ModuleName)
		}
	}

	return res
}

func (s *TaskHandlerService) initFactory() {
	s.taskFactory = map[sh_task.TaskType]func(t sh_task.Task, logger *log.Logger) task.Task{
		task.GlobalHookRun:                           globalhookrun.RegisterTaskHandler(s),
		task.GlobalHookEnableScheduleBindings:        globalhookenableschedulebindings.RegisterTaskHandler(s),
		task.GlobalHookEnableKubernetesBindings:      globalhookenablekubernetesbindings.RegisterTaskHandler(s),
		task.GlobalHookWaitKubernetesSynchronization: globalhookwaitkubernetessynchronization.RegisterTaskHandler(s),
		task.DiscoverHelmReleases:                    discoverhelmrelease.RegisterTaskHandler(s),
		task.ApplyKubeConfigValues:                   applykubeconfigvalues.RegisterTaskHandler(s),
		task.ModuleDelete:                            moduledelete.RegisterTaskHandler(s),
		task.ModuleHookRun:                           modulehookrun.RegisterTaskHandler(s),
		task.ModulePurge:                             modulepurge.RegisterTaskHandler(s),
		task.ModuleEnsureCRDs:                        moduleensurecrds.RegisterTaskHandler(s),
		task.ModuleRun:                               modulerun.RegisterTaskHandler(s),
		task.ConvergeModules:                         convergemodules.RegisterTaskHandler(s),
		task.ParallelModuleRun:                       parallelmodulerun.RegisterTaskHandler(s),
	}
}

func (s *TaskHandlerService) GetKubeClient() *klient.Client {
	return s.kubeClient
}

func (s *TaskHandlerService) GetParallelTaskChannels() *paralleltask.TaskChannels {
	return s.parallelTaskChannels
}

func (s *TaskHandlerService) GetHelm() *helm.ClientFactory {
	return s.helm
}

func (s *TaskHandlerService) GetHelmResourcesManager() helm_resources_manager.HelmResourcesManager {
	return s.helmResourcesManager
}

func (s *TaskHandlerService) GetModuleManager() *module_manager.ModuleManager {
	return s.moduleManager
}

func (s *TaskHandlerService) GetMetricStorage() metric.Storage {
	return s.metricStorage
}

func (s *TaskHandlerService) GetKubeConfigManager() *kube_config_manager.KubeConfigManager {
	return s.kubeConfigManager
}

func (s *TaskHandlerService) GetQueueService() *taskqueue.Service {
	return s.queueService
}

func (s *TaskHandlerService) GetConvergeState() *converge.ConvergeState {
	return s.convergeState
}

func (s *TaskHandlerService) GetCRDExtraLabels() map[string]string {
	return s.crdExtraLabels
}

func (s *TaskHandlerService) GetTaskFactory() map[sh_task.TaskType]func(t sh_task.Task, logger *log.Logger) task.Task {
	return s.taskFactory
}
