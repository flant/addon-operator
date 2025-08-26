package go_hook_test

import (
	"testing"

	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
)

type BenchmarkData struct {
	String     string
	Int        int
	Float      float64
	Bool       bool
	Slice      []int
	Map        map[string]int
	NestedData *BenchmarkData
}

func createTestData() *BenchmarkData {
	return &BenchmarkData{
		String: "benchmark test string with some length to simulate real data",
		Int:    42,
		Float:  3.14159,
		Bool:   true,
		Slice:  []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		Map: map[string]int{
			"key1": 1,
			"key2": 2,
			"key3": 3,
		},
		NestedData: &BenchmarkData{
			String: "nested string",
			Int:    123,
			Float:  2.71828,
			Bool:   false,
		},
	}
}

func BenchmarkUnmarshalTo_Int(b *testing.B) {
	w := &go_hook.Wrapped{Wrapped: 42}
	var target int

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.UnmarshalTo(&target)
	}
}

func BenchmarkUnmarshalToWithoutAssignable_Int(b *testing.B) {
	w := &go_hook.Wrapped{Wrapped: 42}
	var target int

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.UnmarshalToWithoutAssignable(&target)
	}
}

func BenchmarkUnmarshalToOld_Int(b *testing.B) {
	w := &go_hook.Wrapped{Wrapped: 42}
	var target int

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.UnmarshalToOld(&target)
	}
}

func BenchmarkUnmarshalTo_String(b *testing.B) {
	w := &go_hook.Wrapped{Wrapped: "benchmark test string with some length"}
	var target string

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.UnmarshalTo(&target)
	}
}

func BenchmarkUnmarshalToWithoutAssignable_String(b *testing.B) {
	w := &go_hook.Wrapped{Wrapped: "benchmark test string with some length"}
	var target string

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.UnmarshalToWithoutAssignable(&target)
	}
}

func BenchmarkUnmarshalToOld_String(b *testing.B) {
	w := &go_hook.Wrapped{Wrapped: "benchmark test string with some length"}
	var target string

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.UnmarshalToOld(&target)
	}
}

func BenchmarkUnmarshalTo_Slice(b *testing.B) {
	slice := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	w := &go_hook.Wrapped{Wrapped: slice}
	var target []int

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.UnmarshalTo(&target)
	}
}

func BenchmarkUnmarshalToWithoutAssignable_Slice(b *testing.B) {
	slice := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	w := &go_hook.Wrapped{Wrapped: slice}
	var target []int

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.UnmarshalToWithoutAssignable(&target)
	}
}

func BenchmarkUnmarshalToOld_Slice(b *testing.B) {
	slice := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	w := &go_hook.Wrapped{Wrapped: slice}
	var target []int

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.UnmarshalToOld(&target)
	}
}

func BenchmarkUnmarshalTo_Map(b *testing.B) {
	m := map[string]int{"key1": 1, "key2": 2, "key3": 3}
	w := &go_hook.Wrapped{Wrapped: m}
	var target map[string]int

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.UnmarshalTo(&target)
	}
}

func BenchmarkUnmarshalToWithoutAssignable_Map(b *testing.B) {
	m := map[string]int{"key1": 1, "key2": 2, "key3": 3}
	w := &go_hook.Wrapped{Wrapped: m}
	var target map[string]int

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.UnmarshalToWithoutAssignable(&target)
	}
}

func BenchmarkUnmarshalToOld_Map(b *testing.B) {
	m := map[string]int{"key1": 1, "key2": 2, "key3": 3}
	w := &go_hook.Wrapped{Wrapped: m}
	var target map[string]int

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.UnmarshalToOld(&target)
	}
}

func BenchmarkUnmarshalTo_Struct(b *testing.B) {
	data := createTestData()
	w := &go_hook.Wrapped{Wrapped: *data}
	var target BenchmarkData

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.UnmarshalTo(&target)
	}
}

func BenchmarkUnmarshalToWithoutAssignable_Struct(b *testing.B) {
	data := createTestData()
	w := &go_hook.Wrapped{Wrapped: *data}
	var target BenchmarkData

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.UnmarshalToWithoutAssignable(&target)
	}
}

func BenchmarkUnmarshalToOld_Struct(b *testing.B) {
	data := createTestData()
	w := &go_hook.Wrapped{Wrapped: *data}
	var target BenchmarkData

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.UnmarshalToOld(&target)
	}
}

func BenchmarkUnmarshalTo_Pointer(b *testing.B) {
	data := createTestData()
	w := &go_hook.Wrapped{Wrapped: data}
	var target *BenchmarkData

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.UnmarshalTo(&target)
	}
}

func BenchmarkUnmarshalToWithoutAssignable_Pointer(b *testing.B) {
	data := createTestData()
	w := &go_hook.Wrapped{Wrapped: data}
	var target *BenchmarkData

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.UnmarshalToWithoutAssignable(&target)
	}
}

func BenchmarkUnmarshalToOld_Pointer(b *testing.B) {
	data := createTestData()
	w := &go_hook.Wrapped{Wrapped: data}
	var target *BenchmarkData

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.UnmarshalToOld(&target)
	}
}

func BenchmarkUnmarshalTo_PointerToValue(b *testing.B) {
	data := createTestData()
	w := &go_hook.Wrapped{Wrapped: data}
	var target BenchmarkData

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.UnmarshalTo(&target)
	}
}

func BenchmarkUnmarshalToWithoutAssignable_PointerToValue(b *testing.B) {
	data := createTestData()
	w := &go_hook.Wrapped{Wrapped: data}
	var target BenchmarkData

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.UnmarshalToWithoutAssignable(&target)
	}
}

func BenchmarkUnmarshalToOld_PointerToValue(b *testing.B) {
	data := createTestData()
	w := &go_hook.Wrapped{Wrapped: data}
	var target BenchmarkData

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.UnmarshalToOld(&target)
	}
}

func BenchmarkUnmarshalTo_ErrorCase(b *testing.B) {
	w := &go_hook.Wrapped{Wrapped: 42}
	var target string

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.UnmarshalTo(&target)
	}
}

func BenchmarkUnmarshalToWithoutAssignable_ErrorCase(b *testing.B) {
	w := &go_hook.Wrapped{Wrapped: 42}
	var target string

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.UnmarshalToWithoutAssignable(&target)
	}
}

func BenchmarkUnmarshalTo_PointerTypeMismatch(b *testing.B) {
	val := 42
	w := &go_hook.Wrapped{Wrapped: &val}
	var target *string

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.UnmarshalTo(&target)
	}
}

func BenchmarkUnmarshalToWithoutAssignable_PointerTypeMismatch(b *testing.B) {
	val := 42
	w := &go_hook.Wrapped{Wrapped: &val}
	var target *string

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.UnmarshalToWithoutAssignable(&target)
	}
}

func BenchmarkUnmarshalToOld_PointerTypeMismatch(b *testing.B) {
	val := 42
	w := &go_hook.Wrapped{Wrapped: &val}
	var target *string

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.UnmarshalToOld(&target)
	}
}

func BenchmarkUnmarshalTo_DifferentStructs(b *testing.B) {
	type StructA struct {
		Field int
	}
	type StructB struct {
		Field string
	}

	w := &go_hook.Wrapped{Wrapped: StructA{Field: 42}}
	var target StructB

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.UnmarshalTo(&target)
	}
}

func BenchmarkUnmarshalToWithoutAssignable_DifferentStructs(b *testing.B) {
	type StructA struct {
		Field int
	}
	type StructB struct {
		Field string
	}

	w := &go_hook.Wrapped{Wrapped: StructA{Field: 42}}
	var target StructB

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.UnmarshalToWithoutAssignable(&target)
	}
}

func BenchmarkUnmarshalToOld_DifferentStructs(b *testing.B) {
	type StructA struct {
		Field int
	}
	type StructB struct {
		Field string
	}

	w := &go_hook.Wrapped{Wrapped: StructA{Field: 42}}
	var target StructB

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.UnmarshalToOld(&target)
	}
}

func BenchmarkUnmarshalTo_PointerToDifferentStructs(b *testing.B) {
	type StructA struct {
		Field int
	}
	type StructB struct {
		Field string
	}

	val := StructA{Field: 42}
	w := &go_hook.Wrapped{Wrapped: &val}
	var target *StructB

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.UnmarshalTo(&target)
	}
}

func BenchmarkUnmarshalToWithoutAssignable_PointerToDifferentStructs(b *testing.B) {
	type StructA struct {
		Field int
	}
	type StructB struct {
		Field string
	}

	val := StructA{Field: 42}
	w := &go_hook.Wrapped{Wrapped: &val}
	var target *StructB

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.UnmarshalToWithoutAssignable(&target)
	}
}

func BenchmarkUnmarshalToOld_PointerToDifferentStructs(b *testing.B) {
	type StructA struct {
		Field int
	}
	type StructB struct {
		Field string
	}

	val := StructA{Field: 42}
	w := &go_hook.Wrapped{Wrapped: &val}
	var target *StructB

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.UnmarshalToOld(&target)
	}
}

func BenchmarkCompareAllMethods_Int(b *testing.B) {
	w := &go_hook.Wrapped{Wrapped: 42}

	b.Run("UnmarshalTo", func(b *testing.B) {
		var target int
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = w.UnmarshalTo(&target)
		}
	})

	b.Run("UnmarshalToWithoutAssignable", func(b *testing.B) {
		var target int
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = w.UnmarshalToWithoutAssignable(&target)
		}
	})

	b.Run("UnmarshalToOld", func(b *testing.B) {
		var target int
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = w.UnmarshalToOld(&target)
		}
	})
}

func BenchmarkCompareAllMethods_Struct(b *testing.B) {
	data := createTestData()
	w := &go_hook.Wrapped{Wrapped: *data}

	b.Run("UnmarshalTo", func(b *testing.B) {
		var target BenchmarkData
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = w.UnmarshalTo(&target)
		}
	})

	b.Run("UnmarshalToWithoutAssignable", func(b *testing.B) {
		var target BenchmarkData
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = w.UnmarshalToWithoutAssignable(&target)
		}
	})

	b.Run("UnmarshalToOld", func(b *testing.B) {
		var target BenchmarkData
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = w.UnmarshalToOld(&target)
		}
	})
}

func BenchmarkMemoryUsage_Int(b *testing.B) {
	w := &go_hook.Wrapped{Wrapped: 42}

	b.Run("UnmarshalTo", func(b *testing.B) {
		b.ReportAllocs()
		var target int
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = w.UnmarshalTo(&target)
		}
	})

	b.Run("UnmarshalToWithoutAssignable", func(b *testing.B) {
		b.ReportAllocs()
		var target int
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = w.UnmarshalToWithoutAssignable(&target)
		}
	})

	b.Run("UnmarshalToOld", func(b *testing.B) {
		b.ReportAllocs()
		var target int
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = w.UnmarshalToOld(&target)
		}
	})
}

func BenchmarkMemoryUsage_Struct(b *testing.B) {
	data := createTestData()
	w := &go_hook.Wrapped{Wrapped: *data}

	b.Run("UnmarshalTo", func(b *testing.B) {
		b.ReportAllocs()
		var target BenchmarkData
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = w.UnmarshalTo(&target)
		}
	})

	b.Run("UnmarshalToWithoutAssignable", func(b *testing.B) {
		b.ReportAllocs()
		var target BenchmarkData
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = w.UnmarshalToWithoutAssignable(&target)
		}
	})

	b.Run("UnmarshalToOld", func(b *testing.B) {
		b.ReportAllocs()
		var target BenchmarkData
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = w.UnmarshalToOld(&target)
		}
	})
}

func BenchmarkErrorVsPanic(b *testing.B) {
	b.Run("ErrorCase_UnmarshalTo", func(b *testing.B) {
		w := &go_hook.Wrapped{Wrapped: 42}
		var target string
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = w.UnmarshalTo(&target)
		}
	})

	b.Run("ErrorCase_UnmarshalToWithoutAssignable", func(b *testing.B) {
		w := &go_hook.Wrapped{Wrapped: 42}
		var target string
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = w.UnmarshalToWithoutAssignable(&target)
		}
	})

	b.Run("ErrorCase_UnmarshalToOld_PointerMismatch", func(b *testing.B) {
		val := 42
		w := &go_hook.Wrapped{Wrapped: &val}
		var target *string
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = w.UnmarshalToOld(&target)
		}
	})

	b.Run("ErrorCase_UnmarshalToOld_PointerToPointerMismatch", func(b *testing.B) {
		val := 42
		w := &go_hook.Wrapped{Wrapped: &val}
		var target *string
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = w.UnmarshalToOld(&target)
		}
	})

	b.Run("ErrorCase_UnmarshalToOld_PointerToDifferentStructs", func(b *testing.B) {
		type StructA struct {
			Field int
		}
		type StructB struct {
			Field string
		}

		val := StructA{Field: 42}
		w := &go_hook.Wrapped{Wrapped: &val}
		var target *StructB
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = w.UnmarshalToOld(&target)
		}
	})
}

func BenchmarkUnmarshalToOld_ErrorVsPanic(b *testing.B) {
	b.Run("ErrorCase_PointerToDifferentStructs", func(b *testing.B) {
		type StructA struct {
			Field int
		}
		type StructB struct {
			Field string
		}

		val := StructA{Field: 42}
		w := &go_hook.Wrapped{Wrapped: &val}
		var target *StructB
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = w.UnmarshalToOld(&target)
		}
	})

	b.Run("ErrorCase_PointerTypeMismatch", func(b *testing.B) {
		val := 42
		w := &go_hook.Wrapped{Wrapped: &val}
		var target *string
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = w.UnmarshalToOld(&target)
		}
	})

	b.Run("ErrorCase_NilPointerToPointer", func(b *testing.B) {
		var nilPtr *int
		w := &go_hook.Wrapped{Wrapped: nilPtr}
		var target *int
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = w.UnmarshalToOld(&target)
		}
	})
}

func BenchmarkNilPointerCases(b *testing.B) {
	b.Run("NilPointer_UnmarshalTo", func(b *testing.B) {
		var nilPtr *int
		w := &go_hook.Wrapped{Wrapped: nilPtr}
		var target int
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = w.UnmarshalTo(&target)
		}
	})

	b.Run("NilPointer_UnmarshalToWithoutAssignable", func(b *testing.B) {
		var nilPtr *int
		w := &go_hook.Wrapped{Wrapped: nilPtr}
		var target int
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = w.UnmarshalToWithoutAssignable(&target)
		}
	})

	b.Run("NilPointer_UnmarshalToOld", func(b *testing.B) {
		var nilPtr *int
		w := &go_hook.Wrapped{Wrapped: nilPtr}
		var target *int
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = w.UnmarshalToOld(&target)
		}
	})
}

func BenchmarkCompareDirectCast_ValueStruct(b *testing.B) {
	data := createTestData()
	var anyVal any = *data

	b.Run("DirectCast", func(b *testing.B) {
		var target BenchmarkData
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			target = anyVal.(BenchmarkData)
		}
		_ = target
	})

	b.Run("UnmarshalToOld", func(b *testing.B) {
		w := &go_hook.Wrapped{Wrapped: *data}
		var target BenchmarkData
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = w.UnmarshalToOld(&target)
		}
	})

	b.Run("UnmarshalToWithoutAssignable", func(b *testing.B) {
		w := &go_hook.Wrapped{Wrapped: *data}
		var target BenchmarkData
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = w.UnmarshalToWithoutAssignable(&target)
		}
	})
}

func BenchmarkCompareDirectCast_PointerStruct(b *testing.B) {
	data := createTestData()
	var anyVal any = data

	b.Run("DirectCast", func(b *testing.B) {
		var target *BenchmarkData
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			target = anyVal.(*BenchmarkData)
		}
		_ = target
	})

	b.Run("UnmarshalToOld", func(b *testing.B) {
		w := &go_hook.Wrapped{Wrapped: data}
		var target *BenchmarkData
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = w.UnmarshalToOld(&target)
		}
	})

	b.Run("UnmarshalToWithoutAssignable", func(b *testing.B) {
		w := &go_hook.Wrapped{Wrapped: data}
		var target *BenchmarkData
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = w.UnmarshalToWithoutAssignable(&target)
		}
	})
}
