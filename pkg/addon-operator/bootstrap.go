package addon_operator

import (
	"fmt"

	log "github.com/sirupsen/logrus"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/kube_config_manager"
	"github.com/flant/addon-operator/pkg/module_manager"
	sh_app "github.com/flant/shell-operator/pkg/app"
	"github.com/flant/shell-operator/pkg/config"
	"github.com/flant/shell-operator/pkg/debug"
	shell_operator "github.com/flant/shell-operator/pkg/shell-operator"
)

// Bootstrap inits all dependencies for a full-fledged AddonOperator instance.
func (op *AddonOperator) bootstrap() error {
	runtimeConfig := config.NewConfig()
	// Init logging subsystem.
	sh_app.SetupLogging(runtimeConfig)
	log.Infof(sh_app.AppStartMessage)

	log.Infof("Search modules in: %s", app.ModulesDir)

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

	err = op.Assemble(app.ModulesDir, globalHooksDir, tempDir, debugServer, runtimeConfig)
	if err != nil {
		log.Errorf("Fatal: %s", err)
		return err
	}

	return nil
}

func (op *AddonOperator) Assemble(modulesDir string, globalHooksDir string, tempDir string, debugServer *debug.Server, runtimeConfig *config.Config) (err error) {
	op.RegisterDefaultRoutes()
	if app.AdmissionServerEnabled {
		op.AdmissionServer.start(op.ctx)
	}
	RegisterAddonOperatorMetrics(op.MetricStorage)
	StartLiveTicksUpdater(op.MetricStorage)
	StartTasksQueueLengthUpdater(op.MetricStorage, op.TaskQueues)

	// Register routes in debug server.
	shell_operator.RegisterDebugQueueRoutes(debugServer, op.ShellOperator)
	shell_operator.RegisterDebugConfigRoutes(debugServer, runtimeConfig)
	op.RegisterDebugGlobalRoutes(debugServer)
	op.RegisterDebugModuleRoutes(debugServer)
	op.OnFirstConvergeDone()

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

	op.SetupModuleManager(modulesDir, globalHooksDir, tempDir, runtimeConfig)

	err = op.InitModuleManager()
	if err != nil {
		return err
	}

	return nil
}

func (op *AddonOperator) SetupModuleManager(modulesDir string, globalHooksDir string, tempDir string, runtimeConfig *config.Config) {
	// Create manager to check values in ConfigMap.
	kcfg := kube_config_manager.Config{
		Namespace:     app.Namespace,
		ConfigMapName: app.ConfigMapName,
		KubeClient:    op.KubeClient,
		RuntimeConfig: runtimeConfig,
	}
	manager := kube_config_manager.NewKubeConfigManager(op.ctx, &kcfg)
	op.KubeConfigManager = manager

	// Create manager that runs modules and hooks.
	dirConfig := module_manager.DirectoryConfig{
		ModulesDir:     modulesDir,
		GlobalHooksDir: globalHooksDir,
		TempDir:        tempDir,
	}
	deps := module_manager.ModuleManagerDependencies{
		KubeObjectPatcher:    op.ObjectPatcher,
		KubeEventsManager:    op.KubeEventsManager,
		KubeConfigManager:    manager,
		ScheduleManager:      op.ScheduleManager,
		Helm:                 op.Helm,
		HelmResourcesManager: op.HelmResourcesManager,
		MetricStorage:        op.MetricStorage,
		HookMetricStorage:    op.HookMetricStorage,
	}

	cfg := module_manager.ModuleManagerConfig{
		DirectoryConfig: dirConfig,
		Dependencies:    deps,
	}

	op.ModuleManager = module_manager.NewModuleManager(op.ctx, &cfg)
}
