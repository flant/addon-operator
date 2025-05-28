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
