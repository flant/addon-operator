package utils

import (
	"fmt"

	"gopkg.in/yaml.v2"

	utils_checksum "github.com/flant/shell-operator/pkg/utils/checksum"
)

var ModuleEnabled = true
var ModuleDisabled = false

type ModuleConfig struct {
	ModuleName string
	IsEnabled  *bool
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
		IsEnabled:  nil,
		Values:     make(Values),
		ModuleConfigKey: ModuleNameToValuesKey(moduleName),
		ModuleEnabledKey: ModuleNameToValuesKey(moduleName) + "Enabled",
		RawConfig: make([]string, 0),
	}
}

func (mc *ModuleConfig) WithEnabled(v bool) *ModuleConfig {
	if v {
		mc.IsEnabled = &ModuleEnabled
	} else {
		mc.IsEnabled = &ModuleDisabled
	}
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

// LoadFromValues loads module config from a map.
//
// Values for module in `values` map are addressed by a key.
// This key should be produced with ModuleNameToValuesKey.
func (mc *ModuleConfig) LoadFromValues(values Values) (*ModuleConfig, error) {

	if moduleValuesData, hasModuleData := values[mc.ModuleConfigKey]; hasModuleData {
		switch v := moduleValuesData.(type) {
		case map[interface{}]interface{}, map[string]interface{}, []interface{}:
			data := map[interface{}]interface{}{mc.ModuleConfigKey: v}

			values, err := NewValues(data)
			if err != nil {
				return nil, err
			}
			mc.Values = values

		default:
			return nil, fmt.Errorf("load '%s' values: module config should be array or map. Got: %#v", mc.ModuleName, moduleValuesData)
		}
	}

	if moduleEnabled, hasModuleEnabled := values[mc.ModuleEnabledKey]; hasModuleEnabled {
		switch v := moduleEnabled.(type) {
		case bool:
			mc.WithEnabled(v)
		default:
			return nil, fmt.Errorf("load '%s' enable config: enabled value should be bool. Got: %#v", mc.ModuleName, moduleEnabled)
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
	values := make(Values)

	err := yaml.Unmarshal(yamlString, &values)
	if err != nil {
		return nil, fmt.Errorf("unmarshal module '%s' yaml config: %s\n%s", mc.ModuleName, err, string(yamlString))
	}

	mc.RawConfig = []string{string(yamlString)}

	return mc.LoadFromValues(values)
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
	moduleConfigData := make(Values) // map[interface{}]interface{}{}

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

	return mc.LoadFromValues(moduleConfigData)
}

func (mc *ModuleConfig) Checksum() string {
	return utils_checksum.CalculateChecksum(mc.RawConfig...)
}
