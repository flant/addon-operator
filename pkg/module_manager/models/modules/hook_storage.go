package modules

import (
	"sort"
	"sync"

	"github.com/flant/addon-operator/pkg/module_manager/models/hooks"
	sh_op_types "github.com/flant/shell-operator/pkg/hook/types"
)

// HooksStorage keep module hooks in order
type HooksStorage struct {
	registered       bool
	controllersReady bool
	// registrationMu serializes whole-module hook registration. Separate from
	// lock: registration execs hook binaries and must not hold the index lock
	// across that.
	registrationMu sync.Mutex
	lock           sync.RWMutex
	byBinding      map[sh_op_types.BindingType][]*hooks.ModuleHook
	byName         map[string]*hooks.ModuleHook
}

func newHooksStorage() *HooksStorage {
	return &HooksStorage{
		registered: false,
		byBinding:  make(map[sh_op_types.BindingType][]*hooks.ModuleHook),
		byName:     make(map[string]*hooks.ModuleHook),
	}
}

func (hs *HooksStorage) AddHook(hk *hooks.ModuleHook) {
	hs.lock.Lock()
	defer hs.lock.Unlock()

	hName := hk.GetName()

	// Re-registration (e.g. ModuleRun retry after a transient failure) must
	// replace the previous object, not accumulate duplicates: a stale entry
	// keeps a nil HookController and crashes the kube-events dispatcher.
	if old, ok := hs.byName[hName]; ok {
		hs.removeHookFromBindings(old)
	}

	hs.byName[hName] = hk
	for _, binding := range hk.GetHookConfig().Bindings() {
		hs.byBinding[binding] = append(hs.byBinding[binding], hk)
	}
}

// removeHookFromBindings deletes all byBinding entries pointing to the given
// hook. Call under hs.lock.
func (hs *HooksStorage) removeHookFromBindings(hk *hooks.ModuleHook) {
	for binding, hks := range hs.byBinding {
		filtered := make([]*hooks.ModuleHook, 0, len(hks))
		for _, h := range hks {
			if h != hk {
				filtered = append(filtered, h)
			}
		}

		if len(filtered) == 0 {
			delete(hs.byBinding, binding)
		} else {
			hs.byBinding[binding] = filtered
		}
	}
}

func (hs *HooksStorage) isRegistered() bool {
	hs.lock.RLock()
	defer hs.lock.RUnlock()

	return hs.registered
}

func (hs *HooksStorage) setRegistered() {
	hs.lock.Lock()
	defer hs.lock.Unlock()

	hs.registered = true
}

func (hs *HooksStorage) isControllersReady() bool {
	hs.lock.RLock()
	defer hs.lock.RUnlock()

	return hs.controllersReady
}

func (hs *HooksStorage) setControllersReady() {
	hs.lock.Lock()
	defer hs.lock.Unlock()

	hs.controllersReady = true
}

func (hs *HooksStorage) getHooks(bt ...sh_op_types.BindingType) []*hooks.ModuleHook {
	hs.lock.RLock()
	defer hs.lock.RUnlock()

	if len(bt) > 0 {
		t := bt[0]

		res, ok := hs.byBinding[t]
		if !ok {
			return []*hooks.ModuleHook{}
		}

		// Sort a copy: the shared slice must not be mutated under RLock, and
		// callers must not observe later index updates through the returned
		// header.
		out := make([]*hooks.ModuleHook, len(res))
		copy(out, res)

		sort.Slice(out, func(i, j int) bool {
			oi, oj := out[i].Order(t), out[j].Order(t)
			if oi != oj {
				return oi < oj
			}

			return out[i].GetName() < out[j].GetName()
		})

		return out
	}

	// return all hooks
	res := make([]*hooks.ModuleHook, 0, len(hs.byName))
	for _, h := range hs.byName {
		res = append(res, h)
	}

	sort.Slice(res, func(i, j int) bool {
		return res[i].GetName() < res[j].GetName()
	})

	return res
}

func (hs *HooksStorage) getHookByName(name string) *hooks.ModuleHook {
	hs.lock.RLock()
	defer hs.lock.RUnlock()

	return hs.byName[name]
}

func (hs *HooksStorage) clean() {
	hs.lock.Lock()
	defer hs.lock.Unlock()

	hs.byBinding = make(map[sh_op_types.BindingType][]*hooks.ModuleHook)
	hs.byName = make(map[string]*hooks.ModuleHook)
	hs.registered = false
	hs.controllersReady = false
}
