package schema

import (
	"github.com/go-openapi/spec"
)

type RequiredForHelmTransformer struct{}

const XRequiredForHelm = "x-required-for-helm"

func (t *RequiredForHelmTransformer) Transform(s *spec.Schema) *spec.Schema {
	if s == nil {
		return s
	}

	s.Required = MergeRequiredFields(s.Extensions, s.Required)

	// Deep transform.
	transformRequired(s.Properties)
	return s
}

func transformRequired(props map[string]spec.Schema) {
	for k, prop := range props {
		prop.Required = MergeRequiredFields(prop.Extensions, prop.Required)
		props[k] = prop
		transformRequired(props[k].Properties)
	}
}

func MergeArrays(ar1 []string, ar2 []string) []string {
	res := make([]string, 0)
	m := make(map[string]struct{})
	for _, item := range ar1 {
		res = append(res, item)
		m[item] = struct{}{}
	}
	for _, item := range ar2 {
		if _, ok := m[item]; !ok {
			res = append(res, item)
		}
	}
	return res
}

func MergeRequiredFields(ext spec.Extensions, required []string) []string {
	var xReqFields []string
	_, hasField := ext[XRequiredForHelm]
	if !hasField {
		return required
	}
	field, ok := ext.GetString(XRequiredForHelm)
	if ok {
		xReqFields = []string{field}
	} else {
		xReqFields, _ = ext.GetStringSlice(XRequiredForHelm)
	}
	// Merge x-required with required
	return MergeArrays(required, xReqFields)
}
