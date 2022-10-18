package validation

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-openapi/loads"
	"github.com/go-openapi/spec"
	"github.com/go-openapi/swag"
	"sigs.k8s.io/yaml"

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

type SchemaStorage struct {
	GlobalSchemas map[SchemaType]*spec.Schema
	ModuleSchemas map[string]map[SchemaType]*spec.Schema
}

func NewSchemaStorage() *SchemaStorage {
	return &SchemaStorage{
		GlobalSchemas: map[SchemaType]*spec.Schema{},
		ModuleSchemas: map[string]map[SchemaType]*spec.Schema{},
	}
}

// GlobalValuesSchema returns ready-to-use schema for global values.
// schemaType is "config", "values" or "helm"
func (st *SchemaStorage) GlobalValuesSchema(schemaType SchemaType) *spec.Schema {
	return st.GlobalSchemas[schemaType]
}

// ModuleValuesSchema returns ready-to-use schema for module values.
// schemaType is "config" of "values"
func (st *SchemaStorage) ModuleValuesSchema(moduleName string, schemaType SchemaType) *spec.Schema {
	if _, ok := st.ModuleSchemas[moduleName]; !ok {
		return nil
	}
	return st.ModuleSchemas[moduleName][schemaType]
}

// AddGlobalValuesSchemas prepares and stores three schemas: config, config+values, config+values+required.
func (st *SchemaStorage) AddGlobalValuesSchemas(configBytes, valuesBytes []byte) error {
	schemas, err := PrepareSchemas(configBytes, valuesBytes)
	if err != nil {
		return fmt.Errorf("prepare global schemas: %s", err)
	}
	st.GlobalSchemas = schemas
	return nil
}

// AddModuleValuesSchemas creates schema for module values.
func (st *SchemaStorage) AddModuleValuesSchemas(moduleName string, configBytes, valuesBytes []byte) error {
	schemas, err := PrepareSchemas(configBytes, valuesBytes)
	if err != nil {
		return fmt.Errorf("prepare module '%s' schemas: %s", moduleName, err)
	}

	if _, ok := st.ModuleSchemas[moduleName]; !ok {
		st.ModuleSchemas[moduleName] = map[SchemaType]*spec.Schema{}
	}
	st.ModuleSchemas[moduleName] = schemas
	return nil
}

// ModuleSchemasDescription describes which schemas are present in storage for the module.
func (st *SchemaStorage) ModuleSchemasDescription(moduleName string) string {
	types := availableSchemaTypes(st.ModuleSchemas[moduleName])
	if len(types) == 0 {
		return "No OpenAPI schemas"
	}
	return fmt.Sprintf("OpenAPI schemas: %s.", strings.Join(types, ", "))
}

// GlobalSchemasDescription describes which global schemas are present in storage.
func (st *SchemaStorage) GlobalSchemasDescription() string {
	types := availableSchemaTypes(st.GlobalSchemas)
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
	var res = make(map[SchemaType]*spec.Schema)
	if configBytes != nil {
		schemaObj, err := LoadSchemaFromBytes(configBytes)
		if err != nil {
			return nil, fmt.Errorf("load global '%s' schema: %s", ConfigValuesSchema, err)
		}
		res[ConfigValuesSchema] = schema.TransformSchema(
			schemaObj,
			&schema.AdditionalPropertiesTransformer{},
		)
	}

	if valuesBytes != nil {
		schemaObj, err := LoadSchemaFromBytes(valuesBytes)
		if err != nil {
			return nil, fmt.Errorf("load global '%s' schema: %s", ValuesSchema, err)
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
