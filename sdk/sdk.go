package sdk

import (
	"regexp"
	"runtime"

	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
)

// Register is a method to define go hooks.
// return value is for trick with
//   var _ =
var Register = func(_ go_hook.GoHook) bool { return false }
var RegisterFunc = func(config *go_hook.HookConfig, reconcileFunc reconcileFunc) bool { return false }

var _ go_hook.GoHook = (*commonGoHook)(nil)

// /path/.../global-hooks/a/b/c/hook-name.go
// $1 - hook path
// $3 - hook name
var globalRe = regexp.MustCompile(`/global-hooks/(([^/]+/)*([^/]+))$`)

// /path/.../modules/module-name/hooks/a/b/c/hook-name.go
// $1 - hook path
// $2 - module name
// $4 - hook name
var moduleRe = regexp.MustCompile(`/modules/(([^/]+)/hooks/([^/]+/)*([^/]+))$`)

var moduleNameRe = regexp.MustCompile(`^[0-9][0-9][0-9]-(.*)$`)

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

func (*commonGoHook) Metadata() *go_hook.HookMetadata {
	hookMeta := &go_hook.HookMetadata{
		Name:       "",
		Path:       "",
		Global:     false,
		Module:     false,
		ModuleName: "",
	}

	_, f, _, _ := runtime.Caller(1)

	matches := globalRe.FindStringSubmatch(f)
	if matches != nil {
		hookMeta.Global = true
		hookMeta.Name = matches[3]
		hookMeta.Path = matches[1]
	} else {
		matches = moduleRe.FindStringSubmatch(f)
		if matches != nil {
			hookMeta.Module = true
			hookMeta.Name = matches[4]
			hookMeta.Path = matches[1]
			modNameMatches := moduleNameRe.FindStringSubmatch(matches[2])
			if modNameMatches != nil {
				hookMeta.ModuleName = modNameMatches[1]
			}
		}
	}

	return hookMeta
}

func (h *commonGoHook) Run(input *go_hook.HookInput) error {
	return h.reconcileFunc(input)
}
