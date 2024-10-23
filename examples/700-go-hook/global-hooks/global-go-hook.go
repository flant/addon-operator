package global_hooks

import (
	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/sdk"
	"github.com/flant/shell-operator/pkg/unilogger"
)

// TODO: remove global logger?
var _ = sdk.RegisterFunc(&go_hook.HookConfig{
	OnStartup: &go_hook.OrderedConfig{Order: 10},
}, handler, unilogger.Default())

func handler(input *go_hook.HookInput) error {
	input.LogEntry.Infof("Start Global Go hook")
	return nil
}
