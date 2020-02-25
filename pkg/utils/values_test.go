package utils

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/davecgh/go-spew/spew"
	. "github.com/onsi/gomega"

	"github.com/evanphx/json-patch"
	"github.com/stretchr/testify/assert"
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

func TestJsonPatch_Loading(t *testing.T) {
	g := NewWithT(t)

	input := `{"op":"add","path":"/key","value":"foobar"}
 [{"op":"remove","path":"/key"},
   {"op":"add","path":"/key","value":"bazzz"}]
{"op":"remove","path":"/key2"}

`

	patch, err := JsonPatchFromString(input)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(patch).Should(HaveLen(4))

}

// ValuesPatchFromBytes should work with one valid json objects and with a stream of objects
func TestValuesPatch_Loading(t *testing.T) {
	g := NewWithT(t)

	input := `{"op":"add","path":"/key","value":"foobar"}
 [{"op":"remove","path":"/key"},
   {"op":"add","path":"/key","value":"bazzz"}]
{"op":"remove","path":"/key2"}

`

	vp, err := ValuesPatchFromBytes([]byte(input))
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(vp.Operations).Should(HaveLen(4))

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

func Test_ApplyRemove_NonExistent(t *testing.T) {
	t.SkipNow()
	patch1, _ := jsonpatch.DecodePatch([]byte(`[{"op":"remove", "path":"/test_key"}]`))

	origDoc := []byte(`{"asd":"foof"}`)

	expectNewDoc := []byte(`{"asd":"foof"}`)

	newDoc, err := patch1.Apply(origDoc)
	if err != nil {
		t.Logf("patch Apply: %v", err)
		t.FailNow()
	}

	assert.True(t, jsonpatch.Equal(newDoc, expectNewDoc), "%v is not equal to %v", string(newDoc), string(expectNewDoc))

}

func Test_Apply_Remove_ObjectAndArray(t *testing.T) {
	patch1, _ := jsonpatch.DecodePatch([]byte(`[{"op":"remove", "path":"/test_obj"}, {"op":"remove", "path":"/test_array"}]`))

	//origDoc := []byte(`{"asd":"foof", "test_key":"123"}`)
	origDoc := []byte(`{"asd":"foof", "test_obj":{"color":"red", "model":"sedan"}, "test_array":["uno", "deux", "three"]}`)

	expectNewDoc := []byte(`{"asd":"foof"}`)

	newDoc, err := patch1.Apply(origDoc)
	if err != nil {
		t.Logf("patch Apply: %v", err)
		t.FailNow()
	}

	assert.True(t, jsonpatch.Equal(newDoc, expectNewDoc), "%v is not equal to %v", string(newDoc), string(expectNewDoc))

}

// Replace non existent path is error!
func Test_Apply_Replace_NonExistent(t *testing.T) {
	t.SkipNow()
	//patch1, _ := jsonpatch.DecodePatch([]byte(`{"op":"remove", "path":"/test_key", "value":"baz"}`))
	patch1, _ := jsonpatch.DecodePatch([]byte(`[{"op":"replace", "path":"/test_key", "value":"qwe"}]`))

	//origDoc := []byte(`{"asd":"foof", "test_key":"123"}`)
	origDoc := []byte(`{"asd":"foof"}`)

	expectNewDoc := []byte(`{"asd":"foof"}`)

	newDoc, err := patch1.Apply(origDoc)
	if err != nil {
		t.Logf("patch Apply: %v", err)
		t.FailNow()
	}

	assert.True(t, jsonpatch.Equal(newDoc, expectNewDoc), "%v is not equal to %v", string(newDoc), string(expectNewDoc))

}

func Test_Apply_Add_WithNonExistentParent(t *testing.T) {
	// Not working!
	//	patch1, _ := jsonpatch.DecodePatch([]byte(`
	//[{"op":"add", "path":"/level1/level2/test_key", "value":"qwe"}]`))

	patch1, _ := jsonpatch.DecodePatch([]byte(`
[
  {"op":"add", "path":"/level1", "value":{}},
  {"op":"add", "path":"/level1/level2", "value":{}},
  {"op":"add", "path":"/level1/level2/test_key", "value":"qwe"}
]
`))

	origDoc := []byte(`{"bar":"foo"}`)

	expectNewDoc := []byte(`{"bar":"foo", "level1":{"level2":{"test_key":"qwe"}}}`)

	newDoc, err := patch1.Apply(origDoc)
	if err != nil {
		t.Logf("patch Apply: %v", err)
		t.FailNow()
	}

	assert.True(t, jsonpatch.Equal(newDoc, expectNewDoc), "%v is not equal to %v", string(newDoc), string(expectNewDoc))
}

// add operation with type replace is error!
func Test_Apply_Add_NullObj_AddKey(t *testing.T) {
	t.SkipNow()
	patch1, _ := jsonpatch.DecodePatch([]byte(`
[
	{"op":"add", "path":"/test_obj/key3", "value":"foo"}
]
`))

	origDoc := []byte(`{"test_obj":""}`)

	expectNewDoc := []byte(`{"test_obj":{"key3":"foo"}}`)

	newDoc, err := patch1.Apply(origDoc)
	if err != nil {
		t.Logf("patch Apply: %v", err)
		t.FailNow()
	}

	assert.True(t, jsonpatch.Equal(newDoc, expectNewDoc), "%v is not equal to %v", string(newDoc), string(expectNewDoc))
}

func Test_Apply_MergePatch_Add_NullObj_AddKey(t *testing.T) {
	// It is not working without adding parent keys!
	//	patch1, _ := jsonpatch.DecodePatch([]byte(`
	//[{"op":"add", "path":"/level1/level2/test_key", "value":"qwe"}]`))

	//{"op":"add", "path":"/test_obj", "value":{}},
	//{"op":"add", "path":"/test_obj/key1", "value":"foo"},
	//{"op":"add", "path":"/test_obj/key2", "value":"bar"},
	//	{"op":"remove", "path":"/test_obj"},
	//{"op":"add", "path":"/test_obj", "value":{}},
	patch1 := []byte(`
{"test_obj":{"key3":"foo"}, "remove":null}
`)

	origDoc := []byte(`{"test_obj":"foo"}`)

	expectNewDoc := []byte(`{"test_obj":{"key3":"foo"}}`)

	newDoc, err := jsonpatch.MergePatch(origDoc, patch1)
	if err != nil {
		t.Logf("patch Merge: %v", err)
		t.FailNow()
	}

	assert.True(t, jsonpatch.Equal(newDoc, expectNewDoc), "%v is not equal to %v", string(newDoc), string(expectNewDoc))
}

// MergeMergePatches doesn't work as a merge for JSON Patches
func Test_Merge_Patches_By_Steps(t *testing.T) {
	t.SkipNow()
	//patch1, _ := jsonpatch.DecodePatch([]byte(`{"op":"remove", "path":"/test_key", "value":"baz"}`))

	patches := []string{
		`[{"op":"remove", "path":"/test_key"}]`,
		`[{"op":"add", "path":"/test_key", "value":333}]`,
		`[{"op":"add", "path":"/foo", "value":"bar"}]`,
		`[{"op":"remove", "path":"/test_key"}]`,
		`[{"op":"add", "path":"/foo", "value":"zoo"}]`,
	}

	newPatch := []byte(`[]`)
	var err error

	for i, patch := range patches {
		newPatch, err = jsonpatch.MergeMergePatches(newPatch, []byte(patch))
		if err != nil {
			t.Logf("merge patches: %v", err)
			t.FailNow()
		}
		t.Logf("%d.  %s", i, patch)
		t.Logf(" == %s", newPatch)
	}
}

func Test_CompactPatches_Result(t *testing.T) {
	tests := []struct {
		name                     string
		valuesPatchOperations    []string
		newValuesPatchOperations []string
		expected                 string
	}{
		{
			"remove+add == add",
			[]string{
				`{"op":"remove", "path":"/test_key"}`,
			},
			[]string{
				`{"op":"add", "path":"/test_key", "value":"foo"}`,
			},
			`[{"op":"add", "path":"/test_key", "value":"foo"}]`,
		},
		{
			"add+remove",
			[]string{
				`{"op":"add", "path":"/test_key", "value":"foo"}`,
			},
			[]string{
				`{"op":"remove", "path":"/test_key"}`,
			},
			`[{"op":"add", "path":"/test_key", "value":"foo"},{"op":"remove", "path":"/test_key"}]`,
		},
		{
			"add+remove+add == add",
			[]string{
				`{"op":"add", "path":"/test_key", "value":"foo"}`,
				`{"op":"remove", "path":"/test_key"}`,
			},
			[]string{
				`{"op":"add", "path":"/test_key", "value":"barbaz"}`,
			},
			`[{"op":"add", "path":"/test_key", "value":"barbaz"}]`,
		},
		{
			"add_1+add_2",
			[]string{
				`{"op":"add", "path":"/test_key", "value":"foo"}`,
			},
			[]string{
				`{"op":"add", "path":"/test_key_2", "value": "qwe"}`,
			},
			`[{"op":"add", "path":"/test_key", "value":"foo"}, {"op":"add", "path":"/test_key_2", "value": "qwe"}]`,
		},
		{
			"add_1+add_2+remove_1 == add_1+add_2+remove_1",
			[]string{
				`{"op":"add", "path":"/test_key", "value":"foo"}`,
				`{"op":"add", "path":"/test_key_2", "value": "qwe"}`,
			},
			[]string{
				`{"op":"remove", "path":"/test_key"}`,
			},
			`[{"op":"add", "path":"/test_key", "value":"foo"},{"op":"remove", "path":"/test_key"}, {"op":"add", "path":"/test_key_2", "value": "qwe"}]`,
		},
		{
			"add object + remove parent == add parent + remove parent",
			[]string{
				`{"op":"add", "path":"/test_obj", "value":{}}`,
				`{"op":"add", "path":"/test_obj/key1", "value":"foo"}`,
				`{"op":"add", "path":"/test_obj/key2", "value":"bar"}`,
			},
			[]string{
				`{"op":"remove", "path":"/test_obj"}`,
			},
			`[{"op":"add", "path":"/test_obj", "value":{}},{"op":"remove", "path":"/test_obj"}]`,
		},
		{
			"add parent with keys + remove parent + add new object == add new object",
			[]string{
				`{"op":"add", "path":"/test_obj", "value":{}}`,
				`{"op":"add", "path":"/test_obj/key1", "value":"foo"}`,
				`{"op":"add", "path":"/test_obj/key2", "value":"bar"}`,
				`{"op":"remove", "path":"/test_obj"}`,
				`{"op":"add", "path":"/test_obj", "value":{}}`,
				`{"op":"add", "path":"/test_obj/key3", "value":"foo"}`,
			},
			nil,
			`[{"op":"add", "path":"/test_obj", "value":{}},{"op":"add", "path":"/test_obj/key3", "value":"foo"}]`,
		},
		{
			"add parent + remove parent + add new object == add new object",
			[]string{
				`{"op":"add", "path":"/test_obj", "value":{}}`,
				`{"op":"add", "path":"/test_obj/key1", "value":"foo"}`,
				`{"op":"add", "path":"/test_obj/key2", "value":"bar"}`,
				`{"op":"remove", "path":"/test_obj"}`,
				`{"op":"add", "path":"/test_obj", "value":[]}`,
				`{"op":"add", "path":"/test_obj/0", "value":"0"}`,
			},
			nil,
			`[{"op":"add", "path":"/test_obj", "value":[]},{"op":"add", "path":"/test_obj/0", "value":"0"}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// ValuesPatchFromBytes works with valid json object and with a stream of objects
			valuesPatch, _ := ValuesPatchFromBytes([]byte("[" + strings.Join(tt.valuesPatchOperations, ", ") + "]"))
			newValuesPatch, _ := ValuesPatchFromBytes([]byte(strings.Join(tt.newValuesPatchOperations, "")))

			newPatch := CompactValuesPatches([]ValuesPatch{*valuesPatch}, *newValuesPatch)
			newPatchBytes, err := json.Marshal(newPatch[0].Operations)
			if assert.NoError(t, err) {
				assert.True(t, jsonpatch.Equal(newPatchBytes, []byte(tt.expected)), "%s should be equal to %s", newPatchBytes, tt.expected)
			}
		})
	}
}

func Test_CompactPatches_Apply(t *testing.T) {
	tests := []struct {
		name     string
		patches  []string
		input    string
		expected string
	}{
		{
			"remove+add",
			[]string{
				`[{"op":"remove", "path":"/test_key"}]`,
				`[{"op":"add", "path":"/test_key", "value":"foo"}]`,
			},
			`{"test_key":"wqe"}`,
			`{"test_key":"foo"}`,
		},
		{
			"add+remove",
			[]string{
				`[{"op":"add", "path":"/test_key", "value":"foo"}]`,
				`[{"op":"remove", "path":"/test_key"}]`,
			},
			`{"test_key":"wqe"}`,
			`{}`,
		},
		{
			"add+add",
			[]string{
				`[{"op":"add", "path":"/test_key", "value":"foo"}]`,
				`[{"op":"add", "path":"/test_key_2", "value": "qwe"}]`,
			},
			`{"test_key":"wqe"}`,
			`{"test_key":"foo", "test_key_2":"qwe"}`,
		},
		{
			"add+add+remove",
			[]string{
				`[{"op":"add", "path":"/test_key", "value":"foo"}]`,
				`[{"op":"add", "path":"/test_key_2", "value": "qwe"}]`,
				`[{"op":"remove", "path":"/test_key"}]`,
			},
			`{"test_key":"/123"}`,
			`{"test_key_2":"qwe"}`,
		},
		{
			"add object + remove parent == remove parent",
			[]string{
				`[{"op":"add", "path":"/test_obj", "value":{}}]`,
				`[{"op":"add", "path":"/test_obj/key1", "value":"foo"}]`,
				`[{"op":"add", "path":"/test_obj/key2", "value":"bar"}]`,
				`[{"op":"remove", "path":"/test_obj"}]`,
			},
			`{"test_obj":{}}`,
			`{}`,
		},
		{
			"add parent with keys + remove parent + add new object == add new object",
			[]string{
				`[{"op":"add", "path":"/test_obj", "value":{}}]`,
				`[{"op":"add", "path":"/test_obj/key1", "value":"foo"}]`,
				`[{"op":"add", "path":"/test_obj/key2", "value":"bar"}]`,
				`[{"op":"remove", "path":"/test_obj"}]`,
				`[{"op":"add", "path":"/test_obj", "value":{}}]`,
				`[{"op":"add", "path":"/test_obj/key3", "value":"foo"}]`,
			},
			`{"test_obj":{}}`,
			`{"test_obj":{"key3":"foo"}}`,
		},
		{
			"add parent + remove parent + add new object == add new object",
			[]string{
				`[{"op":"add", "path":"/test_obj", "value":{}}]`,
				`[{"op":"add", "path":"/test_obj/key1", "value":"foo"}]`,
				`[{"op":"add", "path":"/test_obj/key2", "value":"bar"}]`,
				`[{"op":"remove", "path":"/test_obj"}]`,
				`[{"op":"add", "path":"/test_obj", "value":[]}]`,
				`[{"op":"add", "path":"/test_obj/0", "value":"foo"}]`,
			},
			`{"test_obj":{}}`,
			`{"test_obj":["foo"]}`,
		},
		{
			"add parent + remove parent + add new object == add new object",
			[]string{
				`[{"op":"add", "path":"/test_obj", "value":{}}]`,
				`[{"op":"add", "path":"/test_obj/key1", "value":"foo"}]`,
				`[{"op":"add", "path":"/test_obj/key2", "value":"bar"}]`,
				`[{"op":"remove", "path":"/test_obj"}]`,
				`[{"op":"add", "path":"/test_obj", "value":[]}]`,
				`[{"op":"add", "path":"/test_obj/0", "value":"foo"}]`,
			},
			`{"test_ob":{"foo":"bar"}, "test_object":{"foo":"bar"}, "test_obj":{}}`,
			`{"test_ob":{"foo":"bar"}, "test_object":{"foo":"bar"}, "test_obj":["foo"]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var origPatchedDoc []byte = []byte(tt.input)
			var err error

			operations := []*ValuesPatchOperation{}
			for _, patch := range tt.patches {
				vp, _ := ValuesPatchFromBytes([]byte(patch))
				operations = append(operations, vp.Operations...)
				patchedDoc, err := vp.Apply([]byte(origPatchedDoc))
				if !assert.NoError(t, err) {
					t.Logf("%s should apply on %s", patch, origPatchedDoc)
					t.FailNow()
				}
				origPatchedDoc = patchedDoc
			}

			assert.True(t, jsonpatch.Equal(origPatchedDoc, []byte(tt.expected)), "%s should be equal to %s", origPatchedDoc, tt.expected)

			newPatch := CompactPatches(operations)
			if !assert.NoError(t, err) {
				t.FailNow()
			}
			patched, err := newPatch.Apply([]byte(tt.input))
			if assert.NoError(t, err) {
				assert.True(t, jsonpatch.Equal(patched, []byte(tt.expected)), "%s should be equal to %s", patched, tt.expected)
			}
		})
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
