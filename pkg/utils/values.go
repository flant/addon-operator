package utils

import (
	"encoding/json"
	"fmt"
	"github.com/davecgh/go-spew/spew"
	log "github.com/sirupsen/logrus"

	"github.com/peterbourgon/mergemap"
	"github.com/segmentio/go-camelcase"
	"gopkg.in/yaml.v3"
	k8syaml "sigs.k8s.io/yaml"

	utils_checksum "github.com/flant/shell-operator/pkg/utils/checksum"
)

const (
	GlobalValuesKey = "global"
)

// Values stores values for modules or hooks by name.
type Values map[string]interface{}

//func (vl Values) Map() map[string]interface{} {
//	res := map[string]interface{}{}
//	for k, v := range vl {
//		switch obj := v.(type) {
//		case Values:
//			res[k] = obj.Map()
//		case *Values:
//			res[k] = obj.Map()
//		default:
//			res[k] = v
//		}
//	}
//	return res
//}

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

	return Values{
		GlobalValuesKey: section,
	}, nil
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

func (v Values) Checksum() (string, error) {
	valuesJson, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return utils_checksum.CalculateChecksum(string(valuesJson)), nil
}

func (v Values) HasKey(key string) bool {
	_, has := v[key]
	return has
}

func (v Values) HasGlobal() bool {
	_, has := v[GlobalValuesKey]
	return has
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

func (v Values) SectionByKey(key string) Values {
	sectionValues, has := v[key]
	if has {
		data := map[string]interface{}{key: sectionValues}
		newV, err := NewValues(data)
		if err != nil {
			log.Errorf("get section '%s' Values: %s", key, err)
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

// AsConfigMapData returns values as map that can be used as a 'data' field in the ConfigMap.
func (v Values) AsConfigMapData() (map[string]string, error) {
	res := make(map[string]string)

	for k, value := range v {
		dump, err := yaml.Marshal(value)
		if err != nil {
			return nil, err
		}
		res[k] = string(dump)
	}
	return res, nil
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
