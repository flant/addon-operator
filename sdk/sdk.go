package sdk

import (
	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
)

// Register is a method to define go hooks.
// return value is for trick with
//   var _ =
var Register = func(_ go_hook.GoHook) bool { return false }
var RegisterFunc = func(config *go_hook.HookConfig, reconcileFunc reconcileFunc) bool { return false }

var _ go_hook.GoHook = (*commonGoHook)(nil)

type reconcileFunc func(input *go_hook.HookInput) error

type commonGoHook struct {
	config        *go_hook.HookConfig
	reconcileFunc reconcileFunc
}

func newCommonGoHook(config *go_hook.HookConfig, reconcileFunc func(input *go_hook.HookInput) error) *commonGoHook {
	return &commonGoHook{config: config, reconcileFunc: reconcileFunc}
}

func (h *commonGoHook) Config() *go_hook.HookConfig {
	return h.config
}

func (h *commonGoHook) Run(input *go_hook.HookInput) error {
	return h.reconcileFunc(input)
}
