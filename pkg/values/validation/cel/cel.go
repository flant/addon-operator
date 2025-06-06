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
func Validate(schema *spec.Schema, values map[string]interface{}) error {
	env, err := cel.NewEnv(cel.Variable("self", cel.MapType(cel.StringType, cel.DynType)))
	if err != nil {
		return fmt.Errorf("create CEL env: %w", err)
	}

	raw, found := schema.Extensions[ruleKey]
	if !found {
		return nil
	}

	var rules []rule
	switch v := raw.(type) {
	case []interface{}:
		for _, entry := range v {
			mapEntry, ok := entry.(map[string]interface{})
			if !ok || len(mapEntry) == 0 {
				return fmt.Errorf("x-deckhouse-validations invalid")
			}

			if val, ok := mapEntry["expression"]; !ok || len(val.(string)) == 0 {
				return fmt.Errorf("x-deckhouse-validations invalid: missing expression")
			}
			if val, ok := mapEntry["message"]; !ok || len(val.(string)) == 0 {
				return fmt.Errorf("x-deckhouse-validations invalid: missing message")
			}

			rules = append(rules, rule{
				Expression: mapEntry["expression"].(string),
				Message:    mapEntry["message"].(string),
			})
		}
	default:
		return fmt.Errorf("x-deckhouse-validations invalid")
	}

	obj, err := structpb.NewStruct(values)
	if err != nil {
		return fmt.Errorf("convert values to struct: %w", err)
	}

	for _, r := range rules {
		ast, issues := env.Compile(r.Expression)
		if issues.Err() != nil {
			return fmt.Errorf("compile the '%s' rule: %w", r.Expression, issues.Err())
		}

		prg, err := env.Program(ast)
		if err != nil {
			return fmt.Errorf("create program for the '%s' rule: %w", r.Expression, err)
		}

		out, _, err := prg.Eval(map[string]interface{}{"self": obj})
		if err != nil {
			if strings.Contains(err.Error(), "no such key:") {
				continue
			}
			return fmt.Errorf("evaluate the '%s' rule: %w", r.Expression, err)
		}

		pass, ok := out.Value().(bool)
		if !ok {
			return errors.New("rule should return boolean")
		}
		if !pass {
			return errors.New(r.Message)
		}
	}

	return nil
}
