package moduleset

import (
	"sort"
	"sync"

	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
)

type ModulesSet struct {
	lck          sync.RWMutex
	modules      map[string]*modules.BasicModule
	orderedNames []string
}

func (s *ModulesSet) Add(mods ...*modules.BasicModule) {
	if len(mods) == 0 {
		return
	}

	s.lck.Lock()
	defer s.lck.Unlock()

	if s.modules == nil {
		s.modules = make(map[string]*modules.BasicModule)
	}

	for _, module := range mods {
		// Invalidate ordered names cache.
		if _, ok := s.modules[module.GetName()]; ok {
			s.orderedNames = nil
		}
		s.modules[module.GetName()] = module
	}
}

func (s *ModulesSet) Get(name string) *modules.BasicModule {
	s.lck.RLock()
	defer s.lck.RUnlock()
	return s.modules[name]
}

func (s *ModulesSet) List() []*modules.BasicModule {
	s.lck.Lock()
	defer s.lck.Unlock()
	list := make([]*modules.BasicModule, 0, len(s.modules))
	for _, name := range s.namesInOrder() {
		list = append(list, s.modules[name])
	}
	return list
}

func (s *ModulesSet) Len() int {
	s.lck.RLock()
	defer s.lck.RUnlock()

	return len(s.modules)
}

func (s *ModulesSet) NamesInOrder() []string {
	s.lck.Lock()
	defer s.lck.Unlock()
	return s.namesInOrder()
}

func (s *ModulesSet) namesInOrder() []string {
	if s.orderedNames == nil {
		s.orderedNames = s.sortModuleNames()
	}
	return s.orderedNames
}

func (s *ModulesSet) Has(name string) bool {
	s.lck.RLock()
	defer s.lck.RUnlock()
	_, ok := s.modules[name]
	return ok
}

func (s *ModulesSet) sortModuleNames() []string {
	// Get modules array.
	mods := make([]*modules.BasicModule, 0, len(s.modules))
	for _, mod := range s.modules {
		mods = append(mods, mod)
	}
	// Sort by order and name
	sort.Slice(mods, func(i, j int) bool {
		if mods[i].GetOrder() != mods[j].GetOrder() {
			return mods[i].GetOrder() < mods[j].GetOrder()
		}
		return mods[i].GetName() < mods[j].GetName()
	})
	// return names array.
	names := make([]string, 0, len(s.modules))
	for _, mod := range mods {
		names = append(names, mod.GetName())
	}
	return names
}
