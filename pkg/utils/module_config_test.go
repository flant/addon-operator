package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Test_FromYaml creates ModuleConfig objects from different input yaml strings
func Test_FromYaml(t *testing.T) {
	var config *ModuleConfig
	var err error

	tests := []struct {
		name string
		yaml string
		assertFn func()
	}{
		{
			"simple config",
			`
testModule:
  poaram1: "1234"
`,
			func() {
				assert.NoError(t, err)
				assert.NotNil(t, config)
				assert.Nil(t, config.IsEnabled)
			},
		},
		{
			"bad type",
			`testModule: 1234`,
			func() {
				assert.Nil(t, config)
				assert.Error(t, err)
				assert.Containsf(t, err.Error(), "Module config should be array or map", "got unexpected error")
			},
		},
		{
			"disabled module",
			`testModuleEnabled: false`,
			func() {
				assert.NoError(t, err)
				assert.NotNil(t, config)
				assert.Empty(t, config.Values)
				assert.False(t, *config.IsEnabled)
			},
		},
		{
			"enabled module",
			`testModuleEnabled: true`,
			func() {
				assert.NoError(t, err)
				assert.NotNil(t, config)
				assert.Empty(t, config.Values)
				assert.True(t, *config.IsEnabled)
			},
		},
		{
			"full module config",
			`
testModule:
  hello: world
  4: "123"
  5: 5
  aaa:
    numbers:
    - one
    - two
    - three
testModuleEnabled: true
`,
			func() {
				assert.NoError(t, err)
				assert.NotNil(t, config)
				assert.True(t, *config.IsEnabled)
				assert.Contains(t, config.Values, "testModule")
				modVals := config.Values["testModule"]
				//assert.IsType(t, interface{}{}, modVals)

				modValsMap, ok := modVals.(map[string]interface{})
				assert.True(t, ok)
				assert.Equal(t, "world", modValsMap["hello"])

				assert.Contains(t, modValsMap, "4")
				assert.Equal(t, "123", modValsMap["4"])
				assert.Contains(t, modVals, "5")
				assert.Equal(t, 5.0, modValsMap["5"])

				assert.Contains(t, modVals, "aaa")
				aaa, ok := modValsMap["aaa"].(map[string]interface{})
				assert.True(t, ok)

				assert.Contains(t, aaa, "numbers")
				noArray, ok := aaa["numbers"].([]interface{})
				assert.True(t, ok)

				assert.Len(t, noArray, 3)

			},
		},
		{
			"array config",
			`
testModule:
  - a: 1
  - b: 2
`,
			func() {
				assert.NoError(t, err)
				assert.NotNil(t, config)
				assert.Contains(t, config.Values, "testModule")

				vals, ok := config.Values["testModule"].([]interface{})
				assert.True(t, ok, "testModule should be []interface{}")
				assert.Len(t, vals, 2)

				vals0, ok := vals[0].(map[string]interface{})
				assert.True(t, ok)
				assert.Equal(t, vals0["a"], 1.0)
				vals1, ok := vals[1].(map[string]interface{})
				assert.True(t, ok)
				assert.Equal(t, vals1["b"], 2.0)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config = nil
			err = nil
			config, err = NewModuleConfig("test-module").FromYaml([]byte(test.yaml))
			test.assertFn()
		})
	}

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
	assert.NoError(t, err)
	assert.NotNil(t, config)
	assert.Equal(t, expectedData, config.Values)
}
