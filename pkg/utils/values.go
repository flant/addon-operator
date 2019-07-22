package utils

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"reflect"
	"strings"

	"github.com/evanphx/json-patch"
	ghodssyaml "github.com/ghodss/yaml"
	"github.com/go-yaml/yaml"
	"github.com/peterbourgon/mergemap"
	"github.com/segmentio/go-camelcase"

	utils_data "github.com/flant/shell-operator/pkg/utils/data"
)

const (
	GlobalValuesKey = "global"
)

// Values stores values for modules or hooks by name.
type Values map[string]interface{}

type ValuesPatch struct {
	Operations []*ValuesPatchOperation
}

func (p *ValuesPatch) JsonPatch() jsonpatch.Patch {
	data, err := json.Marshal(p.Operations)
	if err != nil {
		panic(err)
	}

	patch, err := jsonpatch.DecodePatch(data)
	if err != nil {
		panic(err)
	}

	return patch
}

type ValuesPatchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value"`
}

func (op *ValuesPatchOperation) ToString() string {
	data, err := json.Marshal(op)
	if err != nil {
		panic(err)
	}
	return string(data)
}

// ModuleNameToValuesKey returns camelCased name from kebab-cased (very-simple-module become verySimpleModule)
func ModuleNameToValuesKey(moduleName string) string {
	return camelcase.Camelcase(moduleName)
}

// ModuleNameFromValuesKey returns kebab-cased name from camelCased (verySimpleModule become ver-simple-module)
func ModuleNameFromValuesKey(moduleValuesKey string) string {
	b := make([]byte, 0, 64)
	l := len(moduleValuesKey)
	i := 0

	for i < l {
		c := moduleValuesKey[i]

		if c >= 'A' && c <= 'Z' {
			if i > 0 {
				// Appends dash module name parts delimiter.
				b = append(b, '-')
			}
			// Appends lowercased symbol.
			b = append(b, c+('a'-'A'))
		} else if c >= '0' && c <= '9' {
			if i > 0 {
				// Appends dash module name parts delimiter.
				b = append(b, '-')
			}
			b = append(b, c)
		} else {
			b = append(b, c)
		}

		i++
	}

	return string(b)
}

func NewValuesFromBytes(data []byte) (Values, error) {
	var rawValues map[interface{}]interface{}

	err := yaml.Unmarshal(data, &rawValues)
	if err != nil {
		return nil, fmt.Errorf("bad values data: %s\n%s", err, string(data))
	}

	return NewValues(rawValues)
}

func NewValues(data map[interface{}]interface{}) (Values, error) {
	values, err := FormatValues(data)
	if err != nil {
		return nil, fmt.Errorf("cannot cast data to JSON compatible format: %s:\n%s", err, utils_data.YamlToString(data))
	}

	return values, nil
}

func FormatValues(someValues map[interface{}]interface{}) (Values, error) {
	yamlDoc, err := yaml.Marshal(someValues)
	if err != nil {
		return nil, err
	}

	jsonDoc, err := ghodssyaml.YAMLToJSON(yamlDoc)
	if err != nil {
		return nil, err
	}

	values := make(Values)
	if err := json.Unmarshal(jsonDoc, &values); err != nil {
		return nil, err
	}

	return values, nil
}

func MustValuesPatch(res *ValuesPatch, err error) *ValuesPatch {
	if err != nil {
		panic(err)
	}
	return res
}

func ValuesPatchFromBytes(data []byte) (*ValuesPatch, error) {
	_, err := jsonpatch.DecodePatch(data)
	if err != nil {
		return nil, fmt.Errorf("bad json-patch data: %s\n%s", err, string(data))
	}

	var operations []*ValuesPatchOperation
	if err := json.Unmarshal(data, &operations); err != nil {
		return nil, fmt.Errorf("bad json-patch data: %s\n%s", err, string(data))
	}

	return &ValuesPatch{Operations: operations}, nil
}

func ValuesPatchFromFile(filePath string) (*ValuesPatch, error) {
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %s", filePath, err)
	}

	if len(data) == 0 {
		return nil, nil
	}

	return ValuesPatchFromBytes(data)
}

func AppendValuesPatch(valuesPatches []ValuesPatch, newValuesPatch ValuesPatch) []ValuesPatch {
	// TODO #280 - придумать более безопасный способ compact-а.
	//compactValuesPatches := CompactValuesPatches(valuesPatches, newValuesPatch)
	return append(valuesPatches, newValuesPatch)
}

func CompactValuesPatches(valuesPatches []ValuesPatch, newValuesPatch ValuesPatch) []ValuesPatch {
	var compactValuesPatches []ValuesPatch
	for _, valuesPatch := range valuesPatches {
		compactValuesPatchOperations := CompactValuesPatchOperations(valuesPatch.Operations, newValuesPatch.Operations)
		if compactValuesPatchOperations != nil {
			valuesPatch.Operations = compactValuesPatchOperations
			compactValuesPatches = append(compactValuesPatches, valuesPatch)
		}
	}
	return compactValuesPatches
}

func CompactValuesPatchOperations(operations []*ValuesPatchOperation, newOperations []*ValuesPatchOperation) []*ValuesPatchOperation {
	var compactOperations []*ValuesPatchOperation

operations:
	for _, operation := range operations {
		for _, newOperation := range newOperations {
			if newOperation.Op == operation.Op {
				equalPath := newOperation.Path == operation.Path
				subpathOfPath := strings.HasPrefix(operation.Path, strings.Join([]string{newOperation.Path, "/"}, ""))

				if equalPath || subpathOfPath {
					continue operations
				}
			}
		}
		compactOperations = append(compactOperations, operation)
	}

	return compactOperations
}

func ApplyValuesPatch(values Values, valuesPatch ValuesPatch) (Values, bool, error) {
	var err error
	resValues := values

	if resValues, err = ApplyJsonPatchToValues(resValues, valuesPatch.JsonPatch()); err != nil {
		return nil, false, err
	}

	valuesChanged := !reflect.DeepEqual(values, resValues)

	return resValues, valuesChanged, nil
}

func ApplyJsonPatchToValues(values Values, patch jsonpatch.Patch) (Values, error) {
	jsonDoc, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}

	resJsonDoc, err := patch.Apply(jsonDoc)
	if err != nil {
		return nil, err
	}

	resValues := make(Values)
	if err = json.Unmarshal(resJsonDoc, &resValues); err != nil {
		return nil, err
	}

	return resValues, nil
}

func MergeValues(values ...Values) Values {
	res := make(Values)

	for _, v := range values {
		res = mergemap.Merge(res, v)
	}

	return res
}

func ValuesToString(values Values) string {
	return utils_data.YamlToString(values)
}

func MustDump(data []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return data
}

func DumpValuesYaml(values Values) ([]byte, error) {
	return yaml.Marshal(values)
}

func DumpValuesJson(values Values) ([]byte, error) {
	return json.Marshal(values)
}
