package go_hook_test

import (
	"testing"

	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/stretchr/testify/assert"
)

func Test_FilterResult(t *testing.T) {
	type SomeStruct struct {
		String string
	}

	t.Run("UnmarhalTo", func(t *testing.T) {
		wrapp := &go_hook.Wrapped{
			Wrapped: &SomeStruct{
				String: "INPUT STRING",
			},
		}

		ss := &SomeStruct{}

		err := wrapp.UnmarhalTo(ss)
		assert.NoError(t, err)

		assert.Equal(t, "INPUT STRING", ss.String)
	})

	t.Run("UnmarhalTo_NilWrapped", func(t *testing.T) {
		wrapp := &go_hook.Wrapped{
			Wrapped: nil,
		}

		ss := &SomeStruct{}

		err := wrapp.UnmarhalTo(ss)
		assert.Error(t, err)
	})

	t.Run("UnmarhalTo_NilTarget", func(t *testing.T) {
		wrapp := &go_hook.Wrapped{
			Wrapped: &SomeStruct{
				String: "INPUT STRING",
			},
		}

		var ss *SomeStruct = nil

		err := wrapp.UnmarhalTo(ss)
		assert.Error(t, err)
	})

	t.Run("UnmarhalTo_DifferentTypes", func(t *testing.T) {
		type OtherStruct struct {
			Field int
		}

		wrapp := &go_hook.Wrapped{
			Wrapped: &SomeStruct{
				String: "INPUT STRING",
			},
		}

		os := &OtherStruct{}

		err := wrapp.UnmarhalTo(os)
		assert.Error(t, err)
	})

	t.Run("AsString", func(t *testing.T) {
		wrapp := &go_hook.Wrapped{
			Wrapped: "test string",
		}

		result := wrapp.String()
		assert.Equal(t, "test string", result)
	})

	t.Run("AsStringEmpty", func(t *testing.T) {
		wrapp := &go_hook.Wrapped{
			Wrapped: "",
		}

		result := wrapp.String()
		assert.Equal(t, "", result)
	})
}
