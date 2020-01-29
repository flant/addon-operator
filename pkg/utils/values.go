package utils

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"reflect"
	"sort"
	"strings"

	"github.com/davecgh/go-spew/spew"
	log "github.com/sirupsen/logrus"

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

// NewValuesFromBytes loads values sections from maps in yaml or json format
func NewValuesFromBytes(data []byte) (Values, error) {
	var values map[string]interface{}

	err := k8syaml.Unmarshal(data, &values)
	if err != nil {
		return nil, fmt.Errorf("bad values data: %s\n%s", err, string(data))
	}

	return Values(values), nil
}

// NewValues load all sections from input data and makes sure that input map
// can be marshaled to yaml and that yaml is compatible with json.
func NewValues(data map[string]interface{}) (Values, error) {
	yamlDoc, err := k8syaml.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("data is not compatible with JSON and YAML: %s, data:\n%s", err, spew.Sdump(data))
	}

	var values Values
	if err := k8syaml.Unmarshal(yamlDoc, &values); err != nil {
		return nil, fmt.Errorf("convert data YAML to values: %s, data:\n%s", err, spew.Sdump(data))
	}

	return values, nil
}

// NewGlobalValues creates Values with global section loaded from input string.
func NewGlobalValues(globalSectionContent string) (Values, error) {
	var section map[string]interface{}
	if err := k8syaml.Unmarshal([]byte(globalSectionContent), &section); err != nil {
		return nil, fmt.Errorf("global section is not compatible with JSON and YAML: %s, data:\n%s", err, globalSectionContent)
	}

	return Values(map[string]interface{}{
		GlobalValuesKey: section,
	}), nil
}

// TODO used only in tests
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
	return CompactValuesPatches(valuesPatches, newValuesPatch)
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

// DebugString returns values as yaml or an error line if dump is failed
func (v Values) DebugString() string {
	b, err := v.YamlBytes()
	if err != nil {
		return "bad values: " + err.Error()
	}
	return string(b)
}

// TODO used for Debugf messages, should be replaced with DebugString
func ValuesToString(values Values) string {
	valuesYaml, err := yaml.Marshal(&values)
	if err != nil {
		panic(fmt.Sprintf("Cannot dump data to YAML: \n%#v\n error: %s", values, err))
	}
	return string(valuesYaml)
}


func (v Values) Checksum() (string, error) {
	valuesJson, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return utils_checksum.CalculateChecksum(string(valuesJson)), nil
}

func (v Values) Global() Values {
	globalValues, has := v[GlobalValuesKey]
	if has {
		data := map[string]interface{}{GlobalValuesKey: globalValues}
		newV, err := NewValues(data)
		if err != nil {
			log.Errorf("get global Values: %s", err)
		}
		return newV
	}
	return make(Values)
}

func (v Values) AsBytes(format string) ([]byte, error) {
	switch format {
	case "json":
		return json.Marshal(v)
	case "yaml":
		fallthrough
	default:
		return yaml.Marshal(v)
	}
}

func (v Values) AsString(format string) (string, error) {
	b, err := v.AsBytes(format)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (v Values) JsonString() (string, error) {
	return v.AsString("json")
}

func (v Values) JsonBytes() ([]byte, error) {
	return v.AsBytes("json")
}

func (v Values) YamlString() (string, error) {
	return v.AsString("yaml")
}

func (v Values) YamlBytes() ([]byte, error) {
	return v.AsBytes("yaml")
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
