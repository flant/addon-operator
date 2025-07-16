package go_hook_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
)

func Test_FilterResult(t *testing.T) {
	type SomeStruct struct {
		String string
	}

	t.Run("UnmarshalTo", func(t *testing.T) {
		w := &go_hook.Wrapped{
			Wrapped: &SomeStruct{
				String: "INPUT STRING",
			},
		}

		ss := &SomeStruct{}

		err := w.UnmarshalTo(ss)
		assert.NoError(t, err)

		assert.Equal(t, "INPUT STRING", ss.String)
	})

	t.Run("UnmarshalTo_StructWithSeveralFields", func(t *testing.T) {
		type SomeStruct2 struct {
			String  string
			String2 string
		}

		w := &go_hook.Wrapped{
			Wrapped: SomeStruct2{
				String:  "INPUT STRING",
				String2: "INPUT TESTING STRING",
			},
		}

		ss := SomeStruct2{}

		err := w.UnmarshalTo(&ss)
		assert.NoError(t, err)

		assert.Equal(t, "INPUT STRING", ss.String)
		assert.Equal(t, "INPUT TESTING STRING", ss.String2)
	})

	t.Run("UnmarshalTo_NilWrapped", func(t *testing.T) {
		w := &go_hook.Wrapped{
			Wrapped: nil,
		}

		ss := &SomeStruct{}

		err := w.UnmarshalTo(ss)
		assert.Error(t, err)
	})

	t.Run("UnmarshalTo_NilTarget", func(t *testing.T) {
		w := &go_hook.Wrapped{
			Wrapped: &SomeStruct{
				String: "INPUT STRING",
			},
		}

		var ss *SomeStruct

		err := w.UnmarshalTo(ss)
		assert.Error(t, err)
	})

	t.Run("UnmarshalTo_DifferentTypes", func(t *testing.T) {
		type OtherStruct struct {
			Field int
		}

		w := &go_hook.Wrapped{
			Wrapped: &SomeStruct{
				String: "INPUT STRING",
			},
		}

		os := &OtherStruct{}

		err := w.UnmarshalTo(os)
		assert.Error(t, err)
	})

	t.Run("UnmarshalTo_CustomStructWithMap", func(t *testing.T) {
		type OtherStruct struct {
			Field map[string][]byte
		}

		w := &go_hook.Wrapped{
			Wrapped: &OtherStruct{
				Field: map[string][]byte{
					"first":  []byte("first"),
					"second": []byte("second"),
				},
			},
		}

		os := &OtherStruct{}

		err := w.UnmarshalTo(os)
		assert.NoError(t, err)
		assert.Equal(t, os, w.Wrapped)
	})

	t.Run("UnmarshalTo_PointerToStruct", func(t *testing.T) {
		type SomeStruct struct {
			String string
		}

		w := &go_hook.Wrapped{
			Wrapped: &SomeStruct{String: "pointer"},
		}

		ss := &SomeStruct{}
		err := w.UnmarshalTo(ss)
		assert.NoError(t, err)
		assert.NotNil(t, ss)
		assert.Equal(t, "pointer", ss.String)
	})

	t.Run("UnmarshalTo_Int32ToString", func(t *testing.T) {
		w := &go_hook.Wrapped{
			Wrapped: int32(6443),
		}

		var portData string
		err := w.UnmarshalTo(&portData)

		assert.Error(t, err)
	})

	t.Run("UnmarshalTo_NestedStruct", func(t *testing.T) {
		type InnerStruct struct {
			Value string
		}
		type OuterStruct struct {
			Inner InnerStruct
		}

		w := &go_hook.Wrapped{
			Wrapped: OuterStruct{
				Inner: InnerStruct{Value: "nested"},
			},
		}

		os := OuterStruct{}
		err := w.UnmarshalTo(&os)
		assert.NoError(t, err)
		assert.Equal(t, "nested", os.Inner.Value)
	})

	t.Run("UnmarshalTo_NilTarget", func(t *testing.T) {
		w := &go_hook.Wrapped{
			Wrapped: &SomeStruct{
				String: "INPUT STRING",
			},
		}

		var ss *SomeStruct
		err := w.UnmarshalTo(ss)
		assert.Error(t, err)
	})

	t.Run("UnmarshalTo_IncompatibleTypes", func(t *testing.T) {
		w := &go_hook.Wrapped{
			Wrapped: "string value",
		}

		var num int
		err := w.UnmarshalTo(&num)
		assert.Error(t, err)
	})

	t.Run("UnmarshalTo_NilPointer", func(t *testing.T) {
		var nilPtr *SomeStruct
		w := &go_hook.Wrapped{
			Wrapped: nilPtr,
		}

		var ss *SomeStruct
		err := w.UnmarshalTo(&ss)
		assert.NoError(t, err)
		assert.Nil(t, ss)
	})

	t.Run("UnmarshalTo_PrimitiveTypes", func(t *testing.T) {
		w := &go_hook.Wrapped{
			Wrapped: 42,
		}

		var num int
		err := w.UnmarshalTo(&num)
		assert.NoError(t, err)
		assert.Equal(t, 42, num)
	})

	t.Run("UnmarshalTo_Slice", func(t *testing.T) {
		w := &go_hook.Wrapped{
			Wrapped: []int{1, 2, 3},
		}

		var slice []int
		err := w.UnmarshalTo(&slice)
		assert.NoError(t, err)
		assert.Equal(t, []int{1, 2, 3}, slice)
	})

	t.Run("UnmarshalTo_Map", func(t *testing.T) {
		w := &go_hook.Wrapped{
			Wrapped: map[string]int{"one": 1, "two": 2},
		}

		var m map[string]int
		err := w.UnmarshalTo(&m)
		assert.NoError(t, err)
		assert.Equal(t, map[string]int{"one": 1, "two": 2}, m)
	})

	t.Run("UnmarshalTo_ZeroValue", func(t *testing.T) {
		w := &go_hook.Wrapped{
			Wrapped: 0,
		}

		var num int
		err := w.UnmarshalTo(&num)
		assert.NoError(t, err)
		assert.Equal(t, 0, num)
	})

	t.Run("UnmarshalTo_UninitializedTarget", func(t *testing.T) {
		w := &go_hook.Wrapped{
			Wrapped: "test",
		}

		var str string
		err := w.UnmarshalTo(&str)
		assert.NoError(t, err)
		assert.Equal(t, "test", str)
	})

	t.Run("AsString", func(t *testing.T) {
		w := &go_hook.Wrapped{
			Wrapped: "test string",
		}

		result := w.String()
		assert.Equal(t, "test string", result)
	})

	t.Run("AsStringEmpty", func(t *testing.T) {
		w := &go_hook.Wrapped{
			Wrapped: "",
		}

		result := w.String()
		assert.Equal(t, "", result)
	})

	t.Run("BoolAsString", func(t *testing.T) {
		w := &go_hook.Wrapped{
			Wrapped: false,
		}

		result := w.String()
		assert.Equal(t, "false", result)
	})

	t.Run("IntAsString", func(t *testing.T) {
		w := &go_hook.Wrapped{
			Wrapped: 333,
		}

		result := w.String()
		assert.Equal(t, "333", result)
	})
}
