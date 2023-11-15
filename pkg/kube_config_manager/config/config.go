package config

import (
	"github.com/flant/addon-operator/pkg/utils"
)

type KubeConfig struct {
	Global  *GlobalKubeConfig
	Modules map[string]*ModuleKubeConfig
}

type GlobalKubeConfig struct {
	Values   utils.Values
	Checksum string
}

// GetValues returns global values, enrich them with top level key 'global'
func (gkc GlobalKubeConfig) GetValues() utils.Values {
	if len(gkc.Values) == 0 {
		return gkc.Values
	}

	if gkc.Values.HasKey("global") {
		return gkc.Values["global"].(utils.Values)
	}

	return gkc.Values
}

type ModuleKubeConfig struct {
	utils.ModuleConfig
	Checksum string
}

func NewConfig() *KubeConfig {
	return &KubeConfig{
		Modules: make(map[string]*ModuleKubeConfig),
	}
}

type KubeConfigEvent string

const (
	KubeConfigChanged KubeConfigEvent = "Changed"
	KubeConfigInvalid KubeConfigEvent = "Invalid"
)

func ParseGlobalKubeConfigFromValues(values utils.Values) (*GlobalKubeConfig, error) {
	if !values.HasGlobal() {
		return nil, nil
	}

	globalValues := values.Global()

	checksum := globalValues.Checksum()

	return &GlobalKubeConfig{
		Values:   globalValues,
		Checksum: checksum,
	}, nil
}

func ParseModuleKubeConfigFromValues(moduleName string, values utils.Values) *ModuleKubeConfig {
	valuesKey := utils.ModuleNameToValuesKey(moduleName)
	if !values.HasKey(valuesKey) {
		return nil
	}

	//nolint: staticcheck
	moduleValues := values.SectionByKey(valuesKey)

	checksum := moduleValues.Checksum()

	return &ModuleKubeConfig{
		ModuleConfig: *utils.NewModuleConfig(moduleName, moduleValues),
		Checksum:     checksum,
	}
}
