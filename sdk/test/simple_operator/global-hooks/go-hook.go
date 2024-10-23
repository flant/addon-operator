package global_hooks

import (
	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/sdk"
	"github.com/flant/shell-operator/pkg/unilogger"
)

func init() {
	// TODO: remove global logger?
	sdk.RegisterFunc(&go_hook.HookConfig{}, main, unilogger.Default())
}

func main(_ *go_hook.HookInput) error {
	return nil
}
