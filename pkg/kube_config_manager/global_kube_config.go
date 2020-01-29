package kube_config_manager

import (
	"fmt"
	"gopkg.in/yaml.v2"

	utils_checksum "github.com/flant/shell-operator/pkg/utils/checksum"

	"github.com/flant/addon-operator/pkg/utils"
)

type GlobalKubeConfig struct {
	Values     utils.Values
	Checksum   string
	ConfigData map[string]string
}

func GetGlobalKubeConfigFromValues(values utils.Values) *GlobalKubeConfig {
	globalValues, hasKey := values[utils.GlobalValuesKey]
	if !hasKey {
		return nil
	}

	yamlData, err := yaml.Marshal(&globalValues)
	if err != nil {
		panic(fmt.Sprintf("cannot dump yaml for global kube config: %s\nfailed values data: %#v", err, globalValues))
	}

	// FIXME checksum is calculated over yaml representation, but other code
	// uses json representation!
	return &GlobalKubeConfig{
		Values:     utils.Values{utils.GlobalValuesKey: globalValues},
		Checksum:   utils_checksum.CalculateChecksum(string(yamlData)),
		ConfigData: map[string]string{utils.GlobalValuesKey: string(yamlData)},
	}
}

func GetGlobalKubeConfigFromConfigData(configData map[string]string) (*GlobalKubeConfig, error) {
	yamlData, hasKey := configData[utils.GlobalValuesKey]
	if !hasKey {
		return nil, nil
	}

	values, err := utils.NewGlobalValues(yamlData)
	if err != nil {
		return nil, fmt.Errorf("ConfigMap: bad yaml at key '%s': %s:\n%s", utils.GlobalValuesKey, err, string(yamlData))
	}

	return &GlobalKubeConfig{
		ConfigData: map[string]string{utils.GlobalValuesKey: yamlData},
		Values:     values,
		Checksum:   utils_checksum.CalculateChecksum(yamlData),
	}, nil
}
