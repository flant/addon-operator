package hooks

import (
	"fmt"
	"strings"

	addon_op_types "github.com/flant/addon-operator/pkg/hook/types"
	gohook "github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/pkg/module_manager/models/hooks/kind"
	shell_op_types "github.com/flant/shell-operator/pkg/hook/types"
)

// GlobalHook is a representation of the hook, which not belongs to any module
type GlobalHook struct {
	executableHook
	hookConfigLoader
	config *GlobalHookConfig
}

type executableHookWithLoad interface {
	executableHook
	hookConfigLoader
}

// NewGlobalHook constructs a new global hook
//
//	ex - is an executable hook instance (GoHook or ShellHook)
func NewGlobalHook(ex executableHookWithLoad) *GlobalHook {
	return &GlobalHook{
		executableHook:   ex,
		hookConfigLoader: ex,
		config:           &GlobalHookConfig{},
	}
}

// InitializeHookConfig initializes the global hook config
// for GoHook config is precompiled, so we just have to fetch it
// for ShellHook, that hook will be run with `--config` flag, returns and parses the config
func (h *GlobalHook) InitializeHookConfig() error {
	err := h.config.LoadAndValidateConfig(h.hookConfigLoader)
	if err != nil {
		return err
	}

	// Make HookController and GetConfigDescription work.
	h.executableHook.BackportHookConfig(&h.config.HookConfig)

	return nil
}

// GetHookConfig returns the global hook configuration
func (h *GlobalHook) GetHookConfig() *GlobalHookConfig {
	return h.config
}

// GetConfigDescription returns config description for debugging/logging
func (h *GlobalHook) GetConfigDescription() string {
	msgs := make([]string, 0)
	if h.config.BeforeAll != nil {
		msgs = append(msgs, fmt.Sprintf("beforeAll:%d", int64(h.config.BeforeAll.Order)))
	}
	if h.config.AfterAll != nil {
		msgs = append(msgs, fmt.Sprintf("afterAll:%d", int64(h.config.AfterAll.Order)))
	}
	msgs = append(msgs, h.executableHook.GetHookConfigDescription())
	return strings.Join(msgs, ", ")
}

// Order return float order number for bindings with order.
func (h *GlobalHook) Order(binding shell_op_types.BindingType) float64 {
	if h.config.HasBinding(binding) {
		switch binding {
		case addon_op_types.BeforeAll:
			return h.config.BeforeAll.Order
		case addon_op_types.AfterAll:
			return h.config.AfterAll.Order
		case shell_op_types.OnStartup:
			return h.config.OnStartup.Order
		}
	}
	return 0.0
}

// GetConfigVersion version on the config
func (h *GlobalHook) GetConfigVersion() string {
	return h.config.Version
}

// SynchronizationNeeded is true if there is binding with executeHookOnSynchronization.
func (h *GlobalHook) SynchronizationNeeded() bool {
	for _, kubeBinding := range h.config.OnKubernetesEvents {
		if kubeBinding.ExecuteHookOnSynchronization {
			return true
		}
	}
	return false
}

// GetGoHookInputSettings proxy method to extract GoHook config settings
func (h *GlobalHook) GetGoHookInputSettings() *gohook.HookConfigSettings {
	if h.GetKind() != kind.HookKindGo {
		return nil
	}

	gohook := h.executableHook.(*kind.GoHook)
	return gohook.GetConfig().Settings
}
