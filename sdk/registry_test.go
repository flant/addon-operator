package sdk

import (
	"testing"

	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegister(t *testing.T) {
	const panicMsg = "OnStartup hook always has binding context without Kubernetes snapshots. To prevent logic errors, don't use OnStartup and Kubernetes bindings in the same Go hook configuration."

	t.Run("Hook with OnStartup and Kubernetes bindings should panic", func(t *testing.T) {
		hook := newCommonGoHook(
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
			assert.Equal(t, panicMsg, r)
		}()
		Registry().Add(hook)
	})

	t.Run("Hook with OnStartup should not panic", func(t *testing.T) {
		hook := newCommonGoHook(
			&go_hook.HookConfig{
				OnStartup: &go_hook.OrderedConfig{Order: 1},
			},
			nil,
		)

		defer func() {
			r := recover()
			assert.NotEqual(t, panicMsg, r)
		}()
		Registry().Add(hook)
	})

	t.Run("Hook with Kubernetes binding should not panic", func(t *testing.T) {
		hook := newCommonGoHook(
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
			assert.NotEqual(t, panicMsg, r)
		}()
		Registry().Add(hook)
	})
}
