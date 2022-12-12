package module_manager

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"
	log "github.com/sirupsen/logrus"

	. "github.com/flant/shell-operator/pkg/hook/binding_context"
	. "github.com/flant/shell-operator/pkg/hook/types"
	. "github.com/flant/shell-operator/pkg/kube_events_manager/types"

	// bindings constants and binding configs
	. "github.com/flant/addon-operator/pkg/hook/types"

	"github.com/flant/shell-operator/pkg/hook/controller"
	"github.com/flant/shell-operator/pkg/kube/object_patch"
	"github.com/flant/shell-operator/pkg/kube_events_manager"
	"github.com/flant/shell-operator/pkg/metric_storage"
	"github.com/flant/shell-operator/pkg/schedule_manager"
	utils_checksum "github.com/flant/shell-operator/pkg/utils/checksum"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/helm_resources_manager"
	"github.com/flant/addon-operator/pkg/kube_config_manager"
	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/addon-operator/pkg/values/validation"
)

// TODO separate modules and hooks storage, values storage and actions

type ModuleManager interface {
	Init() error
	Start()

	// Dependencies
	WithContext(ctx context.Context)
	WithDirectories(modulesDir string, globalHooksDir string, tempDir string) ModuleManager
	WithKubeEventManager(kube_events_manager.KubeEventsManager)
	WithKubeObjectPatcher(*object_patch.ObjectPatcher)
	WithScheduleManager(schedule_manager.ScheduleManager)
	WithKubeConfigManager(kubeConfigManager kube_config_manager.KubeConfigManager)
	WithHelm(factory *helm.ClientFactory)
	WithHelmResourcesManager(manager helm_resources_manager.HelmResourcesManager)
	WithMetricStorage(storage *metric_storage.MetricStorage)
	WithHookMetricStorage(storage *metric_storage.MetricStorage)

	GetGlobalHooksInOrder(bindingType BindingType) []string
	GetGlobalHooksNames() []string
	GetGlobalHook(name string) *GlobalHook

	GetModuleNames() []string
	GetEnabledModuleNames() []string
	IsModuleEnabled(moduleName string) bool
	GetModule(name string) *Module
	GetModuleHookNames(moduleName string) []string
	GetModuleHook(name string) *ModuleHook
	GetModuleHooksInOrder(moduleName string, bindingType BindingType) []string

	GlobalStaticAndConfigValues() utils.Values
	GlobalStaticAndNewValues(newValues utils.Values) utils.Values
	GlobalConfigValues() utils.Values
	GlobalValues() (utils.Values, error)
	GlobalValuesPatches() []utils.ValuesPatch
	UpdateGlobalConfigValues(configValues utils.Values)
	UpdateGlobalDynamicValuesPatches(valuesPatch utils.ValuesPatch)

	ModuleConfigValues(moduleName string) utils.Values
	ModuleDynamicValuesPatches(moduleName string) []utils.ValuesPatch
	UpdateModuleConfigValues(moduleName string, configValues utils.Values)
	UpdateModuleDynamicValuesPatches(moduleName string, valuesPatch utils.ValuesPatch)
	ApplyModuleDynamicValuesPatches(moduleName string, values utils.Values) (utils.Values, error)

	GetValuesValidator() *validation.ValuesValidator

	GetKubeConfigValid() bool
	SetKubeConfigValid(valid bool)

	// Methods to change module manager's state.
	RefreshStateFromHelmReleases(logLabels map[string]string) (*ModulesState, error)
	HandleNewKubeConfig(kubeConfig *kube_config_manager.KubeConfig) (*ModulesState, error)
	RefreshEnabledState(logLabels map[string]string) (*ModulesState, error)

	// Actions for tasks.
	DeleteModule(moduleName string, logLabels map[string]string) error
	RunModule(moduleName string, onStartup bool, logLabels map[string]string, afterStartupCb func() error) (bool, error)
	RunGlobalHook(hookName string, binding BindingType, bindingContext []BindingContext, logLabels map[string]string) (beforeChecksum string, afterChecksum string, err error)
	RunModuleHook(hookName string, binding BindingType, bindingContext []BindingContext, logLabels map[string]string) (beforeChecksum string, afterChecksum string, err error)

	RegisterModuleHooks(module *Module, logLabels map[string]string) error

	HandleKubeEvent(kubeEvent KubeEvent, createGlobalTaskFn func(*GlobalHook, controller.BindingExecutionInfo), createModuleTaskFn func(*Module, *ModuleHook, controller.BindingExecutionInfo))
	HandleGlobalEnableKubernetesBindings(hookName string, createTaskFn func(*GlobalHook, controller.BindingExecutionInfo)) error
	HandleModuleEnableKubernetesBindings(hookName string, createTaskFn func(*ModuleHook, controller.BindingExecutionInfo)) error
	EnableModuleScheduleBindings(moduleName string)
	DisableModuleHooks(moduleName string)
	HandleScheduleEvent(crontab string, createGlobalTaskFn func(*GlobalHook, controller.BindingExecutionInfo), createModuleTaskFn func(*Module, *ModuleHook, controller.BindingExecutionInfo)) error

	DynamicEnabledChecksum() string
	ApplyEnabledPatch(enabledPatch utils.ValuesPatch) error

	GlobalSynchronizationNeeded() bool
	GlobalSynchronizationState() *SynchronizationState
}

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

type moduleManager struct {
	ctx    context.Context
	cancel context.CancelFunc

	// Directories.
	ModulesDir     string
	GlobalHooksDir string
	TempDir        string

	// Dependencies.
	KubeObjectPatcher    *object_patch.ObjectPatcher
	kubeEventsManager    kube_events_manager.KubeEventsManager
	scheduleManager      schedule_manager.ScheduleManager
	kubeConfigManager    kube_config_manager.KubeConfigManager
	helm                 *helm.ClientFactory
	HelmResourcesManager helm_resources_manager.HelmResourcesManager
	metricStorage        *metric_storage.MetricStorage
	hookMetricStorage    *metric_storage.MetricStorage
	ValuesValidator      *validation.ValuesValidator

	// Index of all modules in modules directory. Key is module name.
	allModulesByName map[string]*Module

	// Ordered list of all modules names for ordered iterations of allModulesByName.
	allModulesNamesInOrder []string

	// TODO new layer of values for *Enabled values
	// commonStaticEnabledValues utils.Values // modules/values.yaml
	// kubeEnabledValues utils.Values // ConfigMap
	// dynamicEnabledValues utils.Values // patches from global hooks
	// scriptEnabledValues utils.Values // enabled script results

	// values::set "moduleNameEnabled" "\"true\""
	// module enable values from global hooks
	dynamicEnabled map[string]*bool

	// List of modules enabled by values.yaml or by kube config.
	// This list is changed on ConfigMap updates.
	enabledModulesByConfig map[string]struct{}

	// List of effectively enabled modules after running enabled scripts.
	enabledModules []string

	// Index of all global hooks. Key is global hook name
	globalHooksByName map[string]*GlobalHook
	// Index for searching global hooks by their bindings.
	globalHooksOrder map[BindingType][]*GlobalHook

	// module hooks by module name and binding type ordered by name
	// Note: one module hook can have several binding types.
	modulesHooksOrderByName map[string]map[BindingType][]*ModuleHook

	globalSynchronizationState *SynchronizationState

	// VALUE STORAGES

	// Values from modules/values.yaml file
	commonStaticValues utils.Values

	// A lock to synchronize access to *ConfigValues and *DynamicValuesPatches maps.
	valuesLayersLock sync.RWMutex

	// global values from ConfigMap
	kubeGlobalConfigValues utils.Values
	// module values from ConfigMap, only for enabled modules
	kubeModulesConfigValues map[string]utils.Values

	// addon-operator config is valid.
	kubeConfigValid bool
	// Static and config values are valid using OpenAPI schemas.
	kubeConfigValuesValid bool

	// Patches for dynamic global values
	globalDynamicValuesPatches []utils.ValuesPatch
	// Pathces for dynamic module values
	modulesDynamicValuesPatches map[string][]utils.ValuesPatch
}

var _ ModuleManager = &moduleManager{}

// NewModuleManager returns new MainModuleManager
func NewModuleManager() *moduleManager {
	return &moduleManager{
		ValuesValidator: validation.NewValuesValidator(),

		allModulesByName:            make(map[string]*Module),
		allModulesNamesInOrder:      make([]string, 0),
		enabledModulesByConfig:      make(map[string]struct{}),
		enabledModules:              make([]string, 0),
		dynamicEnabled:              make(map[string]*bool),
		globalHooksByName:           make(map[string]*GlobalHook),
		globalHooksOrder:            make(map[BindingType][]*GlobalHook),
		modulesHooksOrderByName:     make(map[string]map[BindingType][]*ModuleHook),
		commonStaticValues:          make(utils.Values),
		kubeGlobalConfigValues:      make(utils.Values),
		kubeModulesConfigValues:     make(map[string]utils.Values),
		globalDynamicValuesPatches:  make([]utils.ValuesPatch, 0),
		modulesDynamicValuesPatches: make(map[string][]utils.ValuesPatch),

		globalSynchronizationState: NewSynchronizationState(),
	}
}

func (mm *moduleManager) WithDirectories(modulesDir string, globalHooksDir string, tempDir string) ModuleManager {
	mm.ModulesDir = modulesDir
	mm.GlobalHooksDir = globalHooksDir
	mm.TempDir = tempDir
	return mm
}

func (mm *moduleManager) WithKubeConfigManager(kubeConfigManager kube_config_manager.KubeConfigManager) {
	mm.kubeConfigManager = kubeConfigManager
}

func (mm *moduleManager) WithHelm(helm *helm.ClientFactory) {
	mm.helm = helm
}

func (mm *moduleManager) WithKubeEventManager(mgr kube_events_manager.KubeEventsManager) {
	mm.kubeEventsManager = mgr
}

func (mm *moduleManager) WithKubeObjectPatcher(patcher *object_patch.ObjectPatcher) {
	mm.KubeObjectPatcher = patcher
}

func (mm *moduleManager) WithScheduleManager(mgr schedule_manager.ScheduleManager) {
	mm.scheduleManager = mgr
}

func (mm *moduleManager) WithHelmResourcesManager(manager helm_resources_manager.HelmResourcesManager) {
	mm.HelmResourcesManager = manager
}

func (mm *moduleManager) WithMetricStorage(storage *metric_storage.MetricStorage) {
	mm.metricStorage = storage
}

func (mm *moduleManager) WithHookMetricStorage(storage *metric_storage.MetricStorage) {
	mm.hookMetricStorage = storage
}

func (mm *moduleManager) WithContext(ctx context.Context) {
	mm.ctx, mm.cancel = context.WithCancel(ctx)
}

func (mm *moduleManager) Stop() {
	if mm.cancel != nil {
		mm.cancel()
	}
}

// runModulesEnabledScript runs enable script for each module from the list.
// Each 'enabled' script receives a list of previously enabled modules.
func (mm *moduleManager) runModulesEnabledScript(modules []string, logLabels map[string]string) ([]string, error) {
	enabled := make([]string, 0)

	for _, moduleName := range modules {
		module := mm.GetModule(moduleName)
		isEnabled, err := module.runEnabledScript(enabled, logLabels)
		if err != nil {
			return nil, err
		}

		if isEnabled {
			enabled = append(enabled, moduleName)
		}
	}

	return enabled, nil
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
func (mm *moduleManager) HandleNewKubeConfig(kubeConfig *kube_config_manager.KubeConfig) (*ModulesState, error) {
	var err error

	mm.warnAboutUnknownModules(kubeConfig)

	// Get map of enabled modules after ConfigMap changes.
	newEnabledByConfig := mm.calculateEnabledModulesByConfig(kubeConfig)

	// Check if values in new KubeConfig are valid. Return error to prevent poisoning caches with invalid values.
	err = mm.validateKubeConfig(kubeConfig, newEnabledByConfig)
	if err != nil {
		return nil, fmt.Errorf("config not valid: %v", err)
	}

	// Detect changes in global section.
	hasGlobalChange := false
	newGlobalValues := mm.kubeGlobalConfigValues
	if (kubeConfig == nil || kubeConfig.Global == nil) && mm.kubeGlobalConfigValues.HasGlobal() {
		hasGlobalChange = true
		newGlobalValues = make(utils.Values)
	}
	if kubeConfig != nil && kubeConfig.Global != nil {
		globalChecksum, err := mm.kubeGlobalConfigValues.Checksum()
		if err != nil {
			return nil, err
		}

		if kubeConfig.Global.Checksum != globalChecksum {
			hasGlobalChange = true
		}
		newGlobalValues = kubeConfig.Global.Values
	}

	// Full reload if enabled flags are changed.
	isEnabledChanged := false
	for moduleName := range mm.allModulesByName {
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
	if !isEnabledChanged {
		// enabledModules is a subset of enabledModulesByConfig.
		// Module can be enabled by config, but disabled with enabled script.
		// So check only sections for effectively enabled modules.
		for _, moduleName := range mm.enabledModules {
			modValues, hasConfigValues := mm.kubeModulesConfigValues[moduleName]
			// New module state from ConfigMap.
			hasNewKubeConfig := false
			var newModConfig *kube_config_manager.ModuleKubeConfig
			if kubeConfig != nil {
				newModConfig, hasNewKubeConfig = kubeConfig.Modules[moduleName]
			}

			// Section added or disappeared from ConfigMap, values changed.
			if hasConfigValues != hasNewKubeConfig {
				modulesChanged = append(modulesChanged, moduleName)
				continue
			}

			// Compare checksums for new and saved values.
			if hasConfigValues && hasNewKubeConfig {
				modValuesChecksum, err := modValues.Checksum()
				if err != nil {
					return nil, err
				}
				newModValuesChecksum, err := newModConfig.Values.Checksum()
				if err != nil {
					return nil, err
				}
				if modValuesChecksum != newModValuesChecksum {
					modulesChanged = append(modulesChanged, moduleName)
				}
			}
		}
	}

	// Create new map with config values.
	newKubeModuleConfigValues := make(map[string]utils.Values)
	if kubeConfig != nil {
		for moduleName, moduleConfig := range kubeConfig.Modules {
			newKubeModuleConfigValues[moduleName] = moduleConfig.Values
		}
	}

	// Update caches from ConfigMap content.
	mm.valuesLayersLock.Lock()
	mm.enabledModulesByConfig = newEnabledByConfig
	mm.kubeGlobalConfigValues = newGlobalValues
	mm.kubeModulesConfigValues = newKubeModuleConfigValues
	mm.valuesLayersLock.Unlock()

	// Return empty state on global change.
	if hasGlobalChange || isEnabledChanged {
		return &ModulesState{}, nil
	}

	// Return list of changed modules when only values are changed.
	if len(modulesChanged) > 0 {
		return &ModulesState{
			AllEnabledModules: mm.enabledModules,
			ModulesToReload:   modulesChanged,
		}, nil
	}

	// Return nil if cached state is not changed by ConfigMap.
	return nil, nil
}

// warnAboutUnknownModules prints to log all unknown module section names.
func (mm *moduleManager) warnAboutUnknownModules(kubeConfig *kube_config_manager.KubeConfig) {
	// Ignore empty kube config.
	if kubeConfig == nil {
		return
	}

	unknownNames := make([]string, 0)
	for moduleName := range kubeConfig.Modules {
		if _, isKnown := mm.allModulesByName[moduleName]; !isKnown {
			unknownNames = append(unknownNames, moduleName)
		}
	}
	if len(unknownNames) > 0 {
		log.Warnf("ConfigMap/%s has values for unknown modules: %+v", app.ConfigMapName, unknownNames)
	}
}

// calculateEnabledModulesByConfig determine enable state for all modules
// by checking *Enabled fields in values.yaml, ConfigMap and dynamicEnable map.
// Method returns list of enabled modules.
//
// Module is enabled by config if module section in ConfigMap is a map or an array
// or ConfigMap has no module section and module has a map or an array in values.yaml
func (mm *moduleManager) calculateEnabledModulesByConfig(config *kube_config_manager.KubeConfig) map[string]struct{} {
	enabledByConfig := make(map[string]struct{})

	for moduleName, module := range mm.allModulesByName {
		var kubeConfigEnabled *bool
		var kubeConfigEnabledStr string
		if config != nil {
			if kubeConfig, hasKubeConfig := config.Modules[moduleName]; hasKubeConfig {
				kubeConfigEnabled = kubeConfig.IsEnabled
				kubeConfigEnabledStr = kubeConfig.GetEnabled()
			}
		}

		isEnabled := mergeEnabled(
			module.CommonStaticConfig.IsEnabled,
			module.StaticConfig.IsEnabled,
			kubeConfigEnabled,
		)

		if isEnabled {
			enabledByConfig[moduleName] = struct{}{}
		}

		log.Debugf("enabledByConfig: module '%s' enabled flags: common '%v', static '%v', kubeConfig '%v', result: '%v'",
			module.Name,
			module.CommonStaticConfig.GetEnabled(),
			module.StaticConfig.GetEnabled(),
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
func (mm *moduleManager) calculateEnabledModulesWithDynamic(enabledByConfig map[string]struct{}) []string {
	log.Debugf("calculateEnabled: dynamicEnabled is %s", mm.DumpDynamicEnabled())

	enabled := make([]string, 0)
	for _, moduleName := range mm.allModulesNamesInOrder {
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
func (mm *moduleManager) Init() error {
	log.Debug("Init ModuleManager")

	if err := mm.RegisterGlobalHooks(); err != nil {
		return err
	}

	if err := mm.RegisterModules(); err != nil {
		return err
	}

	return nil
}

// validateKubeConfig checks validity of all sections in ConfigMap with OpenAPI schemas.
func (mm *moduleManager) validateKubeConfig(kubeConfig *kube_config_manager.KubeConfig, enabledModules map[string]struct{}) error {
	// Ignore empty kube config.
	if kubeConfig == nil {
		mm.SetKubeConfigValuesValid(true)
		return nil
	}
	// Validate values in global section merged with static values.
	var validationErr error
	if kubeConfig.Global != nil {
		err := mm.ValuesValidator.ValidateGlobalConfigValues(mm.GlobalStaticAndNewValues(kubeConfig.Global.Values))
		if err != nil {
			validationErr = multierror.Append(
				validationErr,
				fmt.Errorf("'global' section in ConfigMap/%s is not valid", app.ConfigMapName),
				err,
			)
		}
	}

	// Validate config values for enabled modules.
	for moduleName := range enabledModules {
		modCfg, has := kubeConfig.Modules[moduleName]
		if !has {
			continue
		}
		mod := mm.GetModule(moduleName)
		moduleErr := mm.ValuesValidator.ValidateModuleConfigValues(mod.ValuesKey(), mod.StaticAndNewValues(modCfg.Values))
		if moduleErr != nil {
			validationErr = multierror.Append(
				validationErr,
				fmt.Errorf("'%s' module section in ConfigMap/%s is not valid", mod.ValuesKey(), app.ConfigMapName),
				moduleErr,
			)
		}
	}

	// Set valid flag to false if there is validation error
	mm.SetKubeConfigValuesValid(validationErr == nil)

	return validationErr
}

func (mm *moduleManager) GetKubeConfigValid() bool {
	return mm.kubeConfigValid
}

func (mm *moduleManager) SetKubeConfigValid(valid bool) {
	mm.kubeConfigValid = valid
}

func (mm *moduleManager) SetKubeConfigValuesValid(valid bool) {
	mm.kubeConfigValuesValid = valid
}

// checkConfig increases config_values_errors_total metric when kubeConfig becomes invalid.
func (mm *moduleManager) checkConfig() {
	for {
		if mm.ctx.Err() != nil {
			return
		}
		if !mm.kubeConfigValid || !mm.kubeConfigValuesValid {
			mm.metricStorage.CounterAdd("{PREFIX}config_values_errors_total", 1.0, map[string]string{})
		}
		time.Sleep(5 * time.Second)
	}
}

// Start runs service go routine.
func (mm *moduleManager) Start() {
	// Start checking kubeConfigIsValid flag in go routine.
	go mm.checkConfig()
}

// RefreshStateFromHelmReleases retrieves all Helm releases. It treats releases for known modules as
// an initial list of enabled modules.
// Run this method once at startup.
func (mm *moduleManager) RefreshStateFromHelmReleases(logLabels map[string]string) (*ModulesState, error) {
	if mm.helm == nil {
		return &ModulesState{}, nil
	}
	releasedModules, err := mm.helm.NewClient(logLabels).ListReleasesNames(nil)
	if err != nil {
		return nil, err
	}

	state := mm.stateFromHelmReleases(releasedModules)

	// Initiate enabled modules list.
	mm.enabledModules = state.AllEnabledModules

	return state, nil
}

// stateFromHelmReleases calculates enabled modules and modules to purge from Helm releases.
func (mm *moduleManager) stateFromHelmReleases(releases []string) *ModulesState {
	releasesMap := utils.ListToMapStringStruct(releases)

	// Filter out known modules.
	enabledModules := make([]string, 0)
	for _, modName := range mm.allModulesNamesInOrder {
		// Remove known module to detect unknown ones.
		if _, has := releasesMap[modName]; has {
			// Treat known module as enabled module.
			enabledModules = append(enabledModules, modName)
		}
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
	return &ModulesState{
		AllEnabledModules: enabledModules,
		ModulesToPurge:    purge,
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
func (mm *moduleManager) RefreshEnabledState(logLabels map[string]string) (*ModulesState, error) {
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
	newlyEnabledModules := utils.ListSubtract(enabledModules, mm.enabledModules)

	// Disabled modules are that present in the list of currently enabled modules
	// but not present in the list after running enabled scripts
	disabledModules := utils.ListSubtract(mm.enabledModules, enabledModules)
	disabledModules = utils.SortReverseByReference(disabledModules, mm.allModulesNamesInOrder)

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
	mm.enabledModules = enabledModules

	// Return lists for ConvergeModules task.
	return &ModulesState{
		AllEnabledModules: mm.enabledModules,
		ModulesToDisable:  disabledModules,
		ModulesToEnable:   newlyEnabledModules,
	}, nil
}

func (mm *moduleManager) GetModule(name string) *Module {
	module, exist := mm.allModulesByName[name]
	if exist {
		return module
	} else {
		log.Errorf("Possible bug!!! GetModule: no module '%s' in ModuleManager indexes", name)
		return nil
	}
}

func (mm *moduleManager) GetModuleNames() []string {
	return mm.allModulesNamesInOrder
}

func (mm *moduleManager) GetEnabledModuleNames() []string {
	return mm.enabledModules
}

func (mm *moduleManager) IsModuleEnabled(moduleName string) bool {
	for _, modName := range mm.enabledModules {
		if modName == moduleName {
			return true
		}
	}
	return false
}

func (mm *moduleManager) GetGlobalHook(name string) *GlobalHook {
	globalHook, exist := mm.globalHooksByName[name]
	if exist {
		return globalHook
	} else {
		log.Errorf("Possible bug!!! GetGlobalHook: no global hook '%s' in ModuleManager indexes", name)
		return nil
	}
}

func (mm *moduleManager) GetModuleHook(name string) *ModuleHook {
	for _, bindingHooks := range mm.modulesHooksOrderByName {
		for _, hooks := range bindingHooks {
			for _, hook := range hooks {
				if hook.Name == name {
					return hook
				}
			}
		}
	}
	log.Errorf("Possible bug!!! GetModuleHook: no module hook '%s' in ModuleManager indexes", name)
	return nil
}

func (mm *moduleManager) GetGlobalHooksNames() []string {
	names := make([]string, 0)

	for name := range mm.globalHooksByName {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

func (mm *moduleManager) GetGlobalHooksInOrder(bindingType BindingType) []string {
	globalHooks, ok := mm.globalHooksOrder[bindingType]
	if !ok {
		return []string{}
	}

	sort.Slice(globalHooks[:], func(i, j int) bool {
		return globalHooks[i].Order(bindingType) < globalHooks[j].Order(bindingType)
	})

	var globalHooksNames []string
	for _, globalHook := range globalHooks {
		globalHooksNames = append(globalHooksNames, globalHook.Name)
	}

	return globalHooksNames
}

func (mm *moduleManager) GetModuleHooksInOrder(moduleName string, bindingType BindingType) []string {

	moduleHooksByBinding, ok := mm.modulesHooksOrderByName[moduleName]
	if !ok {
		return []string{}
	}

	moduleBindingHooks, ok := moduleHooksByBinding[bindingType]
	if !ok {
		return []string{}
	}

	sort.Slice(moduleBindingHooks[:], func(i, j int) bool {
		return moduleBindingHooks[i].Order(bindingType) < moduleBindingHooks[j].Order(bindingType)
	})

	var moduleHooksNames []string
	for _, moduleHook := range moduleBindingHooks {
		moduleHooksNames = append(moduleHooksNames, moduleHook.Name)
	}

	return moduleHooksNames
}

func (mm *moduleManager) GetModuleHookNames(moduleName string) []string {
	moduleHooksByBinding, ok := mm.modulesHooksOrderByName[moduleName]
	if !ok {
		return []string{}
	}

	moduleHookNamesMap := make(map[string]struct{})
	for _, moduleHooks := range moduleHooksByBinding {
		for _, moduleHook := range moduleHooks {
			moduleHookNamesMap[moduleHook.Name] = struct{}{}
		}
	}

	return utils.MapStringStructKeys(moduleHookNamesMap)
}

// TODO: moduleManager.GetModule(modName).Delete()
func (mm *moduleManager) DeleteModule(moduleName string, logLabels map[string]string) error {
	module := mm.GetModule(moduleName)

	// Stop kubernetes informers and remove scheduled functions
	mm.DisableModuleHooks(moduleName)

	if err := module.Delete(logLabels); err != nil {
		return err
	}

	// Unregister module hooks.
	delete(mm.modulesHooksOrderByName, moduleName)

	return nil
}

// RunModule runs beforeHelm hook, helm upgrade --install and afterHelm or afterDeleteHelm hook
func (mm *moduleManager) RunModule(moduleName string, onStartup bool, logLabels map[string]string, afterStartupCb func() error) (bool, error) {
	module := mm.GetModule(moduleName)

	// Do not send to mm.moduleValuesChanged, changed values are handled by TaskHandler.
	return module.Run(logLabels)
}

func (mm *moduleManager) RunGlobalHook(hookName string, binding BindingType, bindingContext []BindingContext, logLabels map[string]string) (string, string, error) {
	globalHook := mm.GetGlobalHook(hookName)

	beforeValues, err := mm.GlobalValues()
	if err != nil {
		return "", "", err
	}
	beforeChecksum, err := beforeValues.Checksum()
	if err != nil {
		return "", "", err
	}

	// Update kubernetes snapshots just before execute a hook
	if binding == OnKubernetesEvent || binding == Schedule {
		bindingContext = globalHook.HookController.UpdateSnapshots(bindingContext)
	}

	if binding == BeforeAll || binding == AfterAll {
		snapshots := globalHook.HookController.KubernetesSnapshots()
		newBindingContext := []BindingContext{}
		for _, context := range bindingContext {
			context.Snapshots = snapshots
			context.Metadata.IncludeAllSnapshots = true
			newBindingContext = append(newBindingContext, context)
		}
		bindingContext = newBindingContext
	}

	if err := globalHook.Run(binding, bindingContext, logLabels); err != nil {
		return "", "", err
	}

	afterValues, err := mm.GlobalValues()
	if err != nil {
		return "", "", err
	}
	afterChecksum, err := afterValues.Checksum()
	if err != nil {
		return "", "", err
	}

	return beforeChecksum, afterChecksum, nil
}

func (mm *moduleManager) RunModuleHook(hookName string, binding BindingType, bindingContext []BindingContext, logLabels map[string]string) (beforeChecksum string, afterChecksum string, err error) {
	moduleHook := mm.GetModuleHook(hookName)

	values, err := moduleHook.Module.Values()
	if err != nil {
		return "", "", err
	}
	valuesChecksum, err := values.Checksum()
	if err != nil {
		return "", "", err
	}

	// Update kubernetes snapshots just before execute a hook
	// Note: BeforeHelm and AfterHelm are run by runHookByBinding
	if binding == OnKubernetesEvent || binding == Schedule {
		bindingContext = moduleHook.HookController.UpdateSnapshots(bindingContext)
	}

	metricLabels := map[string]string{
		"module":     moduleHook.Module.Name,
		"hook":       moduleHook.Name,
		"binding":    string(binding),
		"queue":      logLabels["queue"],
		"activation": logLabels["event.type"],
	}

	if err := moduleHook.Run(binding, bindingContext, logLabels, metricLabels); err != nil {
		return "", "", err
	}

	newValues, err := moduleHook.Module.Values()
	if err != nil {
		return "", "", err
	}
	newValuesChecksum, err := newValues.Checksum()
	if err != nil {
		return "", "", err
	}

	return valuesChecksum, newValuesChecksum, nil
}

// GlobalConfigValues return raw global values only from a ConfigMap.
func (mm *moduleManager) GlobalConfigValues() utils.Values {
	return MergeLayers(
		// Init global section.
		utils.Values{"global": map[string]interface{}{}},
		// Merge overrides from ConfigMap.
		mm.kubeGlobalConfigValues,
	)
}

// GlobalStaticAndConfigValues return global values defined in
// various values.yaml files and in a ConfigMap
func (mm *moduleManager) GlobalStaticAndConfigValues() utils.Values {
	return MergeLayers(
		// Init global section.
		utils.Values{"global": map[string]interface{}{}},
		// Merge static values from modules/values.yaml.
		mm.commonStaticValues.Global(),
		// Apply config values defaults before ConfigMap overrides.
		&ApplyDefaultsForGlobal{validation.ConfigValuesSchema, mm.ValuesValidator},
		// Merge overrides from ConfigMap.
		mm.kubeGlobalConfigValues,
	)
}

// GlobalStaticAndNewValues return global values defined in
// various values.yaml files merged with newValues
func (mm *moduleManager) GlobalStaticAndNewValues(newValues utils.Values) utils.Values {
	return MergeLayers(
		// Init global section.
		utils.Values{"global": map[string]interface{}{}},
		// Merge static values from modules/values.yaml.
		mm.commonStaticValues.Global(),
		// Apply config values defaults before overrides.
		&ApplyDefaultsForGlobal{validation.ConfigValuesSchema, mm.ValuesValidator},
		// Merge overrides from newValues.
		newValues,
	)
}

// GlobalValues return current global values with applied patches
func (mm *moduleManager) GlobalValues() (utils.Values, error) {
	var err error

	res := MergeLayers(
		// Init global section.
		utils.Values{"global": map[string]interface{}{}},
		// Merge static values from modules/values.yaml.
		mm.commonStaticValues.Global(),
		// Apply config values defaults before ConfigMap overrides.
		&ApplyDefaultsForGlobal{validation.ConfigValuesSchema, mm.ValuesValidator},
		// Merge overrides from ConfigMap.
		mm.kubeGlobalConfigValues,
		// Apply dynamic values defaults before patches.
		&ApplyDefaultsForGlobal{validation.ValuesSchema, mm.ValuesValidator},
	)

	// Invariant: do not store patches that does not apply
	// Give user error for patches early, after patch receive
	for _, patch := range mm.globalDynamicValuesPatches {
		res, _, err = utils.ApplyValuesPatch(res, patch, utils.IgnoreNonExistentPaths)
		if err != nil {
			return nil, fmt.Errorf("apply global patch error: %s", err)
		}
	}

	return res, nil
}

// GlobalValues return patches for global values
func (mm *moduleManager) GlobalValuesPatches() []utils.ValuesPatch {
	return mm.globalDynamicValuesPatches
}

// UpdateGlobalConfigValues sets updated global config values.
func (mm *moduleManager) UpdateGlobalConfigValues(configValues utils.Values) {
	mm.valuesLayersLock.Lock()
	defer mm.valuesLayersLock.Unlock()

	mm.kubeGlobalConfigValues = configValues
}

// UpdateGlobalDynamicValuesPatches appends patches for global dynamic values.
func (mm *moduleManager) UpdateGlobalDynamicValuesPatches(valuesPatch utils.ValuesPatch) {
	mm.valuesLayersLock.Lock()
	defer mm.valuesLayersLock.Unlock()

	mm.globalDynamicValuesPatches = utils.AppendValuesPatch(
		mm.globalDynamicValuesPatches,
		valuesPatch)
}

// ModuleConfigValues returns config values for module.
func (mm *moduleManager) ModuleConfigValues(moduleName string) utils.Values {
	mm.valuesLayersLock.RLock()
	defer mm.valuesLayersLock.RUnlock()
	return mm.kubeModulesConfigValues[moduleName]
}

// ModuleConfigValues returns all patches for dynamic values.
func (mm *moduleManager) ModuleDynamicValuesPatches(moduleName string) []utils.ValuesPatch {
	mm.valuesLayersLock.RLock()
	defer mm.valuesLayersLock.RUnlock()
	return mm.modulesDynamicValuesPatches[moduleName]
}

// UpdateModuleConfigValues sets updated config values for module.
func (mm *moduleManager) UpdateModuleConfigValues(moduleName string, configValues utils.Values) {
	mm.valuesLayersLock.Lock()
	defer mm.valuesLayersLock.Unlock()
	mm.kubeModulesConfigValues[moduleName] = configValues
}

// UpdateModuleDynamicValuesPatches appends patches for dynamic values for module.
func (mm *moduleManager) UpdateModuleDynamicValuesPatches(moduleName string, valuesPatch utils.ValuesPatch) {
	mm.valuesLayersLock.Lock()
	defer mm.valuesLayersLock.Unlock()

	mm.modulesDynamicValuesPatches[moduleName] = utils.AppendValuesPatch(
		mm.modulesDynamicValuesPatches[moduleName],
		valuesPatch)
}

func (mm *moduleManager) ApplyModuleDynamicValuesPatches(moduleName string, values utils.Values) (utils.Values, error) {
	mm.valuesLayersLock.RLock()
	defer mm.valuesLayersLock.RUnlock()

	var err error
	res := values
	for _, patch := range mm.modulesDynamicValuesPatches[moduleName] {
		// Invariant: do not store patches that does not apply
		// Give user error for patches early, after patch receive
		res, _, err = utils.ApplyValuesPatch(res, patch, utils.IgnoreNonExistentPaths)
		if err != nil {
			return nil, err
		}
	}

	return res, nil
}

func (mm *moduleManager) GetValuesValidator() *validation.ValuesValidator {
	return mm.ValuesValidator
}

func (mm *moduleManager) HandleKubeEvent(kubeEvent KubeEvent, createGlobalTaskFn func(*GlobalHook, controller.BindingExecutionInfo), createModuleTaskFn func(*Module, *ModuleHook, controller.BindingExecutionInfo)) {
	mm.LoopByBinding(OnKubernetesEvent, func(gh *GlobalHook, m *Module, mh *ModuleHook) {
		if gh != nil {
			if gh.HookController.CanHandleKubeEvent(kubeEvent) {
				gh.HookController.HandleKubeEvent(kubeEvent, func(info controller.BindingExecutionInfo) {
					if createGlobalTaskFn != nil {
						createGlobalTaskFn(gh, info)
					}
				})
			}
		} else {
			if mh.HookController.CanHandleKubeEvent(kubeEvent) {
				mh.HookController.HandleKubeEvent(kubeEvent, func(info controller.BindingExecutionInfo) {
					if createModuleTaskFn != nil {
						createModuleTaskFn(m, mh, info)
					}
				})
			}
		}
	})
}

func (mm *moduleManager) HandleGlobalEnableKubernetesBindings(hookName string, createTaskFn func(*GlobalHook, controller.BindingExecutionInfo)) error {
	gh := mm.GetGlobalHook(hookName)

	err := gh.HookController.HandleEnableKubernetesBindings(func(info controller.BindingExecutionInfo) {
		if createTaskFn != nil {
			createTaskFn(gh, info)
		}
	})
	if err != nil {
		return err
	}

	return nil
}

func (mm *moduleManager) HandleModuleEnableKubernetesBindings(moduleName string, createTaskFn func(*ModuleHook, controller.BindingExecutionInfo)) error {
	kubeHooks := mm.GetModuleHooksInOrder(moduleName, OnKubernetesEvent)

	for _, hookName := range kubeHooks {
		mh := mm.GetModuleHook(hookName)
		err := mh.HookController.HandleEnableKubernetesBindings(func(info controller.BindingExecutionInfo) {
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

func (mm *moduleManager) EnableModuleScheduleBindings(moduleName string) {
	schHooks := mm.GetModuleHooksInOrder(moduleName, Schedule)
	for _, hookName := range schHooks {
		mh := mm.GetModuleHook(hookName)
		mh.HookController.EnableScheduleBindings()
	}
}

func (mm *moduleManager) DisableModuleHooks(moduleName string) {
	kubeHooks := mm.GetModuleHooksInOrder(moduleName, OnKubernetesEvent)

	for _, hookName := range kubeHooks {
		mh := mm.GetModuleHook(hookName)
		mh.HookController.StopMonitors()
	}

	schHooks := mm.GetModuleHooksInOrder(moduleName, Schedule)
	for _, hookName := range schHooks {
		mh := mm.GetModuleHook(hookName)
		mh.HookController.DisableScheduleBindings()
	}
}

func (mm *moduleManager) HandleScheduleEvent(crontab string, createGlobalTaskFn func(*GlobalHook, controller.BindingExecutionInfo), createModuleTaskFn func(*Module, *ModuleHook, controller.BindingExecutionInfo)) error {
	mm.LoopByBinding(Schedule, func(gh *GlobalHook, m *Module, mh *ModuleHook) {
		if gh != nil {
			if gh.HookController.CanHandleScheduleEvent(crontab) {
				gh.HookController.HandleScheduleEvent(crontab, func(info controller.BindingExecutionInfo) {
					if createGlobalTaskFn != nil {
						createGlobalTaskFn(gh, info)
					}
				})
			}
		} else {
			if mh.HookController.CanHandleScheduleEvent(crontab) {
				mh.HookController.HandleScheduleEvent(crontab, func(info controller.BindingExecutionInfo) {
					if createModuleTaskFn != nil {
						createModuleTaskFn(m, mh, info)
					}
				})
			}
		}
	})

	return nil
}

func (mm *moduleManager) LoopByBinding(binding BindingType, fn func(gh *GlobalHook, m *Module, mh *ModuleHook)) {
	globalHooks := mm.GetGlobalHooksInOrder(binding)

	for _, hookName := range globalHooks {
		gh := mm.GetGlobalHook(hookName)
		fn(gh, nil, nil)
	}

	for _, moduleName := range mm.enabledModules {
		m := mm.GetModule(moduleName)
		moduleHooks := mm.GetModuleHooksInOrder(moduleName, binding)
		for _, hookName := range moduleHooks {
			mh := mm.GetModuleHook(hookName)
			fn(nil, m, mh)
		}
	}
}

// ApplyEnabledPatch changes "dynamicEnabled" map with patches.
func (mm *moduleManager) ApplyEnabledPatch(enabledPatch utils.ValuesPatch) error {
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
				return fmt.Errorf("apply enabled patch operation '%s' for %s: ", op.Op, op.Path)
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
func (mm *moduleManager) DynamicEnabledChecksum() string {
	jsonBytes, err := json.Marshal(mm.dynamicEnabled)
	if err != nil {
		log.Errorf("dynamicEnabled checksum calculate from '%s': %v", mm.DumpDynamicEnabled(), err)
	}
	return utils_checksum.CalculateChecksum(string(jsonBytes))
}

func (mm *moduleManager) DumpDynamicEnabled() string {
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
func (mm *moduleManager) GlobalSynchronizationNeeded() bool {
	for _, ghName := range mm.GetGlobalHooksInOrder(OnKubernetesEvent) {
		gHook := mm.GetGlobalHook(ghName)
		if gHook.SynchronizationNeeded() {
			return true
		}
	}
	return false
}

func (mm *moduleManager) GlobalSynchronizationState() *SynchronizationState {
	return mm.globalSynchronizationState
}

func (mm *moduleManager) ApplyBindingActions(moduleHook *ModuleHook, bindingActions []go_hook.BindingAction) error {
	for _, action := range bindingActions {
		bindingIdx := -1
		for i, binding := range moduleHook.Config.OnKubernetesEvents {
			if binding.BindingName == action.Name {
				bindingIdx = i
			}
		}
		if bindingIdx == -1 {
			continue
		}

		monitorCfg := moduleHook.Config.OnKubernetesEvents[bindingIdx].Monitor
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
		err := moduleHook.HookController.UpdateMonitor(monitorCfg.Metadata.MonitorId, action.Kind, action.ApiVersion)
		if err != nil {
			return err
		}
	}
	return nil
}

// mergeEnabled merges enabled flags. Enabled flag can be nil.
//
// If all flags are nil, then false is returned — module is disabled by default.
func mergeEnabled(enabledFlags ...*bool) bool {
	result := false
	for _, enabled := range enabledFlags {
		if enabled == nil {
			continue
		} else {
			result = *enabled
		}
	}

	return result
}
