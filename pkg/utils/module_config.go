package utils

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/davecgh/go-spew/spew"

	utils_checksum "github.com/flant/shell-operator/pkg/utils/checksum"
)

var (
	ModuleEnabled  = true
	ModuleDisabled = false
)

type ModuleConfig struct {
	ModuleName string
	IsEnabled  *bool
	// module values, don't read it directly, use GetValues() for reading
	values Values
}

// String returns description of ModuleConfig values.
func (mc *ModuleConfig) String() string {
	return fmt.Sprintf("Module(Name=%s IsEnabled=%v Values:\n%s)", mc.ModuleName, mc.IsEnabled, mc.values.DebugString())
}

// ModuleConfigKey transforms module kebab-case name to the config camelCase name
func (mc *ModuleConfig) ModuleConfigKey() string {
	return ModuleNameToValuesKey(mc.ModuleName)
}

// ModuleEnabledKey transforms module kebab-case name to the config camelCase name with 'Enabled' suffix
func (mc *ModuleConfig) ModuleEnabledKey() string {
	return ModuleNameToValuesKey(mc.ModuleName) + "Enabled"
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

func NewModuleConfig(moduleName string, values Values) *ModuleConfig {
	if values == nil {
		values = make(Values)
	}
	return &ModuleConfig{
		ModuleName: moduleName,
		IsEnabled:  nil,
		values:     values,
	}
}

// GetValues enrich module values with module's name top level key
// if key is already present - returns values as it
// module: test-module with values {"a": "b", "c": "d} will return:
//
//	testModule:
//	  a: b
//	  c: d
/* TODO: since we have specified struct for module values, we don't need to encapsulate them into the map {"<moduleName>": ... }
   we have to change this behavior somewhere in the module-manager */
func (mc *ModuleConfig) GetValues() Values {
	if len(mc.values) == 0 {
		return mc.values
	}

	if mc.values.HasKey(ModuleNameToValuesKey(mc.ModuleName)) {
		return mc.values
	}

	return Values{ModuleNameToValuesKey(mc.ModuleName): mc.values}
}

// LoadFromValues loads module config from a map.
//
// Values for module in `values` map are addressed by a key.
// This key should be produced with ModuleNameToValuesKey.
func (mc *ModuleConfig) LoadFromValues(values Values) (*ModuleConfig, error) {
	if moduleValuesData, hasModuleData := values[mc.ModuleConfigKey()]; hasModuleData {
		switch v := moduleValuesData.(type) {
		case map[string]interface{}, []interface{}:
			data := map[string]interface{}{mc.ModuleConfigKey(): v}

			values, err := NewValues(data)
			if err != nil {
				return nil, err
			}
			mc.values = values
		default:
			return nil, fmt.Errorf("load '%s' values: module config should be array or map. Got: %s", mc.ModuleName, spew.Sdump(moduleValuesData))
		}
	}

	if moduleEnabled, hasModuleEnabled := values[mc.ModuleEnabledKey()]; hasModuleEnabled {
		switch v := moduleEnabled.(type) {
		case bool:
			mc.IsEnabled = &v
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

	return mc.LoadFromValues(values)
}

func (mc *ModuleConfig) Checksum() string {
	vChecksum := mc.values.Checksum()
	enabled := ""
	if mc.IsEnabled != nil {
		enabled = strconv.FormatBool(*mc.IsEnabled)
	}
	return utils_checksum.CalculateChecksum(enabled, vChecksum)
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
