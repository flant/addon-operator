package utils

import (
	"testing"

	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	"k8s.io/utils/pointer"
)

// Test_FromYaml creates ModuleConfig objects from different input yaml strings
func Test_FromYaml(t *testing.T) {
	g := NewWithT(t)

	var config *ModuleConfig
	var err error

	tests := []struct {
		name     string
		yaml     string
		assertFn func()
	}{
		{
			"simple config",
			`
testModule:
  param1: "1234"
`,
			func() {
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(config).ToNot(BeNil())
				g.Expect(config.IsEnabled).To(BeNil())
			},
		},
		{
			"bad type",
			`testModule: 1234`,
			func() {
				g.Expect(err).Should(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring("module config should be array or map"), "got unexpected error")
			},
		},
		{
			"disabled module",
			`testModuleEnabled: false`,
			func() {
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(config).ToNot(BeNil())
				g.Expect(config.GetValues()).To(BeEmpty())
				g.Expect(config.IsEnabled).To(Equal(&ModuleDisabled))
			},
		},
		{
			"enabled module",
			`testModuleEnabled: true`,
			func() {
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(config).ToNot(BeNil())
				g.Expect(config.GetValues()).To(BeEmpty())
				g.Expect(config.IsEnabled).To(Equal(&ModuleEnabled))
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
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(config).ToNot(BeNil())
				g.Expect(config.IsEnabled).To(Equal(&ModuleEnabled))

				g.Expect(config.GetValues()).ToNot(BeEmpty())
				g.Expect(config.GetValues()).To(HaveKey("testModule"))
				g.Expect(config.GetValues()["testModule"]).To(BeAssignableToTypeOf(map[string]interface{}{}))

				modValsMap := config.GetValues()["testModule"].(map[string]interface{})
				g.Expect(modValsMap["hello"]).To(Equal("world"))
				g.Expect(modValsMap["4"]).To(Equal("123"))
				g.Expect(modValsMap["5"]).To(Equal(5.0))

				g.Expect(modValsMap["aaa"]).To(BeAssignableToTypeOf(map[string]interface{}{}))
				aaaMap := modValsMap["aaa"].(map[string]interface{})

				g.Expect(aaaMap["numbers"]).To(BeAssignableToTypeOf([]interface{}{}))
				arr := aaaMap["numbers"].([]interface{})
				g.Expect(arr).To(HaveLen(3))
			},
		},
		{
			"array config",
			`
testModule:
  - id: "0"
    a: 1
  - id: "1"
    b: 2
`,
			func() {
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(config).ToNot(BeNil())

				arrayID := func(element interface{}) string {
					return (element.(map[string]interface{})["id"]).(string)
				}

				g.Expect(config.GetValues()).To(MatchAllKeys(Keys{
					"testModule": MatchAllElements(arrayID, Elements{
						"0": MatchAllKeys(Keys{
							"a":  Equal(1.0),
							"id": Ignore(),
						}),
						"1": MatchAllKeys(Keys{
							"b":  Equal(2.0),
							"id": Ignore(),
						}),
					}),
				}))
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config = nil
			err = nil
			config, err = NewModuleConfig("test-module", nil).FromYaml([]byte(test.yaml))
			test.assertFn()
		})
	}
}

func Test_LoadValues(t *testing.T) {
	g := NewWithT(t)

	var config *ModuleConfig
	var err error

	inputData := map[string]interface{}{
		"testModule": map[string]interface{}{
			"hello": "world", "4": "123", "5": 5,
			"aaa": map[string]interface{}{"no": []interface{}{"one", "two", "three"}},
		},
	}

	inputValuesYaml := `
testModule:
  hello: world
  4: "123"
  5: 5
  aaa:
    "no":
    - one
    - two
    - three
testModuleEnabled: true
`

	expectedData := Values{
		"testModule": map[string]interface{}{
			"hello": "world", "4": "123", "5": 5.0,
			"aaa": map[string]interface{}{"no": []interface{}{"one", "two", "three"}},
		},
	}

	config, err = NewModuleConfig("test-module", nil).LoadFromValues(inputData)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(config).ToNot(BeNil())
	g.Expect(config.GetValues()).To(Equal(expectedData))

	config, err = NewModuleConfig("test-module", nil).FromYaml([]byte(inputValuesYaml))
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(config).ToNot(BeNil())
	g.Expect(config.GetValues()).To(Equal(expectedData))
	g.Expect(config.IsEnabled).ToNot(BeNil())
	g.Expect(config.IsEnabled).To(Equal(&ModuleEnabled))
}

func Test_GetEnabled(t *testing.T) {
	g := NewWithT(t)

	var config *ModuleConfig

	tests := []struct {
		name     string
		fn       func()
		expected string
	}{
		{
			"nil",
			func() {
				config = &ModuleConfig{}
			},
			"n/d",
		},
		{
			"nil",
			func() {
				config = &ModuleConfig{}
				config.IsEnabled = &ModuleEnabled
			},
			"true",
		},
		{
			"nil",
			func() {
				config = &ModuleConfig{}
				config.IsEnabled = &ModuleDisabled
			},
			"false",
		},
		{
			"nil",
			func() {
				config = &ModuleConfig{}
				config.IsEnabled = pointer.Bool(true)
			},
			"true",
		},
		{
			"nil",
			func() {
				config = &ModuleConfig{}
				config.IsEnabled = pointer.Bool(false)
			},
			"false",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.fn()
			actual := config.GetEnabled()
			g.Expect(actual).To(Equal(test.expected))
		})
	}
}
