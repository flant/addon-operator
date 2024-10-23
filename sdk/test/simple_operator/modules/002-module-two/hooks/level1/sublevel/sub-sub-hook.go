package sublevel

import (
	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/sdk"
	"github.com/flant/shell-operator/pkg/unilogger"
)

// TODO: remove global logger?
var _ = sdk.RegisterFunc(&go_hook.HookConfig{}, main, unilogger.Default())

func main(_ *go_hook.HookInput) error {
	return nil
}
