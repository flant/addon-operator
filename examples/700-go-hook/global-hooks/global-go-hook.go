package global_hooks

import (
	gohook "github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/sdk"
)

var _ = sdk.RegisterFunc(&gohook.HookConfig{
	OnStartup: &gohook.OrderedConfig{Order: 10},
}, handler)

func handler(input *gohook.HookInput) error {
	input.Logger.Info("Start Global Go hook")
	return nil
}
