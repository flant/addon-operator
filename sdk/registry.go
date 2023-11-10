package sdk

import (
	"github.com/flant/addon-operator/pkg/module_manager/models/hooks/kind"
	"regexp"
	"runtime"
	"sync"

	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
)

const bindingsPanicMsg = "OnStartup hook always has binding context without Kubernetes snapshots. To prevent logic errors, don't use OnStartup and Kubernetes bindings in the same Go hook configuration."

// /path/.../global-hooks/a/b/c/Hook-name.go
// $1 - Hook path for sorting (/global/hooks/subdir/v1-hook-onstartup.go)
// $2 - Hook name for identification (subdir/v1-hook-onstartup.go)
var globalRe = regexp.MustCompile(`(/global[\/\-]hooks/(([^/]+/)*([^/]+)))$`)

// /path/.../modules/module-name/hooks/a/b/c/Hook-name.go
// $1 - Hook path for sorting (/modules/002-helm-and-hooks/hooks/subdir/some_hook)
// $2 - Hook name for identification (002-helm-and-hooks/hooks/subdir/some_hook)
// $3 - Path element with module name (002-helm-and-hooks)
var moduleRe = regexp.MustCompile(`(/modules/(([^/]+)/hooks/([^/]+/)*([^/]+)))$`)

// TODO: This regexp should be changed. We shouldn't force users to name modules with a number prefix.
var moduleNameRe = regexp.MustCompile(`^[0-9][0-9][0-9]-(.*)$`)

var RegisterFunc = func(config *go_hook.HookConfig, reconcileFunc kind.ReconcileFunc) bool {
	Registry().Add(kind.NewGoHook(config, reconcileFunc))
	return true
}

//type HookWithMetadata struct {
//	Hook     go_hook.GoHook
//	Metadata *go_hook.HookMetadata
//}
//
//func (hwm HookWithMetadata) GetPath() string {
//	return hwm.Metadata.Path
//}
//
//func (hwm HookWithMetadata) GetName() string {
//	return hwm.Metadata.Name
//}
//
//func (hwm HookWithMetadata) GetType() string {
//	return "go"
//}

type HookRegistry struct {
	m            sync.Mutex
	globalHooks  []*kind.GoHook
	modulesHooks map[string][]*kind.GoHook // [<module-name>]<hook>
}

var (
	instance *HookRegistry
	once     sync.Once
)

func Registry() *HookRegistry {
	once.Do(func() {
		instance = &HookRegistry{
			globalHooks:  make([]*kind.GoHook, 0),
			modulesHooks: make(map[string][]*kind.GoHook),
		}
	})
	return instance
}

//func (h *HookRegistry) Hooks() []HookWithMetadata {
//	return h.hooks
//}

func (h *HookRegistry) GetModuleHooks(moduleName string) []*kind.GoHook {
	return h.modulesHooks[moduleName]
}

func (h *HookRegistry) GetGlobalHooks() []*kind.GoHook {
	return h.globalHooks
}

// Hooks returns all (module and global) hooks
// Deprecated: method exists for backward compatibility, use GetGlobalHooks or GetModuleHooks instead
func (h *HookRegistry) Hooks() []*kind.GoHook {
	res := make([]*kind.GoHook, 0, len(h.globalHooks)+len(h.modulesHooks))

	for _, hooks := range h.modulesHooks {
		res = append(res, hooks...)
	}

	for _, ghook := range h.globalHooks {
		res = append(res, ghook)
	}

	return res
}

func (h *HookRegistry) Add(hook *kind.GoHook) {
	config := hook.GetConfig()
	if config.OnStartup != nil && len(config.Kubernetes) > 0 {
		panic(bindingsPanicMsg)
	}

	hookMeta := &go_hook.HookMetadata{}

	pc := make([]uintptr, 50)
	n := runtime.Callers(0, pc)
	if n == 0 {
		panic("runtime.Callers is empty")
	}
	pc = pc[:n] // pass only valid pcs to runtime.CallersFrames
	frames := runtime.CallersFrames(pc)

	for {
		frame, more := frames.Next()
		matches := globalRe.FindStringSubmatch(frame.File)
		if matches != nil {
			hookMeta.Global = true
			hookMeta.Name = matches[2]
			hookMeta.Path = matches[1]
			break
		}

		matches = moduleRe.FindStringSubmatch(frame.File)
		if matches != nil {
			hookMeta.Module = true
			hookMeta.Name = matches[2]
			hookMeta.Path = matches[1]
			modNameMatches := moduleNameRe.FindStringSubmatch(matches[3])
			if modNameMatches != nil {
				hookMeta.ModuleName = modNameMatches[1]
			}
			break
		}

		if !more {
			break
		}
	}

	if len(hookMeta.Name) == 0 {
		panic("cannot extract metadata from GoHook")
	}

	hook.AddMetadata(hookMeta)

	h.m.Lock()
	defer h.m.Unlock()
	if hookMeta.Global {
		h.globalHooks = append(h.globalHooks, hook)
	} else if hookMeta.Module {
		h.modulesHooks[hookMeta.ModuleName] = append(h.modulesHooks[hookMeta.ModuleName], hook)
	} else {
		panic("neither module nor global hook. Who are you?")
	}
}
