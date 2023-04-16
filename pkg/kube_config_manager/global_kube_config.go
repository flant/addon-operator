package kube_config_manager

import (
	"fmt"

	"github.com/flant/addon-operator/pkg/utils"
)

type GlobalKubeConfig struct {
	Values     utils.Values
	Checksum   string
	ConfigData map[string]string
}

func GetGlobalKubeConfigFromValues(values utils.Values) (*GlobalKubeConfig, error) {
	if !values.HasGlobal() {
		return nil, nil
	}

	globalValues := values.Global()

	configData, err := globalValues.AsConfigMapData()
	if err != nil {
		return nil, fmt.Errorf("cannot dump yaml for global kube config: %s. Failed values data: %#v", err, globalValues.DebugString())
	}

	checksum, err := globalValues.Checksum()
	if err != nil {
		return nil, fmt.Errorf("global kube config checksum: %s", err)
	}

	return &GlobalKubeConfig{
		Values:     globalValues,
		Checksum:   checksum,
		ConfigData: configData,
	}, nil
}

func GetGlobalKubeConfigFromConfigData(configData map[string]string) (*GlobalKubeConfig, error) {
	yamlData, hasKey := configData[utils.GlobalValuesKey]
	if !hasKey {
		return nil, nil
	}

	values, err := utils.NewGlobalValues(yamlData)
	if err != nil {
		return nil, fmt.Errorf("ConfigMap: bad yaml at key '%s': %s:\n%s", utils.GlobalValuesKey, err, yamlData)
	}

	checksum, err := values.Checksum()
	if err != nil {
		return nil, fmt.Errorf("ConfigMap: global kube config checksum: %s", err)
	}

	return &GlobalKubeConfig{
		ConfigData: map[string]string{utils.GlobalValuesKey: yamlData},
		Values:     values,
		Checksum:   checksum,
	}, nil
}
