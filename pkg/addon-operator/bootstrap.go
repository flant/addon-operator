package addon_operator

import (
	"github.com/deckhouse/deckhouse/pkg/log"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/kube_config_manager"
	"github.com/flant/addon-operator/pkg/kube_config_manager/backend"
	"github.com/flant/addon-operator/pkg/module_manager"
	sh_app "github.com/flant/shell-operator/pkg/app"
	"github.com/flant/shell-operator/pkg/debug"
	shell_operator "github.com/flant/shell-operator/pkg/shell-operator"
)

// Bootstrap inits all dependencies for a full-fledged AddonOperator instance.
func (op *AddonOperator) bootstrap() error {
	log.Info(sh_app.AppStartMessage)

	log.Infof("Search modules in: %s", app.ModulesDir)

	log.Infof("Addon-operator namespace: %s", app.Namespace)

	// Debug server.
	// TODO: rewrite sh_app global variables to the addon-operator ones
	var err error
	op.DebugServer, err = shell_operator.RunDefaultDebugServer(sh_app.DebugUnixSocket, sh_app.DebugHttpServerAddr, op.Logger.Named("debug-server"))
	if err != nil {
		log.Errorf("Fatal: start Debug server: %s", err)
		return err
	}

	err = op.Assemble(op.DebugServer)
	if err != nil {
		log.Errorf("Fatal: %s", err)
		return err
	}

	return nil
}

func (op *AddonOperator) Assemble(debugServer *debug.Server) (err error) {
	op.registerDefaultRoutes()
	if app.AdmissionServerEnabled {
		op.AdmissionServer.start(op.ctx)
	}
	StartLiveTicksUpdater(op.engine.MetricStorage)
	StartTasksQueueLengthUpdater(op.engine.MetricStorage, op.engine.TaskQueues)

	// Register routes in debug server.
	op.engine.RegisterDebugQueueRoutes(debugServer)
	op.engine.RegisterDebugConfigRoutes(debugServer, op.runtimeConfig)
	op.RegisterDebugGlobalRoutes(debugServer)
	op.RegisterDebugModuleRoutes(debugServer)
	op.RegisterDiscoveryRoute(debugServer)

	err = op.InitModuleManager()
	if err != nil {
		return err
	}

	op.RegisterDebugGraphRoutes(debugServer)

	return nil
}

// SetupKubeConfigManager sets manager, which reads configuration for Modules from a cluster
func (op *AddonOperator) SetupKubeConfigManager(bk backend.ConfigHandler) {
	if op.KubeConfigManager != nil {
		log.Warnf("KubeConfigManager is already set")
		// return if kube config manager is already set
		return
	}

	op.KubeConfigManager = kube_config_manager.NewKubeConfigManager(op.ctx, bk, op.runtimeConfig, op.Logger.Named("kube-config-manager"))
}

func (op *AddonOperator) SetupModuleManager(modulesDir string, globalHooksDir string, tempDir string) {
	// Create manager that runs modules and hooks.
	dirConfig := module_manager.DirectoryConfig{
		ModulesDir:     modulesDir,
		GlobalHooksDir: globalHooksDir,
		TempDir:        tempDir,
	}
	deps := module_manager.ModuleManagerDependencies{
		KubeObjectPatcher:    op.engine.ObjectPatcher,
		KubeEventsManager:    op.engine.KubeEventsManager,
		KubeConfigManager:    op.KubeConfigManager,
		ScheduleManager:      op.engine.ScheduleManager,
		Helm:                 op.Helm,
		HelmResourcesManager: op.HelmResourcesManager,
		MetricStorage:        op.engine.MetricStorage,
		HookMetricStorage:    op.engine.HookMetricStorage,
		TaskQueues:           op.engine.TaskQueues,
	}

	cfg := module_manager.ModuleManagerConfig{
		DirectoryConfig: dirConfig,
		Dependencies:    deps,
	}

	op.ModuleManager = module_manager.NewModuleManager(op.ctx, &cfg, op.Logger.Named("module-manager"))
}
