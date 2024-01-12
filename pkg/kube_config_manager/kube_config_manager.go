package kube_config_manager

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"sync"

	log "github.com/sirupsen/logrus"

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

	logEntry *log.Entry

	// Checksums to ignore self-initiated updates.
	knownChecksums *Checksums

	// Channel to emit events.
	configEventCh chan config.KubeConfigEvent
	backend       backend.ConfigHandler

	m             sync.Mutex
	currentConfig *config.KubeConfig
}

func NewKubeConfigManager(ctx context.Context, bk backend.ConfigHandler, runtimeConfig *runtimeConfig.Config) *KubeConfigManager {
	cctx, cancel := context.WithCancel(ctx)
	logger := log.WithField("component", "KubeConfigManager")
	logger.WithField("backend", fmt.Sprintf("%T", bk)).Infof("Setup KubeConfigManager backend")

	// Runtime config to enable logging all events from the ConfigMap at runtime.
	if runtimeConfig != nil {
		runtimeConfig.Register(
			"log.configmap.events",
			fmt.Sprintf("Set to true to log all operations with Configuration manager/%s", reflect.TypeOf(bk)),
			"false",
			func(oldValue string, newValue string) error {
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
		logEntry:       logger,
		backend:        bk,
	}
}

func (kcm *KubeConfigManager) IsModuleEnabled(moduleName string) bool {
	moduleConfig, found := kcm.currentConfig.Modules[moduleName]
	if !found {
		return false
	}

	return moduleConfig.IsEnabled != nil && *moduleConfig.IsEnabled
}

func (kcm *KubeConfigManager) Init() error {
	kcm.logEntry.Debug("Init: KubeConfigManager")

	// Load config and calculate checksums at start. No locking required.
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
	newModuleConfig, err := kcm.backend.LoadConfig(kcm.ctx, moduleName)
	if err != nil {
		return err
	}

	if moduleConfig, found := newModuleConfig.Modules[moduleName]; found {
		if kcm.knownChecksums != nil {
			kcm.knownChecksums.Set(moduleName, moduleConfig.Checksum)
		}

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

	if newConfig.Global != nil {
		kcm.knownChecksums.Set(utils.GlobalValuesKey, newConfig.Global.Checksum)
	}

	for moduleName, moduleConfig := range newConfig.Modules {
		kcm.knownChecksums.Set(moduleName, moduleConfig.Checksum)
	}

	kcm.currentConfig = newConfig
	return nil
}

// currentModuleNames gather modules names from the checksums map and from the currentConfig struct.
func (kcm *KubeConfigManager) currentModuleNames() map[string]struct{} {
	names := make(map[string]struct{})
	for name := range kcm.currentConfig.Modules {
		names[name] = struct{}{}
	}
	return names
}

// isGlobalChanged returns true when changes in "global" section requires firing event.
func (kcm *KubeConfigManager) isGlobalChanged(newConfig *config.KubeConfig) bool {
	if newConfig.Global == nil {
		// Fire event when global section is deleted: ConfigMap has no global section but global config is cached.
		// Note: no checksum checking here, "save" operations can't delete global section.
		if kcm.currentConfig.Global != nil {
			kcm.logEntry.Infof("Global section deleted")
			return true
		}
		kcm.logEntry.Debugf("Global section is empty")
		return false
	}

	newChecksum := newConfig.Global.Checksum
	// Global section is updated if a new checksum not equal to the saved one and not in knownChecksum.
	if kcm.knownChecksums.HasEqualChecksum(utils.GlobalValuesKey, newChecksum) {
		// Remove known checksum, do not fire event on self-update.
		kcm.knownChecksums.Remove(utils.GlobalValuesKey, newChecksum)
		kcm.logEntry.Debugf("Global section self-update")
		return false
	}

	if kcm.currentConfig.Global == nil {
		// "global" section is added after initialization.
		kcm.logEntry.Infof("Global section added")
		return true
	}
	// Consider "global" change when new checksum is not equal to the saved.
	if kcm.currentConfig.Global.Checksum != newChecksum {
		kcm.logEntry.Infof("Global section updated")
		return true
	}

	return false
}

// handleConfigEvent determine changes in kube config. It sends KubeConfigChanged event if something
// changed or KubeConfigInvalid event if Config is incorrect.
func (kcm *KubeConfigManager) handleConfigEvent(obj config.Event) {
	if obj.Err != nil {
		// Do not update caches to detect changes on next update.
		kcm.configEventCh <- config.KubeConfigInvalid
		kcm.logEntry.Errorf("Config/%s invalid: %v", obj.Key, obj.Err)
		return
	}

	switch obj.Key {
	case "":
		// Config backend was reset
		kcm.m.Lock()
		kcm.currentConfig = config.NewConfig()
		kcm.m.Unlock()
		kcm.configEventCh <- config.KubeConfigChanged

	case utils.GlobalValuesKey:
		// global values

		kcm.m.Lock()
		globalChanged := kcm.isGlobalChanged(obj.Config)
		// Update state after successful parsing.
		kcm.currentConfig.Global = obj.Config.Global
		kcm.m.Unlock()
		if globalChanged {
			kcm.configEventCh <- config.KubeConfigChanged
		}

	default:
		// some module values
		modulesChanged := false

		// module update
		kcm.m.Lock()
		defer kcm.m.Unlock()
		moduleName := obj.Key
		moduleCfg := obj.Config.Modules[obj.Key]
		if obj.Op == config.EventDelete {
			kcm.logEntry.Infof("Module section deleted: %+v", moduleName)
			moduleCfg.DropValues()
			moduleCfg.Checksum = moduleCfg.ModuleConfig.Checksum()
			kcm.currentConfig.Modules[obj.Key] = moduleCfg
			kcm.configEventCh <- config.KubeConfigChanged
			return
		}
		// Module section is changed if new checksum not equal to saved one and not in known checksums.
		if kcm.knownChecksums.HasEqualChecksum(moduleName, moduleCfg.Checksum) {
			// Remove known checksum, do not fire event on self-update.
			kcm.knownChecksums.Remove(moduleName, moduleCfg.Checksum)
		} else {
			if currModuleCfg, has := kcm.currentConfig.Modules[moduleName]; has {
				if currModuleCfg.Checksum != moduleCfg.Checksum {
					modulesChanged = true
					kcm.logEntry.Infof("Module section '%s' changed. Enabled flag transition: %s--%s",
						moduleName,
						kcm.currentConfig.Modules[moduleName].GetEnabled(),
						moduleCfg.GetEnabled(),
					)
				}
			} else {
				modulesChanged = true
				kcm.logEntry.Infof("Module section '%s' added. Enabled flag: %s", moduleName, moduleCfg.GetEnabled())
			}
		}

		if modulesChanged {
			kcm.currentConfig.Modules[obj.Key] = moduleCfg
			kcm.configEventCh <- config.KubeConfigChanged
		}
	}
}

func (kcm *KubeConfigManager) handleBatchConfigEvent(obj config.Event) {
	if obj.Err != nil {
		// Do not update caches to detect changes on next update.
		kcm.configEventCh <- config.KubeConfigInvalid
		kcm.logEntry.Errorf("Batch Config invalid: %v", obj.Err)
		return
	}

	if obj.Key == "" {
		// Config backend was reset
		kcm.m.Lock()
		kcm.currentConfig = config.NewConfig()
		kcm.m.Unlock()
		kcm.configEventCh <- config.KubeConfigChanged
	}

	newConfig := obj.Config

	// Lock to read known checksums and update config.
	kcm.m.Lock()

	globalChanged := kcm.isGlobalChanged(newConfig)

	// Parse values in module sections, create new ModuleConfigs and checksums map.
	currentModuleNames := kcm.currentModuleNames()
	modulesChanged := false

	for moduleName, moduleCfg := range newConfig.Modules {
		// Remove module name from current names to detect deleted sections.
		delete(currentModuleNames, moduleName)

		// Module section is changed if new checksum not equal to saved one and not in known checksums.
		if kcm.knownChecksums.HasEqualChecksum(moduleName, moduleCfg.Checksum) {
			// Remove known checksum, do not fire event on self-update.
			kcm.knownChecksums.Remove(moduleName, moduleCfg.Checksum)
		} else {
			if currModuleCfg, has := kcm.currentConfig.Modules[moduleName]; has {
				if currModuleCfg.Checksum != moduleCfg.Checksum {
					modulesChanged = true
					kcm.logEntry.Infof("Module section '%s' changed. Enabled flag transition: %s--%s",
						moduleName,
						kcm.currentConfig.Modules[moduleName].GetEnabled(),
						moduleCfg.GetEnabled(),
					)
				}
			} else {
				modulesChanged = true
				kcm.logEntry.Infof("Module section '%s' added. Enabled flag: %s", moduleName, moduleCfg.GetEnabled())
			}
		}
	}

	// currentModuleNames now contains deleted module sections.
	if len(currentModuleNames) > 0 {
		modulesChanged = true
		kcm.logEntry.Infof("Module sections deleted: %+v", currentModuleNames)
	}

	// Update state after successful parsing.
	kcm.currentConfig = newConfig
	kcm.m.Unlock()

	// Fire event if ConfigMap has changes.
	if globalChanged || modulesChanged {
		kcm.configEventCh <- config.KubeConfigChanged
	}
}

func (kcm *KubeConfigManager) Start() {
	kcm.logEntry.Debugf("Start kube config manager")

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
			kcm.logEntry.Debugf("Stop kube config manager")
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
