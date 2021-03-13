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

func Test_ApplyValuesPatch(t *testing.T) {
	tests := []struct {
		name                  string
		operations            ValuesPatch
		values                Values
		expectedValues        Values
		expectedValuesChanged bool
	}{
		{
			"add",
			ValuesPatch{
				[]*ValuesPatchOperation{
					{
						Op:    "add",
						Path:  "/test_key_3",
						Value: "baz",
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
			"remove",
			ValuesPatch{
				[]*ValuesPatchOperation{
					{
						Op:    "remove",
						Path:  "/test_key_3",
						Value: "baz",
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
		{
			"add+remove+add+remove+add",
			ValuesPatch{
				[]*ValuesPatchOperation{
					{
						Op:    "add",
						Path:  "/test_key_3",
						Value: "baz",
					},
					{
						Op:   "remove",
						Path: "/test_key_3",
					},
					{
						Op:    "add",
						Path:  "/test_key_3",
						Value: "baz",
					},
					{
						Op:   "remove",
						Path: "/test-parent/test_key_nonexist",
					},
					{
						Op:    "add",
						Path:  "/test_key_3",
						Value: "baz",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newValues, changed, err := ApplyValuesPatch(tt.values, tt.operations)

			if err != nil {
				t.Errorf("ApplyValuesPatch error: %s", err)
				return
			}

			if !reflect.DeepEqual(tt.expectedValues, newValues) {
				t.Errorf("\n[EXPECTED]: %#v\n[GOT]: %#v", tt.expectedValues, newValues)
			}

			if changed != tt.expectedValuesChanged {
				t.Errorf("\n[EXPECTED]: %#v\n[GOT]: %#v", tt.expectedValuesChanged, changed)
			}
		})
	}
}

func Test_ApplyValuesPatch_Strict(t *testing.T) {
	tests := []struct {
		name       string
		operations ValuesPatch
		values     Values
	}{
		{
			"remove",
			ValuesPatch{
				[]*ValuesPatchOperation{
					{
						Op:    "remove",
						Path:  "/test_key_3",
						Value: "baz",
					},
				},
			},
			Values{
				"test_key_1": "foo",
				"test_key_2": "bar",
			},
		},
		{
			"add+remove+add+remove+add",
			ValuesPatch{
				[]*ValuesPatchOperation{
					{
						Op:    "add",
						Path:  "/test_key_3",
						Value: "baz",
					},
					{
						Op:   "remove",
						Path: "/test_key_3",
					},
					{
						Op:    "add",
						Path:  "/test_key_3",
						Value: "baz",
					},
					{
						Op:   "remove",
						Path: "/test-parent/test_key_nonexist",
					},
					{
						Op:    "add",
						Path:  "/test_key_3",
						Value: "baz",
					},
				},
			},
			Values{
				"test_key_1": "foo",
				"test_key_2": "bar",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := ApplyValuesPatch(tt.values, tt.operations, Strict)

			if err == nil {
				t.Errorf("ApplyValuesPatch in Strict mode should return error (%s)", tt.name)
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

// 'remove' operation on non-existent path should return error.
func Test_jsonpatch_Remove_NonExistent_IsError(t *testing.T) {
	g := NewWithT(t)

	// Apply method in jsonpatch library should return error when
	// remove nonexistent path. Errors are different for root keys
	// and for keys with parents.
	// ValuesPatch.Apply method detects these errors to ignore them.
	// Here are asserts to detect changed error messages
	// in future versions of jsonpath library.

	origDoc := []byte(`{"foo":"bar"}`)
	rootKeyPatch := []byte(`[{"op":"remove", "path":"/test_key"}]`)
	withParentsPatch := []byte(`[{"op":"remove", "path":"/test_parent/test_sub/test_key"}]`)

	// 1. Root key.
	patch1, _ := jsonpatch.DecodePatch(rootKeyPatch)
	_, err := patch1.Apply(origDoc)
	g.Expect(err).Should(HaveOccurred(), "jsonpatch Apply should return error")
	rootKeyPrefix := "error in remove for path:"
	g.Expect(err.Error()).Should(HavePrefix(rootKeyPrefix), "jsonpatch apply should return error with prefix '%s'", rootKeyPrefix)

	// 2. Key with parents.
	patch2, _ := jsonpatch.DecodePatch(withParentsPatch)
	_, err = patch2.Apply(origDoc)
	g.Expect(err).Should(HaveOccurred(), "jsonpatch Apply should return error")
	withParentsPrefix := "remove operation does not apply: doc is missing path"
	g.Expect(err.Error()).Should(HavePrefix(withParentsPrefix), "jsonpatch apply should return error with prefix '%s'", withParentsPrefix)

	// Values' Apply should not return error on missing path.

	// 1. Root key.
	vp1, _ := ValuesPatchFromBytes(rootKeyPatch)
	_, err = vp1.Apply(origDoc)
	g.Expect(err).ShouldNot(HaveOccurred(), "values_patch Apply should not return error")

	// 2. Key with parents.
	vp2, _ := ValuesPatchFromBytes(withParentsPatch)
	_, err = vp2.Apply(origDoc)
	g.Expect(err).ShouldNot(HaveOccurred(), "values_patch Apply should not return error")

	// 3. No path in operation.
	vp3, _ := ValuesPatchFromBytes([]byte(`[{"op":"remove"}]`))
	_, err = vp3.Apply(origDoc)
	g.Expect(err).Should(HaveOccurred(), "values_patch Apply should return error on invalid path")
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

// https://github.com/evanphx/json-patch/pull/85
// A "replace" patch operation referencing a key that does not exist
// in the target object are currently being accepted; ultimately becoming
// an "add" operation on the target object. This is incorrect per the specification.
//
// 11.2020 the PR was merged but then reverted in https://github.com/evanphx/json-patch/pull/111
//
// The fix is in v5 now. So this test should work with v5.
func Test_jsonpatch_Replace_NonExistent_IsError(t *testing.T) {
	t.SkipNow()
	g := NewWithT(t)

	origDoc := []byte(`{"foo":"bar"}`)

	patch1, _ := jsonpatch.DecodePatch([]byte(`[{"op":"replace", "path":"/test_key", "value":"qwe"}]`))

	expectNewDoc := []byte(`{"foo":"bar"}`)

	newDoc, err := patch1.Apply(origDoc)
	g.Expect(err).Should(HaveOccurred(), "replace operation should ")
	g.Expect(jsonpatch.Equal(newDoc, expectNewDoc)).Should(BeTrue(), "%v is not equal to %v", string(newDoc), string(expectNewDoc))
}

func Test_jsonpatch_Replace_NonExistent_v4_9_0_behaviour_is_incorrect(t *testing.T) {
	g := NewWithT(t)

	origDoc := []byte(`{"foo":"bar"}`)

	patch1, _ := jsonpatch.DecodePatch([]byte(`[{"op":"replace", "path":"/test_key", "value":"qwe"}]`))

	expectNewDoc := []byte(`{"foo":"bar", "test_key":"qwe"}`)

	newDoc, err := patch1.Apply(origDoc)
	g.Expect(err).ShouldNot(HaveOccurred(), "replace operation should ")
	g.Expect(jsonpatch.Equal(newDoc, expectNewDoc)).Should(BeTrue(), "%v is not equal to %v", string(newDoc), string(expectNewDoc))
}

// Applying op:add of path with non-existent parents should give error.
// No auto instantiating!
func Test_jsonpatch_Add_WithNonExistentParent_is_error(t *testing.T) {
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
