package module_manager

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/hashicorp/go-multierror"
	log "github.com/sirupsen/logrus"

	. "github.com/flant/shell-operator/pkg/hook/binding_context"
	. "github.com/flant/shell-operator/pkg/hook/types"
	. "github.com/flant/shell-operator/pkg/kube_events_manager/types"

	// bindings constants and binding configs
	. "github.com/flant/addon-operator/pkg/hook/types"

	"github.com/flant/shell-operator/pkg/hook/controller"
	"github.com/flant/shell-operator/pkg/kube"
	"github.com/flant/shell-operator/pkg/kube/object_patch"
	"github.com/flant/shell-operator/pkg/kube_events_manager"
	"github.com/flant/shell-operator/pkg/metric_storage"
	"github.com/flant/shell-operator/pkg/schedule_manager"
	utils_checksum "github.com/flant/shell-operator/pkg/utils/checksum"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/helm_resources_manager"
	"github.com/flant/addon-operator/pkg/kube_config_manager"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/addon-operator/pkg/values/validation"
)

// TODO separate modules and hooks storage, values storage and actions

type ModuleManager interface {
	Init() error
	Start()
	Ch() chan Event

	// Dependencies
	WithContext(ctx context.Context)
	WithDirectories(modulesDir string, globalHooksDir string, tempDir string) ModuleManager
	WithKubeEventManager(kube_events_manager.KubeEventsManager)
	WithKubeObjectPatcher(*object_patch.ObjectPatcher)
	WithScheduleManager(schedule_manager.ScheduleManager)
	WithKubeConfigManager(kubeConfigManager kube_config_manager.KubeConfigManager) ModuleManager
	WithHelmResourcesManager(manager helm_resources_manager.HelmResourcesManager)
	WithMetricStorage(storage *metric_storage.MetricStorage)
	WithHookMetricStorage(storage *metric_storage.MetricStorage)

	GetGlobalHooksInOrder(bindingType BindingType) []string
	GetGlobalHook(name string) *GlobalHook

	GetModuleNamesInOrder() []string
	GetModule(name string) *Module
	GetModuleHookNames(moduleName string) []string
	GetModuleHook(name string) *ModuleHook
	GetModuleHooksInOrder(moduleName string, bindingType BindingType) []string

	GlobalStaticAndConfigValues() utils.Values
	GlobalStaticAndNewValues(newValues utils.Values) utils.Values
	GlobalConfigValues() utils.Values
	GlobalValues() (utils.Values, error)
	GlobalValuesPatches() []utils.ValuesPatch

	// Actions for tasks
	DiscoverModulesState(logLabels map[string]string) (*ModulesState, error)
	DeleteModule(moduleName string, logLabels map[string]string) error
	RunModule(moduleName string, onStartup bool, logLabels map[string]string, afterStartupCb func() error) (bool, error)
	RunGlobalHook(hookName string, binding BindingType, bindingContext []BindingContext, logLabels map[string]string) (beforeChecksum string, afterChecksum string, err error)
	RunModuleHook(hookName string, binding BindingType, bindingContext []BindingContext, logLabels map[string]string) error
	Retry()

	RegisterModuleHooks(module *Module, logLabels map[string]string) error

	HandleKubeEvent(kubeEvent KubeEvent, createGlobalTaskFn func(*GlobalHook, controller.BindingExecutionInfo), createModuleTaskFn func(*Module, *ModuleHook, controller.BindingExecutionInfo))
	HandleGlobalEnableKubernetesBindings(hookName string, createTaskFn func(*GlobalHook, controller.BindingExecutionInfo)) error
	HandleModuleEnableKubernetesBindings(hookName string, createTaskFn func(*ModuleHook, controller.BindingExecutionInfo)) error
	StartModuleHooks(moduleName string)
	//EnableScheduleBindings()
	DisableModuleHooks(moduleName string)
	HandleScheduleEvent(crontab string, createGlobalTaskFn func(*GlobalHook, controller.BindingExecutionInfo), createModuleTaskFn func(*Module, *ModuleHook, controller.BindingExecutionInfo)) error

	DynamicEnabledChecksum() string
	ApplyEnabledPatch(enabledPatch utils.ValuesPatch) error

	GlobalSynchronizationNeeded() bool
	GlobalSynchronizationDone() bool
	SynchronizationQueued(id string)
	SynchronizationDone(id string)

	DumpState()
}

// ModulesState is a result of Discovery process, that determines which
// modules should be enabled, disabled or purged.
type ModulesState struct {
	// modules that should be run
	EnabledModules []string
	// modules that should be deleted
	ModulesToDisable []string
	// modules that should be purged
	ReleasedUnknownModules []string
	// modules that was disabled and now are enabled
	NewlyEnabledModules []string
}

type moduleManager struct {
	ctx    context.Context
	cancel context.CancelFunc

	ValuesLock sync.Mutex

	// Directories
	ModulesDir     string
	GlobalHooksDir string
	TempDir        string

	EventCh chan Event

	KubeClient           kube.KubernetesClient
	KubeObjectPatcher    *object_patch.ObjectPatcher
	kubeEventsManager    kube_events_manager.KubeEventsManager
	scheduleManager      schedule_manager.ScheduleManager
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
	enabledModulesByConfig []string

	// TODO calculate from enabledValues
	// Effective list of enabled modules after enabled script running.
	// List is sorted by module name.
	// This list is changed on ConfigMap changes.
	enabledModulesInOrder []string

	// Index of all global hooks. Key is global hook name
	globalHooksByName map[string]*GlobalHook
	// Index for searching global hooks by their bindings.
	globalHooksOrder map[BindingType][]*GlobalHook

	// module hooks by module name and binding type ordered by name
	// Note: one module hook can have several binding types.
	modulesHooksOrderByName map[string]map[BindingType][]*ModuleHook

	kubernetesBindingSynchronizationState map[string]*KubernetesBindingSynchronizationState

	// VALUE STORAGES

	// Values from modules/values.yaml file
	commonStaticValues utils.Values

	// global values from ConfigMap
	kubeGlobalConfigValues utils.Values
	// module values from ConfigMap, only for enabled modules
	kubeModulesConfigValues map[string]utils.Values

	// Invariant: do not store patches that cannot be applied.
	// Give user error for patches early, after patch receive.

	// Patches for dynamic global values
	globalDynamicValuesPatches []utils.ValuesPatch
	// Pathces for dynamic module values
	modulesDynamicValuesPatches map[string][]utils.ValuesPatch

	// Internal event: module values are changed.
	// This event leads to module run action.
	moduleValuesChanged chan string
	// Internal event: global values are changed.
	// This event leads to module discovery action.
	globalValuesChanged chan bool

	kubeConfigManager kube_config_manager.KubeConfigManager

	// Saved values from ConfigMap to handle Ambiguous state.
	moduleConfigsUpdateBeforeAmbiguos kube_config_manager.ModuleConfigs
	// Internal event: module manager needs to be restarted.
	retryOnAmbiguous chan bool
}

var _ ModuleManager = &moduleManager{}

// EventType are events for the main loop.
type EventType string

const (
	// There are modules with changed values.
	ModulesChanged EventType = "MODULES_CHANGED"
	// Global section is changed.
	GlobalChanged EventType = "GLOBAL_CHANGED"
	// Something wrong with module manager.
	AmbiguousState EventType = "AMBIGUOUS_STATE"
)

// ChangeType are types of module changes.
type ChangeType string

const (
	// All other types are deprecated. This const can be removed in future versions.
	// Module values are changed
	Changed ChangeType = "MODULE_CHANGED"
)

// ModuleChange contains module name and type of module changes.
type ModuleChange struct {
	Name       string
	ChangeType ChangeType
}

// Event is used to send module events to the main loop.
type Event struct {
	ModulesChanges []ModuleChange
	Type           EventType
}

// NewMainModuleManager returns new MainModuleManager
func NewMainModuleManager() *moduleManager {
	return &moduleManager{
		EventCh:    make(chan Event),
		ValuesLock: sync.Mutex{},

		ValuesValidator: validation.NewValuesValidator(),

		allModulesByName:            make(map[string]*Module),
		allModulesNamesInOrder:      make([]string, 0),
		enabledModulesByConfig:      make([]string, 0),
		enabledModulesInOrder:       make([]string, 0),
		dynamicEnabled:              make(map[string]*bool),
		globalHooksByName:           make(map[string]*GlobalHook),
		globalHooksOrder:            make(map[BindingType][]*GlobalHook),
		modulesHooksOrderByName:     make(map[string]map[BindingType][]*ModuleHook),
		commonStaticValues:          make(utils.Values),
		kubeGlobalConfigValues:      make(utils.Values),
		kubeModulesConfigValues:     make(map[string]utils.Values),
		globalDynamicValuesPatches:  make([]utils.ValuesPatch, 0),
		modulesDynamicValuesPatches: make(map[string][]utils.ValuesPatch),

		moduleValuesChanged: make(chan string, 1),
		globalValuesChanged: make(chan bool, 1),

		kubeConfigManager: nil,

		moduleConfigsUpdateBeforeAmbiguos: make(kube_config_manager.ModuleConfigs),
		retryOnAmbiguous:                  make(chan bool, 1),

		kubernetesBindingSynchronizationState: make(map[string]*KubernetesBindingSynchronizationState),
	}
}

func (mm *moduleManager) WithDirectories(modulesDir string, globalHooksDir string, tempDir string) ModuleManager {
	mm.ModulesDir = modulesDir
	mm.GlobalHooksDir = globalHooksDir
	mm.TempDir = tempDir
	return mm
}

func (mm *moduleManager) WithKubeConfigManager(kubeConfigManager kube_config_manager.KubeConfigManager) ModuleManager {
	mm.kubeConfigManager = kubeConfigManager
	return mm
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

// RunModulesEnabledScript runs enable script for each module that is enabled by config.
// Enable script receives a list of previously enabled modules.
func (mm *moduleManager) RunModulesEnabledScript(enabledByConfig []string, logLabels map[string]string) ([]string, error) {
	enabledModules := make([]string, 0)

	for _, name := range utils.SortByReference(enabledByConfig, mm.allModulesNamesInOrder) {
		moduleLogLabels := utils.MergeLabels(logLabels)
		moduleLogLabels["module"] = name
		module := mm.allModulesByName[name]
		moduleIsEnabled, err := module.checkIsEnabledByScript(enabledModules, moduleLogLabels)
		if err != nil {
			return nil, err
		}

		if moduleIsEnabled {
			enabledModules = append(enabledModules, name)
		}
	}

	return enabledModules, nil
}

// kubeUpdate
type kubeUpdate struct {
	EnabledModulesByConfig  []string
	KubeGlobalConfigValues  utils.Values
	KubeModulesConfigValues map[string]utils.Values
	Events                  []Event
}

func (mm *moduleManager) applyKubeUpdate(kubeUpdate *kubeUpdate) error {
	log.Debugf("Apply kubeupdate %+v", kubeUpdate)
	mm.kubeGlobalConfigValues = kubeUpdate.KubeGlobalConfigValues
	mm.kubeModulesConfigValues = kubeUpdate.KubeModulesConfigValues
	mm.enabledModulesByConfig = kubeUpdate.EnabledModulesByConfig

	for _, event := range kubeUpdate.Events {
		mm.EventCh <- event
	}

	return nil
}

func (mm *moduleManager) handleNewKubeConfig(newConfig kube_config_manager.Config) (*kubeUpdate, error) {
	logEntry := log.WithField("operator.component", "ModuleManager").
		WithField("operator.action", "handleNewKubeConfig")
	logEntry.Debugf("new kube config received")

	res := &kubeUpdate{
		KubeGlobalConfigValues: newConfig.Values,
		Events:                 []Event{{Type: GlobalChanged}},
	}

	var unknown []utils.ModuleConfig
	res.EnabledModulesByConfig, res.KubeModulesConfigValues, unknown = mm.calculateEnabledModulesByConfig(newConfig.ModuleConfigs)

	for _, moduleConfig := range unknown {
		logEntry.Warnf("Ignore ConfigMap section '%s' for absent module : \n%s",
			moduleConfig.ModuleName,
			moduleConfig.String(),
		)
	}

	return res, nil
}

func (mm *moduleManager) handleNewKubeModuleConfigs(moduleConfigs kube_config_manager.ModuleConfigs) (*kubeUpdate, error) {
	logLabels := map[string]string{
		"operator.component": "HandleConfigMap",
	}
	logEntry := log.WithFields(utils.LabelsToLogFields(logLabels))

	logEntry.Debugf("handle changes in module sections")

	res := &kubeUpdate{
		Events:                 make([]Event, 0),
		KubeGlobalConfigValues: mm.kubeGlobalConfigValues,
	}

	// NOTE: values for non changed modules were copied from mm.kubeModulesConfigValues[moduleName].
	// Now calculateEnabledModulesByConfig got values for modules from moduleConfigs — as they are in ConfigMap now.
	// TODO this should not be a problem because of a checksum matching in kube_config_manager
	var unknown []utils.ModuleConfig
	res.EnabledModulesByConfig, res.KubeModulesConfigValues, unknown = mm.calculateEnabledModulesByConfig(moduleConfigs)

	for _, moduleConfig := range unknown {
		logEntry.Warnf("ignore module section for unknown module '%s':\n%s",
			moduleConfig.ModuleName, moduleConfig.String())
	}

	// Detect removed module sections for statically enabled modules.
	// This removal should be handled like kube config update.
	updateOnSectionRemove := make(map[string]bool)
	for moduleName, module := range mm.allModulesByName {
		_, hasKubeConfig := moduleConfigs[moduleName]
		if !hasKubeConfig {
			isEnabled := mergeEnabled(
				module.CommonStaticConfig.IsEnabled,
				module.StaticConfig.IsEnabled,
				mm.dynamicEnabled[moduleName])
			_, hasValues := mm.kubeModulesConfigValues[moduleName]
			if isEnabled && hasValues {
				updateOnSectionRemove[moduleName] = true
			}
		}
	}

	// New version of mm.enabledModulesByConfig
	res.EnabledModulesByConfig = utils.SortByReference(res.EnabledModulesByConfig, mm.allModulesNamesInOrder)

	// Run enable scripts
	logEntry.Debugf("Run enabled script for %+v", res.EnabledModulesByConfig)
	enabledModules, err := mm.RunModulesEnabledScript(res.EnabledModulesByConfig, logLabels)
	if err != nil {
		return nil, err
	}
	logEntry.Infof("Modules enabled by script: %+v", enabledModules)

	// Configure events
	if !reflect.DeepEqual(mm.enabledModulesInOrder, enabledModules) {
		// Enabled modules set is changed — return GlobalChanged event, that will
		// create a Discover task, run enabled scripts again, init new module hooks,
		// update mm.enabledModulesInOrder
		logEntry.Debugf("enabled modules set changed from %v to %v: generate GlobalChanged event", mm.enabledModulesInOrder, res.EnabledModulesByConfig)
		res.Events = append(res.Events, Event{Type: GlobalChanged})
	} else {
		// Enabled modules set is not changed, only values in configmap are changed.
		logEntry.Debugf("generate ModulesChanged events...")

		moduleChanges := make([]ModuleChange, 0)

		// make Changed event for each enabled module with updated config
		for _, name := range enabledModules {
			// Module has updated kube config
			isUpdated := false
			moduleConfig, hasKubeConfig := moduleConfigs[name]

			if hasKubeConfig {
				isUpdated = moduleConfig.IsUpdated
				// skip not updated module configs
				if !isUpdated {
					logEntry.Debugf("ignore module '%s': kube config is not updated", name)
					continue
				}
			}

			// Update module if kube config is removed
			_, shouldUpdateAfterRemoval := updateOnSectionRemove[name]

			if (hasKubeConfig && isUpdated) || shouldUpdateAfterRemoval {
				moduleChanges = append(moduleChanges, ModuleChange{Name: name, ChangeType: Changed})
			}
		}

		if len(moduleChanges) > 0 {
			logEntry.Infof("fire ModulesChanged event for %d modules", len(moduleChanges))
			logEntry.Debugf("event changes: %v", moduleChanges)
			res.Events = append(res.Events, Event{Type: ModulesChanged, ModulesChanges: moduleChanges})
		}
	}

	return res, nil
}

// calculateEnabledModulesByConfig determine enable state for all modules
// by values.yaml, ConfigMap and dynamicEnable map.
// Method returns list of enabled modules and their values. Also the map of disabled modules and a list of unknown
// keys in a ConfigMap.
//
// Module is enabled by config if module section in ConfigMap is a map or an array
// or ConfigMap has no module section and module has a map or an array in values.yaml
func (mm *moduleManager) calculateEnabledModulesByConfig(moduleConfigs kube_config_manager.ModuleConfigs) (enabled []string, values map[string]utils.Values, unknown []utils.ModuleConfig) {
	values = make(map[string]utils.Values)

	log.Debugf("calculateEnabled: dynamicEnabled is %s", mm.DumpDynamicEnabled())

	for moduleName, module := range mm.allModulesByName {
		kubeConfig, hasKubeConfig := moduleConfigs[moduleName]
		if hasKubeConfig {
			isEnabled := mergeEnabled(
				module.CommonStaticConfig.IsEnabled,
				module.StaticConfig.IsEnabled,
				kubeConfig.IsEnabled,
				mm.dynamicEnabled[moduleName])

			if isEnabled {
				enabled = append(enabled, moduleName)
				values[moduleName] = kubeConfig.Values
			}
			log.Debugf("calculateEnabled: module '%s': static enabled %v, kubeConfig: enabled %v, updated %v, dynamic enabled: %v",
				module.Name,
				module.StaticConfig.GetEnabled(),
				kubeConfig.IsEnabled,
				kubeConfig.IsUpdated,
				mm.dynamicEnabled[moduleName])
		} else {
			isEnabled := mergeEnabled(
				module.CommonStaticConfig.IsEnabled,
				module.StaticConfig.IsEnabled,
				mm.dynamicEnabled[moduleName])

			if isEnabled {
				enabled = append(enabled, moduleName)
			}
			log.Debugf("calculateEnabled: module '%s': static enabled %v, no kubeConfig, dynamic enabled: %v",
				module.Name,
				module.StaticConfig.GetEnabled(),
				mm.dynamicEnabled[moduleName])
		}
	}

	for _, kubeConfig := range moduleConfigs {
		if _, hasKey := mm.allModulesByName[kubeConfig.ModuleName]; !hasKey {
			unknown = append(unknown, kubeConfig)
		}
	}

	enabled = utils.SortByReference(enabled, mm.allModulesNamesInOrder)

	return
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

	kubeConfig := mm.kubeConfigManager.InitialConfig()
	mm.kubeGlobalConfigValues = kubeConfig.Values

	var unknown []utils.ModuleConfig
	mm.enabledModulesByConfig, mm.kubeModulesConfigValues, unknown = mm.calculateEnabledModulesByConfig(kubeConfig.ModuleConfigs)

	unknownNames := []string{}
	for _, config := range unknown {
		unknownNames = append(unknownNames, config.ModuleName)
	}
	if len(unknownNames) > 0 {
		log.Warnf("ConfigMap/%s has values for absent modules: %+v", app.ConfigMapName, unknownNames)
	}

	return mm.validateKubeConfig(mm.kubeConfigManager.CurrentConfig())
}

func (mm *moduleManager) validateKubeConfig(kubeConfig *kube_config_manager.Config) error {
	// Validate global and module sections in ConfigMap merged with static values.
	var validationErr error
	globalErr := mm.ValuesValidator.ValidateGlobalConfigValues(mm.GlobalStaticAndNewValues(kubeConfig.Values))
	if globalErr != nil {
		validationErr = multierror.Append(
			validationErr,
			fmt.Errorf("'global' section in ConfigMap/%s is not valid", app.ConfigMapName),
			globalErr,
		)
	}

	for _, moduleName := range mm.enabledModulesByConfig {
		mod := mm.allModulesByName[moduleName]
		modCfg, has := kubeConfig.ModuleConfigs[moduleName]
		if !has {
			continue
		}
		moduleErr := mm.ValuesValidator.ValidateModuleConfigValues(mod.ValuesKey(), mod.StaticAndNewValues(modCfg.Values))
		if moduleErr != nil {
			validationErr = multierror.Append(
				validationErr,
				fmt.Errorf("'%s' module section in ConfigMap/%s is not valid", mod.ValuesKey(), app.ConfigMapName),
				moduleErr,
			)
		}
	}

	if validationErr != nil {
		mm.metricStorage.CounterAdd("{PREFIX}config_values_errors_total", 1.0, map[string]string{})
	}

	return validationErr
}

// Module manager loop
func (mm *moduleManager) Start() {
	go mm.kubeConfigManager.Start()

	go func() {
		for {
			select {
			case <-mm.globalValuesChanged:
				log.Debugf("MODULE_MANAGER_RUN global values changed")
				mm.EventCh <- Event{Type: GlobalChanged}

			case moduleName := <-mm.moduleValuesChanged:
				log.Debugf("MODULE_MANAGER_RUN module '%s' values changed", moduleName)

				// Перезапускать enabled-скрипт не нужно, т.к.
				// изменение values модуля не может вызвать
				// изменение состояния включенности модуля
				mm.EventCh <- Event{
					Type: ModulesChanged,
					ModulesChanges: []ModuleChange{
						{Name: moduleName, ChangeType: Changed},
					},
				}

			case newKubeConfig := <-kube_config_manager.ConfigUpdated:
				// For simplicity, check the whole config.
				err := mm.validateKubeConfig(mm.kubeConfigManager.CurrentConfig())
				if err != nil {
					log.Errorf("MODULE_MANAGER_RUN ConfigMap changed and is not valid, no ReloadAllModules: %v", err)
					break
				}

				handleRes, err := mm.handleNewKubeConfig(newKubeConfig)
				if err != nil {
					log.Errorf("MODULE_MANAGER_RUN unable to handle kube config update: %s", err)
				}
				if handleRes != nil {
					err = mm.applyKubeUpdate(handleRes)
					if err != nil {
						log.Errorf("MODULE_MANAGER_RUN cannot apply kube config update: %s", err)
					}
				}

			case newModuleConfigs := <-kube_config_manager.ModuleConfigsUpdated:
				// Сбросить запомненные перед ошибкой конфиги
				mm.moduleConfigsUpdateBeforeAmbiguos = kube_config_manager.ModuleConfigs{}

				// For simplicity, check the whole config.
				err := mm.validateKubeConfig(mm.kubeConfigManager.CurrentConfig())
				if err != nil {
					log.Errorf("MODULE_MANAGER_RUN ConfigMap changed and is not valid, no module restart: %v", err)
					break
				}

				moduleUpdates, err := mm.handleNewKubeModuleConfigs(newModuleConfigs)
				if err != nil {
					mm.moduleConfigsUpdateBeforeAmbiguos = newModuleConfigs
					log.Errorf("Unable to handle update of ConfigMap for modules [%s]: %s", strings.Join(newModuleConfigs.Names(), ", "), err)
				}
				if moduleUpdates != nil {
					err = mm.applyKubeUpdate(moduleUpdates)
					if err != nil {
						log.Errorf("ConfigMap update cannot be applied to values for modules %s: %s", strings.Join(newModuleConfigs.Names(), ", "), err)
					}
				}

			case <-mm.retryOnAmbiguous:
				if len(mm.moduleConfigsUpdateBeforeAmbiguos) != 0 {
					log.Infof("MODULE_MANAGER_RUN Retry saved moduleConfigs: %v", mm.moduleConfigsUpdateBeforeAmbiguos)
					kube_config_manager.ModuleConfigsUpdated <- mm.moduleConfigsUpdateBeforeAmbiguos
				} else {
					log.Debugf("MODULE_MANAGER_RUN Retry IS NOT needed")
				}
			}
		}
	}()
}

func (mm *moduleManager) Retry() {
	mm.retryOnAmbiguous <- true
}

func (mm *moduleManager) Ch() chan Event {
	return mm.EventCh
}

// DiscoverModulesState handles DiscoverModulesState event: it calculates new arrays of enabled modules,
// modules that should be disabled and modules that should be purged.
//
// This method updates module state indices and values
// - mm.enabledModulesByConfig
// - mm.enabledModulesInOrder
// - mm.kubeModulesConfigValues are updated.
func (mm *moduleManager) DiscoverModulesState(logLabels map[string]string) (state *ModulesState, err error) {
	discoverLogLabels := utils.MergeLabels(logLabels, map[string]string{
		"operator.component": "moduleManager.discoverModulesState",
	})
	logEntry := log.WithFields(utils.LabelsToLogFields(discoverLogLabels))

	logEntry.Debugf("DISCOVER state current:\n"+
		"    mm.enabledModulesByConfig: %v\n"+
		"    mm.enabledModulesInOrder:  %v\n",
		mm.enabledModulesByConfig,
		mm.enabledModulesInOrder)

	currentEnabledModules := mm.enabledModulesInOrder

	updateEnabledModules, updateModuleValues, _ := mm.calculateEnabledModulesByConfig(mm.kubeConfigManager.CurrentConfig().ModuleConfigs)
	updateEnabledModules = utils.SortByReference(updateEnabledModules, mm.allModulesNamesInOrder)

	mm.enabledModulesByConfig = updateEnabledModules
	mm.kubeModulesConfigValues = updateModuleValues

	logEntry.Debugf("DISCOVER state updated:\n"+
		"    mm.enabledModulesByConfig: %v\n"+
		"    mm.enabledModulesInOrder:  %v\n",
		mm.enabledModulesByConfig,
		mm.enabledModulesInOrder)

	state = &ModulesState{
		EnabledModules:         []string{},
		ModulesToDisable:       []string{},
		ReleasedUnknownModules: []string{},
		NewlyEnabledModules:    []string{},
	}

	releasedModules, err := helm.NewClient(discoverLogLabels).ListReleasesNames(nil)
	if err != nil {
		return nil, err
	}

	// calculate unknown released modules to purge them in reverse order
	state.ReleasedUnknownModules = utils.ListSubtract(releasedModules, mm.allModulesNamesInOrder)
	// purge unknown modules in reverse order
	state.ReleasedUnknownModules = utils.SortReverse(state.ReleasedUnknownModules)
	if len(state.ReleasedUnknownModules) > 0 {
		logEntry.Infof("found modules with releases: %s", state.ReleasedUnknownModules)
	}

	// ignore unknown released modules for next operations
	releasedModules = utils.ListIntersection(releasedModules, mm.allModulesNamesInOrder)

	// modules finally enabled with enable script
	// no need to refresh mm.enabledModulesByConfig because
	// it is updated before in Init or in applyKubeUpdate
	logEntry.Debugf("Run enabled script for %+v", mm.enabledModulesByConfig)
	enabledModules, err := mm.RunModulesEnabledScript(mm.enabledModulesByConfig, logLabels)
	logEntry.Infof("Modules enabled by script: %+v", enabledModules)

	if err != nil {
		return nil, err
	}

	for _, moduleName := range enabledModules {
		if err = mm.RegisterModuleHooks(mm.allModulesByName[moduleName], logLabels); err != nil {
			return nil, err
		}
	}

	state.EnabledModules = enabledModules

	state.NewlyEnabledModules = utils.ListSubtract(enabledModules, mm.enabledModulesInOrder)
	// save enabled modules for future usages
	mm.enabledModulesInOrder = enabledModules

	// Calculate disabled known modules that has helm release and/or was enabled.
	// Sort them in reverse order for proper deletion.
	state.ModulesToDisable = utils.ListSubtract(mm.allModulesNamesInOrder, enabledModules)
	enabledAndReleased := utils.ListUnion(currentEnabledModules, releasedModules)
	state.ModulesToDisable = utils.ListIntersection(state.ModulesToDisable, enabledAndReleased)
	// disable modules in reverse order
	state.ModulesToDisable = utils.SortReverseByReference(state.ModulesToDisable, mm.allModulesNamesInOrder)

	logEntry.Debugf("DISCOVER state results:\n"+
		"    mm.enabledModulesByConfig: %v\n"+
		"    mm.enabledModulesInOrder: %v\n"+
		"    releasedModules: %v\n"+
		"    ReleasedUnknownModules: %v\n"+
		"    ModulesToDisable: %v\n"+
		"    NewlyEnabled: %v\n",
		mm.enabledModulesByConfig,
		mm.enabledModulesInOrder,
		releasedModules,
		state.ReleasedUnknownModules,
		state.ModulesToDisable,
		state.NewlyEnabledModules)
	return
}

// TODO replace with Module and ModuleShouldExists
func (mm *moduleManager) GetModule(name string) *Module {
	module, exist := mm.allModulesByName[name]
	if exist {
		return module
	} else {
		log.Errorf("Possible bug!!! GetModule: no module '%s' in ModuleManager indexes", name)
		return nil
	}
}

func (mm *moduleManager) GetModuleNamesInOrder() []string {
	return mm.enabledModulesInOrder
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

	moduleHookNamesMap := map[string]bool{}
	for _, moduleHooks := range moduleHooksByBinding {
		for _, moduleHook := range moduleHooks {
			moduleHookNamesMap[moduleHook.Name] = true
		}
	}

	var moduleHookNames []string
	for name := range moduleHookNamesMap {
		moduleHookNames = append(moduleHookNames, name)
	}

	return moduleHookNames
}

// TODO: moduleManager.GetModule(modName).Delete()
func (mm *moduleManager) DeleteModule(moduleName string, logLabels map[string]string) error {
	module := mm.GetModule(moduleName)

	// Stop kubernetes informers and remove scheduled functions
	mm.DisableModuleHooks(moduleName)

	if err := module.Delete(logLabels); err != nil {
		return err
	}

	// remove module hooks from indexes
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

	// ValuesLock.Lock()
	//
	beforeValues, err := mm.GlobalValues()
	if err != nil {
		return "", "", err
	}
	beforeChecksum, err := beforeValues.Checksum()
	if err != nil {
		return "", "", err
	}

	log.Debugf("RunGH: %s %s", hookName, binding)

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

	//	ValuesLock.Lock()
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

func (mm *moduleManager) RunModuleHook(hookName string, binding BindingType, bindingContext []BindingContext, logLabels map[string]string) error {
	moduleHook := mm.GetModuleHook(hookName)

	values, err := moduleHook.Module.Values()
	if err != nil {
		return err
	}
	valuesChecksum, err := values.Checksum()
	if err != nil {
		return err
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
		return err
	}

	newValues, err := moduleHook.Module.Values()
	if err != nil {
		return err
	}
	newValuesChecksum, err := newValues.Checksum()
	if err != nil {
		return err
	}

	if newValuesChecksum != valuesChecksum {
		switch binding {
		case Schedule, OnKubernetesEvent:
			mm.moduleValuesChanged <- moduleHook.Module.Name
		}
	}

	return nil
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
		res, _, err = utils.ApplyValuesPatch(res, patch)
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

/*

mm.GetModule(moduleName).HandleEnableKubernetesBindings(createTaskFn)

*/

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

func (mm *moduleManager) StartModuleHooks(moduleName string) {
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

//func (mm *moduleManager) EnableModuleScheduleBindings(moduleName) {
//
//}

//func (mm *moduleManager) EnableGlobalScheduleBindings(moduleName) {
//
//}

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

	modules := mm.enabledModulesInOrder

	for _, moduleName := range modules {
		m := mm.GetModule(moduleName)
		moduleHooks := mm.GetModuleHooksInOrder(moduleName, binding)
		for _, hookName := range moduleHooks {
			mh := mm.GetModuleHook(hookName)

			fn(nil, m, mh)
		}

	}
}

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

	log.Infof("dynamic enabled after patch: %s", mm.DumpDynamicEnabled())

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

func (mm *moduleManager) GlobalSynchronizationDone() bool {
	done := true
	for _, state := range mm.kubernetesBindingSynchronizationState {
		if !state.Done {
			done = false
		}
	}
	return done
}

func (mm *moduleManager) SynchronizationQueued(id string) {
	var state *KubernetesBindingSynchronizationState
	state, ok := mm.kubernetesBindingSynchronizationState[id]
	if !ok {
		state = &KubernetesBindingSynchronizationState{}
		mm.kubernetesBindingSynchronizationState[id] = state
	}
	state.Queued = true
}

func (mm *moduleManager) SynchronizationDone(id string) {
	var state *KubernetesBindingSynchronizationState
	state, ok := mm.kubernetesBindingSynchronizationState[id]
	if !ok {
		state = &KubernetesBindingSynchronizationState{}
		mm.kubernetesBindingSynchronizationState[id] = state
	}
	state.Done = true
}

func (mm *moduleManager) DumpState() {
	for id, state := range mm.kubernetesBindingSynchronizationState {
		log.Infof("%s: queue=%v done=%v", id, state.Queued, state.Done)
	}
}

// mergeEnabled merges enabled flags. Enabled flag can be nil.
//
// If all flags are nil, then false is returned — module is disabled by default.
//
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
