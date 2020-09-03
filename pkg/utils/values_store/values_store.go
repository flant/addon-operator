package values_store

import (
	"encoding/json"
	"fmt"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"gopkg.in/yaml.v3"

	"github.com/flant/addon-operator/pkg/utils"
)

type ValuesStore struct {
	JsonRepr []byte
}

func NewValuesStoreFromValues(values utils.Values) *ValuesStore {
	jsonBytes, _ := values.JsonBytes()
	return &ValuesStore{JsonRepr: jsonBytes}
}

func (store *ValuesStore) Get(path string) ValuesResult {
	gjsonResult := gjson.GetBytes(store.JsonRepr, path)
	result := ValuesResult{Result: gjsonResult}
	return result
}

func (store *ValuesStore) GetAsYaml() []byte {
	yamlRaw, _ := ConvertJsonToYaml(store.JsonRepr)
	return yamlRaw
}

func (store *ValuesStore) SetByPath(path string, value interface{}) error {
	newValues, err := sjson.SetBytes(store.JsonRepr, path, value)
	if err != nil {
		return fmt.Errorf("failed to set values by path \"%s\": %s\n\nin JSON:\n%s", path, err, store.JsonRepr)
	}
	store.JsonRepr = newValues
	return nil
}

func (store *ValuesStore) SetByPathFromYaml(path string, yamlRaw []byte) {
	jsonRaw, _ := ConvertYamlToJson(yamlRaw)

	newValues, _ := sjson.SetRawBytes(store.JsonRepr, path, jsonRaw)

	store.JsonRepr = newValues
}

func (store *ValuesStore) SetByPathFromJson(path string, jsonRaw []byte) {
	newValues, _ := sjson.SetRawBytes(store.JsonRepr, path, jsonRaw)

	store.JsonRepr = newValues
}

func (store *ValuesStore) DeleteByPath(path string) {
	newValues, _ := sjson.DeleteBytes(store.JsonRepr, path)

	store.JsonRepr = newValues
}

func ConvertYamlToJson(yamlBytes []byte) ([]byte, error) {
	var obj interface{}

	err := yaml.Unmarshal(yamlBytes, &obj)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal YAML:%s\n\n%s", err, yamlBytes)
	}

	jsonBytes, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON:%s\n\n%+v", err, obj)
	}

	return jsonBytes, nil
}

func ConvertJsonToYaml(jsonBytes []byte) ([]byte, error) {
	var obj interface{}

	err := json.Unmarshal(jsonBytes, &obj)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON:%s\n\n%s", err, jsonBytes)
	}

	yamlBytes, err := yaml.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to marshol YAML:%s\n\n%+v", err, obj)
	}

	return yamlBytes, nil
}

type ValuesResult struct {
	gjson.Result
}

func (kr ValuesResult) AsStringSlice() []string {
	array := kr.Array()

	result := make([]string, 0, len(array))
	for _, element := range array {
		result = append(result, element.String())
	}

	return result
}
