package module_manager

import (
	"context"
	"errors"
	"fmt"
	"image"
	"log/slog"
	"runtime/trace"
	"strings"
	"sync"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	metricsstorage "github.com/deckhouse/deckhouse/pkg/metrics-storage"
	"github.com/hashicorp/go-multierror"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/helm_resources_manager"
	. "github.com/flant/addon-operator/pkg/hook/types"
	"github.com/flant/addon-operator/pkg/kube_config_manager/config"
	environmentmanager "github.com/flant/addon-operator/pkg/module_manager/environment_manager"
	gohook "github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/pkg/module_manager/loader"
	"github.com/flant/addon-operator/pkg/module_manager/loader/fs"
	"github.com/flant/addon-operator/pkg/module_manager/models/hooks"
	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
	"github.com/flant/addon-operator/pkg/module_manager/models/modules/events"
	"github.com/flant/addon-operator/pkg/module_manager/models/moduleset"
	"github.com/flant/addon-operator/pkg/module_manager/scheduler"
	"github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders"
	dynamic_extender "github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders/dynamically_enabled"
	kube_config_extender "github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders/kube_config"
	script_extender "github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders/script_enabled"
	static_extender "github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders/static"
	"github.com/flant/addon-operator/pkg/task"
	"github.com/flant/addon-operator/pkg/utils"
	. "github.com/flant/shell-operator/pkg/hook/binding_context"
	"github.com/flant/shell-operator/pkg/hook/controller"
	. "github.com/flant/shell-operator/pkg/hook/types"
	objectpatch "github.com/flant/shell-operator/pkg/kube/object_patch"
	kubeeventsmanager "github.com/flant/shell-operator/pkg/kube_events_manager"
	. "github.com/flant/shell-operator/pkg/kube_events_manager/types"
	schedulemanager "github.com/flant/shell-operator/pkg/schedule_manager"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
)

const (
	moduleInfoMetricGroup = "mm_module_info"
	moduleInfoMetricName  = "{PREFIX}mm_module_info"

	moduleMaintenanceMetricGroup = "mm_module_maintenance"
	moduleMaintenanceMetricName  = "{PREFIX}mm_module_maintenance"

	moduleManagerServiceName = "module-manager"
)

// ModulesState determines which modules should be enabled, disabled or reloaded.
type ModulesState struct {
	// All enabled modules.
	AllEnabledModules []string
	// All enabled modules grouped by order.
	AllEnabledModulesByOrder [][]string
	// Modules that should be deleted.
	ModulesToDisable []string
	// Modules that was disabled and now are enabled.
	ModulesToEnable []string
	// Modules changed after ConfigMap changes
	ModulesToReload []string
	// Helm releases without module directory (unknown modules).
	ModulesToPurge []string
}

// DirectoryConfig configures directories for ModuleManager
type DirectoryConfig struct {
	ModulesDir     string
	GlobalHooksDir string
	TempDir        string
	ChrootDir      string
}

type KubeConfigManager interface {
	SaveConfigValues(key string, values utils.Values) error
	IsModuleEnabled(moduleName string) *bool
	UpdateModuleConfig(moduleName string) error
	SafeReadConfig(handler func(config *config.KubeConfig))
	KubeConfigEventCh() chan config.KubeConfigEvent
}

// ModuleManagerDependencies pass dependencies for ModuleManager
type ModuleManagerDependencies struct {
	KubeObjectPatcher    *objectpatch.ObjectPatcher
	KubeEventsManager    kubeeventsmanager.KubeEventsManager
	KubeConfigManager    KubeConfigManager
	ScheduleManager      schedulemanager.ScheduleManager
	Helm                 *helm.ClientFactory
	HelmResourcesManager helm_resources_manager.HelmResourcesManager
	MetricStorage        metricsstorage.Storage
	HookMetricStorage    metricsstorage.Storage
	TaskQueues           *queue.TaskQueueSet
}

type ModuleManagerConfig struct {
	DirectoryConfig DirectoryConfig
	Dependencies    ModuleManagerDependencies
}

type ModuleManager struct {
	ctx    context.Context
	cancel context.CancelFunc

	// Directories.
	ModulesDir       string
	GlobalHooksDir   string
	TempDir          string
	defaultNamespace string

	moduleLoader loader.ModuleLoader

	// Dependencies.
	dependencies *ModuleManagerDependencies

	global *modules.GlobalModule

	globalSynchronizationState *modules.SynchronizationState

	kubeConfigLock sync.RWMutex
	// addon-operator config is valid.
	kubeConfigValid bool
	// Static and config values are valid using OpenAPI schemas.
	kubeConfigValuesValid bool

	moduleEventC chan events.ModuleEvent

	moduleScheduler *scheduler.Scheduler

	environmentManager *environmentmanager.Manager

	logger *log.Logger

	l sync.Mutex
	// All known modules from specified directories ($MODULES_DIR)
	modules *moduleset.ModulesSet
}

var once sync.Once

// NewModuleManager returns new MainModuleManager
func NewModuleManager(ctx context.Context, cfg *ModuleManagerConfig, logger *log.Logger) *ModuleManager {
	cctx, cancel := context.WithCancel(ctx)

	// default loader, maybe we can register another one on startup
	fsLoader := fs.NewFileSystemLoader(cfg.DirectoryConfig.ModulesDir, logger.Named("file-system-loader"))

	mm := &ModuleManager{
		ctx:    cctx,
		cancel: cancel,

		ModulesDir:     cfg.DirectoryConfig.ModulesDir,
		GlobalHooksDir: cfg.DirectoryConfig.GlobalHooksDir,
		TempDir:        cfg.DirectoryConfig.TempDir,

		defaultNamespace: app.Namespace,

		moduleLoader: fsLoader,

		dependencies: &cfg.Dependencies,

		modules: new(moduleset.ModulesSet),

		globalSynchronizationState: modules.NewSynchronizationState(),

		moduleScheduler: scheduler.NewScheduler(cctx, logger.Named("scheduler")),

		logger: logger,
	}

	if len(cfg.DirectoryConfig.ChrootDir) > 0 {
		mm.environmentManager = environmentmanager.NewManager(cfg.DirectoryConfig.ChrootDir, mm.logger)
	}

	return mm
}

// AddObjectsToChrootEnvironment sets the list of objects to implement in the modules' chroot directories
func (mm *ModuleManager) AddObjectsToChrootEnvironment(objects ...environmentmanager.ObjectDescriptor) {
	if mm.EnvironmentManagerEnabled() {
		mm.logger.Debug("Add objects to the module's chroot environment")
		mm.environmentManager.AddObjectsToEnvironment(objects...)
	}
}

func (mm *ModuleManager) Stop() {
	if mm.cancel != nil {
		mm.cancel()
	}
}

func (mm *ModuleManager) SetModuleLoader(ld loader.ModuleLoader) {
	mm.moduleLoader = ld
}

// GetDependencies fetch dependencies struct from ModuleManager
// note: not the best way but it's required in some hooks
func (mm *ModuleManager) GetDependencies() *ModuleManagerDependencies {
	return mm.dependencies
}

func (mm *ModuleManager) GetGlobal() *modules.GlobalModule {
	return mm.global
}

// ApplyNewKubeConfigValues validates and applies new config values with config schemas.
func (mm *ModuleManager) ApplyNewKubeConfigValues(kubeConfig *config.KubeConfig, globalValuesChanged bool) error {
	if kubeConfig == nil {
		// have no idea, how it could be, just skip run
		mm.logger.Warn("No KubeConfig is set")
		return nil
	}

	mm.warnAboutUnknownModules(kubeConfig)

	allModules := make(map[string]struct{}, len(mm.modules.NamesInOrder()))
	for _, module := range mm.modules.NamesInOrder() {
		allModules[module] = struct{}{}
	}

	valuesMap, validationErr := mm.validateNewKubeConfig(kubeConfig, allModules)
	if validationErr != nil {
		mm.SetKubeConfigValuesValid(false)
		return validationErr
	}

	mm.SetKubeConfigValuesValid(true)

	newGlobalValues, ok := valuesMap[mm.global.GetName()]
	if ok {
		if globalValuesChanged {
			mm.logger.Debug("Applying global values",
				slog.String("values", fmt.Sprintf("%v", newGlobalValues)))
			mm.global.SaveConfigValues(newGlobalValues)
		}
		delete(valuesMap, mm.global.GetName())
	}

	// Detect changed module sections for enabled modules.
	for moduleName, values := range valuesMap {
		mod := mm.GetModule(moduleName)
		if mod == nil {
			continue
		}

		if mod.GetConfigValues(false).Checksum() != values.Checksum() {
			mm.logger.Debug("Applying values to module",
				slog.String("moduleName", moduleName),
				slog.String("values", fmt.Sprintf("%v", values)),
				slog.String("oldValues", fmt.Sprintf("%v", mod.GetConfigValues(false))))
			mod.SaveConfigValues(values)
		}
	}

	return nil
}

func (mm *ModuleManager) validateNewKubeConfig(kubeConfig *config.KubeConfig, allModules map[string]struct{}) (map[string]utils.Values, error) {
	validationErrors := &multierror.Error{}

	valuesMap := make(map[string]utils.Values)

	// validate global config
	if kubeConfig.Global != nil {
		newValues, validationErr := mm.global.GenerateNewConfigValues(kubeConfig.Global.GetValues(), true)
		if validationErr != nil {
			_ = multierror.Append(validationErrors, validationErr)
		}
		valuesMap[mm.global.GetName()] = newValues
	}

	// validate module configs
	for moduleName, moduleConfig := range kubeConfig.Modules {
		mod := mm.GetModule(moduleName)
		if mod == nil {
			// unknown module
			continue
		}

		if moduleConfig.GetMaintenanceState() == utils.NoResourceReconciliation {
			mm.dependencies.MetricStorage.Grouped().GaugeSet(moduleMaintenanceMetricGroup, moduleMaintenanceMetricName, 1, map[string]string{"moduleName": moduleName, "state": utils.NoResourceReconciliation.String()})
			mod.SetMaintenanceState(moduleConfig.GetMaintenanceState())
		}

		validateConfig := false
		// Check if enabledModules are valid
		// if module is enabled, we have to check config is valid
		// otherwise we have to just save the config, because we can have some absent defaults or something like that
		if _, moduleExists := allModules[moduleName]; moduleExists && (moduleConfig.GetEnabled() == "true" || mm.IsModuleEnabled(moduleName)) {
			validateConfig = true
		}

		// if module config values are empty - return empty values (without static and openapi default values)
		if len(moduleConfig.GetValues()) == 0 {
			valuesMap[mod.GetName()] = utils.Values{}
		} else {
			newValues, validationErr := mod.GenerateNewConfigValues(moduleConfig.GetValues(), validateConfig)
			if validationErr != nil {
				_ = multierror.Append(validationErrors, validationErr)
			}
			valuesMap[mod.GetName()] = newValues
		}
	}

	return valuesMap, validationErrors.ErrorOrNil()
}

// warnAboutUnknownModules prints to log all unknown module section names.
func (mm *ModuleManager) warnAboutUnknownModules(kubeConfig *config.KubeConfig) {
	// Ignore empty kube config.
	if kubeConfig == nil {
		return
	}

	unknownNames := make([]string, 0)
	for moduleName := range kubeConfig.Modules {
		if !mm.modules.Has(moduleName) {
			unknownNames = append(unknownNames, moduleName)
		}
	}
	if len(unknownNames) > 0 {
		mm.logger.Warn("KubeConfigManager has values for unknown modules",
			slog.Any("modules", unknownNames))
	}
}

// FilterModuleByExtender returns filtering result for the specified extender and module
func (mm *ModuleManager) FilterModuleByExtender(extName extenders.ExtenderName, moduleName string, logLabels map[string]string) (*bool, error) {
	return mm.moduleScheduler.Filter(extName, moduleName, logLabels)
}

// Init — initialize module manager
func (mm *ModuleManager) Init(logger *log.Logger) error {
	logger.Debug("Init ModuleManager")

	mm.logger = logger

	gv, err := mm.loadGlobalValues()
	if err != nil {
		return fmt.Errorf("couldn't load global values: %w", err)
	}

	staticExtender, err := static_extender.NewExtender(mm.ModulesDir)
	if err != nil {
		return fmt.Errorf("couldn't create static extender: %w", err)
	}
	if err := mm.moduleScheduler.AddExtender(staticExtender); err != nil {
		return fmt.Errorf("couldn't add static extender: %w", err)
	}

	err = mm.registerGlobalModule(gv.globalValues, gv.configSchema, gv.valuesSchema)
	if err != nil {
		return fmt.Errorf("couldn't register global module: %w", err)
	}

	kubeConfigExtender := kube_config_extender.NewExtender(mm.dependencies.KubeConfigManager)
	if err := mm.moduleScheduler.AddExtender(kubeConfigExtender); err != nil {
		return fmt.Errorf("couldn't add kube config extender: %w", err)
	}

	scriptEnabledExtender, err := script_extender.NewExtender(mm.TempDir)
	if err != nil {
		return fmt.Errorf("couldn't create script_enabled extender: %w", err)
	}

	if err := mm.moduleScheduler.AddExtender(scriptEnabledExtender); err != nil {
		return fmt.Errorf("couldn't add scrpt_enabled extender: %w", err)
	}

	// by this point, we must have all required scheduler extenders attached
	if err := mm.moduleScheduler.ApplyExtenders(app.AppliedExtenders); err != nil {
		return fmt.Errorf("couldn't apply extenders to the module scheduler: %w", err)
	}

	return mm.registerModules(scriptEnabledExtender)
}

func (mm *ModuleManager) GetKubeConfigValid() bool {
	mm.kubeConfigLock.RLock()
	defer mm.kubeConfigLock.RUnlock()

	return mm.kubeConfigValid
}

func (mm *ModuleManager) SetKubeConfigValid(valid bool) {
	mm.kubeConfigLock.Lock()
	mm.kubeConfigValid = valid
	mm.kubeConfigLock.Unlock()
}

func (mm *ModuleManager) SetKubeConfigValuesValid(valid bool) {
	mm.kubeConfigLock.Lock()
	mm.kubeConfigValuesValid = valid
	mm.kubeConfigLock.Unlock()
}

// checkConfig increases config_values_errors_total metric when kubeConfig becomes invalid.
func (mm *ModuleManager) checkConfig() {
	for {
		if mm.ctx.Err() != nil {
			return
		}
		mm.kubeConfigLock.RLock()
		if !mm.kubeConfigValid || !mm.kubeConfigValuesValid {
			mm.dependencies.MetricStorage.CounterAdd("{PREFIX}config_values_errors_total", 1.0, map[string]string{})
		}
		mm.kubeConfigLock.RUnlock()
		time.Sleep(5 * time.Second)
	}
}

// Start runs service go routine.
func (mm *ModuleManager) Start() {
	// Start checking kubeConfigIsValid flag in go routine.
	go mm.checkConfig()
}

// RefreshStateFromHelmReleases retrieves all Helm releases. It marks all unknown modules as needed to be purged.
// Run this method once at startup.
func (mm *ModuleManager) RefreshStateFromHelmReleases(logLabels map[string]string) (*ModulesState, error) {
	if mm.dependencies.Helm == nil {
		return &ModulesState{}, nil
	}

	helmClientOptions := []helm.ClientOption{
		helm.WithLogLabels(logLabels),
	}

	releasedModules, err := mm.dependencies.Helm.NewClient(mm.logger.Named("helm-client"), helmClientOptions...).ListReleasesNames()
	if err != nil {
		return nil, err
	}

	mm.logger.Debug("Following releases found",
		slog.Any("modules", releasedModules))

	return mm.stateFromHelmReleases(releasedModules), nil
}

// stateFromHelmReleases calculates modules to purge from Helm releases.
func (mm *ModuleManager) stateFromHelmReleases(releases []string) *ModulesState {
	releasesMap := utils.ListToMapStringStruct(releases)
	enabledModules := mm.GetEnabledModuleNames()

	for _, moduleName := range enabledModules {
		delete(releasesMap, moduleName)
	}

	purge := utils.MapStringStructKeys(releasesMap)
	purge = utils.SortReverse(purge)

	if len(purge) > 0 {
		mm.logger.Info("Modules to purge found",
			slog.Any("modules", purge))
	}

	return &ModulesState{
		ModulesToPurge: purge,
	}
}

func (mm *ModuleManager) GetGraphImage(ctx context.Context) (image.Image, error) {
	return mm.moduleScheduler.GetGraphImage(ctx)
}

func (mm *ModuleManager) GetGraphDOTDescription() ([]byte, error) {
	return mm.moduleScheduler.GetGraphDOTDescription()
}

// SetGlobalDiscoveryAPIVersions applies global values patch to .global.discovery.apiVersions key
// if non-default moduleLoader is in use
func (mm *ModuleManager) SetGlobalDiscoveryAPIVersions(apiVersions []string) {
	// We've to ignore apiVersions patch in case default moduleLoader is in use, otherwise it breaks applying global hooks patches with default moduleLoader
	switch mm.moduleLoader.(type) {
	case *fs.FileSystemLoader:
	default:
		mm.logger.Debug("non-default module loader detected - applying apiVersions patch")
		mm.global.SetAvailableAPIVersions(apiVersions)
	}
}

// UpdateModulesMetrics updates modules' states metrics
func (mm *ModuleManager) UpdateModulesMetrics() {
	mm.dependencies.MetricStorage.Grouped().ExpireGroupMetricByName(moduleInfoMetricGroup, moduleInfoMetricName)
	for _, module := range mm.GetModuleNames() {
		enabled := "false"
		if mm.IsModuleEnabled(module) {
			enabled = "true"
		}
		mm.dependencies.MetricStorage.Grouped().GaugeSet(moduleInfoMetricGroup, moduleInfoMetricName, 1, map[string]string{"moduleName": module, "enabled": enabled})
	}
}

func (mm *ModuleManager) SetModuleMaintenanceState(moduleName string, state utils.Maintenance) {
	if bm := mm.GetModule(moduleName); bm != nil {
		bm.SetMaintenanceState(state)
		mm.logger.Info("set module management state",
			slog.String("module", moduleName),
			slog.String("state", state.String()))
		if state == utils.NoResourceReconciliation {
			mm.dependencies.MetricStorage.Grouped().GaugeSet(moduleMaintenanceMetricGroup, moduleMaintenanceMetricName, 1, map[string]string{"moduleName": moduleName, "state": utils.NoResourceReconciliation.String()})
		} else {
			mm.dependencies.MetricStorage.Grouped().ExpireGroupMetricByName(moduleMaintenanceMetricGroup, moduleMaintenanceMetricName)
		}
	}
}

// RefreshEnabledState gets current diff of the graph and forms ModuleState
// - mm.enabledModules
func (mm *ModuleManager) RefreshEnabledState(logLabels map[string]string) (*ModulesState, error) {
	refreshLogLabels := utils.MergeLabels(logLabels, map[string]string{
		"operator.component": "ModuleManager.RefreshEnabledState",
	})
	logEntry := utils.EnrichLoggerWithLabels(mm.logger, refreshLogLabels)

	enabledModules, enabledModulesByOrder, modulesDiff, err := mm.moduleScheduler.GetGraphState(refreshLogLabels)
	if err != nil {
		return nil, err
	}

	logEntry.Info("Enabled modules",
		slog.Any("modules", enabledModules))
	once.Do(mm.modules.SetInited)

	var (
		modulesToEnable  []string
		modulesToDisable []string
	)

	go mm.UpdateModulesMetrics()

	for module, enabled := range modulesDiff {
		if enabled {
			modulesToEnable = append(modulesToEnable, module)
		} else {
			modulesToDisable = append(modulesToDisable, module)
		}
	}

	modulesToDisable = utils.SortReverseByReference(modulesToDisable, mm.modules.NamesInOrder())
	modulesToEnable = utils.SortByReference(modulesToEnable, mm.modules.NamesInOrder())

	logEntry.Debug("Refresh state results",
		slog.Any("enabledModules", enabledModules),
		slog.Any("modulesToDisable", modulesToDisable),
		slog.Any("modulesToEnable", modulesToEnable))

	// We've to ignore enabledModules patch in case default moduleLoader is in use, otherwise it breaks applying global hooks patches with default moduleLoader
	switch mm.moduleLoader.(type) {
	case *fs.FileSystemLoader:
	default:
		logEntry.Debug("non-default module loader detected - applying enabledModules patch")
		enabledModulesAndFakeCRDmodules := make([]string, 0, len(enabledModules))
		for _, moduleName := range enabledModules {
			if mm.ModuleHasCRDs(moduleName) {
				enabledModulesAndFakeCRDmodules = append(enabledModulesAndFakeCRDmodules, fmt.Sprintf("%s-crd", moduleName))
			}
			enabledModulesAndFakeCRDmodules = append(enabledModulesAndFakeCRDmodules, moduleName)
		}
		mm.global.SetEnabledModules(enabledModulesAndFakeCRDmodules)
	}

	// Return lists for ConvergeModules task.
	return &ModulesState{
		AllEnabledModules:        enabledModules,
		AllEnabledModulesByOrder: enabledModulesByOrder,
		ModulesToDisable:         modulesToDisable,
		ModulesToEnable:          modulesToEnable,
	}, nil
}

func (mm *ModuleManager) GetModule(name string) *modules.BasicModule {
	return mm.modules.Get(name)
}

func (mm *ModuleManager) GetCritical(moduleName string) bool {
	module := mm.GetModule(moduleName)
	if module == nil {
		return false
	}

	return module.GetCritical()
}

func (mm *ModuleManager) GetFunctionalDependencies() map[string][]extenders.Hint {
	return mm.moduleScheduler.GetFunctionalDependencies()
}

func (mm *ModuleManager) GetModuleNames() []string {
	return mm.modules.NamesInOrder()
}

// GetEnabledModuleNames runs corresponding method of the module scheduler
func (mm *ModuleManager) GetEnabledModuleNames() []string {
	return mm.moduleScheduler.GetEnabledModuleNames()
}

// IsModuleEnabled returns current state of the module according to the scheduler
func (mm *ModuleManager) IsModuleEnabled(moduleName string) bool {
	return mm.moduleScheduler.IsModuleEnabled(moduleName)
}

// GetUpdatedByExtender returns the name of the extender that determined the module's state
func (mm *ModuleManager) GetUpdatedByExtender(moduleName string) (string, error) {
	return mm.moduleScheduler.GetUpdatedByExtender(moduleName)
}

func (mm *ModuleManager) AddExtender(ex extenders.Extender) error {
	return mm.moduleScheduler.AddExtender(ex)
}

func (mm *ModuleManager) GetGlobalHook(name string) *hooks.GlobalHook {
	return mm.global.GetHookByName(name)
}

func (mm *ModuleManager) GetGlobalHooksNames() []string {
	hks := mm.global.GetHooks()

	names := make([]string, 0, len(hks))
	for _, hk := range hks {
		names = append(names, hk.GetName())
	}

	return names
}

func (mm *ModuleManager) GetGlobalHooksInOrder(bindingType BindingType) []string {
	hks := mm.global.GetHooks(bindingType)

	names := make([]string, 0, len(hks))

	for _, hk := range hks {
		names = append(names, hk.GetName())
	}

	return names
}

func (mm *ModuleManager) DeleteModule(ctx context.Context, moduleName string, logLabels map[string]string) error {
	ctx, span := otel.Tracer(moduleManagerServiceName).Start(ctx, "DeleteModule")
	defer span.End()

	ml := mm.GetModule(moduleName)

	// Note: keep kubernetes monitors alive until afterDeleteHelm runs,
	// so hooks can access snapshots. We'll disable hooks after running them.

	// DELETE
	{
		span.AddEvent("ModuleDelete-HelmPhase")

		deleteLogLabels := utils.MergeLabels(logLabels,
			map[string]string{
				"module": ml.GetName(),
				"queue":  "main",
			})
		logEntry := utils.EnrichLoggerWithLabels(mm.logger, deleteLogLabels)

		// Stop resources monitor before deleting release
		mm.dependencies.HelmResourcesManager.StopMonitor(ml.GetName())
		schemaStorage := ml.GetValuesStorage().GetSchemaStorage()
		// Module has chart, but there is no release -> log a warning.
		// Module has chart and release -> execute helm delete.
		hmdeps := modules.HelmModuleDependencies{
			HelmClientFactory:   mm.dependencies.Helm,
			HelmResourceManager: mm.dependencies.HelmResourcesManager,
			MetricsStorage:      mm.dependencies.MetricStorage,
			HelmValuesValidator: schemaStorage,
		}

		helmModule, _ := modules.NewHelmModule(ml, mm.defaultNamespace, mm.TempDir, &hmdeps, schemaStorage, modules.WithLogger(mm.logger.Named("helm-module")))
		if helmModule != nil {
			helmClientOptions := []helm.ClientOption{
				helm.WithLogLabels(logLabels),
			}

			releaseExists, err := mm.dependencies.Helm.NewClient(mm.logger, helmClientOptions...).IsReleaseExists(ml.GetName())
			if !releaseExists {
				if err != nil {
					logEntry.Warn("Cannot find helm release for module",
						slog.String("module", ml.GetName()),
						log.Err(err))
				} else {
					logEntry.Warn("Cannot find helm release for module.",
						slog.String("module", ml.GetName()))
				}
			} else {
				helmClientOptions := []helm.ClientOption{
					helm.WithLogLabels(deleteLogLabels),
				}
				// Chart and release are existed, so run helm delete command
				err := mm.dependencies.Helm.NewClient(mm.logger, helmClientOptions...).DeleteRelease(ml.GetName())
				if err != nil {
					return fmt.Errorf("create helm client: %w", err)
				}
			}
		}

		err := ml.RunHooksByBinding(ctx, AfterDeleteHelm, deleteLogLabels)
		if err != nil {
			return fmt.Errorf("run hooks by bindng: %w", err)
		}

		// Now it is safe to stop kubernetes informers and remove scheduled functions
		mm.DisableModuleHooks(moduleName)

		// Cleanup state.
		ml.ResetState()
	}

	// Unregister module hooks.
	ml.DeregisterHooks()

	return ml.DisassembleEnvironmentForModule()
}

// RunModule runs beforeHelm hook, helm upgrade --install and afterHelm or afterDeleteHelm hook
func (mm *ModuleManager) RunModule(ctx context.Context, moduleName string, logLabels map[string]string) ( /*valuesChanged*/ bool, error) {
	ctx, span := otel.Tracer(moduleManagerServiceName).Start(ctx, "RunModule")
	defer span.End()

	bm := mm.GetModule(moduleName)

	// Do not send to mm.moduleValuesChanged, changed values are handled by TaskHandler.
	span.AddEvent("ModuleRun-HelmPhase")

	logLabels = utils.MergeLabels(logLabels, map[string]string{
		"module": bm.GetName(),
		"queue":  "main",
	})

	// Hooks can delete release resources, so pause resources monitor before run hooks.
	mm.dependencies.HelmResourcesManager.PauseMonitor(bm.GetName())
	defer mm.dependencies.HelmResourcesManager.ResumeMonitor(bm.GetName())

	var err error

	treg := trace.StartRegion(ctx, "ModuleRun-HelmPhase-beforeHelm")
	err = bm.RunHooksByBinding(ctx, BeforeHelm, logLabels)
	treg.End()
	if err != nil {
		return false, fmt.Errorf("run hooks by binding: %w", err)
	}

	treg = trace.StartRegion(ctx, "ModuleRun-HelmPhase-helm")
	schemaStorage := bm.GetValuesStorage().GetSchemaStorage()
	deps := &modules.HelmModuleDependencies{
		HelmClientFactory:   mm.dependencies.Helm,
		HelmResourceManager: mm.dependencies.HelmResourcesManager,
		MetricsStorage:      mm.dependencies.MetricStorage,
		HelmValuesValidator: schemaStorage,
	}

	helmModule, err := modules.NewHelmModule(bm, mm.defaultNamespace, mm.TempDir, deps, schemaStorage, modules.WithLogger(mm.logger.Named("helm-module")))
	if err != nil && !errors.Is(err, modules.ErrModuleIsNotHelm) {
		return false, fmt.Errorf("helm module create: %w", err)
	}

	if err == nil {
		err = helmModule.RunHelmInstall(ctx, logLabels, bm.GetMaintenanceState())
		if err != nil && errors.Is(err, modules.ErrReleaseIsUnmanaged) {
			bm.SetUnmanaged()
			mm.dependencies.HelmResourcesManager.StopMonitor(moduleName)

			err = nil
		}
	}

	treg.End()
	if err != nil && !errors.Is(err, modules.ErrModuleIsNotHelm) {
		return false, fmt.Errorf("run helm install: %w", err)
	}

	oldValues := bm.GetValues(false)
	oldValuesChecksum := oldValues.Checksum()
	treg = trace.StartRegion(context.Background(), "ModuleRun-HelmPhase-afterHelm")
	err = bm.RunHooksByBinding(ctx, AfterHelm, logLabels)
	treg.End()
	if err != nil {
		return false, fmt.Errorf("run hooks by binding: %w", err)
	}

	newValues := bm.GetValues(false)
	newValuesChecksum := newValues.Checksum()

	// Do not send to mm.moduleValuesChanged, changed values are handled by TaskHandler.
	return oldValuesChecksum != newValuesChecksum, nil
}

func (mm *ModuleManager) RunGlobalHook(ctx context.Context, hookName string, binding BindingType, bindingContext []BindingContext, logLabels map[string]string) (string, string, error) {
	return mm.global.RunHookByName(ctx, hookName, binding, bindingContext, logLabels)
}

func (mm *ModuleManager) RunModuleHook(ctx context.Context, moduleName, hookName string, binding BindingType, bindingContext []BindingContext, logLabels map[string]string) (string /*beforeChecksum*/, string /*afterChecksum*/, error) {
	ml := mm.GetModule(moduleName)

	return ml.RunHookByName(ctx, hookName, binding, bindingContext, logLabels)
}

func (mm *ModuleManager) HandleKubeEvent(
	ctx context.Context,
	kubeEvent KubeEvent,
	createGlobalTaskFn func(*hooks.GlobalHook, controller.BindingExecutionInfo) sh_task.Task,
	createModuleTaskFn func(*modules.BasicModule, *hooks.ModuleHook, controller.BindingExecutionInfo) sh_task.Task,
) []sh_task.Task {
	// Process hooks by binding type OnKubernetesEvent
	tasks := mm.CreateTasksByBinding(OnKubernetesEvent, func(gh *hooks.GlobalHook, m *modules.BasicModule, mh *hooks.ModuleHook) []sh_task.Task {
		// Handle global hooks
		if gh != nil {
			if !gh.GetHookController().CanHandleKubeEvent(kubeEvent) {
				return nil
			}

			task := gh.GetHookController().HandleKubeEventWithFormTask(ctx, kubeEvent, func(info controller.BindingExecutionInfo) sh_task.Task {
				if createGlobalTaskFn == nil {
					return nil
				}

				return createGlobalTaskFn(gh, info)
			})

			return []sh_task.Task{task}
		}

		// Handle module hooks
		if !mh.GetHookController().CanHandleKubeEvent(kubeEvent) {
			return nil
		}

		task := mh.GetHookController().HandleKubeEventWithFormTask(ctx, kubeEvent, func(info controller.BindingExecutionInfo) sh_task.Task {
			if createModuleTaskFn == nil {
				return nil
			}

			return createModuleTaskFn(m, mh, info)
		})

		return []sh_task.Task{task}
	})

	return tasks
}

func (mm *ModuleManager) HandleGlobalEnableKubernetesBindings(ctx context.Context, hookName string, createTaskFn func(*hooks.GlobalHook, controller.BindingExecutionInfo)) error {
	gh := mm.GetGlobalHook(hookName)

	err := gh.GetHookController().HandleEnableKubernetesBindings(ctx, func(info controller.BindingExecutionInfo) {
		if createTaskFn != nil {
			createTaskFn(gh, info)
		}
	})
	if err != nil {
		return fmt.Errorf("handle enable kubernetes bindings: %w", err)
	}

	return nil
}

func (mm *ModuleManager) HandleModuleEnableKubernetesBindings(ctx context.Context, moduleName string, createTaskFn func(*hooks.ModuleHook, controller.BindingExecutionInfo)) error {
	ml := mm.GetModule(moduleName)

	kubeHooks := ml.GetHooks(OnKubernetesEvent)

	for _, mh := range kubeHooks {
		err := mh.GetHookController().HandleEnableKubernetesBindings(ctx, func(info controller.BindingExecutionInfo) {
			if createTaskFn != nil {
				createTaskFn(mh, info)
			}
		})
		if err != nil {
			return fmt.Errorf("handle enable kubernetes bindings: %w", err)
		}
	}

	return nil
}

func (mm *ModuleManager) EnableModuleScheduleBindings(moduleName string) {
	ml := mm.GetModule(moduleName)

	schHooks := ml.GetHooks(Schedule)
	for _, mh := range schHooks {
		mh.GetHookController().EnableScheduleBindings()
	}
}

// DisableModuleScheduleBindings disables schedule bindings of the module's hooks
func (mm *ModuleManager) DisableModuleScheduleBindings(moduleName string) {
	ml := mm.GetModule(moduleName)
	if !ml.HooksControllersReady() {
		return
	}
	schHooks := ml.GetHooks(Schedule)
	for _, mh := range schHooks {
		mh.GetHookController().DisableScheduleBindings()
	}
}

// DisableModuleHooks disables monitors/bindings of the module's hooks
// It's advisable to use this method only if next step is to completely disable the module (as a short-term commitment).
// Otherwise, as hooks are rather stateless, their confiuration may get overwritten, resulting in unexpected consequences.
func (mm *ModuleManager) DisableModuleHooks(moduleName string) {
	ml := mm.GetModule(moduleName)

	if !ml.HooksControllersReady() {
		return
	}

	kubeHooks := ml.GetHooks(OnKubernetesEvent)
	for _, mh := range kubeHooks {
		mh.GetHookController().StopMonitors()
	}

	schHooks := ml.GetHooks(Schedule)
	for _, mh := range schHooks {
		mh.GetHookController().DisableScheduleBindings()
	}

	mm.SetModulePhaseAndNotify(ml, modules.HooksDisabled)
}

func (mm *ModuleManager) HandleScheduleEvent(
	ctx context.Context,
	crontab string,
	createGlobalTaskFn func(*hooks.GlobalHook, controller.BindingExecutionInfo) sh_task.Task,
	createModuleTaskFn func(*modules.BasicModule, *hooks.ModuleHook, controller.BindingExecutionInfo) sh_task.Task,
) []sh_task.Task {
	tasks := mm.CreateTasksByBinding(Schedule, func(gh *hooks.GlobalHook, m *modules.BasicModule, mh *hooks.ModuleHook) []sh_task.Task {
		// Handle global hooks
		if gh != nil {
			if !gh.GetHookController().CanHandleScheduleEvent(crontab) {
				return nil
			}

			newTasks := gh.GetHookController().HandleScheduleEventWithFormTasks(ctx, crontab, func(info controller.BindingExecutionInfo) sh_task.Task {
				if createGlobalTaskFn == nil {
					return nil
				}

				return createGlobalTaskFn(gh, info)
			})

			return newTasks
		}

		// Handle module hooks
		if !mh.GetHookController().CanHandleScheduleEvent(crontab) {
			return nil
		}

		newTasks := mh.GetHookController().HandleScheduleEventWithFormTasks(ctx, crontab, func(info controller.BindingExecutionInfo) sh_task.Task {
			if createGlobalTaskFn == nil {
				return nil
			}

			return createModuleTaskFn(m, mh, info)
		})

		return newTasks
	})

	return tasks
}

func (mm *ModuleManager) CreateTasksByBinding(binding BindingType, createTasksFunc func(gh *hooks.GlobalHook, m *modules.BasicModule, mh *hooks.ModuleHook) []sh_task.Task) []sh_task.Task {
	var allTasks []sh_task.Task

	// Process global hooks
	allTasks = append(allTasks, mm.createTasksFromGlobalHooks(binding, createTasksFunc)...)

	// Process module hooks for enabled modules
	allTasks = append(allTasks, mm.createTasksFromModuleHooks(binding, createTasksFunc)...)

	return allTasks
}

// createTasksFromGlobalHooks processes all global hooks for a given binding type
func (mm *ModuleManager) createTasksFromGlobalHooks(binding BindingType, createTasksFunc func(gh *hooks.GlobalHook, m *modules.BasicModule, mh *hooks.ModuleHook) []sh_task.Task) []sh_task.Task {
	var tasks []sh_task.Task

	for _, hookName := range mm.GetGlobalHooksInOrder(binding) {
		gh := mm.GetGlobalHook(hookName)
		if newTasks := createTasksFunc(gh, nil, nil); len(newTasks) > 0 {
			tasks = append(tasks, newTasks...)
		}
	}

	return tasks
}

// createTasksFromModuleHooks processes all module hooks for enabled modules and a given binding type
func (mm *ModuleManager) createTasksFromModuleHooks(binding BindingType, createTasksFunc func(gh *hooks.GlobalHook, m *modules.BasicModule, mh *hooks.ModuleHook) []sh_task.Task) []sh_task.Task {
	var tasks []sh_task.Task

	for _, moduleName := range mm.moduleScheduler.GetEnabledModuleNames() {
		module := mm.GetModule(moduleName)
		// Skip module if its hooks don't have hook controllers set
		if !module.HooksControllersReady() {
			continue
		}

		for _, moduleHook := range module.GetHooks(binding) {
			if newTasks := createTasksFunc(nil, module, moduleHook); len(newTasks) > 0 {
				tasks = append(tasks, newTasks...)
			}
		}
	}

	return tasks
}

func (mm *ModuleManager) runDynamicEnabledLoop(extender *dynamic_extender.Extender) {
	for report := range mm.global.EnabledReportChannel() {
		err := mm.applyEnabledPatch(report.Patch, extender)
		report.Done <- err
	}
}

// applyEnabledPatch changes "dynamicEnabled" map with patches.
// TODO: can add some optimization here
func (mm *ModuleManager) applyEnabledPatch(enabledPatch utils.ValuesPatch, extender *dynamic_extender.Extender) error {
	for _, op := range enabledPatch.Operations {
		// Extract module name from json patch: '"path": "/moduleNameEnabled"'
		modName := strings.TrimSuffix(op.Path, utils.EnabledSuffix)
		modName = strings.TrimPrefix(modName, "/")
		modName = utils.ModuleNameFromValuesKey(modName)
		v, err := utils.ModuleEnabledValue(op.Value)
		if err != nil {
			return fmt.Errorf("apply enabled patch operation '%s' for %s: %w", op.Op, op.Path, err)
		}
		switch op.Op {
		case "add":
			mm.logger.Debug("apply dynamic enable",
				slog.String("module", modName),
				slog.Bool("value", *v))
		case "remove":
			mm.logger.Debug("apply dynamic enable: module removed from dynamic enable",
				slog.String("module", modName))
		}
		extender.UpdateStatus(modName, op.Op, *v)
		mm.logger.Info("dynamically enabled module status change",
			slog.String("module", modName),
			slog.String("operation", op.Op),
			slog.Bool("state", *v))
	}

	return nil
}

// RecalculateGraph runs corresponding scheduler method that returns true if the graph's state has changed
func (mm *ModuleManager) RecalculateGraph(logLabels map[string]string) bool {
	stateChanged, verticesToUpdate := mm.moduleScheduler.RecalculateGraph(logLabels)
	for _, module := range verticesToUpdate {
		mm.SendModuleEvent(events.ModuleEvent{
			ModuleName: module,
			EventType:  events.ModuleStateChanged,
		})
	}

	return stateChanged
}

// GlobalSynchronizationNeeded is true if there is at least one global
// kubernetes hook with executeHookOnSynchronization.
func (mm *ModuleManager) GlobalSynchronizationNeeded() bool {
	for _, ghName := range mm.GetGlobalHooksInOrder(OnKubernetesEvent) {
		gHook := mm.GetGlobalHook(ghName)
		if gHook.SynchronizationNeeded() {
			return true
		}
	}

	return false
}

func (mm *ModuleManager) GlobalSynchronizationState() *modules.SynchronizationState {
	return mm.globalSynchronizationState
}

func (mm *ModuleManager) ApplyBindingActions(moduleHook *hooks.ModuleHook, bindingActions []gohook.BindingAction) error {
	for _, action := range bindingActions {
		bindingIdx := -1
		for i, binding := range moduleHook.GetHookConfig().OnKubernetesEvents {
			if binding.BindingName == action.Name {
				bindingIdx = i
			}
		}
		if bindingIdx == -1 {
			continue
		}

		monitorCfg := moduleHook.GetHookConfig().OnKubernetesEvents[bindingIdx].Monitor
		switch strings.ToLower(action.Action) {
		case "disable":
			// Empty kind - "null" monitor.
			monitorCfg.Kind = ""
			monitorCfg.ApiVersion = ""
			monitorCfg.Metadata.MetricLabels["kind"] = ""
		case "updatekind":
			monitorCfg.Kind = action.Kind
			monitorCfg.ApiVersion = action.ApiVersion
			monitorCfg.Metadata.MetricLabels["kind"] = action.Kind
		default:
			continue
		}

		// Recreate monitor. Synchronization phase is ignored, kubernetes events are allowed.
		err := moduleHook.GetHookController().UpdateMonitor(monitorCfg.Metadata.MonitorId, action.Kind, action.ApiVersion)
		if err != nil {
			return fmt.Errorf("update monitor: %w", err)
		}
	}
	return nil
}

func (mm *ModuleManager) SendModuleEvent(ev events.ModuleEvent) {
	if mm.moduleEventC == nil {
		return
	}

	mm.moduleEventC <- ev
}

func (mm *ModuleManager) SetModulePhaseAndNotify(module *modules.BasicModule, phase modules.ModuleRunPhase) {
	module.SetPhase(phase)
	mm.SendModuleEvent(events.ModuleEvent{
		ModuleName: module.GetName(),
		EventType:  events.ModuleStateChanged,
	})
}

func (mm *ModuleManager) UpdateModuleHookStatusAndNotify(module *modules.BasicModule, hookName string, err error) {
	module.SaveHookError(hookName, err)
	mm.SendModuleEvent(events.ModuleEvent{
		ModuleName: module.GetName(),
		EventType:  events.ModuleStateChanged,
	})
}

func (mm *ModuleManager) UpdateModuleLastErrorAndNotify(module *modules.BasicModule, err error) {
	module.SetError(err)
	mm.SendModuleEvent(events.ModuleEvent{
		ModuleName: module.GetName(),
		EventType:  events.ModuleStateChanged,
	})
}

// PushRunModuleTask pushes moduleRun task for a module into the main queue if there is no such a task for the module
func (mm *ModuleManager) PushRunModuleTask(moduleName string, doModuleStartup bool) error {
	// check if there is already moduleRun task in the main queue for the module
	if queueHasPendingModuleRunTaskWithStartup(mm.dependencies.TaskQueues.GetMain(), moduleName) {
		return nil
	}

	newTask := sh_task.NewTask(task.ModuleRun).
		WithQueueName("main").
		WithMetadata(task.HookMetadata{
			EventDescription: "ModuleManager-Update-Module",
			ModuleName:       moduleName,
			DoModuleStartup:  doModuleStartup,
		})
	newTask.SetProp("triggered-by", "ModuleManager")

	mm.dependencies.TaskQueues.GetMain().AddLast(newTask.WithQueuedAt(time.Now()))

	return nil
}

// AreModulesInited returns true if modulemanager's moduleset has already been initialized
func (mm *ModuleManager) AreModulesInited() bool {
	mm.l.Lock()
	defer mm.l.Unlock()
	return mm.modules.IsInited()
}

// RunModuleWithNewOpenAPISchema updates the module's OpenAPI schema from modulePath directory and pushes RunModuleTask if the module is enabled
func (mm *ModuleManager) RunModuleWithNewOpenAPISchema(moduleName string) error {
	currentModule := mm.modules.Get(moduleName)
	if currentModule == nil {
		return fmt.Errorf("failed to get basic module - not found")
	}

	basicModule, err := mm.moduleLoader.LoadModule(moduleName)
	if err != nil {
		return fmt.Errorf("load module: %w", err)
	}

	err = currentModule.ApplyNewSchemaStorage(basicModule.GetSchemaStorage())
	if err != nil {
		return fmt.Errorf("apply new schema storage: %w", err)
	}

	if mm.IsModuleEnabled(moduleName) {
		return mm.PushRunModuleTask(moduleName, false)
	}

	return nil
}

// RegisterModule checks if a module already exists and reapplies(reloads) its configuration.
// If it's a new module - converges all modules
func (mm *ModuleManager) RegisterModule(ctx context.Context, moduleName string) error {
	ctx, span := otel.Tracer(moduleManagerServiceName).Start(ctx, "RegisterModule")
	defer span.End()

	span.SetAttributes(attribute.String("module", moduleName))

	mm.logger.Debug("register the module", slog.String("module", moduleName))

	if module := mm.GetModule(moduleName); module != nil {
		mm.logger.Debug("unregister the module", slog.String("module", moduleName))

		mm.DisableModuleHooks(moduleName)
		mm.dependencies.HelmResourcesManager.StopMonitor(moduleName)
		module.ResetState()
		module.DeregisterHooks()
	}

	module, err := mm.moduleLoader.LoadModule(moduleName)
	if err != nil {
		return fmt.Errorf("load the module '%s': %w", moduleName, err)
	}

	// load and registry global hooks
	dep := &hooks.HookExecutionDependencyContainer{
		HookMetricsStorage: mm.dependencies.HookMetricStorage,
		KubeConfigManager:  mm.dependencies.KubeConfigManager,
		KubeObjectPatcher:  mm.dependencies.KubeObjectPatcher,
		MetricStorage:      mm.dependencies.MetricStorage,
		GlobalValuesGetter: mm.global,
	}

	module.WithDependencies(dep)

	// add module to set
	mm.modules.Add(module)

	// add module to scheduler
	if err = mm.moduleScheduler.AddModuleVertex(module); err != nil {
		return err
	}

	// reinit scheduler
	if err = mm.moduleScheduler.Initialize(); err != nil {
		return fmt.Errorf("reinitialize module scheduler: %w", err)
	}

	mm.SendModuleEvent(events.ModuleEvent{
		ModuleName: module.Name,
		EventType:  events.ModuleRegistered,
	})

	// send event to trigger reconverge
	mm.SchedulerEventCh() <- extenders.ExtenderEvent{
		ExtenderName:      dynamic_extender.Name,
		EncapsulatedEvent: dynamic_extender.DynamicExtenderEvent{},
	}

	return nil
}

// registerModules load all available modules from modules directory.
func (mm *ModuleManager) registerModules(scriptEnabledExtender *script_extender.Extender) error {
	if mm.ModulesDir == "" {
		mm.logger.Warn("empty modules directory is passed, no modules to load")

		return nil
	}

	if mm.moduleLoader == nil {
		mm.logger.Error("no module loader set")

		return fmt.Errorf("no module loader set")
	}

	mm.logger.Debug("search and register modules")

	mods, err := mm.moduleLoader.LoadModules()
	if err != nil {
		return fmt.Errorf("failed to load modules: %w", err)
	}

	set := &moduleset.ModulesSet{}

	dep := &hooks.HookExecutionDependencyContainer{
		HookMetricsStorage: mm.dependencies.HookMetricStorage,
		KubeConfigManager:  mm.dependencies.KubeConfigManager,
		KubeObjectPatcher:  mm.dependencies.KubeObjectPatcher,
		MetricStorage:      mm.dependencies.MetricStorage,
		GlobalValuesGetter: mm.global,
		EnvironmentManager: mm.environmentManager,
	}

	for _, mod := range mods {
		if set.Has(mod.GetName()) {
			mm.logger.Warn("module is not registered, because it has a duplicate",
				slog.String("module", mod.GetName()),
				slog.String("path", mod.GetPath()))
			continue
		}

		mod.WithDependencies(dep)
		set.Add(mod)

		if err = mm.moduleScheduler.AddModuleVertex(mod); err != nil {
			return fmt.Errorf("add module vertex: %w", err)
		}

		scriptEnabledExtender.AddBasicModule(mod)

		mm.SendModuleEvent(events.ModuleEvent{
			ModuleName: mod.GetName(),
			EventType:  events.ModuleRegistered,
		})
	}

	if err = mm.moduleScheduler.Initialize(); err != nil {
		return fmt.Errorf("initialize scheduler: %w", err)
	}

	mm.logger.Debug("Found modules", slog.Any("modules", set.NamesInOrder()))

	mm.l.Lock()
	mm.modules = set
	mm.l.Unlock()

	return nil
}

// SetModuleEventsChannel sets an event channel for Module Manager
func (mm *ModuleManager) SetModuleEventsChannel(ec chan events.ModuleEvent) {
	if mm.moduleEventC == nil {
		mm.moduleEventC = ec
	}
}

// GetModuleEventsChannel returns a channel with events that occur during module processing
// events channel is created only if someone is reading it
func (mm *ModuleManager) GetModuleEventsChannel() chan events.ModuleEvent {
	if mm.moduleEventC == nil {
		mm.moduleEventC = make(chan events.ModuleEvent, 50)
	}

	return mm.moduleEventC
}

func (mm *ModuleManager) SchedulerEventCh() chan extenders.ExtenderEvent {
	return mm.moduleScheduler.EventCh()
}

func (mm *ModuleManager) ModuleHasCRDs(moduleName string) bool {
	return mm.GetModule(moduleName).CRDExist()
}

func (mm *ModuleManager) EnvironmentManagerEnabled() bool {
	return mm.environmentManager != nil
}

// queueHasPendingModuleRunTaskWithStartup returns true if queue has pending tasks
// with the type "ModuleRun" related to the module "moduleName" and DoModuleStartup is set to true.
func queueHasPendingModuleRunTaskWithStartup(q *queue.TaskQueue, moduleName string) bool {
	if q == nil {
		return false
	}
	modules := modulesWithPendingTasks(q, task.ModuleRun)
	meta, has := modules[moduleName]
	return has && meta.doStartup
}

func modulesWithPendingTasks(q *queue.TaskQueue, taskType sh_task.TaskType) map[string]struct{ doStartup bool } {
	if q == nil {
		return nil
	}

	modules := make(map[string]struct{ doStartup bool })

	skipFirstTask := true

	q.IterateSnapshot(func(t sh_task.Task) {
		// Skip the first task in the queue as it can be executed already, i.e. "not pending".
		if skipFirstTask {
			skipFirstTask = false
			return
		}

		if t.GetType() == taskType {
			hm := task.HookMetadataAccessor(t)
			modules[hm.ModuleName] = struct{ doStartup bool }{doStartup: hm.DoModuleStartup}
		}
	})

	return modules
}
