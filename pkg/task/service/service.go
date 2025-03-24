package service

import (
	"context"
	"log/slog"

	"github.com/deckhouse/deckhouse/pkg/log"

	"github.com/flant/addon-operator/pkg"
	"github.com/flant/addon-operator/pkg/addon-operator/converge"
	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/helm_resources_manager"
	"github.com/flant/addon-operator/pkg/kube_config_manager"
	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
	"github.com/flant/addon-operator/pkg/task"
	applykubeconfigvalues "github.com/flant/addon-operator/pkg/task/apply-kube-config-values"
	discoverhelmrelease "github.com/flant/addon-operator/pkg/task/discover-helm-release"
	globalhookenablekubernetesbindings "github.com/flant/addon-operator/pkg/task/global-hook-enable-kubernetes-bindings"
	globalhookenableschedulebindings "github.com/flant/addon-operator/pkg/task/global-hook-enable-schedule-bindings"
	globalhookrun "github.com/flant/addon-operator/pkg/task/global-hook-run"
	globalhookwaitkubernetessynchronization "github.com/flant/addon-operator/pkg/task/global-hook-wait-kubernetes-synchronization"
	"github.com/flant/addon-operator/pkg/task/helpers"
	moduledelete "github.com/flant/addon-operator/pkg/task/module-delete"
	moduleensurecrds "github.com/flant/addon-operator/pkg/task/module-ensure-crds"
	modulehookrun "github.com/flant/addon-operator/pkg/task/module-hook-run"
	modulepurge "github.com/flant/addon-operator/pkg/task/module-purge"
	"github.com/flant/addon-operator/pkg/utils"
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
	KubeConfigManager    *kube_config_manager.KubeConfigManager
	ConvergeState        *converge.ConvergeState
	CRDExtraLabels       map[string]string
}

type TaskHandlerService struct {
	engine *shell_operator.ShellOperator

	helm *helm.ClientFactory

	// helmResourcesManager monitors absent resources created for modules.
	helmResourcesManager helm_resources_manager.HelmResourcesManager

	moduleManager *module_manager.ModuleManager

	metricStorage     metric.Storage
	kubeConfigManager *kube_config_manager.KubeConfigManager

	convergeState *converge.ConvergeState

	// crdExtraLabels contains labels for processing CRD files
	// like heritage=addon-operator
	crdExtraLabels map[string]string

	taskFactory map[sh_task.TaskType]func(t sh_task.Task, logger *log.Logger) task.Task

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
		kubeConfigManager:    config.KubeConfigManager,
		convergeState:        config.ConvergeState,
		crdExtraLabels:       config.CRDExtraLabels,
		logger:               logger,
	}

	svc.initFactory()

	return svc
}

func (s *TaskHandlerService) Handle(ctx context.Context, t sh_task.Task) queue.TaskResult {
	taskLogLabels := t.GetLogLabels()
	logger := utils.EnrichLoggerWithLabels(s.logger, taskLogLabels)

	// uncomment after complete handler refactoring
	// s.logTaskStart(t, logger)

	transformTask, ok := s.taskFactory[t.GetType()]
	if !ok {
		s.logger.Warn("TaskHandlerService: unknown task type", slog.String("task_type", string(t.GetType())))

		return queue.TaskResult{}
	}

	res := transformTask(t, logger).Handle(ctx)

	// uncomment after complete handler refactoring
	// s.logTaskEnd(t, res, logger)

	return res
}

// logTaskStart prints info about task at start. Also prints event source info from task props.
func (s *TaskHandlerService) logTaskStart(tsk sh_task.Task, logger *log.Logger) { //nolint:unused
	// Prevent excess messages for highly frequent tasks.
	if tsk.GetType() == task.GlobalHookWaitKubernetesSynchronization {
		return
	}

	if tsk.GetType() == task.ModuleRun {
		hm := task.HookMetadataAccessor(tsk)
		baseModule := s.moduleManager.GetModule(hm.ModuleName)

		if baseModule.GetPhase() == modules.WaitForSynchronization {
			return
		}
	}

	logger = logger.With(pkg.LogKeyTaskFlow, "start")

	if triggeredBy, ok := tsk.GetProp("triggered-by").([]slog.Attr); ok {
		for _, attr := range triggeredBy {
			logger = logger.With(attr)
		}
	}

	logger.Info(helpers.TaskDescriptionForTaskFlowLog(tsk, "start", s.taskPhase(tsk), ""))
}

// logTaskEnd prints info about task at the end. Info level used only for the ConvergeModules task.
func (s *TaskHandlerService) logTaskEnd(tsk sh_task.Task, result queue.TaskResult, logger *log.Logger) { //nolint:unused
	logger = logger.With(pkg.LogKeyTaskFlow, "end")

	level := log.LevelDebug
	if tsk.GetType() == task.ConvergeModules {
		level = log.LevelInfo
	}

	logger.Log(context.TODO(), level.Level(), helpers.TaskDescriptionForTaskFlowLog(tsk, "end", s.taskPhase(tsk), string(result.Status)))
}

func (s *TaskHandlerService) taskPhase(tsk sh_task.Task) string { //nolint:unused
	switch tsk.GetType() {
	case task.ConvergeModules:
		// return string(s.ConvergeState.Phase)
	case task.ModuleRun:
		hm := task.HookMetadataAccessor(tsk)
		mod := s.moduleManager.GetModule(hm.ModuleName)
		return string(mod.GetPhase())
	}
	return ""
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
	}
}

func (s *TaskHandlerService) GetEngine() *shell_operator.ShellOperator {
	return s.engine
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

func (s *TaskHandlerService) GetConvergeState() *converge.ConvergeState {
	return s.convergeState
}

func (s *TaskHandlerService) GetCRDExtraLabels() map[string]string {
	return s.crdExtraLabels
}

func (s *TaskHandlerService) GetTaskFactory() map[sh_task.TaskType]func(t sh_task.Task, logger *log.Logger) task.Task {
	return s.taskFactory
}
