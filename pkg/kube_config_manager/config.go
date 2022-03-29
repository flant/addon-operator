package kube_config_manager

type KubeConfig struct {
	Global  *GlobalKubeConfig
	Modules map[string]*ModuleKubeConfig
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

func ParseConfigMapData(data map[string]string) (cfg *KubeConfig, err error) {
	cfg = NewConfig()
	// Parse values in global section.
	cfg.Global, err = GetGlobalKubeConfigFromConfigData(data)
	if err != nil {
		return nil, err
	}

	moduleNames, err := GetModulesNamesFromConfigData(data)
	if err != nil {
		return nil, err
	}

	for moduleName := range moduleNames {
		cfg.Modules[moduleName], err = ExtractModuleKubeConfig(moduleName, data)
		if err != nil {
			return nil, err
		}
	}

	return cfg, nil
}
