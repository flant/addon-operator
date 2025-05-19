package addon_operator

import (
	"fmt"
	"log/slog"

	"github.com/deckhouse/deckhouse/pkg/log"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/kube_config_manager"
	"github.com/flant/addon-operator/pkg/kube_config_manager/backend"
	"github.com/flant/addon-operator/pkg/module_manager"
	taskservice "github.com/flant/addon-operator/pkg/task/service"
	shapp "github.com/flant/shell-operator/pkg/app"
	"github.com/flant/shell-operator/pkg/debug"
	shell_operator "github.com/flant/shell-operator/pkg/shell-operator"
)

// Bootstrap inits all dependencies for a full-fledged AddonOperator instance.
// It initializes the debug server and assembles all components needed for operation.
func (op *AddonOperator) bootstrap() error {
	// Log application startup message
	log.Info(shapp.AppStartMessage)

	// Log the path where modules will be searched
	log.Info("Search modules",
		slog.String("path", app.ModulesDir))

	// Log the namespace in which the operator will work
	log.Info("Addon-operator namespace",
		slog.String("namespace", op.DefaultNamespace))

	// Initialize the debug server for troubleshooting and monitoring
	// TODO: rewrite shapp global variables to the addon-operator one
	var err error
	op.DebugServer, err = shell_operator.RunDefaultDebugServer(shapp.DebugUnixSocket, shapp.DebugHttpServerAddr, op.Logger.Named("debug-server"))
	if err != nil {
		log.Error("Fatal: start Debug server", log.Err(err))
		return fmt.Errorf("start Debug server: %w", err)
	}

	// Assemble all operator components including routes, admission server, metrics updaters, and module management
	err = op.Assemble(op.DebugServer)
	if err != nil {
		log.Error("Fatal", log.Err(err))
		return fmt.Errorf("assemble Debug server: %w", err)
	}

	cfg := &taskservice.TaskHandlerServiceConfig{
		Engine:               op.engine,
		ParallelTaskChannels: op.parallelTaskChannels,
		Helm:                 op.Helm,
		HelmResourcesManager: op.HelmResourcesManager,
		ModuleManager:        op.ModuleManager,
		MetricStorage:        op.MetricStorage,
		KubeConfigManager:    op.KubeConfigManager,
		ConvergeState:        op.ConvergeState,
		CRDExtraLabels:       op.CRDExtraLabels,
	}

	op.TaskService = taskservice.NewTaskHandlerService(op.ctx, cfg, op.Logger)

	return nil
}

// Assemble initializes and connects components of the AddonOperator.
// It sets up debugging http endpoints, starts netrics services, and initializes the module manager.
func (op *AddonOperator) Assemble(debugServer *debug.Server) error {
	// Register default HTTP routes for the operator
	op.registerDefaultRoutes()

	// Start the admission server if enabled in application configuration
	if app.AdmissionServerEnabled {
		op.AdmissionServer.start(op.ctx)
	}

	// Start background updaters for metrics
	StartLiveTicksUpdater(op.engine.MetricStorage)
	StartTasksQueueLengthUpdater(op.engine.MetricStorage, op.engine.TaskQueues)

	// Register debug HTTP endpoints to inspect internal state
	op.engine.RegisterDebugQueueRoutes(debugServer)
	op.engine.RegisterDebugConfigRoutes(debugServer, op.runtimeConfig)
	op.RegisterDebugGlobalRoutes(debugServer)
	op.RegisterDebugModuleRoutes(debugServer)
	op.RegisterDiscoveryRoute(debugServer)

	// Initialize the module manager which handles modules lifecycle
	if err := op.InitModuleManager(); err != nil {
		return err
	}

	// Register graph visualization routes after module manager is initialized
	op.RegisterDebugGraphRoutes(debugServer)

	return nil
}

// SetupKubeConfigManager sets manager, which reads configuration for Modules from a cluster
func (op *AddonOperator) SetupKubeConfigManager(bk backend.ConfigHandler) {
	if op.KubeConfigManager != nil {
		log.Warn("KubeConfigManager is already set")
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
		ChrootDir:      app.ShellChrootDir,
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
