package module_manager

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/trace"
	"strings"
	"sync"
	"time"

	// bindings constants and binding configs
	"github.com/hashicorp/go-multierror"
	log "github.com/sirupsen/logrus"

	"github.com/flant/addon-operator/pkg/addon-operator/converge"
	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/helm_resources_manager"
	. "github.com/flant/addon-operator/pkg/hook/types"
	"github.com/flant/addon-operator/pkg/kube_config_manager/config"
	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/pkg/module_manager/loader"
	"github.com/flant/addon-operator/pkg/module_manager/loader/fs"
	"github.com/flant/addon-operator/pkg/module_manager/models/hooks"
	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
	"github.com/flant/addon-operator/pkg/module_manager/models/modules/events"
	"github.com/flant/addon-operator/pkg/module_manager/models/moduleset"
	"github.com/flant/addon-operator/pkg/task"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/addon-operator/pkg/values/validation"
	. "github.com/flant/shell-operator/pkg/hook/binding_context"
	"github.com/flant/shell-operator/pkg/hook/controller"
	. "github.com/flant/shell-operator/pkg/hook/types"
	"github.com/flant/shell-operator/pkg/kube/object_patch"
	"github.com/flant/shell-operator/pkg/kube_events_manager"
	. "github.com/flant/shell-operator/pkg/kube_events_manager/types"
	"github.com/flant/shell-operator/pkg/metric_storage"
	"github.com/flant/shell-operator/pkg/schedule_manager"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
	utils_checksum "github.com/flant/shell-operator/pkg/utils/checksum"
)

// ModulesState determines which modules should be enabled, disabled or reloaded.
type ModulesState struct {
	// All enabled modules.
	AllEnabledModules []string
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
}

type KubeConfigManager interface {
	SaveConfigValues(key string, values utils.Values) error
	IsModuleEnabled(moduleName string) bool
	UpdateModuleConfig(moduleName string) error
	SafeReadConfig(handler func(config *config.KubeConfig))
}

// ModuleManagerDependencies pass dependencies for ModuleManager
type ModuleManagerDependencies struct {
	KubeObjectPatcher    *object_patch.ObjectPatcher
	KubeEventsManager    kube_events_manager.KubeEventsManager
	KubeConfigManager    KubeConfigManager
	ScheduleManager      schedule_manager.ScheduleManager
	Helm                 *helm.ClientFactory
	HelmResourcesManager helm_resources_manager.HelmResourcesManager
	MetricStorage        *metric_storage.MetricStorage
	HookMetricStorage    *metric_storage.MetricStorage
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
	ModulesDir     string
	GlobalHooksDir string
	TempDir        string

	moduleLoader loader.ModuleLoader

	// Dependencies.
	dependencies *ModuleManagerDependencies

	ValuesValidator *validation.ValuesValidator

	// All known modules from specified directories ($MODULES_DIR)
	modules *moduleset.ModulesSet

	global *modules.GlobalModule

	// values::set "moduleNameEnabled" "\"true\""
	// module enable values from global hooks
	dynamicEnabled map[string]*bool

	// List of modules enabled by values.yaml or by kube config.
	// This list is changed on ConfigMap updates.
	enabledModulesByConfig map[string]struct{}

	// List of effectively enabled modules after running enabled scripts.
	enabledModules *eModules

	globalSynchronizationState *modules.SynchronizationState

	kubeConfigLock sync.RWMutex
	// addon-operator config is valid.
	kubeConfigValid bool
	// Static and config values are valid using OpenAPI schemas.
	kubeConfigValuesValid bool

	moduleEventC chan events.ModuleEvent
}

type eModules struct {
	lock    sync.RWMutex
	modules []string
}

func (m *eModules) Add(name string) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.modules = append(m.modules, name)
}

func (m *eModules) Delete(name string) {
	m.lock.Lock()
	defer m.lock.Unlock()
	for i, n := range m.modules {
		if n == name {
			m.modules[i] = m.modules[len(m.modules)-1]
			m.modules = m.modules[:len(m.modules)-1]
			break
		}
	}
}

func (m *eModules) Replace(modules []string) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.modules = modules
}

func (m *eModules) GetAll() []string {
	m.lock.RLock()
	defer m.lock.RUnlock()
	return m.modules
}

func (m *eModules) String() string {
	m.lock.RLock()
	defer m.lock.RUnlock()
	return fmt.Sprintf("%v", m.modules)
}

// NewModuleManager returns new MainModuleManager
func NewModuleManager(ctx context.Context, cfg *ModuleManagerConfig) *ModuleManager {
	cctx, cancel := context.WithCancel(ctx)
	validator := validation.NewValuesValidator()

	// default loader, maybe we can register another one on startup
	fsLoader := fs.NewFileSystemLoader(cfg.DirectoryConfig.ModulesDir, validator)

	return &ModuleManager{
		ctx:    cctx,
		cancel: cancel,

		ModulesDir:     cfg.DirectoryConfig.ModulesDir,
		GlobalHooksDir: cfg.DirectoryConfig.GlobalHooksDir,
		TempDir:        cfg.DirectoryConfig.TempDir,

		moduleLoader: fsLoader,

		dependencies: &cfg.Dependencies,

		ValuesValidator: validator,

		modules: new(moduleset.ModulesSet),

		enabledModulesByConfig: make(map[string]struct{}),
		enabledModules:         &eModules{modules: make([]string, 0)},
		dynamicEnabled:         make(map[string]*bool),

		globalSynchronizationState: modules.NewSynchronizationState(),
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

// runModulesEnabledScript runs enable script for each module from the list.
// Each 'enabled' script receives a list of previously enabled modules.
func (mm *ModuleManager) runModulesEnabledScript(modules []string, logLabels map[string]string) ([]string, error) {
	enabled := make([]string, 0)

	for _, moduleName := range modules {
		ml := mm.GetModule(moduleName)
		isEnabled, err := ml.RunEnabledScript(mm.TempDir, enabled, logLabels)
		if err != nil {
			return nil, err
		}

		if isEnabled {
			enabled = append(enabled, moduleName)
		}
	}

	return enabled, nil
}

func (mm *ModuleManager) GetGlobal() *modules.GlobalModule {
	return mm.global
}

// HandleNewKubeConfig validates new config values with config schemas,
// checks which parts changed and returns state with AllEnabledModules and
// ModulesToReload list if only module sections are changed.
// It returns a nil state if new KubeConfig not changing
// config values or 'enabled by config' state.
//
// This method updates 'config values' caches:
// - mm.enabledModulesByConfig
// - mm.kubeGlobalConfigValues
// - mm.kubeModulesConfigValues
func (mm *ModuleManager) HandleNewKubeConfig(kubeConfig *config.KubeConfig) (*ModulesState, error) {
	if kubeConfig == nil {
		// have no idea, how it could be, just skip run
		log.Warnf("No KubeConfig is set")
		return &ModulesState{}, nil
	}

	mm.warnAboutUnknownModules(kubeConfig)

	// Get map of enabled modules after KubeConfig changes.
	newEnabledByConfig := mm.calculateEnabledModulesByConfig(kubeConfig)
	allModules := make(map[string]struct{}, len(mm.modules.NamesInOrder()))
	for _, module := range mm.modules.NamesInOrder() {
		allModules[module] = struct{}{}
	}

	valuesMap, validationErr := mm.validateNewKubeConfig(kubeConfig, allModules)
	if validationErr != nil {
		mm.SetKubeConfigValuesValid(false)
		return &ModulesState{}, validationErr
	}

	mm.SetKubeConfigValuesValid(true)

	// Detect changes in global section.
	hasGlobalChange := false
	newGlobalValues, ok := valuesMap[mm.global.GetName()]
	if ok {
		if newGlobalValues.Checksum() != mm.global.GetConfigValues(false).Checksum() {
			hasGlobalChange = true
			mm.global.SaveConfigValues(newGlobalValues)
		}
		delete(valuesMap, mm.global.GetName())
	}

	// Full reload if enabled flags are changed.
	isEnabledChanged := false
	for _, moduleName := range mm.modules.NamesInOrder() {
		// Current module state.
		_, wasEnabled := mm.enabledModulesByConfig[moduleName]
		_, isEnabled := newEnabledByConfig[moduleName]

		if wasEnabled != isEnabled {
			isEnabledChanged = true
			break
		}
	}

	// Detect changed module sections for enabled modules.
	modulesChanged := make([]string, 0)

	for moduleName, values := range valuesMap {
		mod := mm.GetModule(moduleName)
		if mod == nil {
			continue
		}

		if mod.GetConfigValues(false).Checksum() != values.Checksum() {
			if mm.IsModuleEnabled(moduleName) {
				modulesChanged = append(modulesChanged, moduleName)
			}
			mod.SaveConfigValues(values)
		}
	}

	mm.enabledModulesByConfig = newEnabledByConfig

	// Return empty state on global change.
	if hasGlobalChange || isEnabledChanged {
		return &ModulesState{}, nil
	}

	// Return list of changed modules when only values are changed.
	if len(modulesChanged) > 0 {
		return &ModulesState{
			AllEnabledModules: mm.enabledModules.GetAll(),
			ModulesToReload:   modulesChanged,
		}, nil
	}

	// Return nil if cached state is not changed by ConfigMap.
	return nil, nil
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
		log.Warnf("KubeConfigManager has values for unknown modules: %+v", unknownNames)
	}
}

// calculateEnabledModulesByConfig determine enable state for all modules
// by checking *Enabled fields in values.yaml, KubeConfig and dynamicEnable map.
// Method returns list of enabled modules.
//
// Module is enabled by config if module section in KubeConfig is a map or an array
// or KubeConfig has no module section and module has a map or an array in values.yaml
func (mm *ModuleManager) calculateEnabledModulesByConfig(config *config.KubeConfig) map[string]struct{} {
	enabledByConfig := make(map[string]struct{})

	for _, ml := range mm.modules.List() {
		var kubeConfigEnabled *bool
		var kubeConfigEnabledStr string
		if config != nil {
			if kubeConfig, hasKubeConfig := config.Modules[ml.GetName()]; hasKubeConfig {
				kubeConfigEnabled = kubeConfig.IsEnabled
				kubeConfigEnabledStr = kubeConfig.GetEnabled()
			}
		}

		_, isEnabledByConfig := mm.enabledModulesByConfig[ml.GetName()]

		isEnabled := mergeEnabled(
			&isEnabledByConfig,
			kubeConfigEnabled,
		)

		if isEnabled {
			enabledByConfig[ml.GetName()] = struct{}{}
		}

		log.Debugf("enabledByConfig: module '%s' enabled flags: moduleConfig'%v', kubeConfig '%v', result: '%v'",
			ml.GetName(),
			isEnabledByConfig,
			kubeConfigEnabledStr,
			isEnabled)
	}

	return enabledByConfig
}

// calculateEnabledModulesWithDynamic determine enable state for all modules
// by checking *Enabled fields in values.yaml, ConfigMap and dynamicEnable map.
// Method returns list of enabled modules.
//
// Module is enabled by config if module section in ConfigMap is a map or an array
// or ConfigMap has no module section and module has a map or an array in values.yaml
func (mm *ModuleManager) calculateEnabledModulesWithDynamic(enabledByConfig map[string]struct{}) []string {
	log.Debugf("calculateEnabled: dynamicEnabled is %s", mm.DumpDynamicEnabled())

	enabled := make([]string, 0)
	for _, moduleName := range mm.modules.NamesInOrder() {
		_, isEnabledByConfig := enabledByConfig[moduleName]

		isEnabled := mergeEnabled(
			&isEnabledByConfig,
			mm.dynamicEnabled[moduleName],
		)

		if isEnabled {
			enabled = append(enabled, moduleName)
		}
	}

	return enabled
}

// Init — initialize module manager
func (mm *ModuleManager) Init() error {
	log.Debug("Init ModuleManager")

	globalValues, enabledModules, err := mm.loadGlobalValues()
	if err != nil {
		return err
	}

	mm.enabledModulesByConfig = enabledModules

	if err := mm.registerGlobalModule(globalValues); err != nil {
		return err
	}

	return mm.registerModules()
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
	releasedModules, err := mm.dependencies.Helm.NewClient(logLabels).ListReleasesNames(nil)
	if err != nil {
		return nil, err
	}

	state := mm.stateFromHelmReleases(releasedModules)

	return state, nil
}

// stateFromHelmReleases calculates modules to purge from Helm releases.
func (mm *ModuleManager) stateFromHelmReleases(releases []string) *ModulesState {
	releasesMap := utils.ListToMapStringStruct(releases)

	// Filter out known modules.
	for _, modName := range mm.modules.NamesInOrder() {
		// Remove known module to detect unknown ones.
		delete(releasesMap, modName)
	}

	// Filter out dynamically enabled modules (a way to save unknown releases).
	for modName, dynEnable := range mm.dynamicEnabled {
		if dynEnable != nil && *dynEnable {
			delete(releasesMap, modName)
		}
	}

	purge := utils.MapStringStructKeys(releasesMap)
	purge = utils.SortReverse(purge)

	log.Infof("Modules to purge found: %v", purge)

	return &ModulesState{
		ModulesToPurge: purge,
	}
}

// RefreshEnabledState runs enabled hooks for all 'enabled by config' modules and
// calculates new arrays of enabled modules. It returns ModulesState with
// lists of modules to disable and enable.
//
// This method is called after beforeAll hooks to take into account
// possible changes to 'dynamic enabled'.
//
// This method updates caches:
// - mm.enabledModules
func (mm *ModuleManager) RefreshEnabledState(logLabels map[string]string) (*ModulesState, error) {
	refreshLogLabels := utils.MergeLabels(logLabels, map[string]string{
		"operator.component": "ModuleManager.RefreshEnabledState",
	})
	logEntry := log.WithFields(utils.LabelsToLogFields(refreshLogLabels))

	logEntry.Debugf("Refresh state current:\n"+
		"    mm.enabledModulesByConfig: %v\n"+
		"    mm.enabledModules: %v\n",
		mm.enabledModulesByConfig,
		mm.enabledModules)

	// Correct enabled modules list with dynamic enabled.
	enabledByDynamic := mm.calculateEnabledModulesWithDynamic(mm.enabledModulesByConfig)
	// Calculate final enabled modules list by running 'enabled' scripts.
	enabledModules, err := mm.runModulesEnabledScript(enabledByDynamic, logLabels)
	if err != nil {
		return nil, err
	}
	logEntry.Infof("Modules enabled by scripts: %+v", enabledModules)

	// Difference between the list of currently enabled modules and the list
	// of enabled modules after running enabled scripts.
	// Newly enabled modules are that present in the list after running enabled scripts
	// but not present in the list of currently enabled modules.
	newlyEnabledModules := utils.ListSubtract(enabledModules, mm.enabledModules.GetAll())

	// Disabled modules are that present in the list of currently enabled modules
	// but not present in the list after running enabled scripts
	disabledModules := utils.ListSubtract(mm.enabledModules.GetAll(), enabledModules)
	disabledModules = utils.SortReverseByReference(disabledModules, mm.modules.NamesInOrder())

	logEntry.Debugf("Refresh state results:\n"+
		"    mm.enabledModulesByConfig: %v\n"+
		"    mm.enabledModules: %v\n"+
		"    ModulesToDisable: %v\n"+
		"    ModulesToEnable: %v\n",
		mm.enabledModulesByConfig,
		mm.enabledModules,
		disabledModules,
		newlyEnabledModules)

	// Update state
	mm.enabledModules.Replace(enabledModules)

	mm.global.SetEnabledModules(mm.enabledModules.GetAll())

	// Return lists for ConvergeModules task.
	return &ModulesState{
		AllEnabledModules: mm.enabledModules.GetAll(),
		ModulesToDisable:  disabledModules,
		ModulesToEnable:   newlyEnabledModules,
	}, nil
}

func (mm *ModuleManager) GetModule(name string) *modules.BasicModule {
	return mm.modules.Get(name)
}

func (mm *ModuleManager) GetModuleNames() []string {
	return mm.modules.NamesInOrder()
}

func (mm *ModuleManager) GetEnabledModuleNames() []string {
	return mm.enabledModules.GetAll()
}

func (mm *ModuleManager) AddEnabledModuleName(name string) {
	mm.enabledModules.Add(name)
}

func (mm *ModuleManager) DeleteEnabledModuleName(name string) {
	mm.enabledModules.Delete(name)
}

func (mm *ModuleManager) AddEnabledModuleByConfigName(name string) {
	mm.enabledModulesByConfig[name] = struct{}{}
}

func (mm *ModuleManager) DeleteEnabledModuleByConfigName(name string) {
	delete(mm.enabledModulesByConfig, name)
}

// IsModuleEnabled ...
func (mm *ModuleManager) IsModuleEnabled(moduleName string) bool {
	for _, modName := range mm.enabledModules.GetAll() {
		if modName == moduleName {
			return true
		}
	}
	return false
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

func (mm *ModuleManager) DeleteModule(moduleName string, logLabels map[string]string) error {
	ml := mm.GetModule(moduleName)

	// Stop kubernetes informers and remove scheduled functions
	mm.DisableModuleHooks(moduleName)

	// DELETE
	{
		defer trace.StartRegion(context.Background(), "ModuleDelete-HelmPhase").End()

		deleteLogLabels := utils.MergeLabels(logLabels,
			map[string]string{
				"module": ml.GetName(),
				"queue":  "main",
			})
		logEntry := log.WithFields(utils.LabelsToLogFields(deleteLogLabels))

		// Stop resources monitor before deleting release
		mm.dependencies.HelmResourcesManager.StopMonitor(ml.GetName())

		// Module has chart, but there is no release -> log a warning.
		// Module has chart and release -> execute helm delete.
		hmdeps := modules.HelmModuleDependencies{
			HelmClientFactory:   mm.dependencies.Helm,
			HelmResourceManager: mm.dependencies.HelmResourcesManager,
			MetricsStorage:      mm.dependencies.MetricStorage,
			HelmValuesValidator: mm.ValuesValidator,
		}
		helmModule, _ := modules.NewHelmModule(ml, mm.TempDir, &hmdeps, mm.ValuesValidator)
		if helmModule != nil {
			releaseExists, err := mm.dependencies.Helm.NewClient(deleteLogLabels).IsReleaseExists(ml.GetName())
			if !releaseExists {
				if err != nil {
					logEntry.Warnf("Cannot find helm release '%s' for module '%s'. Helm error: %s", ml.GetName(), ml.GetName(), err)
				} else {
					logEntry.Warnf("Cannot find helm release '%s' for module '%s'.", ml.GetName(), ml.GetName())
				}
			} else {
				// Chart and release are existed, so run helm delete command
				err := mm.dependencies.Helm.NewClient(deleteLogLabels).DeleteRelease(ml.GetName())
				if err != nil {
					return err
				}
			}
		}

		err := ml.RunHooksByBinding(AfterDeleteHelm, deleteLogLabels)
		if err != nil {
			return err
		}

		// Cleanup state.
		ml.ResetState()
	}

	// Unregister module hooks.
	ml.DeregisterHooks()

	return nil
}

// RunModule runs beforeHelm hook, helm upgrade --install and afterHelm or afterDeleteHelm hook
func (mm *ModuleManager) RunModule(moduleName string, logLabels map[string]string) ( /*valuesChanged*/ bool, error) {
	bm := mm.GetModule(moduleName)

	// Do not send to mm.moduleValuesChanged, changed values are handled by TaskHandler.
	defer trace.StartRegion(context.Background(), "ModuleRun-HelmPhase").End()

	logLabels = utils.MergeLabels(logLabels, map[string]string{
		"module": bm.GetName(),
		"queue":  "main",
	})

	// Hooks can delete release resources, so pause resources monitor before run hooks.
	mm.dependencies.HelmResourcesManager.PauseMonitor(bm.GetName())
	defer mm.dependencies.HelmResourcesManager.ResumeMonitor(bm.GetName())

	var err error

	treg := trace.StartRegion(context.Background(), "ModuleRun-HelmPhase-beforeHelm")
	err = bm.RunHooksByBinding(BeforeHelm, logLabels)
	treg.End()
	if err != nil {
		return false, err
	}

	treg = trace.StartRegion(context.Background(), "ModuleRun-HelmPhase-helm")
	deps := &modules.HelmModuleDependencies{
		HelmClientFactory:   mm.dependencies.Helm,
		HelmResourceManager: mm.dependencies.HelmResourcesManager,
		MetricsStorage:      mm.dependencies.MetricStorage,
		HelmValuesValidator: mm.ValuesValidator,
	}
	helmModule, err := modules.NewHelmModule(bm, mm.TempDir, deps, mm.ValuesValidator)
	if err != nil {
		return false, err
	}
	if helmModule != nil {
		// could be nil, if it doesn't contain helm chart
		err = helmModule.RunHelmInstall(logLabels)
	}
	treg.End()
	if err != nil {
		return false, err
	}

	oldValues := bm.GetValues(false)
	oldValuesChecksum := oldValues.Checksum()
	treg = trace.StartRegion(context.Background(), "ModuleRun-HelmPhase-afterHelm")
	err = bm.RunHooksByBinding(AfterHelm, logLabels)
	treg.End()
	if err != nil {
		return false, err
	}

	newValues := bm.GetValues(false)
	newValuesChecksum := newValues.Checksum()

	// Do not send to mm.moduleValuesChanged, changed values are handled by TaskHandler.
	return oldValuesChecksum != newValuesChecksum, nil
}

func (mm *ModuleManager) RunGlobalHook(hookName string, binding BindingType, bindingContext []BindingContext, logLabels map[string]string) (string, string, error) {
	return mm.global.RunHookByName(hookName, binding, bindingContext, logLabels)
}

func (mm *ModuleManager) RunModuleHook(moduleName, hookName string, binding BindingType, bindingContext []BindingContext, logLabels map[string]string) (beforeChecksum string, afterChecksum string, err error) {
	ml := mm.GetModule(moduleName)

	return ml.RunHookByName(hookName, binding, bindingContext, logLabels)
}

func (mm *ModuleManager) GetValuesValidator() *validation.ValuesValidator {
	return mm.ValuesValidator
}

func (mm *ModuleManager) HandleKubeEvent(kubeEvent KubeEvent, createGlobalTaskFn func(*hooks.GlobalHook, controller.BindingExecutionInfo), createModuleTaskFn func(*modules.BasicModule, *hooks.ModuleHook, controller.BindingExecutionInfo)) {
	mm.LoopByBinding(OnKubernetesEvent, func(gh *hooks.GlobalHook, m *modules.BasicModule, mh *hooks.ModuleHook) {
		defer func() {
			if err := recover(); err != nil {
				logEntry := log.WithField("function", "HandleKubeEvent").WithField("event", "OnKubernetesEvent")

				if gh != nil {
					logEntry.WithField("GlobalHook name", gh.GetName()).WithField("GlobakHook path", gh.GetPath())
				}

				if mh != nil {
					logEntry.WithField("ModuleHook name", mh.GetName()).WithField("ModuleHook path", mh.GetPath())
				}

				logEntry.Errorf("panic occurred: %s", err)
			}
		}()

		if gh != nil {
			if gh.GetHookController().CanHandleKubeEvent(kubeEvent) {
				gh.GetHookController().HandleKubeEvent(kubeEvent, func(info controller.BindingExecutionInfo) {
					if createGlobalTaskFn != nil {
						createGlobalTaskFn(gh, info)
					}
				})
			}
		} else {
			if mh.GetHookController().CanHandleKubeEvent(kubeEvent) {
				mh.GetHookController().HandleKubeEvent(kubeEvent, func(info controller.BindingExecutionInfo) {
					if createModuleTaskFn != nil {
						createModuleTaskFn(m, mh, info)
					}
				})
			}
		}
	})
}

func (mm *ModuleManager) HandleGlobalEnableKubernetesBindings(hookName string, createTaskFn func(*hooks.GlobalHook, controller.BindingExecutionInfo)) error {
	gh := mm.GetGlobalHook(hookName)

	err := gh.GetHookController().HandleEnableKubernetesBindings(func(info controller.BindingExecutionInfo) {
		if createTaskFn != nil {
			createTaskFn(gh, info)
		}
	})
	if err != nil {
		return err
	}

	return nil
}

func (mm *ModuleManager) HandleModuleEnableKubernetesBindings(moduleName string, createTaskFn func(*hooks.ModuleHook, controller.BindingExecutionInfo)) error {
	ml := mm.GetModule(moduleName)

	kubeHooks := ml.GetHooks(OnKubernetesEvent)

	for _, mh := range kubeHooks {
		err := mh.GetHookController().HandleEnableKubernetesBindings(func(info controller.BindingExecutionInfo) {
			if createTaskFn != nil {
				createTaskFn(mh, info)
			}
		})
		if err != nil {
			return err
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

func (mm *ModuleManager) DisableModuleHooks(moduleName string) {
	ml := mm.GetModule(moduleName)

	kubeHooks := ml.GetHooks(OnKubernetesEvent)

	for _, mh := range kubeHooks {
		mh.GetHookController().StopMonitors()
	}

	schHooks := ml.GetHooks(Schedule)
	for _, mh := range schHooks {
		mh.GetHookController().DisableScheduleBindings()
	}
}

func (mm *ModuleManager) HandleScheduleEvent(crontab string, createGlobalTaskFn func(*hooks.GlobalHook, controller.BindingExecutionInfo), createModuleTaskFn func(*modules.BasicModule, *hooks.ModuleHook, controller.BindingExecutionInfo)) error {
	mm.LoopByBinding(Schedule, func(gh *hooks.GlobalHook, m *modules.BasicModule, mh *hooks.ModuleHook) {
		if gh != nil {
			if gh.GetHookController().CanHandleScheduleEvent(crontab) {
				gh.GetHookController().HandleScheduleEvent(crontab, func(info controller.BindingExecutionInfo) {
					if createGlobalTaskFn != nil {
						createGlobalTaskFn(gh, info)
					}
				})
			}
		} else {
			if mh.GetHookController().CanHandleScheduleEvent(crontab) {
				mh.GetHookController().HandleScheduleEvent(crontab, func(info controller.BindingExecutionInfo) {
					if createModuleTaskFn != nil {
						createModuleTaskFn(m, mh, info)
					}
				})
			}
		}
	})

	return nil
}

func (mm *ModuleManager) LoopByBinding(binding BindingType, fn func(gh *hooks.GlobalHook, m *modules.BasicModule, mh *hooks.ModuleHook)) {
	globalHooks := mm.GetGlobalHooksInOrder(binding)

	for _, hookName := range globalHooks {
		gh := mm.GetGlobalHook(hookName)
		fn(gh, nil, nil)
	}

	for _, moduleName := range mm.enabledModules.GetAll() {
		m := mm.GetModule(moduleName)
		moduleHooks := m.GetHooks(binding)
		for _, mh := range moduleHooks {
			fn(nil, m, mh)
		}
	}
}

func (mm *ModuleManager) runDynamicEnabledLoop() {
	for report := range mm.global.EnabledReportChannel() {
		err := mm.applyEnabledPatch(report.Patch)
		report.Done <- err
	}
}

// applyEnabledPatch changes "dynamicEnabled" map with patches.
// TODO: can add some optimization here
func (mm *ModuleManager) applyEnabledPatch(enabledPatch utils.ValuesPatch) error {
	newDynamicEnabled := map[string]*bool{}
	for k, v := range mm.dynamicEnabled {
		newDynamicEnabled[k] = v
	}

	for _, op := range enabledPatch.Operations {
		// Extract module name from json patch: '"path": "/moduleNameEnabled"'
		modName := strings.TrimSuffix(op.Path, "Enabled")
		modName = strings.TrimPrefix(modName, "/")
		modName = utils.ModuleNameFromValuesKey(modName)

		switch op.Op {
		case "add":
			v, err := utils.ModuleEnabledValue(op.Value)
			if err != nil {
				return fmt.Errorf("apply enabled patch operation '%s' for %s: %v", op.Op, op.Path, err)
			}
			log.Debugf("apply dynamic enable: module %s set to '%v'", modName, *v)
			newDynamicEnabled[modName] = v
		case "remove":
			log.Debugf("apply dynamic enable: module %s removed from dynamic enable", modName)
			delete(newDynamicEnabled, modName)
		}
	}

	mm.dynamicEnabled = newDynamicEnabled

	log.Infof("dynamic enabled modules list after patch: %s", mm.DumpDynamicEnabled())

	return nil
}

// DynamicEnabledChecksum returns checksum for dynamicEnabled map
func (mm *ModuleManager) DynamicEnabledChecksum() string {
	jsonBytes, err := json.Marshal(mm.dynamicEnabled)
	if err != nil {
		log.Errorf("dynamicEnabled checksum calculate from '%s': %v", mm.DumpDynamicEnabled(), err)
	}
	return utils_checksum.CalculateChecksum(string(jsonBytes))
}

func (mm *ModuleManager) DumpDynamicEnabled() string {
	dump := "["
	for k, v := range mm.dynamicEnabled {
		enabled := "nil"
		if v != nil {
			if *v {
				enabled = "true"
			} else {
				enabled = "false"
			}
		}
		dump += fmt.Sprintf("%s(%s), ", k, enabled)
	}
	return dump + "]"
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

func (mm *ModuleManager) ApplyBindingActions(moduleHook *hooks.ModuleHook, bindingActions []go_hook.BindingAction) error {
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
			return err
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

// mergeEnabled merges enabled flags. Enabled flag can be nil.
//
// If all flags are nil, then false is returned — module is disabled by default.
func mergeEnabled(enabledFlags ...*bool) bool {
	result := false
	for _, enabled := range enabledFlags {
		if enabled == nil {
			continue
		}
		result = *enabled
	}

	return result
}

// PushDeleteModule pushes moduleDelete task for a module into the main queue
func (mm *ModuleManager) PushDeleteModuleTask(moduleName string) {
	// check if there is already moduleDelete task in the main queue for the module
	if queueHasPendingModuleDeleteTask(mm.dependencies.TaskQueues.GetMain(), moduleName) {
		return
	}

	newTask := sh_task.NewTask(task.ModuleDelete).
		WithQueueName("main").
		WithMetadata(task.HookMetadata{
			EventDescription: "ModuleManager-Delete-Module",
			ModuleName:       moduleName,
		})
	newTask.SetProp("triggered-by", "ModuleManager")

	mm.dependencies.TaskQueues.GetMain().AddLast(newTask.WithQueuedAt(time.Now()))

	log.Infof("Push ConvergeModules task because %q Module was disabled", moduleName)
	mm.PushConvergeModulesTask(moduleName, "disabled")
}

// PushConvergeModulesTask pushes ConvergeModulesTask into the main queue to update all modules on a module enable/disable event
func (mm *ModuleManager) PushConvergeModulesTask(moduleName, moduleState string) {
	newConvergeTask := sh_task.NewTask(task.ConvergeModules).
		WithQueueName("main").
		WithMetadata(task.HookMetadata{
			EventDescription: fmt.Sprintf("ModuleManager-%s-Module", moduleState),
			ModuleName:       moduleName,
		}).
		WithQueuedAt(time.Now())
	newConvergeTask.SetProp("triggered-by", "ModuleManager")
	newConvergeTask.SetProp(converge.ConvergeEventProp, converge.ReloadAllModules)

	mm.dependencies.TaskQueues.GetMain().AddLast(newConvergeTask.WithQueuedAt(time.Now()))
}

// PushRunModuleTask pushes moduleRun task for a module into the main queue if there is no such a task for the module
func (mm *ModuleManager) PushRunModuleTask(moduleName string) error {
	// update module's kube config
	err := mm.UpdateModuleKubeConfig(moduleName)
	if err != nil {
		return err
	}

	// check if there is already moduleRun task in the main queue for the module
	if queueHasPendingModuleRunTaskWithStartup(mm.dependencies.TaskQueues.GetMain(), moduleName) {
		return nil
	}

	newTask := sh_task.NewTask(task.ModuleRun).
		WithQueueName("main").
		WithMetadata(task.HookMetadata{
			EventDescription: "ModuleManager-Update-Module",
			ModuleName:       moduleName,
			DoModuleStartup:  true,
		})
	newTask.SetProp("triggered-by", "ModuleManager")

	mm.dependencies.TaskQueues.GetMain().AddLast(newTask.WithQueuedAt(time.Now()))

	return nil
}

// UpdateModuleKubeConfig updates a module's kube config
func (mm *ModuleManager) UpdateModuleKubeConfig(moduleName string) error {
	err := mm.dependencies.KubeConfigManager.UpdateModuleConfig(moduleName)
	if err != nil {
		return fmt.Errorf("couldn't update module %s kube config: %w", moduleName, err)
	}

	mm.dependencies.KubeConfigManager.SafeReadConfig(func(config *config.KubeConfig) {
		_, err = mm.HandleNewKubeConfig(config)
	})
	if err != nil {
		return fmt.Errorf("couldn't reload kube config: %s", err)
	}

	return nil
}

// AreModulesInited returns true if modulemanager's moduleset has already been initialized
func (mm *ModuleManager) AreModulesInited() bool {
	return mm.modules.IsInited()
}

// RegisterModule checks if a module already exists and reapplies(reloads) its configuration.
// If it's a new module - converges all modules
func (mm *ModuleManager) RegisterModule(moduleSource, modulePath string) error {
	if !mm.modules.IsInited() {
		return moduleset.ErrNotInited
	}

	if mm.ModulesDir == "" {
		log.Warnf("Empty modules directory is passed! No modules to load.")
		return nil
	}

	if mm.moduleLoader == nil {
		log.Errorf("no module loader set")
		return fmt.Errorf("no module loader set")
	}

	// get basic module definition
	basicModule, err := mm.moduleLoader.LoadModule(moduleSource, modulePath)
	if err != nil {
		return fmt.Errorf("failed to get basic module's definition: %w", err)
	}
	moduleName := basicModule.GetName()

	// load and registry global hooks
	dep := &hooks.HookExecutionDependencyContainer{
		HookMetricsStorage: mm.dependencies.HookMetricStorage,
		KubeConfigManager:  mm.dependencies.KubeConfigManager,
		KubeObjectPatcher:  mm.dependencies.KubeObjectPatcher,
		MetricStorage:      mm.dependencies.MetricStorage,
		GlobalValuesGetter: mm.global,
	}

	basicModule.WithDependencies(dep)

	// check if module already exists
	if mm.modules.Has(moduleName) {
		// if module is disabled in module manager
		if !mm.IsModuleEnabled(moduleName) {
			// update(upsert) module config in moduleset
			mm.modules.Add(basicModule)
			// if module is disabled in the module kube config - exit
			if !mm.dependencies.KubeConfigManager.IsModuleEnabled(moduleName) {
				return nil
			}
			mm.AddEnabledModuleByConfigName(moduleName)

			// if the module kube config has enabled true, check enable script
			isEnabled, err := basicModule.RunEnabledScript(mm.TempDir, mm.GetEnabledModuleNames(), map[string]string{})
			if err != nil {
				return err
			}

			if isEnabled {
				ev := events.ModuleEvent{
					ModuleName: moduleName,
					EventType:  events.ModuleEnabled,
				}
				mm.SendModuleEvent(ev)

				err := mm.UpdateModuleKubeConfig(moduleName)
				if err != nil {
					return err
				}
				log.Infof("Push ConvergeModules task because %q Module was re-enabled", moduleName)
				mm.PushConvergeModulesTask(moduleName, "re-enabled")
			}
			return nil
		}
		// module is enabled, disable its hooks
		mm.DisableModuleHooks(moduleName)

		module := mm.GetModule(moduleName)
		// check for nil to prevent operator from panicking
		if module == nil {
			return fmt.Errorf("couldn't get %s module configuration", moduleName)
		}

		// deregister modules' hooks
		module.DeregisterHooks()

		// upsert a new module in the moduleset
		mm.modules.Add(basicModule)

		// check if module is enabled via enabled scripts
		isEnabled, err := basicModule.RunEnabledScript(mm.TempDir, mm.GetEnabledModuleNames(), map[string]string{})
		if err != nil {
			return err
		}

		if isEnabled {
			ev := events.ModuleEvent{
				ModuleName: moduleName,
				EventType:  events.ModuleEnabled,
			}
			mm.SendModuleEvent(ev)
			// enqueue module startup sequence if it is enabled
			mm.PushRunModuleTask(moduleName)
		} else {
			ev := events.ModuleEvent{
				ModuleName: moduleName,
				EventType:  events.ModuleDisabled,
			}
			mm.SendModuleEvent(ev)
			mm.PushDeleteModuleTask(moduleName)
			// modules is disabled - update modulemanager's state
			mm.DeleteEnabledModuleName(moduleName)
		}
		return nil
	}

	// module doesn't exist
	mm.modules.Add(basicModule)

	// a new module requires to be registered
	mm.SendModuleEvent(events.ModuleEvent{
		ModuleName: moduleName,
		EventType:  events.ModuleRegistered,
	})

	// if module is disabled in the module kube config - exit
	if !mm.dependencies.KubeConfigManager.IsModuleEnabled(moduleName) {
		return nil
	}
	mm.AddEnabledModuleByConfigName(moduleName)

	// if the module kube config has enabled true, check enable script
	isEnabled, err := basicModule.RunEnabledScript(mm.TempDir, mm.GetEnabledModuleNames(), map[string]string{})
	if err != nil {
		return err
	}

	if isEnabled {
		err := mm.UpdateModuleKubeConfig(moduleName)
		if err != nil {
			return err
		}
		log.Infof("Push ConvergeModules task because %q Module was enabled", moduleName)
		mm.PushConvergeModulesTask(moduleName, "registered-and-enabled")
	}
	return nil
}

// registerModules load all available modules from modules directory.
func (mm *ModuleManager) registerModules() error {
	if mm.ModulesDir == "" {
		log.Warnf("Empty modules directory is passed! No modules to load.")
		return nil
	}

	if mm.moduleLoader == nil {
		log.Errorf("no module loader set")
		return fmt.Errorf("no module loader set")
	}

	log.Debug("Search and register modules")

	mods, err := mm.moduleLoader.LoadModules()
	if err != nil {
		return fmt.Errorf("failed to load modules: %w", err)
	}

	set := &moduleset.ModulesSet{}

	// load and registry global hooks
	dep := &hooks.HookExecutionDependencyContainer{
		HookMetricsStorage: mm.dependencies.HookMetricStorage,
		KubeConfigManager:  mm.dependencies.KubeConfigManager,
		KubeObjectPatcher:  mm.dependencies.KubeObjectPatcher,
		MetricStorage:      mm.dependencies.MetricStorage,
		GlobalValuesGetter: mm.global,
	}

	for _, mod := range mods {
		if set.Has(mod.GetName()) {
			log.Warnf("module %q from path %q is not registered, because it has a duplicate", mod.GetName(), mod.GetPath())
			continue
		}

		mod.WithDependencies(dep)

		set.Add(mod)

		mm.SendModuleEvent(events.ModuleEvent{
			ModuleName: mod.GetName(),
			EventType:  events.ModuleRegistered,
		})
	}

	log.Debugf("Found modules: %v", set.NamesInOrder())

	mm.modules = set
	mm.modules.SetInited()

	return nil
}

// GetModuleEventsChannel returns a channel with events that occur during module processing
// events channel is created only if someone is reading it
func (mm *ModuleManager) GetModuleEventsChannel() chan events.ModuleEvent {
	if mm.moduleEventC == nil {
		mm.moduleEventC = make(chan events.ModuleEvent, 50)
	}

	return mm.moduleEventC
}

// ValidateModule this method is outdated, have to change it with module validation
// Deprecated: move it to module constructor
// TODO: rethink this
func (mm *ModuleManager) ValidateModule(mod *modules.BasicModule) error {
	valuesKey := utils.ModuleNameToValuesKey(mod.GetName())
	restoredName := utils.ModuleNameFromValuesKey(valuesKey)

	log.Infof("Validating module %q from %q", mod.GetName(), mod.GetPath())

	if mod.GetName() != restoredName {
		return fmt.Errorf("'%s' name should be in kebab-case and be restorable from camelCase: consider renaming to '%s'", mod.GetName(), restoredName)
	}

	// load static config from values.yaml
	staticValues, err := loadStaticValues(mod.GetName(), mod.GetPath())
	if err != nil {
		return fmt.Errorf("load values.yaml failed: %v", err)
	}

	if staticValues != nil {
		return fmt.Errorf("please use openapi schema instead of values.yaml")
	}

	valuesModuleName := utils.ModuleNameToValuesKey(mod.GetName())

	// Load validation schemas
	openAPIPath := filepath.Join(mod.GetPath(), "openapi")
	configBytes, valuesBytes, err := readOpenAPIFiles(openAPIPath)
	if err != nil {
		return fmt.Errorf("read openAPI schemas failed: %v", err)
	}

	err = mm.ValuesValidator.SchemaStorage.AddModuleValuesSchemas(
		valuesModuleName,
		configBytes,
		valuesBytes,
	)
	if err != nil {
		return fmt.Errorf("add schemas failed: %v", err)
	}

	return nil
}

// loadStaticValues loads config for module from values.yaml
// Module is enabled if values.yaml is not exists.
func loadStaticValues(moduleName, modulePath string) (utils.Values, error) {
	valuesYamlPath := filepath.Join(modulePath, utils.ValuesFileName)

	if _, err := os.Stat(valuesYamlPath); os.IsNotExist(err) {
		log.Debugf("module %s has no static values", moduleName)
		return nil, nil
	}

	return utils.LoadValuesFileFromDir(modulePath)
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

// queueHasPendingModuleDeleteTask returns true if queue has pending tasks
// with the type "ModuleDelete" related to the module "moduleName"
func queueHasPendingModuleDeleteTask(q *queue.TaskQueue, moduleName string) bool {
	if q == nil {
		return false
	}
	modules := modulesWithPendingTasks(q, task.ModuleDelete)
	meta, has := modules[moduleName]
	return has && meta.doStartup
}

func modulesWithPendingTasks(q *queue.TaskQueue, taskType sh_task.TaskType) map[string]struct{ doStartup bool } {
	if q == nil {
		return nil
	}

	modules := make(map[string]struct{ doStartup bool })

	skipFirstTask := true

	q.Iterate(func(t sh_task.Task) {
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
