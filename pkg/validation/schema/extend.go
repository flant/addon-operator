package schema

import (
	"encoding/json"

	"github.com/go-openapi/spec"
)

const XExtendKey = "x-extend"

type ExtendSettings struct {
	Schema *string `json:"schema,omitempty"`
}

type ExtendTransformer struct {
	Parent *spec.Schema
}

func (t *ExtendTransformer) Transform(s *spec.Schema) *spec.Schema {
	if t.Parent == nil {
		return s
	}

	extendSettings := ExtractExtendSettings(s)

	if extendSettings == nil || extendSettings.Schema == nil {
		return s
	}

	// TODO check extendSettings.Schema. No need to do it for now.

	s.Definitions = mergeDefinitions(s, t.Parent)
	s.Extensions = mergeExtensions(s, t.Parent)
	s.Required = mergeRequired(s, t.Parent)
	s.Properties = mergeProperties(s, t.Parent)
	s.PatternProperties = mergePatternProperties(s, t.Parent)
	s.Title = mergeTitle(s, t.Parent)
	s.Description = mergeDescription(s, t.Parent)

	return s
}

func ExtractExtendSettings(s *spec.Schema) *ExtendSettings {
	if s == nil {
		return nil
	}

	extendSettingsObj, ok := s.Extensions[XExtendKey]
	if !ok {
		return nil
	}

	if extendSettingsObj == nil {
		return nil
	}

	tmpBytes, _ := json.Marshal(extendSettingsObj)

	res := new(ExtendSettings)

	_ = json.Unmarshal(tmpBytes, res)
	return res
}

func mergeRequired(s *spec.Schema, parent *spec.Schema) []string {
	res := make([]string, 0)
	resIdx := make(map[string]struct{})

	for _, name := range parent.Required {
		res = append(res, name)
		resIdx[name] = struct{}{}
	}

	for _, name := range s.Required {
		if _, ok := resIdx[name]; !ok {
			res = append(res, name)
		}
	}

	return res
}

func mergeProperties(s *spec.Schema, parent *spec.Schema) map[string]spec.Schema {
	res := make(map[string]spec.Schema)

	for k, v := range parent.Properties {
		res[k] = v
	}
	for k, v := range s.Properties {
		res[k] = v
	}

	return res
}

func mergePatternProperties(s *spec.Schema, parent *spec.Schema) map[string]spec.Schema {
	res := make(map[string]spec.Schema)

	for k, v := range parent.PatternProperties {
		res[k] = v
	}
	for k, v := range s.PatternProperties {
		res[k] = v
	}

	return res
}

func mergeDefinitions(s *spec.Schema, parent *spec.Schema) spec.Definitions {
	res := make(spec.Definitions)

	for k, v := range parent.Definitions {
		res[k] = v
	}
	for k, v := range s.Definitions {
		res[k] = v
	}

	return res
}

func mergeExtensions(s *spec.Schema, parent *spec.Schema) spec.Extensions {
	ext := make(spec.Extensions)

	for k, v := range parent.Extensions {
		ext.Add(k, v)
	}
	for k, v := range s.Extensions {
		ext.Add(k, v)
	}

	return ext
}

func mergeTitle(s *spec.Schema, parent *spec.Schema) string {
	if s.Title != "" {
		return s.Title
	}
	return parent.Title
}

func mergeDescription(s *spec.Schema, parent *spec.Schema) string {
	if s.Description != "" {
		return s.Description
	}
	return parent.Description
}
