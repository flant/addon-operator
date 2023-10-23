package utils

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/davecgh/go-spew/spew"
	"sigs.k8s.io/yaml"

	utils_checksum "github.com/flant/shell-operator/pkg/utils/checksum"
)

var (
	ModuleEnabled  = true
	ModuleDisabled = false
)

type ModuleConfig struct {
	ModuleName       string
	IsEnabled        *bool
	Values           Values
	IsUpdated        bool
	ModuleConfigKey  string
	ModuleEnabledKey string
	RawConfig        []string
}

// String returns description of ModuleConfig values.
func (mc *ModuleConfig) String() string {
	return fmt.Sprintf("Module(Name=%s IsEnabled=%v IsUpdated=%v Values:\n%s)", mc.ModuleName, mc.IsEnabled, mc.IsUpdated, mc.Values.DebugString())
}

// GetEnabled returns string description of enabled status.
func (mc *ModuleConfig) GetEnabled() string {
	if mc == nil {
		return ""
	}
	switch {
	case mc.IsEnabled == nil:
		return "n/d"
	case *mc.IsEnabled:
		return "true"
	default:
		return "false"
	}
}

func NewModuleConfig(moduleName string) *ModuleConfig {
	return &ModuleConfig{
		ModuleName:       moduleName,
		IsEnabled:        nil,
		Values:           make(Values),
		ModuleConfigKey:  ModuleNameToValuesKey(moduleName),
		ModuleEnabledKey: ModuleNameToValuesKey(moduleName) + "Enabled",
		RawConfig:        make([]string, 0),
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
		case map[string]interface{}, []interface{}:
			data := map[string]interface{}{mc.ModuleConfigKey: v}

			values, err := NewValues(data)
			if err != nil {
				return nil, err
			}
			mc.Values = values
		default:
			return nil, fmt.Errorf("load '%s' values: module config should be array or map. Got: %s", mc.ModuleName, spew.Sdump(moduleValuesData))
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
//
//	param1: 10
//	param2: 120
//
// simpleModuleEnabled: true
func (mc *ModuleConfig) FromYaml(yamlString []byte) (*ModuleConfig, error) {
	values, err := NewValuesFromBytes(yamlString)
	if err != nil {
		return nil, fmt.Errorf("load module '%s' yaml config: %s\n%s", mc.ModuleName, err, string(yamlString))
	}

	mc.RawConfig = []string{string(yamlString)}

	return mc.LoadFromValues(values)
}

// FromConfigMapData loads module config from a structure with string keys and yaml string values (ConfigMap)
//
// Example:
//
// simpleModule: |
//   param1: 10
//   param2: 120
// simpleModuleEnabled: "true"

// TODO "msg": "Kube config manager: cannot handle ConfigMap update: ConfigMap:
//  bad yaml at key 'deployWithHooks':
//  data is not compatible with JSON and YAML:
//  error marshaling into JSON:
//  json: unsupported type: map[interface {}]interface {}, data:
//  (map[string]interface {}) (len=1) {\n (string) (len=15) \"deployWithHooks\": (map[interface {}]interface {}) (len=3) {\n  (string) (len=6) \"param2\": (string) (len=11) \"srqweqweqwe\",\n  (string) (len=8) \"paramArr\": ([]interface {}) (len=4 cap=4) {\n   (string) (len=3) \"asd\",\n   (string) (len=3) \"qwe\",\n   (string) (len=6) \"sadasd\",\n   (string) (len=5) \"salad\"\n  },\n  (string) (len=6) \"param1\": (int) 1\n }\n}\n",

func (mc *ModuleConfig) FromConfigMapData(configData map[string]string) (*ModuleConfig, error) {
	// create Values with moduleNameKey and moduleEnabled keys
	configValues := make(Values)

	// if there is data for module, unmarshal it and put into configValues
	valuesYaml, hasKey := configData[mc.ModuleConfigKey]
	if hasKey {
		var moduleValues interface{}

		err := yaml.Unmarshal([]byte(valuesYaml), &moduleValues)
		if err != nil {
			return nil, fmt.Errorf("unmarshal yaml data in a module config key '%s': %v", mc.ModuleConfigKey, err)
		}

		configValues[mc.ModuleConfigKey] = moduleValues

		mc.RawConfig = append(mc.RawConfig, valuesYaml)
	}

	// if there is enabled key, treat it as boolean
	enabledString, hasKey := configData[mc.ModuleEnabledKey]
	if hasKey {
		var enabled bool

		switch enabledString {
		case "true":
			enabled = true
		case "false":
			enabled = false
		default:
			return nil, fmt.Errorf("module enabled key '%s' should have a boolean value, got '%v'", mc.ModuleEnabledKey, enabledString)
		}

		configValues[mc.ModuleEnabledKey] = enabled

		mc.RawConfig = append(mc.RawConfig, enabledString)
	}

	if len(configValues) == 0 {
		return mc, nil
	}

	return mc.LoadFromValues(configValues)
}

func (mc *ModuleConfig) Checksum() string {
	return utils_checksum.CalculateChecksum(mc.RawConfig...)
}

func ModuleEnabledValue(i interface{}) (*bool, error) {
	switch v := i.(type) {
	case string:
		switch strings.ToLower(v) {
		case "true":
			return &ModuleEnabled, nil
		case "false":
			return &ModuleDisabled, nil
		}
	// TODO(nabokihms): we only need to check the json.RawMessage after the patch refactoring.
	case json.RawMessage:
		switch strings.ToLower(string(v)) {
		case "true":
			return &ModuleEnabled, nil
		case "false":
			return &ModuleDisabled, nil
		}
	case bool:
		if v {
			return &ModuleEnabled, nil
		}
		return &ModuleDisabled, nil
	}
	return nil, fmt.Errorf("unsupported module enabled value: %v", i)
}
