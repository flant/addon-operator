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

	t.Run("UnmarhalTo", func(t *testing.T) {
		w := &go_hook.Wrapped{
			Wrapped: &SomeStruct{
				String: "INPUT STRING",
			},
		}

		ss := &SomeStruct{}

		err := w.UnmarhalTo(ss)
		assert.NoError(t, err)

		assert.Equal(t, "INPUT STRING", ss.String)
	})

	t.Run("UnmarhalTo_NilWrapped", func(t *testing.T) {
		w := &go_hook.Wrapped{
			Wrapped: nil,
		}

		ss := &SomeStruct{}

		err := w.UnmarhalTo(ss)
		assert.Error(t, err)
	})

	t.Run("UnmarhalTo_NilTarget", func(t *testing.T) {
		w := &go_hook.Wrapped{
			Wrapped: &SomeStruct{
				String: "INPUT STRING",
			},
		}

		var ss *SomeStruct

		err := w.UnmarhalTo(ss)
		assert.Error(t, err)
	})

	t.Run("UnmarhalTo_DifferentTypes", func(t *testing.T) {
		type OtherStruct struct {
			Field int
		}

		w := &go_hook.Wrapped{
			Wrapped: &SomeStruct{
				String: "INPUT STRING",
			},
		}

		os := &OtherStruct{}

		err := w.UnmarhalTo(os)
		assert.Error(t, err)
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
}
