package utils

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)


// Test_FromYaml creates ModuleConfig objects from different input yaml strings
func Test_FromYaml2(t *testing.T) {
	var config *ModuleConfig
	var err error

	simpleCfg := `testModule:
  poaram1: "1234"`

	config, err = NewModuleConfig("test-module").FromYaml([]byte(simpleCfg))
	assert.NoError(t, err)
	assert.NotNil(t, config)


	badTypeCfg := `testModule: 1234
`
	config, err = NewModuleConfig("test-module").FromYaml([]byte(badTypeCfg))
	assert.Nil(t, config)
	assert.Error(t, err)
	assert.Containsf(t, err.Error(), "Module config should be array or map", "got unexpected error")

	disabledCfg:= `testModuleEnabled: "false"`

	config, err = NewModuleConfig("test-module").FromYaml([]byte(disabledCfg))
	assert.NoError(t, err)
	assert.NotNil(t, config)
	assert.Empty(t, config.Values)
	assert.False(t, config.IsEnabled)

	enabledCfg:= `testModuleEnabled: "true"`

	config, err = NewModuleConfig("test-module").FromYaml([]byte(enabledCfg))
	assert.NoError(t, err)
	assert.NotNil(t, config)
	assert.Empty(t, config.Values)
	assert.True(t, config.IsEnabled)


	fullCfg := `
testModule:
  hello: world
  4: "123"
  5: 5
  aaa:
    no:
    - one
    - two
    - three
testModuleEnabled: "true"
`

	config, err = NewModuleConfig("test-module").FromYaml([]byte(fullCfg))
	assert.NoError(t, err)
	assert.NotNil(t, config)
	assert.True(t, config.IsEnabled)
	assert.Contains(t, config.Values, "hello")
	assert.Equal(t, "world", config.Values["hello"])
	assert.Contains(t, config.Values, "4")
	assert.Equal(t, "123", config.Values["4"])
	assert.Contains(t, config.Values, "5")
	assert.Equal(t, 5.0, config.Values["5"])
	assert.Contains(t, config.Values, "aaa")
	assert.Contains(t, config.Values["aaa"], "no")



	//if err == nil {
//		t.Errorf("Expected error, got ModuleConfig: %v", config)
	//} else if !strings.HasPrefix(err.Error(), "Module config should be array or map") {
	//	t.Errorf("Got unexpected error: %s", err)
	//}

}


func Test_LoadValues(t *testing.T) {
	var config *ModuleConfig
	var err error


	inputData := map[interface{}]interface{}{
		"testModule": map[interface{}]interface{}{
			"hello": "world", 4: "123", 5: 5,
			"aaa": map[interface{}]interface{}{"no": []interface{}{"one", "two", "three"}},
		},
	}
	expectedData := Values{
		"testModule": map[string]interface{}{
			"hello": "world", "4": "123", "5": 5.0,
			"aaa": map[string]interface{}{"no": []interface{}{"one", "two", "three"}},
		},
	}

	config, err = NewModuleConfig("test-module").LoadValues(inputData)
	if err != nil {
		t.Error(err)
	}
	if !config.IsEnabled {
		t.Errorf("Expected module to be enabled")
	}

	if !reflect.DeepEqual(config.Values, expectedData) {
		t.Errorf("Got unexpected config values: %+v", config.Values)
	}
}

func TestNewModuleConfigByValuesYamlData(t *testing.T) {
	configStr := `
testModule:
  a: 1
  b: 2
`
	expectedData := Values{
		"testModule": map[string]interface{}{
			"a": 1.0, "b": 2.0,
		},
	}
	config, err := NewModuleConfig("test-module").FromYaml([]byte(configStr))
	if err != nil {
		t.Error(err)
	}
	if !config.IsEnabled {
		t.Errorf("Expected module to be enabled")
	}
	if !reflect.DeepEqual(config.Values, expectedData) {
		t.Errorf("Got unexpected config values: %+v", config.Values)
	}

	config, err = NewModuleConfig("test-module").FromYaml([]byte("testModule: false\n"))
	if err != nil {
		t.Error(err)
	}
	if config.IsEnabled {
		t.Errorf("Expected module to be disabled")
	}

	_, err = NewModuleConfig("test-module").FromYaml([]byte("testModule: falsee\n"))
	if !strings.HasPrefix(err.Error(), "Module config should be bool, array or map") {
		t.Errorf("Got unexpected error: %s", err.Error())
	}

	configStr = `
testModule:
  - a: 1
  - b: 2
`
	expectedData = Values{
		"testModule": []interface{}{
			map[string]interface{}{"a": 1.0},
			map[string]interface{}{"b": 2.0},
		},
	}
	config, err = NewModuleConfig("test-module").FromYaml([]byte(configStr))
	if err != nil {
		t.Error(err)
	}
	if !config.IsEnabled {
		t.Errorf("Expected module to be enabled")
	}
	if !reflect.DeepEqual(config.Values, expectedData) {
		t.Errorf("Got unexpected config values: %+v", config.Values)
	}

}
