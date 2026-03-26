package schema

import (
	"strings"

	"github.com/go-openapi/spec"
)

// DefaultOverride defines a single default value override for a schema property.
// Path uses "/" as a separator (JSON Pointer style, without leading slash).
type DefaultOverride struct {
	Path  string `json:"path" yaml:"path"`
	Value any    `json:"value" yaml:"value"`
}

// DefaultsTransformer is a SchemaTransformer that overrides default values
// on schema properties identified by "/"-separated paths.
type DefaultsTransformer struct {
	Overrides []DefaultOverride
}

func (t *DefaultsTransformer) Transform(s *spec.Schema) *spec.Schema {
	if s == nil || len(t.Overrides) == 0 {
		return s
	}

	for _, override := range t.Overrides {
		segments := strings.Split(override.Path, "/")
		setDefault(s, segments, override.Value)
	}

	return s
}

// setDefault walks the schema tree through Properties and sets
// the Default field on the leaf property.
func setDefault(s *spec.Schema, segments []string, value any) {
	if len(segments) == 0 || s == nil {
		return
	}

	propName := segments[0]
	prop, exists := s.Properties[propName]
	if !exists {
		return
	}

	if len(segments) == 1 {
		prop.Default = value
		s.Properties[propName] = prop
		return
	}

	setDefault(&prop, segments[1:], value)
	s.Properties[propName] = prop
}
