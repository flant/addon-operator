package validation

import (
	"github.com/go-openapi/spec"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/flant/addon-operator/pkg/utils"
)

// ApplyDefaults traverses an object and apply default values from OpenAPI schema.
// It returns true if obj is changed.
//
// See https://github.com/kubernetes/kubernetes/blob/cea1d4e20b4a7886d8ff65f34c6d4f95efcb4742/staging/src/k8s.io/apiextensions-apiserver/pkg/apiserver/schema/defaulting/algorithm.go
//
// Note: check only Properties for object type and List validation for array type.
func ApplyDefaults(obj interface{}, s *spec.Schema) bool {
	if s == nil {
		return false
	}

	res := false

	// Support utils.Values
	switch vals := obj.(type) {
	case utils.Values:
		obj = map[string]interface{}(vals)
	case *utils.Values:
		// rare case
		obj = map[string]interface{}(*vals)
	}

	switch obj := obj.(type) {
	case map[string]interface{}:
		// Apply defaults to properties
		for k, prop := range s.Properties {
			if prop.Default == nil {
				continue
			}
			if _, found := obj[k]; !found {
				obj[k] = runtime.DeepCopyJSONValue(prop.Default)
				res = true
			}
		}
		// Apply to deeper levels.
		for k, v := range obj {
			if prop, found := s.Properties[k]; found {
				deepRes := ApplyDefaults(v, &prop)
				res = res || deepRes
			}
		}
	case []interface{}:
		// If the 'items' section is not specified in the schema, addon-operator will panic here.
		// The schema itself should be validated earlier before applying defaults,
		// but having a panic in runtime is much bigger problem.
		if s.Items == nil {
			return res
		}

		// Only List validation is supported.
		// See https://json-schema.org/understanding-json-schema/reference/array.html#list-validation
		for _, v := range obj {
			deepRes := ApplyDefaults(v, s.Items.Schema)
			res = res || deepRes
		}
	default:
		// scalars, no action
	}

	return res
}
