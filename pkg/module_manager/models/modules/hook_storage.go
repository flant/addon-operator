package modules

import (
	hooks2 "github.com/flant/addon-operator/pkg/module_manager/models/hooks"
	sh_op_types "github.com/flant/shell-operator/pkg/hook/types"
	"sort"
)

type HooksStorage struct {
	registered bool
	byBinding  map[sh_op_types.BindingType][]*hooks2.ModuleHook
	byName     map[string]*hooks2.ModuleHook
}

func newHooksStorage() *HooksStorage {
	return &HooksStorage{
		registered: false,
		byBinding:  make(map[sh_op_types.BindingType][]*hooks2.ModuleHook),
		byName:     make(map[string]*hooks2.ModuleHook),
	}
}

func (hs *HooksStorage) AddHook(hk *hooks2.ModuleHook) {
	hName := hk.GetName()
	hs.byName[hName] = hk
	for _, binding := range hk.GetHookConfig().Bindings() {
		hs.byBinding[binding] = append(hs.byBinding[binding], hk)
	}
}

func (hs *HooksStorage) getHooks(bt ...sh_op_types.BindingType) []*hooks2.ModuleHook {
	if len(bt) > 0 {
		t := bt[0]
		res, ok := hs.byBinding[t]
		if !ok {
			return []*hooks2.ModuleHook{}
		}
		sort.Slice(res, func(i, j int) bool {
			return res[i].Order(t) < res[j].Order(t)
		})

		return res
	}

	// return all hooks
	res := make([]*hooks2.ModuleHook, 0, len(hs.byName))
	for _, h := range hs.byName {
		res = append(res, h)
	}

	sort.Slice(res, func(i, j int) bool {
		return res[i].GetName() < res[j].GetName()
	})

	return res
}

func (hs *HooksStorage) getHookByName(name string) *hooks2.ModuleHook {
	return hs.byName[name]
}

func (hs *HooksStorage) clean() {
	hs.byBinding = make(map[sh_op_types.BindingType][]*hooks2.ModuleHook)
	hs.byName = make(map[string]*hooks2.ModuleHook)
	hs.registered = false
}
