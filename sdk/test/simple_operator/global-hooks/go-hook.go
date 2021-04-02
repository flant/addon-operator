package global_hooks

import (
	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/sdk"
)

func init() {
	sdk.RegisterFunc(&go_hook.HookConfig{}, main)
}

func main(_ *go_hook.HookInput) error {
	return nil
}
