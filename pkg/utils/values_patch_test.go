package utils

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/stretchr/testify/assert"

	. "github.com/onsi/gomega"
)

func Test_JsonPatchFromString(t *testing.T) {
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
func Test_ValuesPatchFromBytes(t *testing.T) {
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
func Test_Apply_Patch(t *testing.T) {
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

/**
 *  jsonpatch playground
 */

// Applying op:remove to non-existent path should give error.
func Test_jsonpatch_Remove_NonExistent_IsError(t *testing.T) {
	g := NewWithT(t)

	patch1, _ := jsonpatch.DecodePatch([]byte(`[{"op":"remove", "path":"/test_key"}]`))

	origDoc := []byte(`{"asd":"foof"}`)

	_, err := patch1.Apply(origDoc)
	g.Expect(err).Should(HaveOccurred(), "patch apply")
}

// op:remove should remove object value and array value
func Test_jsonpatch_Remove_ObjectAndArray(t *testing.T) {
	g := NewWithT(t)

	origDoc := []byte(`{
		"asd":"foof",
		"test_obj":{"color":"red", "model":"sedan"},
		"test_array":["uno", "deux", "three"]
    }`)

	patch1, _ := jsonpatch.DecodePatch([]byte(`[
		{"op":"remove", "path":"/test_obj"},
		{"op":"remove", "path":"/test_array"}
	]`))

	expectNewDoc := []byte(`{"asd":"foof"}`)

	newDoc, err := patch1.Apply(origDoc)
	g.Expect(err).ShouldNot(HaveOccurred(), "patch apply")
	g.Expect(jsonpatch.Equal(newDoc, expectNewDoc)).Should(BeTrue(), "%v is not equal to %v", string(newDoc), string(expectNewDoc))
}

// Applying op:replace of non-existent path is error!
// 11.2020: https://github.com/evanphx/json-patch/pull/85 fix it, but 4.9.0 version has no patch.
func Test_jsonpatch_Replace_NonExistent_IsError(t *testing.T) {
	t.SkipNow() // Test is no working as expected
	g := NewWithT(t)

	origDoc := []byte(`{"asd":"foof"}`)

	patch1, _ := jsonpatch.DecodePatch([]byte(`[{"op":"replace", "path":"/test_key", "value":"qwe"}]`))

	expectNewDoc := []byte(`{"asd":"foof"}`)

	newDoc, err := patch1.Apply(origDoc)
	g.Expect(err).Should(HaveOccurred(), "patch apply")
	g.Expect(jsonpatch.Equal(newDoc, expectNewDoc)).Should(BeTrue(), "%v is not equal to %v", string(newDoc), string(expectNewDoc))
}

// Applying op:add of path with non-existent parents should give error.
// No auto instantiating!
func Test_jsonpatch_Add_WithNonExistentParent(t *testing.T) {
	g := NewWithT(t)

	patch1, _ := jsonpatch.DecodePatch([]byte(`
		[{"op":"add", "path":"/level1/level2/test_key", "value":"qwe"}]
	`))

	origDoc := []byte(`{"bar":"foo"}`)

	_, err := patch1.Apply(origDoc)
	g.Expect(err).Should(HaveOccurred(), "patch apply")
}

// Applying op:add operations of path and its parents.
func Test_jsonpatch_Add_WithParents(t *testing.T) {
	g := NewWithT(t)

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
	g.Expect(err).ShouldNot(HaveOccurred(), "patch apply")
	g.Expect(jsonpatch.Equal(newDoc, expectNewDoc)).Should(BeTrue(), "%v is not equal to %v", string(newDoc), string(expectNewDoc))
}

// Applying op:add operation with key when parent is a string value is an error!
func Test_jsonpatch_Add_Key_To_A_String_Value(t *testing.T) {
	g := NewWithT(t)

	origDoc := []byte(`{"test_obj":""}`)

	patch1, _ := jsonpatch.DecodePatch([]byte(`
[
	{"op":"add", "path":"/test_obj/key3", "value":"foo"}
]
`))

	_, err := patch1.Apply(origDoc)
	g.Expect(err).Should(HaveOccurred(), "patch apply")
}

// Applying op:add of a number value to a string value is not an error.
func Test_jsonpatch_Add_Number_To_A_String_Value(t *testing.T) {
	g := NewWithT(t)

	origDoc := []byte(`{"test_key":""}`)

	patch1, _ := jsonpatch.DecodePatch([]byte(`
[
	{"op":"add", "path":"/test_key", "value":123}
]
`))

	expectNewDoc := []byte(`{"test_key":123}`)

	newDoc, err := patch1.Apply(origDoc)
	g.Expect(err).ShouldNot(HaveOccurred(), "patch apply")
	g.Expect(jsonpatch.Equal(newDoc, expectNewDoc)).Should(BeTrue(), "%v is not equal to %v", string(newDoc), string(expectNewDoc))

}

// Create array, op:add some items, op:remove items by index.
func Test_jsonpatch_Add_Remove_for_array(t *testing.T) {
	g := NewWithT(t)

	// 1. Create array
	origDoc := []byte(`{"root":{}}`)

	patch1, _ := jsonpatch.DecodePatch([]byte(`
[{"op":"add", "path":"/root/array", "value":[]}]
`))

	expectNewDoc := []byte(`{"root":{"array":[]}}`)

	newDoc, err := patch1.Apply(origDoc)
	g.Expect(err).ShouldNot(HaveOccurred(), "patch 1 apply")
	g.Expect(jsonpatch.Equal(newDoc, expectNewDoc)).Should(BeTrue(), "%v is not equal to %v", string(newDoc), string(expectNewDoc))

	// 2. Add last element.
	origDoc = newDoc
	patch1, _ = jsonpatch.DecodePatch([]byte(`
[{"op":"add", "path":"/root/array/-", "value":"azaza"}]
`))

	expectNewDoc = []byte(`{"root":{"array":["azaza"]}}`)

	newDoc, err = patch1.Apply(origDoc)
	g.Expect(err).ShouldNot(HaveOccurred(), "patch 2 apply")
	g.Expect(jsonpatch.Equal(newDoc, expectNewDoc)).Should(BeTrue(), "%v is not equal to %v", string(newDoc), string(expectNewDoc))

	// 3. Add more elements.
	origDoc = newDoc

	patch1, _ = jsonpatch.DecodePatch([]byte(`
[ {"op":"add", "path":"/root/array/-", "value":"ololo"},
  {"op":"add", "path":"/root/array/-", "value":"foobar"},
  {"op":"add", "path":"/root/array/-", "value":"baz"}
]
`))

	expectNewDoc = []byte(`{"root":{"array":["azaza", "ololo", "foobar", "baz"]}}`)

	newDoc, err = patch1.Apply(origDoc)
	g.Expect(err).ShouldNot(HaveOccurred(), "patch 3 apply")
	g.Expect(jsonpatch.Equal(newDoc, expectNewDoc)).Should(BeTrue(), "%v is not equal to %v", string(newDoc), string(expectNewDoc))

	// 4. Remove elements in the middle.
	origDoc = newDoc[:]
	patch1, _ = jsonpatch.DecodePatch([]byte(`
[ {"op":"remove", "path":"/root/array/1"},
  {"op":"remove", "path":"/root/array/2"}
]
`))

	// Operations in patch are not atomic, so after removing index 1, index 2 become 1.
	// "remove 1, remove 2" actually do: "remove 1, remove 3"

	// wrong expectation...
	// expectNewDoc = []byte(`{"root":{"array":["azaza", "baz"]}}`)
	// Actual result
	expectNewDoc = []byte(`{"root":{"array":["azaza", "foobar"]}}`)

	newDoc, err = patch1.Apply(origDoc)
	g.Expect(err).ShouldNot(HaveOccurred(), "patch 4 apply")

	g.Expect(jsonpatch.Equal(newDoc, expectNewDoc)).Should(BeTrue(), "%v is not equal to %v", string(newDoc), string(expectNewDoc))
}

// test of a MergePatch operation
func Test_jsonpatch_MergePatch_Add_NullObj_AddKey(t *testing.T) {
	g := NewWithT(t)

	origDoc := []byte(`{"test_obj":"foo"}`)

	patch1 := []byte(`
{"test_obj":{"key3":"foo"}, "remove":null}
`)

	expectNewDoc := []byte(`{"test_obj":{"key3":"foo"}}`)

	newDoc, err := jsonpatch.MergePatch(origDoc, patch1)

	g.Expect(err).ShouldNot(HaveOccurred(), "patch apply")
	g.Expect(jsonpatch.Equal(newDoc, expectNewDoc)).Should(BeTrue(), "%v is not equal to %v", string(newDoc), string(expectNewDoc))
}

// A failed try to merge rfc6902 JSON patches with MergeMergePatches.
func Test_jsonpatch_Merge_Patches_By_Steps(t *testing.T) {
	t.SkipNow()

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
