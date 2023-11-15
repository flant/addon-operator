package modules

import (
	"sort"
	"sync"

	"github.com/flant/addon-operator/pkg/module_manager/models/hooks"
	sh_op_types "github.com/flant/shell-operator/pkg/hook/types"
)

// HooksStorage keep module hooks in order
type HooksStorage struct {
	registered bool
	lock       sync.RWMutex
	byBinding  map[sh_op_types.BindingType][]*hooks.ModuleHook
	byName     map[string]*hooks.ModuleHook
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
	hs.byName[hName] = hk
	for _, binding := range hk.GetHookConfig().Bindings() {
		hs.byBinding[binding] = append(hs.byBinding[binding], hk)
	}
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
		sort.Slice(res, func(i, j int) bool {
			return res[i].Order(t) < res[j].Order(t)
		})

		return res
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
}
