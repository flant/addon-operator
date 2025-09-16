package sublevel

import (
	"context"

	gohook "github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/sdk"
)

// TODO: remove global logger?
var _ = sdk.RegisterFunc(&gohook.HookConfig{}, main)

func main(_ context.Context, _ *gohook.HookInput) error {
	return nil
}
