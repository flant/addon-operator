package go_hook_test

import (
	"bytes"
	"io"
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

	t.Run("UnmarshalTo_IntToFloat64Error", func(t *testing.T) {
		w := &go_hook.Wrapped{
			Wrapped: 32,
		}

		var target float32
		err := w.UnmarshalTo(&target)

		assert.Error(t, err)
	})

	t.Run("UnmarshalTo_IntToFloat64PtrError", func(t *testing.T) {
		w := &go_hook.Wrapped{
			Wrapped: 42,
		}

		var target *float64
		err := w.UnmarshalTo(&target)

		assert.Error(t, err)
		assert.Nil(t, target)
	})

	t.Run("UnmarshalTo_IntPtrToFloat64Error", func(t *testing.T) {
		val := 42
		w := &go_hook.Wrapped{
			Wrapped: &val,
		}

		var target float64
		err := w.UnmarshalTo(&target)

		assert.Error(t, err)
	})

	t.Run("UnmarshalTo_CustomIntTypeError", func(t *testing.T) {
		type MyInt int
		w := &go_hook.Wrapped{
			Wrapped: MyInt(42),
		}

		var target int
		err := w.UnmarshalTo(&target)

		assert.Error(t, err)
	})

	t.Run("UnmarshalTo_IntToEmptyInterface", func(t *testing.T) {
		w := &go_hook.Wrapped{
			Wrapped: 42,
		}

		var target interface{}
		err := w.UnmarshalTo(&target)

		assert.NoError(t, err)
		assert.Equal(t, 42, target)
	})

	t.Run("UnmarshalTo_DifferentStructs", func(t *testing.T) {
		type StructA struct{ X int }
		type StructB struct{ X int }

		w := &go_hook.Wrapped{
			Wrapped: StructA{X: 42},
		}

		var target StructB
		err := w.UnmarshalTo(&target)

		assert.Error(t, err)
	})

	t.Run("UnmarshalTo_SameStructType", func(t *testing.T) {
		type Person struct {
			Name string
			Age  int
		}

		w := &go_hook.Wrapped{
			Wrapped: Person{Name: "Alice", Age: 30},
		}

		var target Person
		err := w.UnmarshalTo(&target)

		assert.NoError(t, err)
		assert.Equal(t, "Alice", target.Name)
		assert.Equal(t, 30, target.Age)
	})

	t.Run("UnmarshalTo_DifferentStructsWithSameFields", func(t *testing.T) {
		type A struct{ X int }
		type B struct{ X int }

		w := &go_hook.Wrapped{
			Wrapped: A{X: 42},
		}

		var target B
		err := w.UnmarshalTo(&target)

		assert.Error(t, err)
	})

	t.Run("UnmarshalTo_AnonymousField", func(t *testing.T) {
		type Base struct {
			ID int
		}
		type Extended struct {
			Base
			Name string
		}

		w := &go_hook.Wrapped{
			Wrapped: Extended{
				Base: Base{ID: 1},
				Name: "Test",
			},
		}

		var target Extended
		err := w.UnmarshalTo(&target)

		assert.NoError(t, err)
		assert.Equal(t, 1, target.ID)
	})

	t.Run("UnmarshalTo_NoInitPointerToStruct", func(t *testing.T) {
		type Item struct {
			Value string
		}

		w := &go_hook.Wrapped{
			Wrapped: &Item{Value: "pointer"},
		}

		var target *Item
		err := w.UnmarshalTo(&target)

		assert.NoError(t, err)
		assert.Equal(t, "pointer", target.Value)
	})

	t.Run("UnmarshalTo_NotPointerTarget", func(t *testing.T) {
		w := &go_hook.Wrapped{Wrapped: 10}
		var num int
		err := w.UnmarshalTo(num)
		assert.Error(t, err)
	})

	t.Run("UnmarshalTo_CommonInterface", func(t *testing.T) {
		var buf bytes.Buffer

		w := &go_hook.Wrapped{Wrapped: &buf}
		var writer io.Writer
		err := w.UnmarshalTo(&writer)

		assert.NoError(t, err)
		assert.Equal(t, &buf, writer)
	})

	t.Run("UnmarshalTo_NilPointerWrappedToValue", func(t *testing.T) {
		type MyStruct struct{ Field string }

		var nilPtr *MyStruct
		w := &go_hook.Wrapped{Wrapped: nilPtr}

		var target MyStruct
		err := w.UnmarshalTo(&target)
		assert.NoError(t, err)
		assert.Equal(t, "", target.Field)
	})

	t.Run("UnmarshalTo_DoublePointerToPointer", func(t *testing.T) {
		val := 100
		ptr1 := &val
		ptr2 := &ptr1
		w := &go_hook.Wrapped{Wrapped: ptr2}

		var target *int
		err := w.UnmarshalTo(&target)
		assert.NoError(t, err)
		assert.NotNil(t, target)
		assert.Equal(t, 100, *target)
	})

	t.Run("String_ComplexStruct", func(t *testing.T) {
		type Person struct {
			Name string `json:"Name"`
			Age  int    `json:"Age"`
		}
		w := &go_hook.Wrapped{Wrapped: Person{Name: "John", Age: 25}}
		result := w.String()
		assert.Equal(t, `{"Name":"John","Age":25}`, result)
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

func Test_FilterResult_UnmarshalToWithoutAssignable(t *testing.T) {
	type SomeStruct struct {
		String string
	}

	t.Run("ExactTypeMatch", func(t *testing.T) {
		w := &go_hook.Wrapped{
			Wrapped: &SomeStruct{
				String: "INPUT STRING",
			},
		}

		ss := &SomeStruct{}

		err := w.UnmarshalToWithoutAssignable(ss)
		assert.NoError(t, err)

		assert.Equal(t, "INPUT STRING", ss.String)
	})

	t.Run("ExactTypeMatch_ValueToValue", func(t *testing.T) {
		w := &go_hook.Wrapped{
			Wrapped: SomeStruct{
				String: "INPUT STRING",
			},
		}

		ss := SomeStruct{}

		err := w.UnmarshalToWithoutAssignable(&ss)
		assert.NoError(t, err)

		assert.Equal(t, "INPUT STRING", ss.String)
	})

	t.Run("ExactTypeMatch_PointerToPointer", func(t *testing.T) {
		w := &go_hook.Wrapped{
			Wrapped: &SomeStruct{
				String: "INPUT STRING",
			},
		}

		var ss *SomeStruct

		err := w.UnmarshalToWithoutAssignable(&ss)
		assert.NoError(t, err)

		assert.Equal(t, "INPUT STRING", ss.String)
	})

	t.Run("ExactTypeMatch_PrimitiveTypes", func(t *testing.T) {
		w := &go_hook.Wrapped{
			Wrapped: 42,
		}

		var num int
		err := w.UnmarshalToWithoutAssignable(&num)
		assert.NoError(t, err)
		assert.Equal(t, 42, num)
	})

	t.Run("ExactTypeMatch_String", func(t *testing.T) {
		w := &go_hook.Wrapped{
			Wrapped: "test string",
		}

		var str string
		err := w.UnmarshalToWithoutAssignable(&str)
		assert.NoError(t, err)
		assert.Equal(t, "test string", str)
	})

	t.Run("ExactTypeMatch_Slice", func(t *testing.T) {
		w := &go_hook.Wrapped{
			Wrapped: []int{1, 2, 3},
		}

		var slice []int
		err := w.UnmarshalToWithoutAssignable(&slice)
		assert.NoError(t, err)
		assert.Equal(t, []int{1, 2, 3}, slice)
	})

	t.Run("ExactTypeMatch_Map", func(t *testing.T) {
		w := &go_hook.Wrapped{
			Wrapped: map[string]int{"one": 1, "two": 2},
		}

		var m map[string]int
		err := w.UnmarshalToWithoutAssignable(&m)
		assert.NoError(t, err)
		assert.Equal(t, map[string]int{"one": 1, "two": 2}, m)
	})

	t.Run("NilPointerWrapped", func(t *testing.T) {
		var nilPtr *SomeStruct
		w := &go_hook.Wrapped{
			Wrapped: nilPtr,
		}

		var ss *SomeStruct
		err := w.UnmarshalToWithoutAssignable(&ss)
		assert.NoError(t, err)
		assert.Nil(t, ss)
	})

	t.Run("NilPointerWrappedToValue", func(t *testing.T) {
		var nilPtr *SomeStruct
		w := &go_hook.Wrapped{
			Wrapped: nilPtr,
		}

		var ss SomeStruct
		err := w.UnmarshalToWithoutAssignable(&ss)
		assert.NoError(t, err)
		assert.Equal(t, "", ss.String)
	})

	t.Run("DifferentTypes_ShouldFail", func(t *testing.T) {
		type OtherStruct struct {
			Field int
		}

		w := &go_hook.Wrapped{
			Wrapped: &SomeStruct{
				String: "INPUT STRING",
			},
		}

		os := &OtherStruct{}

		err := w.UnmarshalToWithoutAssignable(os)
		assert.Error(t, err)
		assert.Equal(t, go_hook.ErrUnmarshalToTypesNotMatch, err)
	})

	t.Run("DifferentPrimitiveTypes_ShouldFail", func(t *testing.T) {
		w := &go_hook.Wrapped{
			Wrapped: 42,
		}

		var target float64
		err := w.UnmarshalToWithoutAssignable(&target)
		assert.Error(t, err)
		assert.Equal(t, go_hook.ErrUnmarshalToTypesNotMatch, err)
	})

	t.Run("CustomTypeToBaseType_ShouldFail", func(t *testing.T) {
		type MyInt int
		w := &go_hook.Wrapped{
			Wrapped: MyInt(42),
		}

		var target int
		err := w.UnmarshalToWithoutAssignable(&target)
		assert.Error(t, err)
		assert.Equal(t, go_hook.ErrUnmarshalToTypesNotMatch, err)
	})

	t.Run("BaseTypeToCustomType_ShouldFail", func(t *testing.T) {
		type MyInt int
		w := &go_hook.Wrapped{
			Wrapped: 42,
		}

		var target MyInt
		err := w.UnmarshalToWithoutAssignable(&target)
		assert.Error(t, err)
		assert.Equal(t, go_hook.ErrUnmarshalToTypesNotMatch, err)
	})

	t.Run("DifferentStructsWithSameFields_ShouldFail", func(t *testing.T) {
		type A struct{ X int }
		type B struct{ X int }

		w := &go_hook.Wrapped{
			Wrapped: A{X: 42},
		}

		var target B
		err := w.UnmarshalToWithoutAssignable(&target)
		assert.Error(t, err)
		assert.Equal(t, go_hook.ErrUnmarshalToTypesNotMatch, err)
	})

	t.Run("Int32ToString_ShouldFail", func(t *testing.T) {
		w := &go_hook.Wrapped{
			Wrapped: int32(6443),
		}

		var portData string
		err := w.UnmarshalToWithoutAssignable(&portData)
		assert.Error(t, err)
		assert.Equal(t, go_hook.ErrUnmarshalToTypesNotMatch, err)
	})

	t.Run("NilWrapped_ShouldFail", func(t *testing.T) {
		w := &go_hook.Wrapped{
			Wrapped: nil,
		}

		ss := &SomeStruct{}

		err := w.UnmarshalToWithoutAssignable(ss)
		assert.Error(t, err)
		assert.Equal(t, go_hook.ErrEmptyWrapped, err)
	})

	t.Run("NotPointerTarget_ShouldFail", func(t *testing.T) {
		w := &go_hook.Wrapped{Wrapped: 10}
		var num int
		err := w.UnmarshalToWithoutAssignable(num)
		assert.Error(t, err)
	})

	t.Run("NilTarget_ShouldFail", func(t *testing.T) {
		w := &go_hook.Wrapped{
			Wrapped: &SomeStruct{
				String: "INPUT STRING",
			},
		}

		var ss *SomeStruct
		err := w.UnmarshalToWithoutAssignable(ss)
		assert.Error(t, err)
	})

	t.Run("CompareWithOriginalUnmarshalTo", func(t *testing.T) {
		type MyInt int

		w := &go_hook.Wrapped{
			Wrapped: MyInt(42),
		}

		var target int

		err1 := w.UnmarshalTo(&target)
		assert.Error(t, err1)

		err2 := w.UnmarshalToWithoutAssignable(&target)
		assert.Error(t, err2)
		assert.Equal(t, go_hook.ErrUnmarshalToTypesNotMatch, err2)
	})

	t.Run("CompareUnmarshalToOldAndUnmarshalToWithoutAssignable", func(t *testing.T) {
		t.Run("IntToString_ShouldFailInBoth", func(t *testing.T) {
			w := &go_hook.Wrapped{
				Wrapped: 42,
			}

			var target string

			assert.Panics(t, func() {
				w.UnmarshalToOld(&target)
			})

			err2 := w.UnmarshalToWithoutAssignable(&target)
			assert.Error(t, err2)
			assert.Equal(t, go_hook.ErrUnmarshalToTypesNotMatch, err2)
		})

		t.Run("StringToInt_ShouldFailInBoth", func(t *testing.T) {
			w := &go_hook.Wrapped{
				Wrapped: "42",
			}

			var target int

			assert.Panics(t, func() {
				w.UnmarshalToOld(&target)
			})

			err2 := w.UnmarshalToWithoutAssignable(&target)
			assert.Error(t, err2)
			assert.Equal(t, go_hook.ErrUnmarshalToTypesNotMatch, err2)
		})

		t.Run("ExactTypeMatch_ShouldWorkInBoth", func(t *testing.T) {
			w := &go_hook.Wrapped{
				Wrapped: 42,
			}

			var target int

			err1 := w.UnmarshalToOld(&target)
			assert.NoError(t, err1)
			assert.Equal(t, 42, target)

			target = 0

			err2 := w.UnmarshalToWithoutAssignable(&target)
			assert.NoError(t, err2)
			assert.Equal(t, 42, target)
		})

		t.Run("PointerToValue_ShouldWorkInBoth", func(t *testing.T) {
			val := 42
			w := &go_hook.Wrapped{
				Wrapped: &val,
			}

			var target int

			err1 := w.UnmarshalToOld(&target)
			assert.NoError(t, err1)
			assert.Equal(t, 42, target)

			target = 0

			err2 := w.UnmarshalToWithoutAssignable(&target)
			assert.NoError(t, err2)
			assert.Equal(t, 42, target)
		})

		t.Run("ValueToPointer_ShouldFailInBoth", func(t *testing.T) {
			w := &go_hook.Wrapped{
				Wrapped: 42,
			}

			var target *int

			assert.Panics(t, func() {
				w.UnmarshalToOld(&target)
			})

			err2 := w.UnmarshalToWithoutAssignable(&target)
			assert.Error(t, err2)
			assert.Equal(t, go_hook.ErrUnmarshalToTypesNotMatch, err2)
		})

		t.Run("StructExactMatch_ShouldWorkInBoth", func(t *testing.T) {
			type TestStruct struct {
				Field string
			}

			w := &go_hook.Wrapped{
				Wrapped: TestStruct{Field: "test"},
			}

			var target TestStruct

			err1 := w.UnmarshalToOld(&target)
			assert.NoError(t, err1)
			assert.Equal(t, "test", target.Field)

			target = TestStruct{}

			err2 := w.UnmarshalToWithoutAssignable(&target)
			assert.NoError(t, err2)
			assert.Equal(t, "test", target.Field)
		})
	})
}
