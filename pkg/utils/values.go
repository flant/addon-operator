package utils

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/davecgh/go-spew/spew"
	log "github.com/deckhouse/deckhouse/go_lib/log"
	"github.com/ettle/strcase"
	"gopkg.in/yaml.v3"
	k8syaml "sigs.k8s.io/yaml"

	utils_checksum "github.com/flant/shell-operator/pkg/utils/checksum"
)

const (
	GlobalValuesKey = "global"
)

// Values stores values for modules or hooks by name.
type Values map[string]interface{}

// ModuleNameToValuesKey returns camelCased name from kebab-cased (very-simple-module become verySimpleModule)
func ModuleNameToValuesKey(moduleName string) string {
	return strcase.ToCamel(moduleName)
}

// ModuleNameFromValuesKey returns kebab-cased name from camelCased (verySimpleModule become very-simple-module)
func ModuleNameFromValuesKey(moduleValuesKey string) string {
	return strcase.ToKebab(moduleValuesKey)
}

// NewValuesFromBytes loads values sections from maps in yaml or json format
func NewValuesFromBytes(data []byte) (Values, error) {
	var values map[string]interface{}

	err := k8syaml.Unmarshal(data, &values)
	if err != nil {
		return nil, fmt.Errorf("bad values data: %s\n%s", err, string(data))
	}

	return values, nil
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
		res = mergeMap(res, v)
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

func (v Values) Checksum() string {
	valuesJson, _ := json.Marshal(v)

	return utils_checksum.CalculateChecksum(string(valuesJson))
}

func (v Values) HasKey(key string) bool {
	_, has := v[key]
	return has
}

func (v Values) GetKeySection(key string) Values {
	section, has := v[key]
	if !has {
		return Values{}
	}
	switch sec := section.(type) {
	case map[string]interface{}:
		return sec

	case Values:
		return sec
	}

	return Values{}
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

// Deprecated: some useless copy here, probably we don't need that
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

func (v Values) AsString(format string) string {
	b, _ := v.AsBytes(format)

	return string(b)
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

func (v Values) JsonString() string {
	return v.AsString("json")
}

func (v Values) JsonBytes() ([]byte, error) {
	return v.AsBytes("json")
}

func (v Values) YamlString() string {
	return v.AsString("yaml")
}

func (v Values) YamlBytes() ([]byte, error) {
	return v.AsBytes("yaml")
}

func (v Values) IsEmpty() bool {
	return len(v) == 0
}

// Copy returns full deep copy of the Values
func (v Values) Copy() Values {
	return deepCopyMap(v)
}

func deepCopyMap(originalMap map[string]interface{}) map[string]interface{} {
	copiedMap := make(map[string]interface{})
	for key, value := range originalMap {
		copiedMap[key] = valueDeepCopy(value)
	}
	return copiedMap
}

func valueDeepCopy(item interface{}) interface{} {
	if item == nil {
		return nil
	}

	typ := reflect.TypeOf(item)
	val := reflect.ValueOf(item)

	switch typ.Kind() {
	case reflect.Ptr:
		newVal := reflect.New(typ.Elem())
		newVal.Elem().Set(reflect.ValueOf(valueDeepCopy(val.Elem().Interface())))
		return newVal.Interface()

	case reflect.Map:
		newMap := reflect.MakeMap(typ)
		for _, k := range val.MapKeys() {
			newMap.SetMapIndex(k, reflect.ValueOf(valueDeepCopy(val.MapIndex(k).Interface())))
		}
		return newMap.Interface()

	case reflect.Slice:
		newSlice := reflect.MakeSlice(typ, val.Len(), val.Cap())
		for i := 0; i < val.Len(); i++ {
			newSlice.Index(i).Set(reflect.ValueOf(valueDeepCopy(val.Index(i).Interface())))
		}
		return newSlice.Interface()
	}

	return item
}
