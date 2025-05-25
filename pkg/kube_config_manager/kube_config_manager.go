package kube_config_manager

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"strconv"
	"sync"

	"github.com/deckhouse/deckhouse/pkg/log"

	"github.com/flant/addon-operator/pkg/kube_config_manager/backend"
	"github.com/flant/addon-operator/pkg/kube_config_manager/config"
	"github.com/flant/addon-operator/pkg/utils"
	runtimeConfig "github.com/flant/shell-operator/pkg/config"
)

// KubeConfigManager watches for changes in ConfigMap/addon-operator and provides
// methods to change its content.
// It stores values parsed from ConfigMap data. OpenAPI validation of these config values
// is not a responsibility of this component.
type KubeConfigManager struct {
	ctx    context.Context
	cancel context.CancelFunc

	logger *log.Logger

	// Checksums to ignore self-initiated updates.
	knownChecksums *Checksums

	// Channel to emit events.
	configEventCh chan config.KubeConfigEvent
	backend       backend.ConfigHandler

	m             sync.Mutex
	currentConfig *config.KubeConfig
}

func NewKubeConfigManager(ctx context.Context, bk backend.ConfigHandler, runtimeConfig *runtimeConfig.Config, logger *log.Logger) *KubeConfigManager {
	cctx, cancel := context.WithCancel(ctx)
	logger = logger.With("component", "KubeConfigManager")
	logger.With("backend", fmt.Sprintf("%T", bk)).Info("Setup KubeConfigManager backend")

	// Runtime config to enable logging all events from the ConfigMap at runtime.
	if runtimeConfig != nil {
		runtimeConfig.Register(
			"log.configmap.events",
			fmt.Sprintf("Set to true to log all operations with Configuration manager/%s", reflect.TypeOf(bk)),
			"false",
			func(_ string, newValue string) error {
				val, err := strconv.ParseBool(newValue)
				if err != nil {
					return err
				}
				//nolint: revive,staticcheck // basic type is enough here
				cctx = context.WithValue(cctx, "kube-config-manager-debug", val)
				return nil
			},
			nil,
		)
	}

	return &KubeConfigManager{
		ctx:    cctx,
		cancel: cancel,

		currentConfig:  config.NewConfig(),
		knownChecksums: NewChecksums(),
		configEventCh:  make(chan config.KubeConfigEvent, 1),
		logger:         logger,
		backend:        bk,
	}
}

func (kcm *KubeConfigManager) IsModuleEnabled(moduleName string) *bool {
	kcm.m.Lock()
	defer kcm.m.Unlock()
	moduleConfig, found := kcm.currentConfig.Modules[moduleName]
	if !found {
		return nil
	}

	return moduleConfig.IsEnabled
}

func (kcm *KubeConfigManager) Init() error {
	kcm.logger.Debug("Init: KubeConfigManager")

	// Load config and calculate checksums at start.
	err := kcm.loadConfig()
	if err != nil {
		return err
	}

	return nil
}

// SaveConfigValues updates `global` or `module` section in ConfigMap.
// It uses knownChecksums to prevent KubeConfigChanged event on self-update.
func (kcm *KubeConfigManager) SaveConfigValues(key string, values utils.Values) error {
	checksum, err := kcm.backend.SaveConfigValues(kcm.ctx, key, values)
	if err != nil {
		kcm.withLock(func() {
			kcm.knownChecksums.Remove(key, checksum)
		})

		return err
	}

	kcm.withLock(func() {
		kcm.knownChecksums.Add(key, checksum)
	})

	return nil
}

// KubeConfigEventCh return a channel that emits new KubeConfig on ConfigMap changes in global section or enabled modules.
func (kcm *KubeConfigManager) KubeConfigEventCh() chan config.KubeConfigEvent {
	return kcm.configEventCh
}

// UpdateModuleConfig updates a single module config
func (kcm *KubeConfigManager) UpdateModuleConfig(moduleName string) error {
	// Load config outside the lock to reduce contention
	newModuleConfig, err := kcm.backend.LoadConfig(kcm.ctx, moduleName)
	if err != nil {
		return err
	}

	kcm.m.Lock()
	defer kcm.m.Unlock()
	if moduleConfig, found := newModuleConfig.Modules[moduleName]; found {
		kcm.knownChecksums.Set(moduleName, moduleConfig.Checksum)
		kcm.currentConfig.Modules[moduleName] = moduleConfig
	}

	return nil
}

// loadConfig gets config from ConfigMap before starting informer.
// Set checksums for global section and modules.
func (kcm *KubeConfigManager) loadConfig() error {
	newConfig, err := kcm.backend.LoadConfig(kcm.ctx)
	if err != nil {
		return err
	}

	// Protect access to shared state with mutex
	kcm.m.Lock()
	defer kcm.m.Unlock()

	if newConfig.Global != nil {
		kcm.knownChecksums.Set(utils.GlobalValuesKey, newConfig.Global.Checksum)
	}

	for moduleName, moduleConfig := range newConfig.Modules {
		kcm.knownChecksums.Set(moduleName, moduleConfig.Checksum)
	}

	kcm.currentConfig = newConfig
	return nil
}

// isGlobalChanged returns true when changes in "global" section require firing event.
// NOTE: This method must be called with kcm.m locked as it accesses shared state.
func (kcm *KubeConfigManager) isGlobalChanged(newConfig *config.KubeConfig) bool {
	if newConfig.Global == nil {
		// Fire event when global section is deleted: ConfigMap has no global section but global config is cached.
		// Note: no checksum checking here, "save" operations can't delete global section.
		if kcm.currentConfig.Global != nil {
			kcm.logger.Info("Global section deleted")
			return true
		}
		kcm.logger.Debug("Global section is empty")
		return false
	}

	newChecksum := newConfig.Global.Checksum
	// Global section is updated if a new checksum not equal to the saved one and not in knownChecksum.
	if kcm.knownChecksums.HasEqualChecksum(utils.GlobalValuesKey, newChecksum) {
		// Remove known checksum, do not fire event on self-update.
		kcm.knownChecksums.Remove(utils.GlobalValuesKey, newChecksum)
		kcm.logger.Debug("Global section self-update")
		return false
	}

	if kcm.currentConfig.Global == nil {
		// "global" section is added after initialization.
		kcm.logger.Info("Global section added")
		return true
	}
	// Consider "global" change when new checksum is not equal to the saved.
	if kcm.currentConfig.Global.Checksum != newChecksum {
		kcm.logger.Info("Global section updated")
		return true
	}

	return false
}

// sendEventIfNeeded sends the event on the channel if it's not nil
func (kcm *KubeConfigManager) sendEventIfNeeded(eventToSend *config.KubeConfigEvent) {
	if eventToSend != nil {
		kcm.configEventCh <- *eventToSend
	}
}

// handleConfigEvent determine changes in kube config. It sends KubeConfigChanged event if something
// changed or KubeConfigInvalid event if Config is incorrect.
func (kcm *KubeConfigManager) handleConfigEvent(obj config.Event) {
	var eventToSend *config.KubeConfigEvent

	// Lock to protect access to currentConfig and knownChecksums
	kcm.m.Lock()
	if obj.Err != nil {
		// Do not update caches to detect changes on next update.
		eventToSend = &config.KubeConfigEvent{
			Type: config.KubeConfigInvalid,
		}
		kcm.logger.Error("Config invalid",
			slog.String("name", obj.Key),
			log.Err(obj.Err))
		kcm.m.Unlock()
		kcm.sendEventIfNeeded(eventToSend)
		return
	}

	switch obj.Key {
	case "":
		// Config backend was reset
		kcm.currentConfig = config.NewConfig()
		eventToSend = &config.KubeConfigEvent{
			Type: config.KubeConfigChanged,
		}
	case utils.GlobalValuesKey:
		// global values
		globalChanged := kcm.isGlobalChanged(obj.Config)
		// Update state after successful parsing.
		kcm.currentConfig.Global = obj.Config.Global
		if globalChanged {
			eventToSend = &config.KubeConfigEvent{
				Type:                 config.KubeConfigChanged,
				GlobalSectionChanged: globalChanged,
			}
		}
	default:
		// some module values
		modulesChanged := []string{}
		modulesStateChanged := []string{}
		moduleMaintenanceChanged := make(map[string]utils.Maintenance)

		// module update
		moduleName := obj.Key
		moduleCfg := obj.Config.Modules[obj.Key]
		if obj.Op == config.EventDelete {
			kcm.logger.Info("Module section deleted", slog.String("moduleName", moduleName))
			modulesChanged = append(modulesChanged, moduleName)
			if curr, ok := kcm.currentConfig.Modules[moduleName]; ok {
				if curr.GetEnabled() != "" && curr.GetEnabled() != "n/d" {
					modulesStateChanged = append(modulesStateChanged, moduleName)
				}
				if curr.GetMaintenanceState() == utils.NoResourceReconciliation {
					moduleMaintenanceChanged[moduleName] = utils.Managed
				}
			}
			moduleCfg.Reset()
			moduleCfg.Checksum = moduleCfg.ModuleConfig.Checksum()
			kcm.currentConfig.Modules[obj.Key] = moduleCfg
			eventToSend = &config.KubeConfigEvent{
				Type:                      config.KubeConfigChanged,
				ModuleValuesChanged:       modulesChanged,
				ModuleEnabledStateChanged: modulesStateChanged,
				ModuleMaintenanceChanged:  moduleMaintenanceChanged,
			}
			kcm.m.Unlock()
			kcm.sendEventIfNeeded(eventToSend)
			return
		}
		// Module section is changed if a new checksum doesn't equal to saved one and isn't in known checksums, or the module new state doesn't equal to the previous one.
		if kcm.knownChecksums.HasEqualChecksum(moduleName, moduleCfg.Checksum) {
			// Remove known checksum, do not fire event on self-update.
			kcm.knownChecksums.Remove(moduleName, moduleCfg.Checksum)
		} else {
			if currModuleCfg, has := kcm.currentConfig.Modules[moduleName]; has {
				if currModuleCfg.Checksum != moduleCfg.Checksum {
					modulesChanged = append(modulesChanged, moduleName)
				}
				if currModuleCfg.GetEnabled() != moduleCfg.GetEnabled() {
					modulesStateChanged = append(modulesStateChanged, moduleName)
				}
				if currModuleCfg.GetMaintenanceState() != moduleCfg.GetMaintenanceState() {
					moduleMaintenanceChanged[moduleName] = moduleCfg.GetMaintenanceState()
				}
				kcm.logger.Info("Module section changed. Enabled flag transition.",
					slog.String("moduleName", moduleName),
					slog.String("previous", currModuleCfg.GetEnabled()),
					slog.String("current", moduleCfg.GetEnabled()),
					slog.String("maintenanceFlag", moduleCfg.GetMaintenanceState().String()))
			} else {
				modulesChanged = append(modulesChanged, moduleName)
				if moduleCfg.GetEnabled() != "" && moduleCfg.GetEnabled() != "n/d" {
					modulesStateChanged = append(modulesStateChanged, moduleName)
				}

				if moduleCfg.GetMaintenanceState() == utils.NoResourceReconciliation {
					moduleMaintenanceChanged[moduleName] = utils.NoResourceReconciliation
				}
				kcm.logger.Info("Module section added",
					slog.String("moduleName", moduleName),
					slog.String("enabledFlag", moduleCfg.GetEnabled()),
					slog.String("maintenanceFlag", moduleCfg.GetMaintenanceState().String()))
			}
		}

		if len(modulesChanged)+len(modulesStateChanged)+len(moduleMaintenanceChanged) > 0 {
			kcm.currentConfig.Modules[obj.Key] = moduleCfg
			eventToSend = &config.KubeConfigEvent{
				Type:                      config.KubeConfigChanged,
				ModuleValuesChanged:       modulesChanged,
				ModuleEnabledStateChanged: modulesStateChanged,
				ModuleMaintenanceChanged:  moduleMaintenanceChanged,
			}
		}
	}
	kcm.m.Unlock()
	kcm.sendEventIfNeeded(eventToSend)
}

func (kcm *KubeConfigManager) handleBatchConfigEvent(obj config.Event) {
	if obj.Err != nil {
		// Do not update caches to detect changes on next update.
		kcm.configEventCh <- config.KubeConfigEvent{
			Type: config.KubeConfigInvalid,
		}
		kcm.logger.Error("Batch Config invalid", log.Err(obj.Err))
		return
	}

	// Lock to read known checksums and update config.
	kcm.m.Lock()

	if obj.Key == "" {
		// Config backend was reset
		kcm.currentConfig = config.NewConfig()
		eventToSend := config.KubeConfigEvent{
			Type: config.KubeConfigChanged,
		}
		kcm.m.Unlock()
		kcm.configEventCh <- eventToSend
		return
	}

	newConfig := obj.Config

	globalChanged := kcm.isGlobalChanged(newConfig)

	// Parse values in module sections, create new ModuleConfigs and checksums map.
	currentModuleNames := make(map[string]struct{})
	for name := range kcm.currentConfig.Modules {
		currentModuleNames[name] = struct{}{}
	}
	modulesChanged := []string{}
	modulesStateChanged := []string{}
	moduleMaintenanceChanged := make(map[string]utils.Maintenance)

	for moduleName, moduleCfg := range newConfig.Modules {
		// Remove module name from current names to detect deleted sections.
		delete(currentModuleNames, moduleName)

		// Module section is changed if new checksum not equal to saved one and not in known checksums.
		// Module section is changed if a new checksum doesn't equal to the saved one and isn't in known checksums, or the module new state doesn't equal to the previous one.
		if kcm.knownChecksums.HasEqualChecksum(moduleName, moduleCfg.Checksum) {
			// Remove known checksum, do not fire event on self-update.
			kcm.knownChecksums.Remove(moduleName, moduleCfg.Checksum)
		} else {
			if currModuleCfg, has := kcm.currentConfig.Modules[moduleName]; has {
				if currModuleCfg.Checksum != moduleCfg.Checksum {
					modulesChanged = append(modulesChanged, moduleName)
				}
				if currModuleCfg.GetEnabled() != moduleCfg.GetEnabled() {
					modulesStateChanged = append(modulesStateChanged, moduleName)
				}
				if currModuleCfg.GetMaintenanceState() != moduleCfg.GetMaintenanceState() {
					moduleMaintenanceChanged[moduleName] = moduleCfg.GetMaintenanceState()
				}
				kcm.logger.Info("Module section changed. Enabled flag transition",
					slog.String("moduleName", moduleName),
					slog.String("previous", currModuleCfg.GetEnabled()),
					slog.String("current", moduleCfg.GetEnabled()),
					slog.String("maintenanceFlag", moduleCfg.GetMaintenanceState().String()))
			} else {
				modulesChanged = append(modulesChanged, moduleName)
				if moduleCfg.GetEnabled() != "" && moduleCfg.GetEnabled() != "n/d" {
					modulesStateChanged = append(modulesStateChanged, moduleName)
				}

				if moduleCfg.GetMaintenanceState() == utils.NoResourceReconciliation {
					moduleMaintenanceChanged[moduleName] = utils.NoResourceReconciliation
				}

				kcm.logger.Info("Module section added",
					slog.String("moduleName", moduleName),
					slog.String("enabledFlag", moduleCfg.GetEnabled()),
					slog.String("maintenanceFlag", moduleCfg.GetMaintenanceState().String()))
			}
		}
	}

	// currentModuleNames now contains deleted module sections.
	if len(currentModuleNames) > 0 {
		for moduleName := range currentModuleNames {
			modulesChanged = append(modulesChanged, moduleName)
			if curr, ok := kcm.currentConfig.Modules[moduleName]; ok {
				if curr.GetEnabled() != "" && curr.GetEnabled() != "n/d" {
					modulesStateChanged = append(modulesStateChanged, moduleName)
				}
				if curr.GetMaintenanceState() == utils.NoResourceReconciliation {
					moduleMaintenanceChanged[moduleName] = utils.Managed
				}
			}
		}
		kcm.logger.Info("Module sections deleted",
			slog.String("modules", fmt.Sprintf("%+v", currentModuleNames)))
	}

	// Update state after successful parsing.
	kcm.currentConfig = newConfig

	// Prepare event data while holding the lock
	var eventToSend *config.KubeConfigEvent
	if globalChanged || len(modulesChanged)+len(modulesStateChanged)+len(moduleMaintenanceChanged) > 0 {
		eventToSend = &config.KubeConfigEvent{
			Type:                      config.KubeConfigChanged,
			GlobalSectionChanged:      globalChanged,
			ModuleValuesChanged:       modulesChanged,
			ModuleEnabledStateChanged: modulesStateChanged,
			ModuleMaintenanceChanged:  moduleMaintenanceChanged,
		}
	}

	// Unlock before sending event to prevent deadlock
	kcm.m.Unlock()
	kcm.sendEventIfNeeded(eventToSend)
}

func (kcm *KubeConfigManager) Start() {
	kcm.logger.Debug("Start kube config manager")

	go kcm.start()
}

func (kcm *KubeConfigManager) start() {
	eventC := make(chan config.Event, 100)

	kcm.backend.StartInformer(kcm.ctx, eventC)

	for {
		select {
		case event := <-eventC:
			if event.Key == "batch" {
				kcm.handleBatchConfigEvent(event)
			} else {
				kcm.handleConfigEvent(event)
			}

		case <-kcm.ctx.Done():
			kcm.logger.Debug("Stop kube config manager")
			return
		}
	}
}

func (kcm *KubeConfigManager) Stop() {
	if kcm.cancel != nil {
		kcm.cancel()
	}
}

// SafeReadConfig locks currentConfig to safely read from it in external services.
func (kcm *KubeConfigManager) SafeReadConfig(handler func(config *config.KubeConfig)) {
	if handler == nil {
		return
	}
	kcm.withLock(func() {
		handler(kcm.currentConfig)
	})
}

func (kcm *KubeConfigManager) withLock(fn func()) {
	if fn == nil {
		return
	}
	kcm.m.Lock()
	fn()
	kcm.m.Unlock()
}
