package registry

import (
	"sync"

	. "github.com/flant/addon-operator/sdk"
)

var _ = initRegistry()

func initRegistry() bool {
	Register = func(h GoHook) bool {
		Registry().Add(h)
		return true
	}
	return true
}

type HookRegistry interface {
	Hooks() []GoHook
	Add(hook GoHook)
}

type hookRegistry struct {
	hooks []GoHook
	m     sync.Mutex
}

var instance *hookRegistry
var once sync.Once

func Registry() HookRegistry {
	once.Do(func() {
		instance = new(hookRegistry)
	})
	return instance
}

func NewRegistry() HookRegistry {
	return new(hookRegistry)
}

func (h *hookRegistry) Hooks() []GoHook {
	return h.hooks
}

func (h *hookRegistry) Add(hook GoHook) {
	h.m.Lock()
	defer h.m.Unlock()
	h.hooks = append(h.hooks, hook)
}
