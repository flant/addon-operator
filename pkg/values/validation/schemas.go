package validation

import (
	"encoding/json"
	"fmt"

	"github.com/go-openapi/spec"
	"github.com/go-openapi/swag"
)

/**
 * This package validates global and module values against OpenAPI schemas
 * in /global/openapi and /modules/<module_name>/openapi directories.
 *
 *
 */

/**
/global/hooks/...
/global/openapi/config-values.yaml
/global/openapi/values.yaml
/modules/XXXX/openapi/config-values.yaml
/modules/XXXX/openapi/values.yaml
*/

//var GlobalValuesSchemas = map[string]

//var Schemas = map[string]string{
//	"v1": `
//definitions:
//  nameSelector:
//    type: object
//    additionalProperties: false
//    required:
//    - matchNames
//    properties:
//      matchNames:
//        type: array
//        additionalItems: false
//        items:
//          type: string
//  labelSelector:
//    type: object
//    additionalProperties: false
//    minProperties: 1
//    maxProperties: 2
//    properties:
//      matchLabels:
//        type: object
//        additionalProperties:
//          type: string
//      matchExpressions:
//        type: array
//        items:
//          type: object
//          additionalProperties: false
//          required:
//          - key
//          - operator
//          properties:
//            key:
//              type: string
//            operator:
//              type: string
//              enum:
//              - In
//              - NotIn
//              - Exists
//              - DoesNotExist
//            values:
//              type: array
//              items:
//                type: string
//
//type: object
//additionalProperties: false
//required:
//- configVersion
//minProperties: 2
//properties:
//  configVersion:
//    type: string
//    enum:
//    - v1
//  onStartup:
//    title: onStartup binding
//    description: |
//      the value is the order to sort onStartup hooks
//    type: integer
//    example: 10
//  schedule:
//    title: schedule bindings
//    description: |
//      configuration of hooks that should run on schedule
//    type: array
//    additionalItems: false
//    minItems: 1
//    items:
//      type: object
//      additionalProperties: false
//      required:
//      - crontab
//      properties:
//        name:
//          type: string
//        crontab:
//          type: string
//        allowFailure:
//          type: boolean
//          default: false
//        includeSnapshotsFrom:
//          type: array
//          additionalItems: false
//          minItems: 1
//          items:
//            type: string
//        queue:
//          type: string
//        group:
//          type: string
//  kubernetes:
//    title: kubernetes event bindings
//    type: array
//    additionalItems: false
//    minItems: 1
//    items:
//      type: object
//      additionalProperties: false
//      required:
//      - kind
//      patternProperties:
//        "^(watchEvent|executeHookOnEvent)$":
//          type: array
//          additionalItems: false
//          minItems: 0
//          items:
//            type: string
//            enum:
//            - Added
//            - Modified
//            - Deleted
//      properties:
//        name:
//          type: string
//        apiVersion:
//          type: string
//        kind:
//          type: string
//        includeSnapshotsFrom:
//          type: array
//          additionalItems: false
//          minItems: 1
//          items:
//            type: string
//        queue:
//          type: string
//        jqFilter:
//          type: string
//          example: ".metadata.labels"
//        keepFullObjectsInMemory:
//          type: boolean
//        allowFailure:
//          type: boolean
//        executeHookOnSynchronization:
//          type: boolean
//        waitForSynchronization:
//          type: boolean
//        resynchronizationPeriod:
//          type: string
//        nameSelector:
//          "$ref": "#/definitions/nameSelector"
//        labelSelector:
//          "$ref": "#/definitions/labelSelector"
//        fieldSelector:
//          type: object
//          additionalProperties: false
//          required:
//          - matchExpressions
//          properties:
//            matchExpressions:
//              type: array
//              items:
//                type: object
//                additionalProperties: false
//                minProperties: 3
//                maxProperties: 3
//                properties:
//                  field:
//                    type: string
//                  operator:
//                    type: string
//                    enum: ["=", "==", "Equals", "!=", "NotEquals"]
//                  value:
//                    type: string
//        group:
//          type: string
//        namespace:
//          type: object
//          additionalProperties: false
//          minProperties: 1
//          maxProperties: 2
//          properties:
//            nameSelector:
//              "$ref": "#/definitions/nameSelector"
//            labelSelector:
//              "$ref": "#/definitions/labelSelector"
//`,
//	"v0": `
//type: object
//additionalProperties: false
//minProperties: 1
//properties:
//  onStartup:
//    title: onStartup binding
//    description: |
//      the value is the order to sort onStartup hooks
//    type: integer
//  schedule:
//    type: array
//    items:
//      type: object
//  onKubernetesEvent:
//    type: array
//    items:
//      type: object
//`,
//}

type ValuesSchemaType string

const (
	GlobalSchema       ValuesSchemaType = "global"
	ModuleSchema       ValuesSchemaType = "module"
	ConfigValuesSchema ValuesSchemaType = "config"
	MemoryValuesSchema ValuesSchemaType = "memory"
)

var GlobalSchemasCache = map[string]*spec.Schema{}
var ModuleSchemasCache = map[string]map[string]*spec.Schema{}

// GetGlobalValuesSchema returns ready-to-use schema for global values.
func GetGlobalValuesSchema(valuesType string) *spec.Schema {
	if s, ok := GlobalSchemasCache[valuesType]; ok {
		return s
	}
	return nil
}

// GetModuleValuesSchema returns ready-to-use schema for module values.
func GetModuleValuesSchema(moduleName string, valuesType string) *spec.Schema {
	if _, ok := ModuleSchemasCache[moduleName]; !ok {
		return nil
	}

	if s, ok := ModuleSchemasCache[moduleName][valuesType]; ok {
		return s
	}

	return nil
}

func AddGlobalValuesSchema(schemaType string, openApiContent []byte) error {
	schemaObj, err := loadSchema(openApiContent)
	if err != nil {
		return fmt.Errorf("load global '%s' schema: %s", schemaType, err)
	}
	GlobalSchemasCache[schemaType] = schemaObj
	return nil
}

func AddModuleValuesSchema(moduleName string, schemaType string, openApiContent []byte) error {
	schemaObj, err := loadSchema(openApiContent)
	if err != nil {
		return fmt.Errorf("load global '%s' schema: %s", schemaType, err)
	}
	if _, ok := ModuleSchemasCache[moduleName]; !ok {
		ModuleSchemasCache[moduleName] = map[string]*spec.Schema{}
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
