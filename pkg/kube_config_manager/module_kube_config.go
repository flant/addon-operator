package kube_config_manager

import (
	"fmt"
	"strings"

	"github.com/flant/addon-operator/pkg/utils"
	log "github.com/sirupsen/logrus"
)

// TODO make a method of KubeConfig
// TODO LOG: multierror?
// GetModulesNamesFromConfigData returns all keys in kube config except global
// modNameEnabled keys are also handled
func GetModulesNamesFromConfigData(configData map[string]string) map[string]bool {
	res := make(map[string]bool, 0)

	for key := range configData {
		if key == utils.GlobalValuesKey {
			continue
		}

		if strings.HasSuffix(key, "Enabled") {
			key = strings.TrimSuffix(key, "Enabled")
		}

		modName := utils.ModuleNameFromValuesKey(key)

		if utils.ModuleNameToValuesKey(modName) != key {
			log.Errorf("Bad module name '%s': should be camelCased module name: ignoring data", key)
			continue
		}
		res[modName] = true
	}

	return res
}

type ModuleKubeConfig struct {
	utils.ModuleConfig
	Checksum   string
	ConfigData map[string]string
}

// TODO make a method of KubeConfig
func GetModuleKubeConfigFromValues(moduleName string, values utils.Values) (*ModuleKubeConfig, error) {
	if !values.HasKey(moduleName) {
		return nil, nil
	}

	moduleValues := values.SectionByKey(moduleName)

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

// TODO make a method of KubeConfig
// ExtractModuleKubeConfig returns ModuleKubeConfig with values loaded from ConfigMap
func ExtractModuleKubeConfig(moduleName string, configData map[string]string) (*ModuleKubeConfig, error) {
	moduleConfig, err := utils.NewModuleConfig(moduleName).FromConfigMapData(configData)
	if err != nil {
		return nil, fmt.Errorf("ConfigMap: bad yaml at key '%s': %s", utils.ModuleNameToValuesKey(moduleName), err)
	}
	// NOTE this should never happen because of GetModulesNamesFromConfigData
	if moduleConfig == nil {
		return nil, fmt.Errorf("possible bug!!! Kube config for module '%s' is not found in ConfigMap.data", moduleName)
	}

	return &ModuleKubeConfig{
		ModuleConfig: *moduleConfig,
		Checksum:     moduleConfig.Checksum(),
	}, nil
}
