package addon_operator

import (
	"context"
	"fmt"

	sh_app "github.com/flant/shell-operator/pkg/app"
	"github.com/flant/shell-operator/pkg/config"
	"github.com/flant/shell-operator/pkg/debug"
	shell_operator "github.com/flant/shell-operator/pkg/shell-operator"
	log "github.com/sirupsen/logrus"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/kube_config_manager"
	"github.com/flant/addon-operator/pkg/module_manager"
)

// Bootstrap inits all dependencies for a full-fledged AddonOperator instance.
func Bootstrap(op *AddonOperator) error {
	runtimeConfig := config.NewConfig()
	// Init logging subsystem.
	sh_app.SetupLogging(runtimeConfig)
	log.Infof(sh_app.AppStartMessage)

	modulesDir, err := shell_operator.RequireExistingDirectory(app.ModulesDir)
	if err != nil {
		log.Errorf("Fatal: modules directory: %s", err)
		return err
	}
	log.Infof("Modules directory: %s", modulesDir)

	globalHooksDir, err := shell_operator.RequireExistingDirectory(app.GlobalHooksDir)
	if err != nil {
		log.Errorf("Fatal: global hooks directory: %s", err)
		return err
	}
	log.Infof("Global hooks directory: %s", globalHooksDir)

	tempDir, err := shell_operator.EnsureTempDirectory(sh_app.TempDir)
	if err != nil {
		log.Errorf("Fatal: temp directory: %s", err)
		return err
	}

	log.Infof("Addon-operator namespace: %s", app.Namespace)

	op.WithContext(context.Background())

	// Debug server.
	debugServer, err := shell_operator.InitDefaultDebugServer()
	if err != nil {
		log.Errorf("Fatal: start Debug server: %s", err)
		return err
	}

	err = shell_operator.AssembleCommonOperator(op.ShellOperator)
	if err != nil {
		log.Errorf("Fatal: %s", err)
		return err
	}

	err = AssembleAddonOperator(op, modulesDir, globalHooksDir, tempDir, debugServer, runtimeConfig)
	if err != nil {
		log.Errorf("Fatal: %s", err)
		return err
	}

	return nil
}

func AssembleAddonOperator(op *AddonOperator, modulesDir string, globalHooksDir string, tempDir string, debugServer *debug.Server, runtimeConfig *config.Config) (err error) {
	RegisterDefaultRoutes(op)
	RegisterAddonOperatorMetrics(op.MetricStorage)
	StartLiveTicksUpdater(op.MetricStorage)
	StartTasksQueueLengthUpdater(op.MetricStorage, op.TaskQueues)

	// Register routes in debug server.
	shell_operator.RegisterDebugQueueRoutes(debugServer, op.ShellOperator)
	shell_operator.RegisterDebugConfigRoutes(debugServer, runtimeConfig)
	RegisterDebugGlobalRoutes(debugServer, op)
	RegisterDebugModuleRoutes(debugServer, op)

	// Helm client factory.
	op.Helm, err = helm.InitHelmClientFactory(op.KubeClient)
	if err != nil {
		return fmt.Errorf("initialize Helm: %s", err)
	}

	// Helm resources monitor.
	// It uses a separate client-go instance. (Metrics are registered when 'main' client is initialized).
	op.HelmResourcesManager, err = InitDefaultHelmResourcesManager(op.ctx, op.MetricStorage)
	if err != nil {
		return fmt.Errorf("initialize Helm resources manager: %s", err)
	}

	SetupModuleManager(op, modulesDir, globalHooksDir, tempDir, runtimeConfig)

	err = op.InitModuleManager()
	if err != nil {
		return err
	}

	return nil
}

func SetupModuleManager(op *AddonOperator, modulesDir string, globalHooksDir string, tempDir string, runtimeConfig *config.Config) {
	// Create manager to check values in ConfigMap.
	op.KubeConfigManager = kube_config_manager.NewKubeConfigManager()
	op.KubeConfigManager.WithKubeClient(op.KubeClient)
	op.KubeConfigManager.WithContext(op.ctx)
	op.KubeConfigManager.WithNamespace(app.Namespace)
	op.KubeConfigManager.WithConfigMapName(app.ConfigMapName)
	op.KubeConfigManager.WithRuntimeConfig(runtimeConfig)

	// Create manager that runs modules and hooks.
	op.ModuleManager = module_manager.NewModuleManager()
	op.ModuleManager.WithContext(op.ctx)
	op.ModuleManager.WithDirectories(modulesDir, globalHooksDir, tempDir)
	op.ModuleManager.WithKubeConfigManager(op.KubeConfigManager)
	op.ModuleManager.WithHelm(op.Helm)
	op.ModuleManager.WithScheduleManager(op.ScheduleManager)
	op.ModuleManager.WithKubeEventManager(op.KubeEventsManager)
	op.ModuleManager.WithKubeObjectPatcher(op.ObjectPatcher)
	op.ModuleManager.WithMetricStorage(op.MetricStorage)
	op.ModuleManager.WithHookMetricStorage(op.HookMetricStorage)
	op.ModuleManager.WithHelmResourcesManager(op.HelmResourcesManager)
}
