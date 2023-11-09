package sdk

import (
	"github.com/flant/addon-operator/pkg/module_manager/models/hooks/kind"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
)

func TestRegister(t *testing.T) {
	t.Run("Hook with OnStartup and Kubernetes bindings should panic", func(t *testing.T) {
		hook := kind.NewGoHook(
			&go_hook.HookConfig{
				OnStartup: &go_hook.OrderedConfig{Order: 1},
				Kubernetes: []go_hook.KubernetesConfig{
					{
						Name:       "test",
						ApiVersion: "v1",
						Kind:       "Pod",
						FilterFunc: nil,
					},
				},
			},
			nil,
		)

		defer func() {
			r := recover()
			require.NotEmpty(t, r)
			assert.Equal(t, bindingsPanicMsg, r)
		}()
		Registry().Add(hook)
	})

	t.Run("Hook with OnStartup should not panic", func(t *testing.T) {
		hook := kind.NewGoHook(
			&go_hook.HookConfig{
				OnStartup: &go_hook.OrderedConfig{Order: 1},
			},
			nil,
		)

		defer func() {
			r := recover()
			assert.NotEqual(t, bindingsPanicMsg, r)
		}()
		Registry().Add(hook)
	})

	t.Run("Hook with Kubernetes binding should not panic", func(t *testing.T) {
		hook := kind.NewGoHook(
			&go_hook.HookConfig{
				Kubernetes: []go_hook.KubernetesConfig{
					{
						Name:       "test",
						ApiVersion: "v1",
						Kind:       "Pod",
						FilterFunc: nil,
					},
				},
			},
			nil,
		)

		defer func() {
			r := recover()
			assert.NotEqual(t, bindingsPanicMsg, r)
		}()
		Registry().Add(hook)
	})
}
