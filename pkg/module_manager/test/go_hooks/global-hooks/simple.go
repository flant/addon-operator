package global_hooks

import (
	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/sdk"
	"github.com/flant/shell-operator/pkg/metric_storage/operation"
)

var _ = sdk.Register(&Simple{})

type Simple struct {
}

func (s *Simple) Config() *go_hook.HookConfig {
	return &go_hook.HookConfig{
		OnAfterAll: &go_hook.OrderedConfig{Order: 5},
	}
}

func (s *Simple) Run(input *go_hook.HookInput) error {
	input.Values.Set("test", "test")
	*input.Metrics = append(*input.Metrics, operation.MetricOperation{Name: "test"})

	return nil
}
