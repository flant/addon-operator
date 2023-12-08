package validation

import (
	"fmt"
	"reflect"

	"github.com/go-openapi/spec"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/validate"
	"github.com/hashicorp/go-multierror"
	log "github.com/sirupsen/logrus"

	"github.com/flant/addon-operator/pkg/utils"
)

type ValuesValidator struct {
	SchemaStorage *SchemaStorage
}

func NewValuesValidator() *ValuesValidator {
	return &ValuesValidator{
		SchemaStorage: NewSchemaStorage(),
	}
}

func (v *ValuesValidator) ValidateGlobalConfigValues(values utils.Values) error {
	return v.ValidateValues(GlobalSchema, ConfigValuesSchema, "", values)
}

func (v *ValuesValidator) ValidateGlobalValues(values utils.Values) error {
	return v.ValidateValues(GlobalSchema, ValuesSchema, "", values)
}

func (v *ValuesValidator) ValidateModuleConfigValues(moduleName string, values utils.Values) error {
	return v.ValidateValues(ModuleSchema, ConfigValuesSchema, moduleName, values)
}

func (v *ValuesValidator) ValidateModuleValues(moduleName string, values utils.Values) (multiErr error) {
	return v.ValidateValues(ModuleSchema, ValuesSchema, moduleName, values)
}

func (v *ValuesValidator) ValidateModuleHelmValues(moduleName string, values utils.Values) (multiErr error) {
	return v.ValidateValues(ModuleSchema, HelmValuesSchema, moduleName, values)
}

// GetSchema returns a schema from the schema storage.
func (v *ValuesValidator) GetSchema(schemaType SchemaType, valuesType SchemaType, modName string) *spec.Schema {
	switch schemaType {
	case GlobalSchema:
		return v.SchemaStorage.GlobalValuesSchema(valuesType)
	case ModuleSchema:
		return v.SchemaStorage.ModuleValuesSchema(modName, valuesType)
	}
	return nil
}

func (v *ValuesValidator) ValidateValues(schemaType SchemaType, valuesType SchemaType, moduleName string, values utils.Values) error {
	s := v.GetSchema(schemaType, valuesType, moduleName)
	if s == nil {
		log.Warnf("%s schema (%s) for '%s' values is not found", schemaType, moduleName, valuesType)
		fmt.Println(v.SchemaStorage.ModuleSchemas)
		return nil
	}

	rootName := utils.GlobalValuesKey
	if schemaType == ModuleSchema {
		rootName = moduleName
	}

	obj, ok := values[rootName]
	if !ok {
		return fmt.Errorf("root key '%s' not found in input values", rootName)
	}

	validationErr := validateObject(obj, s, rootName)
	if validationErr == nil {
		log.Debugf("'%s' '%s' values are valid", schemaType, valuesType)
	} else {
		log.Debugf("'%s' '%s' values are NOT valid: %v", schemaType, valuesType, validationErr)
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
