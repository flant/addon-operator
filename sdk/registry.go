package sdk

import (
	"regexp"
	"runtime"
	"sync"

	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
)

// /path/.../global-hooks/a/b/c/Hook-name.go
// $1 - Hook path
// $3 - Hook name
var globalRe = regexp.MustCompile(`/global[\/\-]hooks/(([^/]+/)*([^/]+))$`)

// /path/.../modules/module-name/hooks/a/b/c/Hook-name.go
// $1 - Hook path
// $2 - module name
// $4 - Hook name
var moduleRe = regexp.MustCompile(`/modules/(([^/]+)/hooks/([^/]+/)*([^/]+))$`)

// TODO: This regexp should be changed. We shouldn't force users to name modules with a number prefix.
var moduleNameRe = regexp.MustCompile(`^[0-9][0-9][0-9]-(.*)$`)

var RegisterFunc = func(config *go_hook.HookConfig, reconcileFunc reconcileFunc) bool {
	Registry().Add(newCommonGoHook(config, reconcileFunc))
	return true
}

type HookWithMetadata struct {
	Hook     go_hook.GoHook
	Metadata *go_hook.HookMetadata
}

type HookRegistry struct {
	hooks []HookWithMetadata
	m     sync.Mutex
}

var instance *HookRegistry
var once sync.Once

func Registry() *HookRegistry {
	once.Do(func() {
		instance = new(HookRegistry)
	})
	return instance
}

func (h *HookRegistry) Hooks() []HookWithMetadata {
	return h.hooks
}

func (h *HookRegistry) Add(hook go_hook.GoHook) {
	h.m.Lock()
	defer h.m.Unlock()

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
			hookMeta.Name = matches[3]
			hookMeta.Path = matches[1]
			break
		}

		matches = moduleRe.FindStringSubmatch(frame.File)
		if matches != nil {
			hookMeta.Module = true
			hookMeta.Name = matches[4]
			hookMeta.Path = matches[1]
			modNameMatches := moduleNameRe.FindStringSubmatch(matches[2])
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

	h.hooks = append(h.hooks, HookWithMetadata{
		Hook:     hook,
		Metadata: hookMeta,
	})
}
