package utils

import (
	"fmt"
	"reflect"
	"testing"
)


func TestMergeValues(t *testing.T) {
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

func TestModuleNameConversions(t *testing.T) {
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

func TestCompactValuesPatchOperations(t *testing.T) {
	expectations := []struct {
		testName           string
		operations         []*ValuesPatchOperation
		newOperations      []*ValuesPatchOperation
		expectedOperations []*ValuesPatchOperation
	}{
		{
			"path",
			[]*ValuesPatchOperation{
				{
					"add",
					"/a",
					"",
				},
			},
			[]*ValuesPatchOperation{
				{
					"add",
					"/a",
					"",
				},
			},
			nil,
		},
		{
			"subpath",
			[]*ValuesPatchOperation{
				{
					"add",
					"/a/b",
					"",
				},
			},
			[]*ValuesPatchOperation{
				{
					"add",
					"/a",
					"",
				},
			},
			nil,
		},
		{
			"different op",
			[]*ValuesPatchOperation{
				{
					"add",
					"/a",
					"",
				},
			},
			[]*ValuesPatchOperation{
				{
					"delete",
					"/a",
					"",
				},
			},
			[]*ValuesPatchOperation{
				{
					"add",
					"/a",
					"",
				},
			},
		},
		{
			"different path",
			[]*ValuesPatchOperation{
				{
					"add",
					"/a",
					"",
				},
			},
			[]*ValuesPatchOperation{
				{
					"add",
					"/b",
					"",
				},
			},
			[]*ValuesPatchOperation{
				{
					"add",
					"/a",
					"",
				},
			},
		},
		{
			"sample",
			[]*ValuesPatchOperation{
				{
					"add",
					"/a",
					"",
				},
				{
					"add",
					"/a/b",
					"",
				},
				{
					"add",
					"/b",
					"",
				},
				{
					"delete",
					"/c",
					"",
				},
			},
			[]*ValuesPatchOperation{
				{
					"add",
					"/a",
					"",
				},
				{
					"delete",
					"/c",
					"",
				},
				{
					"add",
					"/d",
					"",
				},
			},
			[]*ValuesPatchOperation{
				{
					"add",
					"/b",
					"",
				},
			},
		},
	}

	for _, expectation := range expectations {
		t.Run(expectation.testName, func(t *testing.T) {
			compactOperations := CompactValuesPatchOperations(expectation.operations, expectation.newOperations)

			if !reflect.DeepEqual(expectation.expectedOperations, compactOperations) {
				t.Errorf("\n[EXPECTED]: %#v\n[GOT]: %#v", expectation.expectedOperations, compactOperations)
			}
		})
	}
}

// TODO поправить после изменения алгоритма compact
func TestApplyPatch(t *testing.T) {
	t.SkipNow()
	expectations := []struct {
		testName              string
		operations            ValuesPatch
		values                Values
		expectedValues        Values
		expectedValuesChanged bool
	}{
		{
			"path",
			ValuesPatch{
				[]*ValuesPatchOperation{
					{
						"add",
						"/test_key_3",
						"baz",
					},
				},
			},
			Values{
				"test_key_1": "foo",
				"test_key_2": "bar",
			},
			Values{
				"test_key_1": "foo",
				"test_key_2": "bar",
				"test_key_3": "baz",
			},
			true,
		},
		{
			"path",
			ValuesPatch{
				[]*ValuesPatchOperation{
					{
						"remove",
						"/test_key_3",
						"baz",
					},
				},
			},
			Values{
				"test_key_1": "foo",
				"test_key_2": "bar",
			},
			Values{
				"test_key_1": "foo",
				"test_key_2": "bar",
			},
			false,
		},
	}

	for _, expectation := range expectations {
		t.Run(expectation.testName, func(t *testing.T) {
			newValues, changed, err := ApplyValuesPatch(expectation.values, expectation.operations)

			if err != nil {
				t.Errorf("ApplyValuesPatch error: %s", err)
				return
			}

			if !reflect.DeepEqual(expectation.expectedValues, newValues) {
				t.Errorf("\n[EXPECTED]: %#v\n[GOT]: %#v", expectation.expectedValues, newValues)
			}

			if changed != expectation.expectedValuesChanged {
				t.Errorf("\n[EXPECTED]: %#v\n[GOT]: %#v", expectation.expectedValuesChanged, changed)
			}
		})
	}
}
