package utils

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
	. "github.com/onsi/gomega"
)

func Test_MergeValues(t *testing.T) {
	expectations := []struct {
		testName       string
		values1        Values
		values2        Values
		expectedValues Values
	}{
		{
			"simple",
			Values{"a": 1, "b": 2},
			Values{"b": 3, "c": 4},
			Values{"a": 1, "b": 3, "c": 4},
		},
		{
			"array",
			Values{"a": []interface{}{1}},
			Values{"a": []interface{}{2}},
			Values{"a": []interface{}{2}},
		},
		{
			"map",
			Values{"a": map[string]interface{}{"a": 1, "b": 2}},
			Values{"a": map[string]interface{}{"b": 3, "c": 4}},
			Values{"a": map[string]interface{}{"a": 1, "b": 3, "c": 4}},
		},
	}

	for _, expectation := range expectations {
		t.Run(expectation.testName, func(t *testing.T) {
			values := MergeValues(expectation.values1, expectation.values2)

			if !reflect.DeepEqual(expectation.expectedValues, values) {
				t.Errorf("\n[EXPECTED]: %#v\n[GOT]: %#v", expectation.expectedValues, values)
			}
		})
	}
}

func expectStringToEqual(str string, expected string) error {
	if str != expected {
		return fmt.Errorf("Expected '%s' string, got '%s'", expected, str)
	}
	return nil
}

func Test_ModuleName_Conversions(t *testing.T) {
	var err error

	for _, strs := range [][]string{
		{"module-1", "module1"},
		{"prometheus", "prometheus"},
		{"prometheus-operator", "prometheusOperator"},
		{"hello-world-module", "helloWorldModule"},
		{"cert-manager-crd", "certManagerCrd"},
	} {
		moduleName := strs[0]
		moduleValuesKey := strs[1]

		err = expectStringToEqual(ModuleNameToValuesKey(moduleName), moduleValuesKey)
		if err != nil {
			t.Error(err)
		}

		err = expectStringToEqual(ModuleNameFromValuesKey(moduleValuesKey), moduleName)
		if err != nil {
			t.Error(err)
		}
	}
}

func Test_Values_loaders(t *testing.T) {
	g := NewWithT(t)

	jsonInput := []byte(`{
"global": {
  "param1": "value1",
  "param2": "value2"},
"moduleOne": {
  "paramStr": "string",
  "paramNum": 123,
  "paramArr": ["H", "He", "Li"]}
}`)
	yamlInput := []byte(`
global:
  param1: value1
  param2: value2
moduleOne:
  paramStr: string
  paramNum: 123
  paramArr:
  - H
  - He
  - Li
`)
	mapInput := map[string]interface{}{
		"global": map[string]string{
			"param1": "value1",
			"param2": "value2",
		},
		"moduleOne": map[string]interface{}{
			"paramStr": "string",
			"paramNum": 123,
			"paramArr": []string{
				"H",
				"He",
				"Li",
			},
		},
	}

	expected := Values(map[string]interface{}{
		"global": map[string]interface{}{
			"param1": "value1",
			"param2": "value2",
		},
		"moduleOne": map[string]interface{}{
			"paramArr": []interface{}{
				"H",
				"He",
				"Li",
			},
			"paramNum": 123.0,
			"paramStr": "string",
		},
	})

	var values Values
	var err error

	values, err = NewValuesFromBytes(jsonInput)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(values).To(Equal(expected), "expected: %s\nvalues: %s\n", spew.Sdump(expected), spew.Sdump(values))

	values, err = NewValuesFromBytes(yamlInput)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(values).To(Equal(expected), "expected: %s\nvalues: %s\n", spew.Sdump(expected), spew.Sdump(values))

	values, err = NewValues(mapInput)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(values).To(Equal(expected))
}

func Test_Values_NewGlobalValues(t *testing.T) {
	g := NewWithT(t)

	yamlInput := `
paramStr: string
paramNum: 123
paramArr:
- H
- He
- Li
`

	expected := Values(map[string]interface{}{
		"global": map[string]interface{}{
			"paramArr": []interface{}{
				"H",
				"He",
				"Li",
			},
			"paramNum": 123.0,
			"paramStr": "string",
		},
	})

	var values Values
	var err error

	values, err = NewGlobalValues(yamlInput)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(values).To(Equal(expected))
}

func TestNameValuesKeyNameConsistence(t *testing.T) {
	tests := []struct {
		origName        string
		expectValuesKey string
		expectName      string
	}{
		{
			"module",
			"module",
			"module",
		},
		{
			"module-one",
			"moduleOne",
			"module-one",
		},
		{
			"module-one-0-1",
			"moduleOne01",
			"module-one-0-1",
		},
		{
			// Inconsistent module name!
			"module_one",
			"moduleOne",
			"module-one", // Real expectation in "module_one", same as module name.
		},
	}

	for _, test := range tests {
		name := fmt.Sprintf("%s -> %s -> %s", test.origName, test.expectValuesKey, test.expectName)
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)
			valuesKey := ModuleNameToValuesKey(test.origName)
			moduleName := ModuleNameFromValuesKey(valuesKey)
			newName := fmt.Sprintf("%s -> %s -> %s", test.origName, valuesKey, moduleName)
			g.Expect(name).Should(Equal(newName), "should convert values key to original module name")
		})
	}
}
