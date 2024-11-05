package validation

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/go-openapi/loads"
	"github.com/go-openapi/spec"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/go-openapi/validate"
	"github.com/hashicorp/go-multierror"
	"sigs.k8s.io/yaml"

	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/addon-operator/pkg/values/validation/schema"
)

/**
 * This package validates global and module values against OpenAPI schemas
 * in /$GLOBAL_HOOKS_DIR/openapi and /$MODULES_DIR/<module_name>/openapi directories.
 *
 * For example:
 *  /global/hooks/...
 *  /global/openapi/config-values.yaml
 *  /global/openapi/values.yaml
 *  /modules/XXXX/openapi/config-values.yaml
 *  /modules/XXXX/openapi/values.yaml
 */

type SchemaType string

const (
	GlobalSchema       SchemaType = "global"
	ModuleSchema       SchemaType = "module"
	ConfigValuesSchema SchemaType = "config"
	ValuesSchema       SchemaType = "values"
	HelmValuesSchema   SchemaType = "helm"
)

func init() {
	// Add loader to override swag.BytesToYAML marshaling into yaml.MapSlice.
	// This type doesn't support map merging feature of YAML anchors. So additional
	// loader is required to unmarshal into ordinary interface{} before converting to JSON.
	loads.AddLoader(swag.YAMLMatcher, YAMLDocLoader)
}

// YAMLDocLoader loads a yaml document from either http or a file and converts it to json.
func YAMLDocLoader(path string) (json.RawMessage, error) {
	data, err := swag.LoadFromFileOrHTTP(path)
	if err != nil {
		return nil, err
	}

	return YAMLBytesToJSONDoc(data)
}

func NewSchemaStorage(configBytes, valuesBytes []byte) (*SchemaStorage, error) {
	schemas, err := PrepareSchemas(configBytes, valuesBytes)
	if err != nil {
		return nil, fmt.Errorf("prepare schemas: %w", err)
	}

	return &SchemaStorage{Schemas: schemas}, err
}

type SchemaStorage struct {
	Schemas map[SchemaType]*spec.Schema
}

func (st *SchemaStorage) ValidateModuleHelmValues(moduleName string, values utils.Values) error {
	return st.Validate(HelmValuesSchema, moduleName, values)
}

func (st *SchemaStorage) ValidateConfigValues(moduleName string, values utils.Values) error {
	return st.Validate(ConfigValuesSchema, moduleName, values)
}

func (st *SchemaStorage) ValidateValues(moduleName string, values utils.Values) error {
	return st.Validate(ValuesSchema, moduleName, values)
}

func (st *SchemaStorage) Validate(valuesType SchemaType, moduleName string, values utils.Values) error {
	schema := st.Schemas[valuesType]
	if schema == nil {
		log.Warnf("schema (%s) for '%s' values is not found", moduleName, valuesType)
		return nil
	}

	obj, ok := values[moduleName]
	if !ok {
		return fmt.Errorf("root key '%s' not found in input values", moduleName)
	}

	validationErr := validateObject(obj, schema, moduleName)
	if validationErr == nil {
		log.Debugf("'%s' values are valid", valuesType)
	} else {
		log.Debugf("'%s' values are NOT valid: %v", valuesType, validationErr)
	}
	return validationErr
}

// validateObject uses schema to validate data structure in the dataObj.
// See https://github.com/kubernetes/apiextensions-apiserver/blob/1bb376f70aa2c6f2dec9a8c7f05384adbfac7fbb/pkg/apiserver/validation/validation.go#L47
func validateObject(dataObj interface{}, s *spec.Schema, rootName string) (multiErr error) {
	if s == nil {
		return fmt.Errorf("validate config: schema is not provided")
	}

	validator := validate.NewSchemaValidator(s, nil, rootName, strfmt.Default) // , validate.DisableObjectArrayTypeCheck(true)

	switch v := dataObj.(type) {
	case utils.Values:
		dataObj = map[string]interface{}(v)

	case map[string]interface{}:
	// pass

	default:
		return fmt.Errorf("validated data object have to be utils.Values or map[string]interface{}, got %v instead", reflect.TypeOf(v))
	}

	result := validator.Validate(dataObj)
	if result.IsValid() {
		return nil
	}

	var allErrs *multierror.Error
	for _, err := range result.Errors {
		allErrs = multierror.Append(allErrs, err)
	}
	// NOTE: no validation errors, but config is not valid!
	if allErrs == nil || allErrs.Len() == 0 {
		allErrs = multierror.Append(allErrs, fmt.Errorf("configuration is not valid"))
	}
	return allErrs.ErrorOrNil()
}

// ModuleSchemasDescription describes which schemas are present in storage for the module.
func (st *SchemaStorage) ModuleSchemasDescription() string {
	types := availableSchemaTypes(st.Schemas)
	if len(types) == 0 {
		return "No OpenAPI schemas"
	}
	return fmt.Sprintf("OpenAPI schemas: %s.", strings.Join(types, ", "))
}

// GlobalSchemasDescription describes which global schemas are present in storage.
func (st *SchemaStorage) GlobalSchemasDescription() string {
	types := availableSchemaTypes(st.Schemas)
	if len(types) == 0 {
		return "No Global OpenAPI schemas"
	}
	return fmt.Sprintf("Global OpenAPI schemas: %s.", strings.Join(types, ", "))
}

func availableSchemaTypes(schemas map[SchemaType]*spec.Schema) []string {
	types := make([]string, 0)
	if len(schemas) == 0 {
		return types
	}

	if _, has := schemas[ConfigValuesSchema]; has {
		types = append(types, "config values")
	}
	if _, has := schemas[ValuesSchema]; has {
		types = append(types, "values")
	}
	if _, has := schemas[HelmValuesSchema]; has {
		types = append(types, "helm values")
	}
	return types
}

// YAMLBytesToJSONDoc is a replacement of swag.YAMLData and YAMLDoc to Unmarshal into interface{}.
// swag.BytesToYAML uses yaml.MapSlice to unmarshal YAML. This type doesn't support map merge of YAML anchors.
func YAMLBytesToJSONDoc(data []byte) (json.RawMessage, error) {
	var yamlObj interface{}
	err := yaml.Unmarshal(data, &yamlObj)
	if err != nil {
		return nil, fmt.Errorf("yaml unmarshal: %v", err)
	}

	doc, err := swag.YAMLToJSON(yamlObj)
	if err != nil {
		return nil, fmt.Errorf("yaml to json: %v", err)
	}

	return doc, nil
}

// LoadSchemaFromBytes returns spec.Schema object loaded from YAML bytes.
func LoadSchemaFromBytes(openApiContent []byte) (*spec.Schema, error) {
	jsonDoc, err := YAMLBytesToJSONDoc(openApiContent)
	if err != nil {
		return nil, err
	}

	s := new(spec.Schema)
	if err := json.Unmarshal(jsonDoc, s); err != nil {
		return nil, fmt.Errorf("json unmarshal: %v", err)
	}

	err = spec.ExpandSchema(s, s, nil /*new(noopResCache)*/)
	if err != nil {
		return nil, fmt.Errorf("expand schema: %v", err)
	}

	return s, nil
}

// PrepareSchemas loads schemas for config values, values and helm values.
func PrepareSchemas(configBytes, valuesBytes []byte) (schemas map[SchemaType]*spec.Schema, err error) {
	res := make(map[SchemaType]*spec.Schema)
	if len(configBytes) > 0 {
		schemaObj, err := LoadSchemaFromBytes(configBytes)
		if err != nil {
			return nil, fmt.Errorf("load '%s' schema: %w", ConfigValuesSchema, err)
		}
		res[ConfigValuesSchema] = schema.TransformSchema(
			schemaObj,
			&schema.AdditionalPropertiesTransformer{},
		)
	}

	if len(valuesBytes) > 0 {
		schemaObj, err := LoadSchemaFromBytes(valuesBytes)
		if err != nil {
			return nil, fmt.Errorf("load '%s' schema: %w", ValuesSchema, err)
		}
		res[ValuesSchema] = schema.TransformSchema(
			schemaObj,
			&schema.ExtendTransformer{Parent: res[ConfigValuesSchema]},
			&schema.AdditionalPropertiesTransformer{},
		)

		res[HelmValuesSchema] = schema.TransformSchema(
			schemaObj,
			// Copy schema object.
			&schema.CopyTransformer{},
			// Transform x-required-for-helm
			&schema.RequiredForHelmTransformer{},
		)
	}

	return res, nil
}
