package kube_config_manager

import (
	"fmt"
	"strings"

	"github.com/flant/addon-operator/pkg/utils"
	utils_checksum "github.com/flant/shell-operator/pkg/utils/checksum"
	"github.com/romana/rlog"
	"gopkg.in/yaml.v2"
)

// TODO make a method of KubeConfig
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
			rlog.Errorf("Bad module name '%s': should be camelCased module name: ignoring data", key)
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
func GetModuleKubeConfigFromValues(moduleName string, values utils.Values) *ModuleKubeConfig {
	mc := utils.NewModuleConfig(moduleName)

	moduleValues, hasKey := values[mc.ModuleConfigKey]
	if !hasKey {
		return nil
	}

	yamlData, err := yaml.Marshal(&moduleValues)
	if err != nil {
		panic(fmt.Sprintf("cannot dump yaml for module '%s' kube config: %s\nfailed values data: %#v", moduleName, err, moduleValues))
	}


	return &ModuleKubeConfig{
		ModuleConfig: utils.ModuleConfig{
			ModuleName: moduleName,
			Values:     utils.Values{mc.ModuleConfigKey: moduleValues},
		},
		ConfigData: map[string]string{mc.ModuleConfigKey: string(yamlData)},
		Checksum:   utils_checksum.CalculateChecksum(string(yamlData)),
	}
}

// TODO make a method of KubeConfig
// ExtractModuleKubeConfig returns ModuleKubeConfig with values loaded from ConfigMap
func ExtractModuleKubeConfig(moduleName string, configData map[string]string) (*ModuleKubeConfig, error) {
	moduleConfig, err := utils.NewModuleConfig(moduleName).FromKeyYamls(configData)
	if err != nil {
		return nil, fmt.Errorf("'%s' ConfigMap bad yaml at key '%s': %s", ConfigMapName, utils.ModuleNameToValuesKey(moduleName), err)
	}
	// NOTE this should never happen because of GetModulesNamesFromConfigData
	if moduleConfig == nil {
		panic("module kube config must exist!")
	}

	return &ModuleKubeConfig{
		ModuleConfig: *moduleConfig,
		Checksum:     moduleConfig.Checksum(),
	}, nil
}
