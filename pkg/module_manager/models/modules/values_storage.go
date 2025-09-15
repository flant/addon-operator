package modules

import (
	"fmt"
	"sync"

	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/addon-operator/pkg/values/validation"
)

/*
	Module can contain a few sources of module values:
	1. /modules/values.yaml - static (never reload)
	2. /modules/001-module/values.yaml - static (could be reloaded on module Dynamic update (deregister + register))
	3. /modules/001-module/openapi/config-values.yaml - dynamic, default values could be rendered differently
	4. /modules/001-module/openapi/values.yaml - dynamic, default values
	5. ConfigValues from KubeConfigManager - dynamic, get from user settings
	6. JSON patches (in-memory for GoHook or VALUES_JSON_PATCH_PATH for ShellHook) - dynamic, from hooks, have top priority
*/

// ValuesStorage keeps Module's values in order
type ValuesStorage struct {
	// INPUTS:
	// patches from hooks, have top priority over values
	valuesPatches []utils.ValuesPatch

	schemaStorage *validation.SchemaStorage
	moduleName    string

	// we are locking the whole storage on any concurrent operation
	// because it could be called from concurrent hooks (goroutines) and we will have a deadlock on RW mutex
	lock sync.Mutex

	// static staticValues from
	//   /modules/values.yaml
	//   /modules/001-module/values.yaml
	// are set only on module init phase
	staticValues utils.Values
	// configValues are user defined values from KubeConfigManager (ConfigMap or ModuleConfig)
	// without merge with static and openapi values
	configValues utils.Values
	// OUTPUTS:

	// result of the merging all input values
	resultValues utils.Values
}

type Registry struct {
	Base      string `json:"base" yaml:"base"`
	DockerCfg string `json:"dockercfg" yaml:"dockercfg"`
	Scheme    string `json:"scheme" yaml:"scheme"`
	CA        string `json:"ca,omitempty" yaml:"ca,omitempty"`
}

// NewValuesStorage build a new storage for module values
//
//	staticValues - values from /modules/<module-name>/values.yaml, which couldn't be reloaded during the runtime
func NewValuesStorage(moduleName string, staticValues utils.Values, configBytes, valuesBytes []byte) (*ValuesStorage, error) {
	schemaStorage, err := validation.NewSchemaStorage(configBytes, valuesBytes)
	if err != nil {
		return nil, fmt.Errorf("new schema storage: %w", err)
	}

	vs := &ValuesStorage{
		staticValues:  staticValues,
		schemaStorage: schemaStorage,
		moduleName:    moduleName,
	}
	err = vs.calculateResultValues()
	if err != nil {
		return nil, fmt.Errorf("critical error occurred with calculating values for %q: %w", moduleName, err)
	}

	return vs, nil
}

func (vs *ValuesStorage) openapiDefaultsTransformer(schemaType validation.SchemaType) transformer {
	return &applyDefaults{
		SchemaType: schemaType,
		Schemas:    vs.schemaStorage.Schemas,
	}
}

func (vs *ValuesStorage) calculateResultValues() error {
	merged := mergeLayers(
		utils.Values{},
		// Init static values (from modules/values.yaml and modules/XXX/values.yaml)
		vs.staticValues,

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

	moduleValuesKey := utils.ModuleNameToValuesKey(vs.moduleName)

	merged, _, err := utils.ApplyValuesPatch(utils.Values{moduleValuesKey: merged}, ops, utils.IgnoreNonExistentPaths)
	if err != nil {
		return err
	}

	vs.resultValues = merged.GetKeySection(moduleValuesKey)

	return nil
}

func (vs *ValuesStorage) validateConfigValues(values utils.Values) error {
	valuesModuleName := utils.ModuleNameToValuesKey(vs.moduleName)
	validatableValues := utils.Values{valuesModuleName: values}

	return vs.schemaStorage.ValidateConfigValues(valuesModuleName, validatableValues)
}

func (vs *ValuesStorage) validateValues(values utils.Values) error {
	valuesModuleName := utils.ModuleNameToValuesKey(vs.moduleName)
	validatableValues := utils.Values{valuesModuleName: values}

	return vs.schemaStorage.ValidateValues(valuesModuleName, validatableValues)
}

// applyNewSchemaStorage sets new schema storage
func (vs *ValuesStorage) applyNewSchemaStorage(schema *validation.SchemaStorage) error {
	vs.schemaStorage = schema
	return vs.calculateResultValues()
}

// CommitValues apply all patches and create up-to-date values for module
func (vs *ValuesStorage) CommitValues() error {
	vs.lock.Lock()
	defer vs.lock.Unlock()

	return vs.calculateResultValues()
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
	vs.lock.Lock()
	defer vs.lock.Unlock()

	if withPrefix {
		return utils.Values{
			utils.ModuleNameToValuesKey(vs.moduleName): vs.resultValues,
		}
	}

	return vs.resultValues
}

func (vs *ValuesStorage) InjectRegistryValue(registry *Registry) {
	vs.lock.Lock()
	defer vs.lock.Unlock()

	// inject spec to values schema
	vs.schemaStorage.InjectRegistrySpec(validation.ValuesSchema)
	// inject spec to helm schema
	vs.schemaStorage.InjectRegistrySpec(validation.HelmValuesSchema)

	if vs.staticValues == nil {
		vs.staticValues = utils.Values{}
	}

	vs.staticValues["registry"] = registry

	_ = vs.calculateResultValues()
}

// GetConfigValues returns only user defined values
func (vs *ValuesStorage) GetConfigValues(withPrefix bool) utils.Values {
	vs.lock.Lock()
	defer vs.lock.Unlock()

	if withPrefix {
		return utils.Values{
			utils.ModuleNameToValuesKey(vs.moduleName): vs.configValues,
		}
	}

	return vs.configValues
}

func (vs *ValuesStorage) SaveConfigValues(configV utils.Values) {
	vs.lock.Lock()
	defer vs.lock.Unlock()

	vs.configValues = configV
	err := vs.calculateResultValues()
	if err != nil {
		panic(err)
	}
}

// GenerateNewConfigValues generated new config values, based on static and config values. Additionally, if validate is true, it validates
// the resulting values set for consistency. This method always makes a copy of the result values to prevent pointers shallow copying
func (vs *ValuesStorage) GenerateNewConfigValues(configV utils.Values, validate bool) (utils.Values, error) {
	vs.lock.Lock()
	defer vs.lock.Unlock()

	merged := mergeLayers(
		utils.Values{},
		// User configured values (ConfigValues)
		configV,
	)

	// we are making deep copy here to avoid any pointers copying
	merged = merged.Copy()

	if validate {
		mergedWithOpenapiDefault := mergeLayers(
			utils.Values{},
			vs.openapiDefaultsTransformer(validation.ConfigValuesSchema),
			configV,
		)
		if err := vs.validateConfigValues(mergedWithOpenapiDefault); err != nil {
			return nil, err
		}
	}

	return merged, nil
}

func (vs *ValuesStorage) appendValuesPatch(patch utils.ValuesPatch) {
	vs.lock.Lock()
	vs.valuesPatches = utils.AppendValuesPatch(vs.valuesPatches, patch)
	vs.lock.Unlock()
}

func (vs *ValuesStorage) getValuesPatches() []utils.ValuesPatch {
	vs.lock.Lock()
	defer vs.lock.Unlock()

	return vs.valuesPatches
}

func (vs *ValuesStorage) GetSchemaStorage() *validation.SchemaStorage {
	return vs.schemaStorage
}
