package utils

import (
	"fmt"

	utils_checksum "github.com/flant/shell-operator/pkg/utils/checksum"
	"github.com/go-yaml/yaml"
)

type ModuleConfig struct {
	ModuleName string
	IsEnabled  bool
	Values     Values
	IsUpdated  bool
	ModuleConfigKey string
	ModuleEnabledKey string
	RawConfig []string
}

func (mc ModuleConfig) String() string {
	return fmt.Sprintf("Module(Name=%s IsEnabled=%v IsUpdated=%v Values:\n%s)", mc.ModuleName, mc.IsEnabled, mc.IsUpdated, ValuesToString(mc.Values))
}

func NewModuleConfig(moduleName string) *ModuleConfig {
	return &ModuleConfig{
		ModuleName: moduleName,
		IsEnabled:  true,
		Values:     make(Values),
		ModuleConfigKey: ModuleNameToValuesKey(moduleName),
		ModuleEnabledKey: ModuleNameToValuesKey(moduleName) + "Enabled",
		RawConfig: make([]string, 0),
	}
}

func (mc *ModuleConfig) WithEnabled(v bool) *ModuleConfig {
	mc.IsEnabled = v
	return mc
}

func (mc *ModuleConfig) WithUpdated(v bool) *ModuleConfig {
	mc.IsUpdated = v
	return mc
}

func (mc *ModuleConfig) WithValues(values Values) *ModuleConfig {
	mc.Values = values
	return mc
}

// LoadValues loads module config from a map.
//
// Values for module in `values` map are addressed by a key.
// This key should be produced with ModuleNameToValuesKey.
//
// A module is enabled if a key doesn't exist in values.
func (mc *ModuleConfig) LoadValues(values map[interface{}]interface{}) (*ModuleConfig, error) {

	if moduleValuesData, hasModuleData := values[mc.ModuleConfigKey]; hasModuleData {
		switch v := moduleValuesData.(type) {
		case bool:
			mc.IsEnabled = v
		case map[interface{}]interface{}, []interface{}:
			data := map[interface{}]interface{}{mc.ModuleConfigKey: v}

			values, err := NewValues(data)
			if err != nil {
				return nil, err
			}
			mc.IsEnabled = true
			mc.Values = values

		default:
			return nil, fmt.Errorf("Module config should be array or map. Got: %#v", moduleValuesData)
		}
	} else {
		mc.IsEnabled = true
	}

	if moduleEnabled, hasModuleEnabled := values[mc.ModuleEnabledKey]; hasModuleEnabled {
		switch v := moduleEnabled.(type) {
		case bool:
			mc.IsEnabled = v
		default:
			return nil, fmt.Errorf("Module enabled value should be bool. Got: %#v", moduleEnabled)
		}
	}

	return mc, nil
}

// FromYaml loads module config from a yaml string.
//
// Example:
//
// simpleModule:
//   param1: 10
//   param2: 120
// simpleModuleEnabled: true
func (mc *ModuleConfig) FromYaml(yamlString []byte) (*ModuleConfig, error) {
	var values map[interface{}]interface{}

	err := yaml.Unmarshal(yamlString, &values)
	if err != nil {
		return nil, fmt.Errorf("unmarshal module '%s' yaml config: %s\n%s", mc.ModuleName, err, string(yamlString))
	}

	mc.RawConfig = []string{string(yamlString)}

	return mc.LoadValues(values)
}

// FromKeyYamls loads module config from a structure with string keys and yaml string values (ConfigMap)
//
// Example:
//
// simpleModule: |
//   param1: 10
//   param2: 120
// simpleModuleEnabled: "true"
func (mc *ModuleConfig) FromKeyYamls(configData map[string]string) (*ModuleConfig, error) {
	// map with moduleNameKey and moduleEnabled keys
	moduleConfigData := map[interface{}]interface{}{}

	// if there is data for module, unmarshal it and put into moduleConfigData
	valuesYaml, hasKey := configData[mc.ModuleConfigKey]
	if hasKey {
		var values interface{}

		err := yaml.Unmarshal([]byte(valuesYaml), &values)
		if err != nil {
			return nil, fmt.Errorf("unmarshal yaml data in a module config key '%s': %v", mc.ModuleConfigKey, err)
		}

		moduleConfigData[mc.ModuleConfigKey] = values

		mc.RawConfig = append(mc.RawConfig, valuesYaml)
	}

	// if there is enabled key, treat it as boolean
	enabledString, hasKey := configData[mc.ModuleEnabledKey]
	if hasKey {
		var enabled bool

		if enabledString == "true" {
			enabled = true
		} else if enabledString == "false" {
			enabled = false
		} else {
			return nil, fmt.Errorf("module enabled key '%s' should have a boolean value, got '%v'", mc.ModuleEnabledKey, enabledString)
		}

		moduleConfigData[mc.ModuleEnabledKey] = enabled

		mc.RawConfig = append(mc.RawConfig, enabledString)
	}

	if len(moduleConfigData) == 0 {
		return mc, nil
	}

	return mc.LoadValues(moduleConfigData)
}

func (mc *ModuleConfig) Checksum() string {
	return utils_checksum.CalculateChecksum(mc.RawConfig...)
}
