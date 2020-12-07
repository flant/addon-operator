package validation

import (
	"encoding/json"
	"fmt"

	"github.com/go-openapi/spec"
	"github.com/go-openapi/swag"
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
	MemoryValuesSchema SchemaType = "memory"
)

var GlobalSchemasCache = map[SchemaType]*spec.Schema{}
var ModuleSchemasCache = map[string]map[SchemaType]*spec.Schema{}

// GetGlobalValuesSchema returns ready-to-use schema for global values.
// schemaType is "config" of "memory"
func GetGlobalValuesSchema(schemaType SchemaType) *spec.Schema {
	return GlobalSchemasCache[schemaType]
}

// GetModuleValuesSchema returns ready-to-use schema for module values.
// schemaType is "config" of "memory"
func GetModuleValuesSchema(moduleName string, schemaType SchemaType) *spec.Schema {
	if _, ok := ModuleSchemasCache[moduleName]; !ok {
		return nil
	}

	return ModuleSchemasCache[moduleName][schemaType]
}

// schemaType is "config" of "memory"
func AddGlobalValuesSchema(schemaType SchemaType, openApiContent []byte) error {
	schemaObj, err := loadSchema(openApiContent)
	if err != nil {
		return fmt.Errorf("load global '%s' schema: %s", schemaType, err)
	}
	GlobalSchemasCache[schemaType] = schemaObj
	return nil
}

// schemaType is "config" of "memory"
func AddModuleValuesSchema(moduleName string, schemaType SchemaType, openApiContent []byte) error {
	schemaObj, err := loadSchema(openApiContent)
	if err != nil {
		return fmt.Errorf("load global '%s' schema: %s", schemaType, err)
	}
	if _, ok := ModuleSchemasCache[moduleName]; !ok {
		ModuleSchemasCache[moduleName] = map[SchemaType]*spec.Schema{}
	}
	ModuleSchemasCache[moduleName][schemaType] = schemaObj
	return nil
}

// saveSchema returns spec.Schema object loaded from yaml in Schemas map.
func loadSchema(openApiContent []byte) (*spec.Schema, error) {
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
