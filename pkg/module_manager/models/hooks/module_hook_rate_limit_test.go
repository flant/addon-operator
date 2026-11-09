package hooks

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gohook "github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/pkg/module_manager/models/hooks/kind"
)

func TestModuleHookRateLimitIsIndependentAfterGoHookReregistration(t *testing.T) {
	executable := kind.NewGoHook(&gohook.HookConfig{
		OnStartup: &gohook.OrderedConfig{Order: 10},
		Settings: &gohook.HookConfigSettings{
			ExecutionMinInterval: time.Hour,
			ExecutionBurst:       1,
		},
	}, func(context.Context, *gohook.HookInput) error { return nil })
	original := NewModuleHook(executable)
	require.NoError(t, original.InitializeHookConfig())
	require.NoError(t, original.RateLimitWait(context.Background()))

	replacement := NewModuleHook(executable)
	require.NoError(t, replacement.InitializeHookConfig())

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.Error(t, original.RateLimitWait(ctx), "re-registering a Go hook must not replenish the previous registration")
	require.NoError(t, replacement.RateLimitWait(ctx), "the replacement registration has its own initial burst")
	require.Error(t, replacement.RateLimitWait(ctx), "all calls in one registration share its execution limit")
}
