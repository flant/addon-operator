package schema

import "github.com/go-openapi/spec"

type AdditionalPropertiesTransformer struct {
	Parent *spec.Schema
}

func (t *AdditionalPropertiesTransformer) Transform(s *spec.Schema) *spec.Schema {
	if s.AdditionalProperties == nil {
		s.AdditionalProperties = &spec.SchemaOrBool{
			Allows: false,
		}
	}
	return s
}
