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

	EnabledSuffix          = "Enabled"
	MaintenanceStateSuffix = "Maintenance"
)

type Maintenance string

func (m Maintenance) String() string {
	return string(m)
}

const (
	Managed                  Maintenance = "Managed"
	NoResourceReconciliation Maintenance = "NoResourceReconciliation"
)

type ModuleConfig struct {
	ModuleName  string
	IsEnabled   *bool
	Maintenance Maintenance
	// module values, don't read it directly, use GetValues() for reading
	values Values
}

// String returns description of ModuleConfig values.
func (mc *ModuleConfig) String() string {
	return fmt.Sprintf("Module(Name=%s IsEnabled=%v MaintenanceState=%v Values:\n%s)", mc.ModuleName, mc.IsEnabled, mc.Maintenance, mc.values.DebugString())
}

// ModuleConfigKey transforms module kebab-case name to the config camelCase name
func (mc *ModuleConfig) ModuleConfigKey() string {
	return ModuleNameToValuesKey(mc.ModuleName)
}

// ModuleEnabledKey transforms module kebab-case name to the config camelCase name with 'Enabled' suffix
func (mc *ModuleConfig) ModuleEnabledKey() string {
	return ModuleNameToValuesKey(mc.ModuleName) + EnabledSuffix
}

// ModuleMaintenanceStateKey transforms module kebab-case name to the config camelCase name with 'MaintenanceState' suffix
func (mc *ModuleConfig) ModuleMaintenanceStateKey() string {
	return ModuleNameToValuesKey(mc.ModuleName) + MaintenanceStateSuffix
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

func (mc *ModuleConfig) GetMaintenanceState() Maintenance {
	if mc == nil {
		return Managed
	}

	return mc.Maintenance
}

func NewModuleConfig(moduleName string, values Values) *ModuleConfig {
	if values == nil {
		values = make(Values)
	}
	return &ModuleConfig{
		ModuleName:  moduleName,
		IsEnabled:   nil,
		values:      values,
		Maintenance: Managed,
	}
}

// GetValuesWithModuleName enrich module values with module's name top level key
// if key is already present - returns values as it
// module: test-module with values {"a": "b", "c": "d} will return:
//
//	testModule:
//	  a: b
//	  c: d
//
// Deprecated: use GetValues instead
func (mc *ModuleConfig) GetValuesWithModuleName() Values {
	return mc.values
}

// GetValues returns values but without moduleName
//
//	a: b
//	c: d
func (mc *ModuleConfig) GetValues() Values {
	if len(mc.values) == 0 {
		return mc.values
	}

	valuesModuleName := ModuleNameToValuesKey(mc.ModuleName)
	if mc.values.HasKey(valuesModuleName) {
		return mc.values.GetKeySection(valuesModuleName)
	}

	return mc.values
}

// Reset removes values from module config and reset enabled IsEnabled
func (mc *ModuleConfig) Reset() {
	mc.values = Values{}
	mc.IsEnabled = nil
	mc.Maintenance = Managed
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

	if maintenanceState, hasMaintenanceState := values[mc.ModuleMaintenanceStateKey()]; hasMaintenanceState {
		switch v := maintenanceState.(type) {
		case Maintenance:
			mc.Maintenance = v
		default:
			return nil, fmt.Errorf("load '%s' MaintenanceState config: MaintenanceState value should be string. Got: %#v", mc.ModuleName, maintenanceState)
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
	checksum, err := utils_checksum.CalculateChecksum(vChecksum)
	if err != nil {
		return ""
	}
	return strconv.Itoa(int(checksum))
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
