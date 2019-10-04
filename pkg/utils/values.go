package utils

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"reflect"
	"sort"
	"strings"

	"github.com/romana/rlog"

	"github.com/evanphx/json-patch"
	"github.com/peterbourgon/mergemap"
	"github.com/segmentio/go-camelcase"
	"gopkg.in/yaml.v2"
	k8syaml "sigs.k8s.io/yaml"

	utils_checksum "github.com/flant/shell-operator/pkg/utils/checksum"
)

const (
	GlobalValuesKey = "global"
)

type ValuesPatchType string
const ConfigMapPatch ValuesPatchType = "CONFIG_MAP_PATCH"
const MemoryValuesPatch ValuesPatchType = "MEMORY_VALUES_PATCH"

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
	Value interface{} `json:"value,omitempty"`
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
		return nil, fmt.Errorf("cannot cast data to JSON compatible format: %s:\n%s", err, ValuesToString(values))
	}

	return values, nil
}

func FormatValues(someValues map[interface{}]interface{}) (Values, error) {
	yamlDoc, err := yaml.Marshal(someValues)
	if err != nil {
		return nil, err
	}

	jsonDoc, err := k8syaml.YAMLToJSON(yamlDoc)
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
	operations := []*ValuesPatchOperation{}

	for _, patch := range valuesPatches {
		operations = append(operations, patch.Operations...)
	}
	operations = append(operations, newValuesPatch.Operations...)

	return []ValuesPatch{CompactPatches(operations)}
}

func CompactPatches(operations []*ValuesPatchOperation) ValuesPatch {
	patchesTree := map[string]*ValuesPatchOperation{}

	for _, op := range operations {
		patchesTree[op.Path] = op
		// remove subpathes if got 'remove' operation
		if op.Op == "remove" {
			for subPath := range patchesTree {
				if len(op.Path) < len(subPath) && strings.HasPrefix(subPath, op.Path+"/") {
					delete(patchesTree, subPath)
				}
			}
		}

	}

	paths := []string{}
	for path := range patchesTree {
		paths = append(paths, path)
	}

	sort.Strings(paths)

	newOps := []*ValuesPatchOperation{}
	for _, path := range paths {
		newOps = append(newOps, patchesTree[path])
	}

	newValuesPatch := ValuesPatch{Operations: newOps}
	return newValuesPatch
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

func ValidateHookValuesPatch(valuesPatch ValuesPatch, acceptableKey string) error {
	for _, op := range valuesPatch.Operations {
		if op.Op == "replace" {
			return fmt.Errorf("unsupported patch operation '%s': '%s'", op.Op, op.ToString())
		}

		pathParts := strings.Split(op.Path, "/")
		if len(pathParts) > 1 {
			affectedKey := pathParts[1]
			if affectedKey != acceptableKey {
				return fmt.Errorf("unacceptable patch operation path '%s' (only '%s' accepted): '%s'", affectedKey, acceptableKey, op.ToString())
			}
		}
	}

	return nil
}

func MergeValues(values ...Values) Values {
	res := make(Values)

	for _, v := range values {
		res = mergemap.Merge(res, v)
	}

	return res
}

func ValuesToString(values Values) string {
	valuesYaml, err := yaml.Marshal(&values)
	if err != nil {
		panic(fmt.Sprintf("Cannot dump data to YAML: \n%#v\n error: %s", values, err))
	}
	return string(valuesYaml)
}


func ValuesChecksum(valuesArr ...Values) (string, error) {
	valuesJson, err := json.Marshal(MergeValues(valuesArr...))
	if err != nil {
		return "", err
	}
	return utils_checksum.CalculateChecksum(string(valuesJson)), nil
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

func GetGlobalValues(values Values) Values {
	globalValues, has := values[GlobalValuesKey]
	if has {
		data := map[interface{}]interface{}{GlobalValuesKey: globalValues}
		v, err := NewValues(data)
		if err != nil {
			rlog.Errorf("get global Values: %s", err)
		}
		return v
	}
	return make(Values)
}

type ValuesLoader interface {
	Read() (Values, error)
}

type ValuesDumper interface {
	Write(values Values) error
}

// Load values by specific key from loader
func Load(key string, loader ValuesLoader) (Values, error) {
	return nil, nil
}

// LoadAll loads values from all keys from loader
func LoadAll(loader ValuesLoader) (Values, error) {
	return nil, nil
}

func Dump(values Values, dumper ValuesDumper) error {
	return nil
}

type ValuesDumperToJsonFile struct {
	FileName string
}

func NewDumperToJsonFile(path string) ValuesDumper {
	return &ValuesDumperToJsonFile{
		FileName: path,
	}
}

func (*ValuesDumperToJsonFile) Write(values Values) error {
	return fmt.Errorf("implement Write in ValuesDumperToJsonFile")
}

type ValuesLoaderFromJsonFile struct {
	FileName string
}

func NewLoaderFromJsonFile(path string) ValuesLoader {
	return &ValuesLoaderFromJsonFile{
		FileName: path,
	}
}

func (*ValuesLoaderFromJsonFile) Read() (Values, error) {
	return nil, fmt.Errorf("implement Read methoid")
}
