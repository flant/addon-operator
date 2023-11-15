package modules

import (
	"fmt"

	"github.com/go-openapi/spec"
	log "github.com/sirupsen/logrus"

	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/addon-operator/pkg/values/validation"
	"github.com/sasha-s/go-deadlock"
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

// ValuesStorage keeps Module's values in order
type ValuesStorage struct {
	// INPUTS:

	// static staticConfigValues from
	//   /modules/values.yaml
	//   /modules/001-module/values.yaml
	// are set only on module init phase
	staticConfigValues utils.Values

	// patches from hooks, have top priority over values
	valuesPatches []utils.ValuesPatch

	// TODO: actually, we don't need the validator with all Schemas here,
	//   we can put a single openapi schema for the specified module
	validator  validator
	moduleName string

	// lock for any config changes
	clock deadlock.RWMutex
	// configValues are user defined values from KubeConfigManager (ConfigMap or ModuleConfig)
	// without merge with static and openapi values
	configValues utils.Values
	// OUTPUTS:
	mergedConfigValues      utils.Values
	dirtyConfigValues       utils.Values
	dirtyMergedConfigValues utils.Values

	// lock of values changes
	vlock deadlock.RWMutex
	// result of the merging all input values
	resultValues utils.Values
	// dirtyResultValues pre-commit stage of result values, used for two-phase commit with validation
	dirtyResultValues utils.Values
}

// NewValuesStorage build a new storage for module values
//
//	staticValues - values from /modules/<module-name>/values.yaml, which couldn't be reloaded during the runtime
func NewValuesStorage(moduleName string, staticValues utils.Values, validator validator) *ValuesStorage {
	vs := &ValuesStorage{
		staticConfigValues: staticValues,
		validator:          validator,
		moduleName:         moduleName,
	}
	err := vs.calculateResultValues()
	if err != nil {
		log.Errorf("Critical error occurred with calculating values for %q: %s", moduleName, err)
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

	vs.vlock.Lock()
	vs.resultValues = merged
	vs.vlock.Unlock()

	return nil
}

// PreCommitConfigValues save new config values in a dirty state, they are not applied automatically, you have to
// commit them with CommitConfigValues
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

	fmt.Println("CLOCK 0")
	vs.clock.Lock()
	fmt.Println("AFTER CLOCK 0")
	vs.dirtyMergedConfigValues = merged
	vs.dirtyConfigValues = configV
	vs.clock.Unlock()
	fmt.Println("UNLOCK CLOCK 0")

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
	fmt.Println("CLOCK 1")
	defer func() {
		fmt.Println("UCLOCK 1")
	}()
	vs.clock.RLock()
	defer vs.clock.RUnlock()
	fmt.Println("AFTER CLOCK 1")

	if vs.dirtyConfigValues == nil {
		return false
	}

	if vs.configValues.Checksum() != vs.dirtyConfigValues.Checksum() {
		return true
	}

	return false
}

// PreCommitValues can set values after a hook execution, but they are not committed automatically
// you have to commit them with CommitValues
// Probably, we don't need the method, we can use patches instead
func (vs *ValuesStorage) PreCommitValues(v utils.Values) error {
	vs.clock.Lock()
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
	vs.clock.Unlock()

	vs.clock.RLock()
	merged := mergeLayers(
		utils.Values{},
		// Init static values (from modules/values.yaml)
		vs.mergedConfigValues,

		// defaults from openapi value
		vs.openapiDefaultsTransformer(validation.ValuesSchema),

		// new values
		v,
	)
	vs.clock.RUnlock()

	fmt.Println("VLOCK 1")
	vs.vlock.Lock()
	fmt.Println("AFTER VLOCK 1")
	vs.dirtyResultValues = merged
	vs.vlock.Unlock()
	fmt.Println("UNLOCK VLOCK 1")

	return vs.validateValues(merged)
}

// CommitConfigValues move config values from 'dirty' state to the actual
func (vs *ValuesStorage) CommitConfigValues() {
	fmt.Println("CLOCK 2")
	vs.clock.Lock()
	defer vs.clock.Unlock()

	fmt.Println(" AFTER CLOCK 2")
	if vs.dirtyMergedConfigValues != nil {
		vs.mergedConfigValues = vs.dirtyMergedConfigValues
		vs.dirtyMergedConfigValues = nil
	}

	if vs.dirtyConfigValues != nil {
		vs.configValues = vs.dirtyConfigValues
		vs.dirtyConfigValues = nil
	}
	fmt.Println("UNLOCK CLOCK 2")

	_ = vs.calculateResultValues()
}

// CommitValues move result values from the 'dirty' state
func (vs *ValuesStorage) CommitValues() {
	fmt.Println("VLOCK 2")
	vs.vlock.Lock()
	fmt.Println("AFTER VLOCK 2")
	defer vs.vlock.Unlock()
	defer func() {
		fmt.Println("UNLOCK VLOCK 2")
	}()

	if vs.dirtyResultValues == nil {
		return
	}

	vs.resultValues = vs.dirtyResultValues
	vs.dirtyResultValues = nil
}

/*
GetValues return current values with applied patches
withPrefix means, that values will be returned with module name prefix
example:

without prefix:

	```yaml
	replicas: 1
	foo:
	  bar: hello-world
	```

with prefix:

	```yaml
	mySuperModule:
		replicas: 1
		foo:
		  bar: hello-world
	```
*/
func (vs *ValuesStorage) GetValues(withPrefix bool) utils.Values {
	fmt.Println("VLOCK 3")
	vs.vlock.RLock()
	fmt.Println("AFTER VLOCK 3")
	defer vs.vlock.RLock()
	defer func() {
		fmt.Println("UNLOCK VLOCK 3")
	}()

	if withPrefix {
		return utils.Values{
			utils.ModuleNameToValuesKey(vs.moduleName): vs.resultValues,
		}
	}

	return vs.resultValues
}

// GetConfigValues returns only user defined values
func (vs *ValuesStorage) GetConfigValues(withPrefix bool) utils.Values {
	fmt.Println("CLOCK 3")
	vs.clock.RLock()
	fmt.Println("AFTER CLOCK 3")
	defer vs.clock.RUnlock()
	defer func() {
		fmt.Println("UNLOCK CLOCK 3")
	}()

	if withPrefix {
		return utils.Values{
			utils.ModuleNameToValuesKey(vs.moduleName): vs.configValues,
		}
	}

	return vs.configValues
}

func (vs *ValuesStorage) appendValuesPatch(patch utils.ValuesPatch) {
	vs.valuesPatches = utils.AppendValuesPatch(vs.valuesPatches, patch)
}

func (vs *ValuesStorage) getValuesPatches() []utils.ValuesPatch {
	return vs.valuesPatches
}
