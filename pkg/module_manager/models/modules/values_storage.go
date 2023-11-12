package modules

import (
	"fmt"

	"github.com/flant/addon-operator/pkg/values/validation"
	"github.com/go-openapi/spec"

	"github.com/flant/addon-operator/pkg/utils"
)

type validator interface {
	ValidateModuleConfigValues(moduleName string, values utils.Values) error
	ValidateModuleValues(moduleName string, values utils.Values) error
	ValidateGlobalConfigValues(values utils.Values) error
	ValidateGlobalValues(values utils.Values) error
	GetSchema(schemaType validation.SchemaType, valuesType validation.SchemaType, modName string) *spec.Schema
}

/*
	Module can contains a few sources of module values:
	1. /modules/values.yaml - static (never reload)
	2. /modules/001-module/values.yaml - static (could be reload on module Dynamic update (deregister + register))
	3. /modules/001-module/openapi/config-values.yaml - dynamic, default values could be rendered differently
	4. /modules/001-module/openapi/values.yaml - dynamic, default values
	5. ConfigValues from KubeConfigManager - dynamic, get from user settings
	6. JSON patches (in-memory for GoHook or VALUES_JSON_PATCH_PATH for ShellHook) - dynamic, from hooks, have top priority
*/

type ValuesStorage struct {
	// INPUTS:

	// static staticConfigValues from
	//   /modules/values.yaml
	//   /modules/001-module/values.yaml
	// are set only on module init phase
	staticConfigValues utils.Values

	// config values from openapi schema:
	//   /modules/001-module/openapi/config-values.yaml
	//openapiConfigValues utils.Values
	// values from openapi schema:
	//   /modules/001-module/openapi/values.yaml
	//openapiValues utils.Values

	// configValues are user defined values from KubeConfigManager (ConfigMap or ModuleConfig)
	// without merge with static and openapi values
	configValues utils.Values

	// patches from hooks, have top priority
	valuesPatches []utils.ValuesPatch

	// OUTPUTS:

	mergedConfigValues utils.Values
	// result of the merging all input values
	resultValues utils.Values

	// dirtyResultValues pre-commit stage of result values, used for two-phase commit with validation
	dirtyResultValues       utils.Values
	dirtyConfigValues       utils.Values
	dirtyMergedConfigValues utils.Values

	// TODO: acutally, we don't need the validator with all Schemas here,
	//   we can put a single openapi schema for the specified module
	validator  validator
	moduleName string
}

func NewValuesStorage(moduleName string, staticValues utils.Values, validator validator) *ValuesStorage {
	vs := &ValuesStorage{
		staticConfigValues: staticValues,
		validator:          validator,
		moduleName:         moduleName,
	}
	err := vs.calculateResultValues()
	if err != nil {
		panic(err)
	}

	return vs
}

func (vs *ValuesStorage) openapiDefaultsTransformer(schemaType validation.SchemaType) transformer {
	if vs.moduleName == utils.GlobalValuesKey {
		return &applyDefaultsForGlobal{
			SchemaType:      schemaType,
			ValuesValidator: vs.validator,
		}
	}

	return &applyDefaultsForModule{
		ModuleName:      vs.moduleName,
		SchemaType:      schemaType,
		ValuesValidator: vs.validator,
	}
}

func (vs *ValuesStorage) calculateResultValues() error {
	merged := mergeLayers(
		utils.Values{},
		// Init static values (from modules/values.yaml and modules/XXX/values.yaml)
		vs.staticConfigValues,

		// from openapi config spec
		vs.openapiDefaultsTransformer(validation.ConfigValuesSchema),

		// from configValues
		vs.configValues,

		// from openapi values spec
		vs.openapiDefaultsTransformer(validation.ValuesSchema),
	)
	// from patches
	// Compact patches so we could execute all at once.
	// Each ApplyValuesPatch execution invokes json.Marshal for values.
	ops := *utils.NewValuesPatch()

	for _, patch := range vs.valuesPatches {
		ops.Operations = append(ops.Operations, patch.Operations...)
	}

	merged, _, err := utils.ApplyValuesPatch(merged, ops, utils.IgnoreNonExistentPaths)
	if err != nil {
		return err
	}

	vs.resultValues = merged

	return nil
}

func (vs *ValuesStorage) PreCommitConfigValues(configV utils.Values, validate bool) error {
	merged := mergeLayers(
		utils.Values{},
		// Init static values
		vs.staticConfigValues,

		// defaults from openapi
		vs.openapiDefaultsTransformer(validation.ConfigValuesSchema),

		// User configured values (ConfigValues)
		configV,
	)

	vs.dirtyMergedConfigValues = merged
	fmt.Println("SET DIRTY", configV)
	vs.dirtyConfigValues = configV

	if validate {
		return vs.validateConfigValues(merged)
	}

	return nil
}

func (vs *ValuesStorage) validateConfigValues(values utils.Values) error {
	valuesModuleName := utils.ModuleNameToValuesKey(vs.moduleName)
	validatableValues := utils.Values{valuesModuleName: values}

	if vs.moduleName == utils.GlobalValuesKey {
		return vs.validator.ValidateGlobalConfigValues(validatableValues)
	}

	return vs.validator.ValidateModuleConfigValues(valuesModuleName, validatableValues)
}

func (vs *ValuesStorage) validateValues(values utils.Values) error {
	valuesModuleName := utils.ModuleNameToValuesKey(vs.moduleName)
	validatableValues := utils.Values{valuesModuleName: values}

	if vs.moduleName == utils.GlobalValuesKey {
		return vs.validator.ValidateGlobalValues(validatableValues)
	}

	return vs.validator.ValidateModuleValues(valuesModuleName, validatableValues)
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

func (vs *ValuesStorage) PreCommitValues(v utils.Values) error {
	if vs.mergedConfigValues == nil {
		vs.mergedConfigValues = mergeLayers(
			utils.Values{},
			// Init static values
			vs.staticConfigValues,

			// defaults from openapi
			vs.openapiDefaultsTransformer(validation.ConfigValuesSchema),

			// User configured values (ConfigValues)
			vs.configValues,
		)
	}

	merged := mergeLayers(
		utils.Values{},
		// Init static values (from modules/values.yaml)
		vs.mergedConfigValues,

		// defaults from openapi value
		vs.openapiDefaultsTransformer(validation.ValuesSchema),

		// new values
		v,
	)

	vs.dirtyResultValues = merged

	return vs.validateValues(merged)
}

func (vs *ValuesStorage) CommitConfigValues() {
	if vs.dirtyMergedConfigValues != nil {
		vs.mergedConfigValues = vs.dirtyMergedConfigValues
		fmt.Println("COMMIT MERGED", vs.dirtyMergedConfigValues)
		vs.dirtyMergedConfigValues = nil
	}

	if vs.dirtyConfigValues != nil {
		fmt.Println("COMMIT VALUES", vs.dirtyConfigValues)
		vs.configValues = vs.dirtyConfigValues
		vs.dirtyConfigValues = nil
	}

	_ = vs.calculateResultValues()
}

func (vs *ValuesStorage) CommitValues() {
	if vs.dirtyResultValues == nil {
		return
	}

	vs.resultValues = vs.dirtyResultValues
	vs.dirtyResultValues = nil
}

// GetValues return current values with applied patches
func (vs *ValuesStorage) GetValues(withPrefix bool) utils.Values {
	if withPrefix {
		return utils.Values{
			utils.ModuleNameToValuesKey(vs.moduleName): vs.resultValues,
		}
	}

	return vs.resultValues
}

func (vs *ValuesStorage) GetConfigValues(withPrefix bool) utils.Values {
	if withPrefix {
		return utils.Values{
			utils.ModuleNameToValuesKey(vs.moduleName): vs.configValues,
		}
	}

	return vs.configValues
}

func (vs *ValuesStorage) AppendValuesPatch(patch utils.ValuesPatch) {
	vs.valuesPatches = utils.AppendValuesPatch(vs.valuesPatches, patch)
}
