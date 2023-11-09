package modules

import (
	"fmt"

	"github.com/flant/addon-operator/pkg/utils"
)

type validator interface {
	ValidateModuleConfigValues(moduleName string, values utils.Values) error
	ValidateModuleValues(moduleName string, values utils.Values) error
	ValidateGlobalConfigValues(values utils.Values) error
	ValidateGlobalValues(values utils.Values) error
}

type ValuesStorage struct {
	staticConfigValues utils.Values

	// configValues are user defined values
	configValues utils.Values

	// values are staticConfigValues + configValues + default values
	values utils.Values

	dirtyConfigValues utils.Values
	dirtyValues       utils.Values

	validator validator
}

func NewValuesStorage(initialValues utils.Values, validator validator) *ValuesStorage {
	return &ValuesStorage{
		staticConfigValues: initialValues,
		validator:          validator,
		configValues:       initialValues,
		values:             initialValues,
	}
}

func (vs *ValuesStorage) SetStaticConfigValues(v utils.Values) error {
	if !vs.staticConfigValues.IsEmpty() {
		return fmt.Errorf("static values are already set")
	}

	vs.staticConfigValues = v

	return nil
}

func (vs *ValuesStorage) SetNewConfigValues(moduleName string, configV utils.Values) error {
	merged := mergeLayers(
		// Init static values (from modules/values.yaml)
		vs.staticConfigValues,
		// User configured values (ConfigValues)
		configV,
	)

	var err error
	if moduleName == utils.GlobalValuesKey {
		err = vs.validator.ValidateGlobalConfigValues(merged)
	} else {
		err = vs.validator.ValidateModuleConfigValues(moduleName, merged)
	}

	vs.dirtyConfigValues = merged

	return err

	//return modules.mergeLayers(
	//	// Init global section.
	//	utils.Values{"global": map[string]interface{}{}},
	//	// Merge static values from modules/values.yaml.
	//	mm.commonStaticValues.Global(),
	//	// Apply config values defaults before overrides.
	//	&applyDefaultsForGlobal{validation.ConfigValuesSchema, mm.ValuesValidator},
	//	// Merge overrides from newValues.
	//	newValues,
	//)
}

func (vs *ValuesStorage) dirtyConfigValuesHasDiff() bool {
	if vs.dirtyConfigValues == nil {
		return false
	}

	if vs.configValues.Checksum() != vs.dirtyConfigValues.Checksum() {
		return true
	}

	return false
}

func (vs *ValuesStorage) SetNewValues(moduleName string, v utils.Values) error {
	moduleName = utils.ModuleNameToValuesKey(moduleName)
	// WHAT TO MERGE?
	merged := mergeLayers(
		// Init static values (from modules/values.yaml)
		vs.staticConfigValues,
		// User configured values (ConfigValues)
		vs.configValues,
		// new values
		v,
	)

	var err error
	if moduleName == utils.GlobalValuesKey {
		err = vs.validator.ValidateGlobalValues(merged)
	} else {
		err = vs.validator.ValidateModuleValues(moduleName, merged)
	}

	vs.dirtyValues = v

	return err
}

func (vs *ValuesStorage) ApplyDirtyConfigValues() {
	vs.configValues = vs.dirtyConfigValues
	vs.dirtyConfigValues = nil
}

func (vs *ValuesStorage) ApplyDirtyValues() {
	vs.values = vs.dirtyValues
	vs.dirtyValues = nil
}

// GetValues return current values with applied patches
func (vs *ValuesStorage) GetValues() utils.Values {
	return vs.values
}

func (vs *ValuesStorage) GetConfigValues() utils.Values {
	return vs.configValues
}
