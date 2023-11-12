package module_manager

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/trace"
	"strings"
	"time"

	"sigs.k8s.io/yaml"

	"github.com/flant/addon-operator/pkg/module_manager/loader"
	"github.com/flant/addon-operator/pkg/module_manager/loader/fs"

	"github.com/flant/addon-operator/pkg/module_manager/models/moduleset"

	hooks2 "github.com/flant/addon-operator/pkg/module_manager/models/hooks"
	"github.com/flant/addon-operator/pkg/module_manager/models/modules"

	// bindings constants and binding configs
	"github.com/hashicorp/go-multierror"
	log "github.com/sirupsen/logrus"

	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/helm_resources_manager"
	. "github.com/flant/addon-operator/pkg/hook/types"
	"github.com/flant/addon-operator/pkg/kube_config_manager/config"
	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
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
	//globalHooksByName map[string]hooks2.GlobalHook
	// Index for searching global hooks by their bindings.
	//globalHooksOrder map[BindingType][]hooks2.GlobalHook

	// module hooks by module name and binding type ordered by name
	// Note: one module hook can have several binding types.
	//modulesHooksOrderByName map[string]map[BindingType][]*ModuleHook

	globalSynchronizationState *modules.SynchronizationState

	// VALUE STORAGES

	// Values from modules/values.yaml file
	//commonStaticValues utils.Values

	// A lock to synchronize access to *ConfigValues and *DynamicValuesPatches maps.
	//valuesLayersLock sync.RWMutex

	// global values from ConfigMap
	//kubeGlobalConfigValues utils.Values
	// module values from ConfigMap, only for enabled modules
	//kubeModulesConfigValues map[string]utils.Values

	// addon-operator config is valid.
	kubeConfigValid bool
	// Static and config values are valid using OpenAPI schemas.
	kubeConfigValuesValid bool

	// Patches for dynamic global values
	//globalDynamicValuesPatches []utils.ValuesPatch
	// Patches for dynamic module values
	//modulesDynamicValuesPatches map[string][]utils.ValuesPatch
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
		enabledModules:         make([]string, 0),
		dynamicEnabled:         make(map[string]*bool),
		//globalHooksByName:      make(map[string]*hooks2.GlobalHook),
		//globalHooksOrder:       make(map[BindingType][]*hooks2.GlobalHook),
		//modulesHooksOrderByName:     make(map[string]map[BindingType][]*ModuleHook),
		//commonStaticValues: make(utils.Values),
		//kubeGlobalConfigValues:      make(utils.Values),
		//kubeModulesConfigValues:     make(map[string]utils.Values),
		//globalDynamicValuesPatches:  make([]utils.ValuesPatch, 0),
		//modulesDynamicValuesPatches: make(map[string][]utils.ValuesPatch),

		globalSynchronizationState: modules.NewSynchronizationState(),
	}
}

func (mm *ModuleManager) Stop() {
	if mm.cancel != nil {
		mm.cancel()
	}
}

//// SetupModuleProducer registers foreign Module producer, which provides Module for storing in a cluster
//func (mm *ModuleManager) SetupModuleProducer(producer ModuleProducer) {
//	mm.moduleProducer = producer
//}

// GetDependencies fetch dependencies struct from ModuleManager
// note: not the best way but it's required in some hooks
func (mm *ModuleManager) GetDependencies() *ModuleManagerDependencies {
	return mm.dependencies
}

//// GetKubeConfigValues returns config values (from KubeConfigManager) for desired moduleName
//func (mm *ModuleManager) GetKubeConfigValues(moduleName string) utils.Values {
//	if moduleName == utils.GlobalValuesKey {
//		return mm.kubeGlobalConfigValues
//	}
//	return mm.kubeModulesConfigValues[moduleName]
//}

// runModulesEnabledScript runs enable script for each module from the list.
// Each 'enabled' script receives a list of previously enabled modules.
func (mm *ModuleManager) runModulesEnabledScript(modules []string, logLabels map[string]string) ([]string, error) {
	enabled := make([]string, 0)

	for _, moduleName := range modules {
		ml := mm.GetModule(moduleName)
		baseModule := ml.GetBaseModule()
		isEnabled, err := baseModule.RunEnabledScript(mm.TempDir, enabled, logLabels)
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
	validationErrors := &multierror.Error{}

	if kubeConfig == nil {
		// have no idea, how it could be, just skip run
		log.Warnf("No KubeConfig is set")
		return &ModulesState{}, nil
	}

	mm.warnAboutUnknownModules(kubeConfig)

	// Get map of enabled modules after KubeConfig changes.
	newEnabledByConfig := mm.calculateEnabledModulesByConfig(kubeConfig)
	fmt.Printf("NEW ENABLED BY CONFIG: \n%v", newEnabledByConfig)

	// Check if global config values are valid
	globalModule := mm.global
	validationErr := globalModule.ValidateAndSaveConfigValues(kubeConfig.Global.GetValues())
	if validationErr != nil {
		if e := multierror.Append(validationErrors, validationErr); e != nil {
			return &ModulesState{}, e
		}
	}

	// Check if enabledModules are valid
	for moduleName := range newEnabledByConfig {
		modCfg, has := kubeConfig.Modules[moduleName]
		if !has {
			continue
		}
		mod := mm.GetModule(moduleName)

		validationErr = mod.GetBaseModule().ValidateAndSaveConfigValues(modCfg.GetValues())
		if validationErr != nil {
			if e := multierror.Append(validationErrors, validationErr); e != nil {
				return &ModulesState{}, e
			}
		}
	}

	if validationErrors.Len() > 0 {
		mm.SetKubeConfigValuesValid(false)
		return &ModulesState{}, validationErrors.ErrorOrNil()
	}

	//// Check if values in new KubeConfig are valid. Return error to prevent poisoning caches with invalid values.
	//err = mm.validateKubeConfig(kubeConfig, newEnabledByConfig)
	//if err != nil {
	//	return nil, fmt.Errorf("config not valid: %v", err)
	//}

	// Detect changes in global section.
	hasGlobalChange := false
	if kubeConfig.Global != nil && globalModule.ConfigValuesHaveChanges() {
		hasGlobalChange = true
	}

	//newGlobalValues := mm.kubeGlobalConfigValues
	//if (kubeConfig == nil || kubeConfig.Global == nil) && mm.kubeGlobalConfigValues.HasGlobal() {
	//	hasGlobalChange = true
	//	newGlobalValues = make(utils.Values)
	//}
	//if kubeConfig != nil && kubeConfig.Global != nil {
	//	globalChecksum := mm.kubeGlobalConfigValues.Checksum()
	//
	//	if kubeConfig.Global.Checksum != globalChecksum {
	//		hasGlobalChange = true
	//	}
	//	newGlobalValues = kubeConfig.Global.GetValues()
	//}

	// Full reload if enabled flags are changed.
	isEnabledChanged := false
	for _, moduleName := range mm.modules.NamesInOrder() {
		// Current module state.
		_, wasEnabled := mm.enabledModulesByConfig[moduleName]
		_, isEnabled := newEnabledByConfig[moduleName]

		fmt.Printf("MODULE %q. wasEnabled: %t, isEnabled: %t", moduleName, wasEnabled, isEnabled)

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
			mod := mm.GetModule(moduleName).GetBaseModule()
			//modValues, hasConfigValues := mm.kubeModulesConfigValues[moduleName]
			// New module state from ConfigMap.
			//hasNewKubeConfig := false
			//var newModConfig *config.ModuleKubeConfig
			//if kubeConfig != nil {
			//	newModConfig, hasNewKubeConfig = kubeConfig.Modules[moduleName]
			//}

			if mod.ConfigValuesHaveChanges() {
				modulesChanged = append(modulesChanged, moduleName)
				mod.CommitConfigValuesChange()
			}

			//// Section added or disappeared from ConfigMap, values changed.
			//if hasConfigValues != hasNewKubeConfig {
			//	modulesChanged = append(modulesChanged, moduleName)
			//	continue
			//}
			//
			//// Compare checksums for new and saved values.
			//if hasConfigValues && hasNewKubeConfig {
			//	modValuesChecksum := modValues.Checksum()
			//	newModValuesChecksum := newModConfig.GetValues().Checksum()
			//	if modValuesChecksum != newModValuesChecksum {
			//		modulesChanged = append(modulesChanged, moduleName)
			//	}
			//}
		}
	}

	globalModule.CommitConfigValuesChange()
	mm.SetKubeConfigValuesValid(true)

	//// Create new map with config values.
	//newKubeModuleConfigValues := make(map[string]utils.Values)
	//if kubeConfig != nil {
	//	for moduleName, moduleConfig := range kubeConfig.Modules {
	//		newKubeModuleConfigValues[moduleName] = moduleConfig.GetValues()
	//	}
	//}

	// Update caches from ConfigMap content.
	//mm.valuesLayersLock.Lock()
	mm.enabledModulesByConfig = newEnabledByConfig
	//mm.kubeGlobalConfigValues = newGlobalValues
	//mm.kubeModulesConfigValues = newKubeModuleConfigValues
	//mm.valuesLayersLock.Unlock()

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

		fmt.Println("############")

		isEnabled := mergeEnabled(
			&isEnabledByConfig,
			kubeConfigEnabled,
		)

		fmt.Printf("Module %q. EnabledByConfig: %t. KubeConfig: %v. Merge: %t ", ml.GetName(), isEnabledByConfig, kubeConfigEnabled, isEnabled)

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
	fmt.Printf("ENABLED BY CONFIG1: \n%v", enabledModules)

	if err := mm.registerGlobalModule(globalValues); err != nil {
		return err
	}

	return mm.registerModules()
}

//// validateKubeConfig checks validity of all sections in ConfigMap with OpenAPI schemas.
//func (mm *ModuleManager) validateKubeConfig(kubeConfig *config.KubeConfig, enabledModules map[string]struct{}) error {
//	// Ignore empty kube config.
//	if kubeConfig == nil {
//		mm.SetKubeConfigValuesValid(true)
//		return nil
//	}
//	// Validate values in global section merged with static values.
//	var validationErr error
//	if kubeConfig.Global != nil {
//		err := mm.ValuesValidator.ValidateGlobalConfigValues(mm.GlobalStaticAndNewValues(kubeConfig.Global.GetValues()))
//		if err != nil {
//			validationErr = multierror.Append(
//				validationErr,
//				fmt.Errorf("'global' section in ConfigMap/%s is not valid", app.ConfigMapName),
//				err,
//			)
//		}
//	}
//
//	// Validate config values for enabled modules.
//	for moduleName := range enabledModules {
//		modCfg, has := kubeConfig.Modules[moduleName]
//		if !has {
//			continue
//		}
//		mod := mm.GetModule(moduleName)
//		moduleErr := mm.ValuesValidator.ValidateModuleConfigValues(mod.ValuesKey(), mod.StaticAndNewValues(modCfg.GetValues()))
//		if moduleErr != nil {
//			validationErr = multierror.Append(
//				validationErr,
//				fmt.Errorf("'%s' module section in KubeConfig is not valid", mod.ValuesKey()),
//				moduleErr,
//			)
//		}
//	}
//
//	// Set valid flag to false if there is validation error
//	mm.SetKubeConfigValuesValid(validationErr == nil)
//
//	return validationErr
//}

func (mm *ModuleManager) GetKubeConfigValid() bool {
	return mm.kubeConfigValid
}

func (mm *ModuleManager) SetKubeConfigValid(valid bool) {
	mm.kubeConfigValid = valid
}

func (mm *ModuleManager) SetKubeConfigValuesValid(valid bool) {
	mm.kubeConfigValuesValid = valid
}

// checkConfig increases config_values_errors_total metric when kubeConfig becomes invalid.
func (mm *ModuleManager) checkConfig() {
	for {
		if mm.ctx.Err() != nil {
			return
		}
		if !mm.kubeConfigValid || !mm.kubeConfigValuesValid {
			mm.dependencies.MetricStorage.CounterAdd("{PREFIX}config_values_errors_total", 1.0, map[string]string{})
		}
		time.Sleep(5 * time.Second)
	}
}

// Start runs service go routine.
func (mm *ModuleManager) Start() {
	// Start checking kubeConfigIsValid flag in go routine.
	go mm.checkConfig()
}

// RefreshStateFromHelmReleases retrieves all Helm releases. It treats releases for known modules as
// an initial list of enabled modules.
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

	// Initiate enabled modules list.
	mm.enabledModules = state.AllEnabledModules

	return state, nil
}

// stateFromHelmReleases calculates enabled modules and modules to purge from Helm releases.
func (mm *ModuleManager) stateFromHelmReleases(releases []string) *ModulesState {
	releasesMap := utils.ListToMapStringStruct(releases)

	// Filter out known modules.
	enabledModules := make([]string, 0)
	for _, modName := range mm.modules.NamesInOrder() {
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

	log.Infof("Modules to purge found (but they will not be purged): %v", purge)
	// TODO(nabokihms): This is a temporary workaround to prevent deckhouse deleting modules downloaded from sources.
	// Purging modules is required to delete releases for modules that were renamed or deleted in a new release.
	//
	// However, because of a race between downloading modules and running installation tasks on multimaster clusters.
	//
	// In the future, we need to identify and figure out how to handle this race.
	// Users want to rely that their modules will not be deleted.

	return &ModulesState{
		AllEnabledModules: enabledModules,
		ModulesToPurge:    []string{},
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
	newlyEnabledModules := utils.ListSubtract(enabledModules, mm.enabledModules)

	// Disabled modules are that present in the list of currently enabled modules
	// but not present in the list after running enabled scripts
	disabledModules := utils.ListSubtract(mm.enabledModules, enabledModules)
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
	mm.enabledModules = enabledModules

	// TODO: remove me
	tmp := map[string]interface{}{
		"AllEnabledModules": mm.enabledModules,
		"ModulesToDisable":  disabledModules,
		"ModulesToEnable":   newlyEnabledModules,
		"byConfig":          mm.enabledModulesByConfig,
		"dynamic":           enabledByDynamic,
	}

	data, _ := yaml.Marshal(tmp)
	fmt.Println("DEBUGME+\n", string(data))

	// Return lists for ConvergeModules task.
	return &ModulesState{
		AllEnabledModules: mm.enabledModules,
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
	return mm.enabledModules
}

// IsModuleEnabled xx
// Deprecated: remove
func (mm *ModuleManager) IsModuleEnabled(moduleName string) bool {
	for _, modName := range mm.enabledModules {
		if modName == moduleName {
			return true
		}
	}
	return false
}

func (mm *ModuleManager) GetGlobalHook(name string) *hooks2.GlobalHook {
	return mm.global.GetHookByName(name)
}

//func (mm *ModuleManager) GetModuleHook(name string) *ModuleHook {
//	for _, bindingHooks := range mm.modulesHooksOrderByName {
//		for _, hooks := range bindingHooks {
//			for _, hook := range hooks {
//				if hook.Name == name {
//					return hook
//				}
//			}
//		}
//	}
//	return nil
//}

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

//func (mm *ModuleManager) GetModuleHooksInOrder(moduleName string, bindingType BindingType) []string {
//	moduleHooksByBinding, ok := mm.modulesHooksOrderByName[moduleName]
//	if !ok {
//		return []string{}
//	}
//
//	moduleBindingHooks, ok := moduleHooksByBinding[bindingType]
//	if !ok {
//		return []string{}
//	}
//
//	sort.Slice(moduleBindingHooks, func(i, j int) bool {
//		return moduleBindingHooks[i].Order(bindingType) < moduleBindingHooks[j].Order(bindingType)
//	})
//
//	moduleHooksNames := make([]string, 0, len(moduleBindingHooks))
//	for _, moduleHook := range moduleBindingHooks {
//		moduleHooksNames = append(moduleHooksNames, moduleHook.Name)
//	}
//
//	return moduleHooksNames
//}

//func (mm *ModuleManager) GetModuleHookNames(moduleName string) []string {
//	moduleHooksByBinding, ok := mm.modulesHooksOrderByName[moduleName]
//	if !ok {
//		return []string{}
//	}
//
//	moduleHookNamesMap := make(map[string]struct{})
//	for _, moduleHooks := range moduleHooksByBinding {
//		for _, moduleHook := range moduleHooks {
//			moduleHookNamesMap[moduleHook.Name] = struct{}{}
//		}
//	}
//
//	return utils.MapStringStructKeys(moduleHookNamesMap)
//}

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
		// TODO: check helm chart exists
		hmdeps := modules.HelmModuleDependencies{
			ClientFactory:       mm.dependencies.Helm,
			HelmResourceManager: mm.dependencies.HelmResourcesManager,
			MetricsStorage:      mm.dependencies.MetricStorage,
			HelmValuesValidator: mm.ValuesValidator,
		}
		helmModule, _ := modules.NewHelmModule(ml.GetBaseModule(), mm.TempDir, &hmdeps, mm.ValuesValidator)
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

		err := ml.GetBaseModule().RunHooksByBinding(AfterDeleteHelm, deleteLogLabels)
		if err != nil {
			return err
		}

		// Cleanup state.
		ml.GetBaseModule().ResetState()
	}

	// Unregister module hooks.
	ml.GetBaseModule().DeregisterHooks()

	return nil
}

// RunModule runs beforeHelm hook, helm upgrade --install and afterHelm or afterDeleteHelm hook
func (mm *ModuleManager) RunModule(moduleName string, logLabels map[string]string) (bool, error) {
	ml := mm.GetModule(moduleName)
	bm := ml.GetBaseModule()

	// Do not send to mm.moduleValuesChanged, changed values are handled by TaskHandler.
	defer trace.StartRegion(context.Background(), "ModuleRun-HelmPhase").End()

	logLabels = utils.MergeLabels(logLabels, map[string]string{
		"module": ml.GetName(),
		"queue":  "main",
	})

	// Hooks can delete release resources, so pause resources monitor before run hooks.
	mm.dependencies.HelmResourcesManager.PauseMonitor(ml.GetName())
	defer mm.dependencies.HelmResourcesManager.ResumeMonitor(ml.GetName())

	var err error

	treg := trace.StartRegion(context.Background(), "ModuleRun-HelmPhase-beforeHelm")
	err = bm.RunHooksByBinding(BeforeHelm, logLabels)
	treg.End()
	if err != nil {
		return false, err
	}

	treg = trace.StartRegion(context.Background(), "ModuleRun-HelmPhase-helm")
	deps := &modules.HelmModuleDependencies{
		mm.dependencies.Helm,
		mm.dependencies.HelmResourcesManager,
		mm.dependencies.MetricStorage,
		mm.ValuesValidator,
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
	return oldValuesChecksum == newValuesChecksum, nil
}

func (mm *ModuleManager) RunGlobalHook(hookName string, binding BindingType, bindingContext []BindingContext, logLabels map[string]string) (string, string, error) {
	return mm.global.RunHookByName(hookName, binding, bindingContext, logLabels)
}

func (mm *ModuleManager) RunModuleHook(moduleName, hookName string, binding BindingType, bindingContext []BindingContext, logLabels map[string]string) (beforeChecksum string, afterChecksum string, err error) {
	ml := mm.GetModule(moduleName)

	return ml.GetBaseModule().RunHookByName(hookName, binding, bindingContext, logLabels)
}

//// GlobalConfigValues return raw global values only from a ConfigMap.
//func (mm *ModuleManager) GlobalConfigValues() utils.Values {
//	return modules.mergeLayers(
//		// Init global section.
//		utils.Values{"global": map[string]interface{}{}},
//		// Merge overrides from ConfigMap.
//		mm.kubeGlobalConfigValues,
//	)
//}
//
//// GlobalStaticAndConfigValues return global values defined in
//// various values.yaml files and in a ConfigMap
//func (mm *ModuleManager) GlobalStaticAndConfigValues() utils.Values {
//	return modules.mergeLayers(
//		// Init global section.
//		utils.Values{"global": map[string]interface{}{}},
//		// Merge static values from modules/values.yaml.
//		mm.commonStaticValues.Global(),
//		// Apply config values defaults before ConfigMap overrides.
//		&applyDefaultsForGlobal{validation.ConfigValuesSchema, mm.ValuesValidator},
//		// Merge overrides from ConfigMap.
//		mm.kubeGlobalConfigValues,
//	)
//}

//// GlobalStaticAndNewValues return global values defined in
//// various values.yaml files merged with newValues
//func (mm *ModuleManager) GlobalStaticAndNewValues(newValues utils.Values) utils.Values {
//	return modules.mergeLayers(
//		// Init global section.
//		utils.Values{"global": map[string]interface{}{}},
//		// Merge static values from modules/values.yaml.
//		mm.commonStaticValues.Global(),
//		// Apply config values defaults before overrides.
//		&applyDefaultsForGlobal{validation.ConfigValuesSchema, mm.ValuesValidator},
//		// Merge overrides from newValues.
//		newValues,
//	)
//}

//// GlobalValues return current global values with applied patches
//func (mm *ModuleManager) GlobalValues() (utils.Values, error) {
//	var err error
//
//	res := modules.mergeLayers(
//		// Init global section.
//		utils.Values{"global": map[string]interface{}{}},
//		// Merge static values from modules/values.yaml.
//		mm.commonStaticValues.Global(),
//		// Apply config values defaults before ConfigMap overrides.
//		&applyDefaultsForGlobal{validation.ConfigValuesSchema, mm.ValuesValidator},
//		// Merge overrides from ConfigMap.
//		mm.kubeGlobalConfigValues,
//		// Apply dynamic values defaults before patches.
//		&applyDefaultsForGlobal{validation.ValuesSchema, mm.ValuesValidator},
//	)
//
//	// Invariant: do not store patches that does not apply
//	// Give user error for patches early, after patch receive
//
//	// Compact patches so we could execute all at once.
//	// Each ApplyValuesPatch execution invokes json.Marshal for values.
//	ops := *utils.NewValuesPatch() // TODO(nabokihms): The values is always not nil, consider refactoring the method
//
//	for _, patch := range mm.globalDynamicValuesPatches {
//		ops.Operations = append(ops.Operations, patch.Operations...)
//	}
//
//	res, _, err = utils.ApplyValuesPatch(res, ops, utils.IgnoreNonExistentPaths)
//	if err != nil {
//		return nil, fmt.Errorf("apply global patch error: %s", err)
//	}
//
//	return res, nil
//}

//// GlobalValuesPatches return patches for global values.
//// Deprecated: think something about this
//func (mm *ModuleManager) GlobalValuesPatches() []utils.ValuesPatch {
//	return mm.globalDynamicValuesPatches
//}

//// UpdateGlobalConfigValues sets updated global config values.
//func (mm *ModuleManager) UpdateGlobalConfigValues(configValues utils.Values) {
//	mm.valuesLayersLock.Lock()
//	defer mm.valuesLayersLock.Unlock()
//
//	mm.kubeGlobalConfigValues = configValues
//}
//
//// UpdateGlobalDynamicValuesPatches appends patches for global dynamic values.
//func (mm *ModuleManager) UpdateGlobalDynamicValuesPatches(valuesPatch utils.ValuesPatch) {
//	mm.valuesLayersLock.Lock()
//	defer mm.valuesLayersLock.Unlock()
//
//	mm.globalDynamicValuesPatches = utils.AppendValuesPatch(
//		mm.globalDynamicValuesPatches,
//		valuesPatch)
//}

//// ModuleConfigValues returns config values for module.
//func (mm *ModuleManager) ModuleConfigValues(moduleName string) utils.Values {
//	mm.valuesLayersLock.RLock()
//	defer mm.valuesLayersLock.RUnlock()
//	return mm.kubeModulesConfigValues[moduleName]
//}

//// ModuleDynamicValuesPatches returns all patches for dynamic values.
//func (mm *ModuleManager) ModuleDynamicValuesPatches(moduleName string) []utils.ValuesPatch {
//	mm.valuesLayersLock.RLock()
//	defer mm.valuesLayersLock.RUnlock()
//	return mm.modulesDynamicValuesPatches[moduleName]
//}

//// UpdateModuleConfigValues sets updated config values for module.
//func (mm *ModuleManager) UpdateModuleConfigValues(moduleName string, configValues utils.Values) {
//	mm.valuesLayersLock.Lock()
//	defer mm.valuesLayersLock.Unlock()
//	mm.kubeModulesConfigValues[moduleName] = configValues
//}

// UpdateModuleDynamicValuesPatches appends patches for dynamic values for module.
//func (mm *ModuleManager) UpdateModuleDynamicValuesPatches(moduleName string, valuesPatch utils.ValuesPatch) {
//	mm.valuesLayersLock.Lock()
//	defer mm.valuesLayersLock.Unlock()
//
//	mm.modulesDynamicValuesPatches[moduleName] = utils.AppendValuesPatch(
//		mm.modulesDynamicValuesPatches[moduleName],
//		valuesPatch)
//}

//func (mm *ModuleManager) ApplyModuleDynamicValuesPatches(moduleName string, values utils.Values) (utils.Values, error) {
//	var err error
//	res := values
//
//	// Compact patches so we could execute all at once.
//	// Each ApplyValuesPatch execution invokes json.Marshal for values.
//	ops := *utils.NewValuesPatch() // TODO(nabokihms): The values is always not nil, consider refactoring the method
//
//	for _, patch := range mm.ModuleDynamicValuesPatches(moduleName) {
//		ops.Operations = append(ops.Operations, patch.Operations...)
//	}
//
//	res, _, err = utils.ApplyValuesPatch(res, ops, utils.IgnoreNonExistentPaths)
//	if err != nil {
//		return nil, err
//	}
//
//	return res, nil
//}

func (mm *ModuleManager) GetValuesValidator() *validation.ValuesValidator {
	return mm.ValuesValidator
}

func (mm *ModuleManager) HandleKubeEvent(kubeEvent KubeEvent, createGlobalTaskFn func(*hooks2.GlobalHook, controller.BindingExecutionInfo), createModuleTaskFn func(*modules.BasicModule, *hooks2.ModuleHook, controller.BindingExecutionInfo)) {
	mm.LoopByBinding(OnKubernetesEvent, func(gh *hooks2.GlobalHook, m *modules.BasicModule, mh *hooks2.ModuleHook) {
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

func (mm *ModuleManager) HandleGlobalEnableKubernetesBindings(hookName string, createTaskFn func(*hooks2.GlobalHook, controller.BindingExecutionInfo)) error {
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

func (mm *ModuleManager) HandleModuleEnableKubernetesBindings(moduleName string, createTaskFn func(*hooks2.ModuleHook, controller.BindingExecutionInfo)) error {
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

func (mm *ModuleManager) HandleScheduleEvent(crontab string, createGlobalTaskFn func(*hooks2.GlobalHook, controller.BindingExecutionInfo), createModuleTaskFn func(*modules.BasicModule, *hooks2.ModuleHook, controller.BindingExecutionInfo)) error {
	mm.LoopByBinding(Schedule, func(gh *hooks2.GlobalHook, m *modules.BasicModule, mh *hooks2.ModuleHook) {
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

func (mm *ModuleManager) LoopByBinding(binding BindingType, fn func(gh *hooks2.GlobalHook, m *modules.BasicModule, mh *hooks2.ModuleHook)) {
	globalHooks := mm.GetGlobalHooksInOrder(binding)

	for _, hookName := range globalHooks {
		gh := mm.GetGlobalHook(hookName)
		fn(gh, nil, nil)
	}

	for _, moduleName := range mm.enabledModules {
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

//// SetModuleSource set source (external or embedded repository) for a module
//func (mm *ModuleManager) SetModuleSource(moduleName, source string) {
//	if !mm.modules.Has(moduleName) {
//		return
//	}
//
//	module := mm.modules.Get(moduleName)
//	module.Source = source
//}

func (mm *ModuleManager) ApplyBindingActions(moduleHook *hooks2.ModuleHook, bindingActions []go_hook.BindingAction) error {
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
