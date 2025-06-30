package cel

import (
	"errors"
	"fmt"
	"strings"

	"github.com/go-openapi/spec"
	"github.com/google/cel-go/cel"
	"google.golang.org/protobuf/types/known/structpb"
)

const ruleKey = "x-deckhouse-validations"

type rule struct {
	Expression string `json:"expression" yaml:"expression"`
	Message    string `json:"message" yaml:"message"`
}

// Validate validates config values against x-deckhouse-validation rules in schema
func Validate(schema *spec.Schema, values any) ([]error, error) {
	var validationErrs []error
	// First we validate only object nested properties
	if m, ok := values.(map[string]any); ok {
		for propName, propSchema := range schema.Properties {
			if propValue, ok := m[propName]; ok {
				subErrs, err := Validate(&propSchema, propValue)
				if err != nil {
					return nil, err
				}
				validationErrs = append(validationErrs, subErrs...)
			}
		}
	}

	raw, found := schema.Extensions[ruleKey]
	if !found {
		if len(validationErrs) > 0 {
			return validationErrs, nil
		}
		return nil, nil
	}

	var rules []rule
	switch v := raw.(type) {
	case []any:
		for _, entry := range v {
			mapEntry, ok := entry.(map[string]any)
			if !ok || len(mapEntry) == 0 {
				return nil, fmt.Errorf("x-deckhouse-validations invalid")
			}

			if val, ok := mapEntry["expression"]; !ok || len(val.(string)) == 0 {
				return nil, fmt.Errorf("x-deckhouse-validations invalid: missing expression")
			}
			if val, ok := mapEntry["message"]; !ok || len(val.(string)) == 0 {
				return nil, fmt.Errorf("x-deckhouse-validations invalid: missing message")
			}

			rules = append(rules, rule{
				Expression: mapEntry["expression"].(string),
				Message:    mapEntry["message"].(string),
			})
		}
	default:
		return nil, fmt.Errorf("x-deckhouse-validations invalid")
	}

	celSelfType, celSelfValue, err := buildCELValueAndType(values)
	if err != nil {
		return nil, err
	}

	env, err := cel.NewEnv(cel.Variable("self", celSelfType))
	if err != nil {
		return nil, fmt.Errorf("create CEL env: %w", err)
	}
	for _, r := range rules {
		ast, issues := env.Compile(r.Expression)
		if issues.Err() != nil {
			return nil, fmt.Errorf("compile the '%s' rule: %w", r.Expression, issues.Err())
		}

		prg, err := env.Program(ast)
		if err != nil {
			return nil, fmt.Errorf("create program for the '%s' rule: %w", r.Expression, err)
		}

		out, _, err := prg.Eval(map[string]any{"self": celSelfValue})
		if err != nil {
			if strings.Contains(err.Error(), "no such key:") {
				continue
			}
			return nil, fmt.Errorf("evaluate the '%s' rule: %w", r.Expression, err)
		}

		pass, ok := out.Value().(bool)
		if !ok {
			return nil, errors.New("rule should return boolean")
		}
		if !pass {
			validationErrs = append(validationErrs, errors.New(r.Message))
		}
	}

	return validationErrs, nil
}

func buildCELValueAndType(value any) (*cel.Type, any, error) {
	switch v := value.(type) {
	case map[string]any:
		obj, err := structpb.NewStruct(v)
		if err != nil {
			return nil, nil, fmt.Errorf("convert values to struct: %w", err)
		}
		return cel.MapType(cel.StringType, cel.DynType), obj, nil
	case []any:
		list, err := structpb.NewList(v)
		if err != nil {
			return nil, nil, fmt.Errorf("convert array to list: %w", err)
		}
		return cel.ListType(cel.DynType), list, nil
	default:
		val, err := structpb.NewValue(v)
		if err != nil {
			return nil, nil, fmt.Errorf("convert dyn to value: %w", err)
		}
		return cel.DynType, val, nil
	}
}
