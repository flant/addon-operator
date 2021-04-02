package global_go_hook

import (
	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/sdk"
)

var _ = sdk.Register(&Simple{})

type Simple struct {
}

func (s *Simple) Metadata() *go_hook.HookMetadata {
	return &go_hook.HookMetadata{
		Name:   "simple",
		Path:   "global-hooks/simple",
		Global: true,
	}
}

func (s *Simple) Config() *go_hook.HookConfig {
	return &go_hook.HookConfig{
		OnStartup: &go_hook.OrderedConfig{Order: 1},
	}
}

func (s *Simple) Run(input *go_hook.HookInput) error {
	return nil
}
