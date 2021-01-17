package validation

import (
	"encoding/json"
	"fmt"

	"github.com/go-openapi/spec"
	"github.com/go-openapi/swag"

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

// GetGlobalValuesSchema returns ready-to-use schema for global values.
// schemaType is "config", "values" or "helm"
func (st *SchemaStorage) GlobalValuesSchema(schemaType SchemaType) *spec.Schema {
	return st.GlobalSchemas[schemaType]
}

// GetModuleValuesSchema returns ready-to-use schema for module values.
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

// schemaType is "config" of "memory"
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

// loadSchema returns spec.Schema object loaded from yaml bytes.
func LoadSchemaFromBytes(openApiContent []byte) (*spec.Schema, error) {
	yml, err := swag.BytesToYAMLDoc(openApiContent)
	if err != nil {
		return nil, fmt.Errorf("yaml unmarshal: %v", err)
	}
	d, err := swag.YAMLToJSON(yml)
	if err != nil {
		return nil, fmt.Errorf("yaml to json: %v", err)
	}

	s := new(spec.Schema)

	if err := json.Unmarshal(d, s); err != nil {
		return nil, fmt.Errorf("json unmarshal: %v", err)
	}

	err = spec.ExpandSchema(s, s, nil /*new(noopResCache)*/)
	if err != nil {
		return nil, fmt.Errorf("expand schema: %v", err)
	}

	return s, nil
}

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
			// Copy values schema
			&schema.CopyTransformer{},
			// Transform x-required-for-helm
			&schema.RequiredForHelmTransformer{},
		)
	}

	return res, nil
}
