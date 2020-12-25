package validation

import (
	"encoding/json"
	"github.com/go-openapi/spec"
)

const XExtendKey = "x-extend"

type ExtendSettings struct {
	Schema *string `json:"schema,omitempty"`
}

func Extend(s *spec.Schema, parent *spec.Schema) *spec.Schema {
	if parent == nil {
		return s
	}

	extendSettings := ExtractExtendSettings(s)

	if extendSettings == nil || extendSettings.Schema == nil {
		return s
	}

	// TODO check extendSettings.Schema. No need to do it for now.

	s.Definitions = mergeDefinitions(s, parent)
	s.Extensions = mergeExtensions(s, parent)
	s.Required = mergeRequired(s, parent)
	s.Properties = mergeProperties(s, parent)
	s.PatternProperties = mergePatternProperties(s, parent)
	s.Title = mergeTitle(s, parent)
	s.Description = mergeDescription(s, parent)

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
	var res = make(map[string]spec.Schema)

	for k, v := range parent.Properties {
		res[k] = v
	}
	for k, v := range s.Properties {
		res[k] = v
	}

	return res
}

func mergePatternProperties(s *spec.Schema, parent *spec.Schema) map[string]spec.Schema {
	var res = make(map[string]spec.Schema)

	for k, v := range parent.PatternProperties {
		res[k] = v
	}
	for k, v := range s.PatternProperties {
		res[k] = v
	}

	return res
}

func mergeDefinitions(s *spec.Schema, parent *spec.Schema) spec.Definitions {
	var res = make(spec.Definitions)

	for k, v := range parent.Definitions {
		res[k] = v
	}
	for k, v := range s.Definitions {
		res[k] = v
	}

	return res
}

func mergeExtensions(s *spec.Schema, parent *spec.Schema) spec.Extensions {
	var ext = make(spec.Extensions)

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
