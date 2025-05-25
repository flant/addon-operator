package kube_config_manager

import (
	"fmt"
	"log/slog"

	"github.com/deckhouse/deckhouse/pkg/log"

	"github.com/flant/addon-operator/pkg/kube_config_manager/config"
	"github.com/flant/addon-operator/pkg/utils"
)

// handleConfigError handles errors in config events
func (kcm *KubeConfigManager) handleConfigError(key string, err error) *config.KubeConfigEvent {
	// Do not update caches to detect changes on next update.
	kcm.logger.Error("Config invalid",
		slog.String("name", key),
		log.Err(err))
	return &config.KubeConfigEvent{
		Type: config.KubeConfigInvalid,
	}
}

// handleResetConfig handles config backend reset
func (kcm *KubeConfigManager) handleResetConfig() *config.KubeConfigEvent {
	// Let the caller update currentConfig with a new config
	return &config.KubeConfigEvent{
		Type: config.KubeConfigChanged,
	}
}

// handleGlobalConfig handles changes in global config section
// NOTE: This method must be called with kcm.mu locked as it accesses shared state.
func (kcm *KubeConfigManager) handleGlobalConfig(objConfig *config.KubeConfig) *config.KubeConfigEvent {
	globalChanged := kcm.isGlobalChanged(objConfig)
	// Let the caller update the state
	if globalChanged {
		return &config.KubeConfigEvent{
			Type:                 config.KubeConfigChanged,
			GlobalSectionChanged: globalChanged,
		}
	}
	return nil
}

// handleModuleDelete handles module deletion events
// NOTE: This method must be called with kcm.mu locked as it accesses shared state.
func (kcm *KubeConfigManager) handleModuleDelete(moduleName string) *config.KubeConfigEvent {
	kcm.logger.Info("Module section deleted", slog.String("moduleName", moduleName))

	modulesChanged := []string{moduleName}
	modulesStateChanged := []string{}
	moduleMaintenanceChanged := make(map[string]utils.Maintenance)

	if curr, ok := kcm.currentConfig.Modules[moduleName]; ok {
		if curr.GetEnabled() != "" && curr.GetEnabled() != "n/d" {
			modulesStateChanged = append(modulesStateChanged, moduleName)
		}
		if curr.GetMaintenanceState() == utils.NoResourceReconciliation {
			moduleMaintenanceChanged[moduleName] = utils.Managed
		}

		// Prepare the module config but let the caller update currentConfig
		moduleCfg := curr
		moduleCfg.Reset()
		moduleCfg.Checksum = moduleCfg.ModuleConfig.Checksum()
		// The caller will update kcm.currentConfig.Modules[moduleName] = moduleCfg
	}

	return &config.KubeConfigEvent{
		Type:                      config.KubeConfigChanged,
		ModuleValuesChanged:       modulesChanged,
		ModuleEnabledStateChanged: modulesStateChanged,
		ModuleMaintenanceChanged:  moduleMaintenanceChanged,
	}
}

// handleModuleUpdate handles module update events
// NOTE: This method must be called with kcm.mu locked as it accesses shared state.
func (kcm *KubeConfigManager) handleModuleUpdate(moduleName string, moduleCfg *config.ModuleKubeConfig) *config.KubeConfigEvent {
	modulesChanged := []string{}
	modulesStateChanged := []string{}
	moduleMaintenanceChanged := make(map[string]utils.Maintenance)

	// Use atomic operation for checksum checking and removal to avoid race conditions
	hasKnownChecksum := kcm.knownChecksums.HasEqualChecksum(moduleName, moduleCfg.Checksum)
	if hasKnownChecksum {
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
		// Let the caller update currentConfig
		return &config.KubeConfigEvent{
			Type:                      config.KubeConfigChanged,
			ModuleValuesChanged:       modulesChanged,
			ModuleEnabledStateChanged: modulesStateChanged,
			ModuleMaintenanceChanged:  moduleMaintenanceChanged,
		}
	}

	return nil
}

// processBatchDeletedModules processes modules that were deleted during a batch update
// NOTE: This method must be called with kcm.m locked as it accesses shared state.
func (kcm *KubeConfigManager) processBatchDeletedModules(
	currentModuleNames map[string]struct{},
) ([]string, []string, map[string]utils.Maintenance) {
	if len(currentModuleNames) == 0 {
		return nil, nil, nil
	}

	modulesChanged := []string{}
	modulesStateChanged := []string{}
	moduleMaintenanceChanged := make(map[string]utils.Maintenance)

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
		slog.String("modules", formatModuleNames(getModulesFromMap(currentModuleNames))))

	return modulesChanged, modulesStateChanged, moduleMaintenanceChanged
}

// getModulesFromMap extracts module names from the map
func getModulesFromMap(modules map[string]struct{}) []string {
	names := make([]string, 0, len(modules))
	for name := range modules {
		names = append(names, name)
	}
	return names
}

// formatModuleNames formats a slice of module names for logging
func formatModuleNames(names []string) string {
	return fmt.Sprintf("%+v", names)
}
