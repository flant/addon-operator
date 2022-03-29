package kube_config_manager

import (
	"fmt"
	"strings"

	"github.com/flant/addon-operator/pkg/utils"
)

// GetModulesNamesFromConfigData returns all keys in kube config except global
// modNameEnabled keys are also handled
func GetModulesNamesFromConfigData(configData map[string]string) (map[string]bool, error) {
	res := make(map[string]bool)

	for key := range configData {
		// Ignore global section.
		if key == utils.GlobalValuesKey {
			continue
		}

		// Treat Enabled flags as module section.
		key = strings.TrimSuffix(key, "Enabled")

		modName := utils.ModuleNameFromValuesKey(key)

		if utils.ModuleNameToValuesKey(modName) != key {
			return nil, fmt.Errorf("bad module name '%s': should be camelCased", key)
		}
		res[modName] = true
	}

	return res, nil
}

type ModuleKubeConfig struct {
	utils.ModuleConfig
	Checksum   string
	ConfigData map[string]string
}

func (m *ModuleKubeConfig) GetEnabled() string {
	if m == nil {
		return ""
	}
	return m.ModuleConfig.GetEnabled()
}

func GetModuleKubeConfigFromValues(moduleName string, values utils.Values) (*ModuleKubeConfig, error) {
	valuesKey := utils.ModuleNameToValuesKey(moduleName)
	if !values.HasKey(valuesKey) {
		return nil, nil
	}

	moduleValues := values.SectionByKey(valuesKey)

	configData, err := moduleValues.AsConfigMapData()
	if err != nil {
		return nil, fmt.Errorf("cannot dump yaml for module '%s' kube config: %s. Failed values data: %s", moduleName, err, moduleValues.DebugString())
	}

	checksum, err := moduleValues.Checksum()
	if err != nil {
		return nil, fmt.Errorf("module '%s' kube config checksum: %s", moduleName, err)
	}

	return &ModuleKubeConfig{
		ModuleConfig: utils.ModuleConfig{
			ModuleName: moduleName,
			Values:     moduleValues,
		},
		ConfigData: configData,
		Checksum:   checksum,
	}, nil
}

// ExtractModuleKubeConfig returns ModuleKubeConfig with values loaded from ConfigMap
func ExtractModuleKubeConfig(moduleName string, configData map[string]string) (*ModuleKubeConfig, error) {
	moduleConfig, err := utils.NewModuleConfig(moduleName).FromConfigMapData(configData)
	if err != nil {
		return nil, fmt.Errorf("bad yaml at key '%s': %s", utils.ModuleNameToValuesKey(moduleName), err)
	}
	// NOTE this should never happen because of GetModulesNamesFromConfigData
	if moduleConfig == nil {
		return nil, fmt.Errorf("possible bug!!! No section '%s' for module '%s'", utils.ModuleNameToValuesKey(moduleName), moduleName)
	}

	return &ModuleKubeConfig{
		ModuleConfig: *moduleConfig,
		Checksum:     moduleConfig.Checksum(),
	}, nil
}
