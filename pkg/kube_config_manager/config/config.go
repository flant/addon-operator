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
		switch v := gkc.Values["global"].(type) {
		case map[string]interface{}:
			return utils.Values(v)

		case utils.Values:
			return v
		}
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

type (
	KubeConfigType  string
	KubeConfigEvent struct {
		Type                          KubeConfigType
		ModuleEnabledStateChanged     []string
		ModuleValuesChanged           []string
		GlobalSectionChanged          bool
		ModuleMaintenanceStateChanged map[string]utils.MaintenanceState
	}
)

const (
	KubeConfigChanged KubeConfigType = "Changed"
	KubeConfigInvalid KubeConfigType = "Invalid"
)
