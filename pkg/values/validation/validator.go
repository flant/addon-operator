package validation

import (
	"fmt"

	"github.com/go-openapi/spec"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/validate"
	"github.com/hashicorp/go-multierror"
	log "github.com/sirupsen/logrus"

	"github.com/flant/addon-operator/pkg/utils"
)

func ValidateGlobalConfigValues(values utils.Values) error {
	return ValidateValues(GlobalSchema, ConfigValuesSchema, "", values)
}

func ValidateGlobalValues(values utils.Values) error {
	return ValidateValues(GlobalSchema, MemoryValuesSchema, "", values)
}

func ValidateModuleConfigValues(moduleName string, values utils.Values) error {
	return ValidateValues(ModuleSchema, ConfigValuesSchema, moduleName, values)
}

func ValidateModuleValues(moduleName string, values utils.Values) (multiErr error) {
	return ValidateValues(ModuleSchema, MemoryValuesSchema, moduleName, values)
}

func ValidateValues(schemaType SchemaType, valuesType SchemaType, moduleName string, values utils.Values) error {
	var s *spec.Schema
	var obj interface{}
	var ok bool
	var rootName string
	if schemaType == "global" {
		s = GetGlobalValuesSchema(valuesType)
		if s == nil {
			log.Debugf("schema for %s '%s' values is not found", schemaType, valuesType)
			return nil
		}
		obj, ok = values[utils.GlobalValuesKey]
		if !ok {
			return fmt.Errorf("values should have a 'global' key '%s'", utils.GlobalValuesKey)
		}
		rootName = utils.GlobalValuesKey
	} else {
		s = GetModuleValuesSchema(moduleName, valuesType)
		if s == nil {
			log.Debugf("module '%s': schema for '%s' values is not found", moduleName, valuesType)
			return nil
		}
		obj, ok = values[moduleName]
		if !ok {
			return fmt.Errorf("values should have a module name key '%s'", moduleName)
		}
		rootName = moduleName
	}

	validationErr := ValidateObject(obj, s, rootName)
	if validationErr == nil {
		log.Debugf("'%s' '%s' values are valid", schemaType, valuesType)
	} else {
		log.Debugf("'%s' '%s' values are NOT valid: %v", schemaType, valuesType, validationErr)
	}
	return validationErr
}

// See https://github.com/kubernetes/apiextensions-apiserver/blob/1bb376f70aa2c6f2dec9a8c7f05384adbfac7fbb/pkg/apiserver/validation/validation.go#L47
func ValidateObject(dataObj interface{}, s *spec.Schema, rootName string) (multiErr error) {
	if s == nil {
		return fmt.Errorf("validate config: schema is not provided")
	}

	validator := validate.NewSchemaValidator(s, nil, rootName, strfmt.Default) //, validate.DisableObjectArrayTypeCheck(true)

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
