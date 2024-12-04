package global_hooks

import (
	gohook "github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/sdk"
)

func init() {
	// TODO: remove global logger?
	sdk.RegisterFunc(&gohook.HookConfig{}, main)
}

func main(_ *gohook.HookInput) error {
	return nil
}
