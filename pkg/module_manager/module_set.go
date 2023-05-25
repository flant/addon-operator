package module_manager

import (
	"sort"
	"sync"
)

type ModuleSet struct {
	modules      map[string]*Module
	orderedNames []string
	lck          sync.RWMutex
}

func (s *ModuleSet) Add(modules ...*Module) {
	if len(modules) == 0 {
		return
	}

	s.lck.Lock()
	defer s.lck.Unlock()

	if s.modules == nil {
		s.modules = make(map[string]*Module)
	}

	for _, module := range modules {
		// Invalidate ordered names cache.
		if _, ok := s.modules[module.Name]; ok {
			s.orderedNames = nil
		}
		s.modules[module.Name] = module
	}
}

func (s *ModuleSet) Get(name string) *Module {
	s.lck.RLock()
	defer s.lck.RUnlock()
	return s.modules[name]
}

func (s *ModuleSet) List() []*Module {
	s.lck.Lock()
	defer s.lck.Unlock()
	list := make([]*Module, 0, len(s.modules))
	for _, name := range s.namesInOrder() {
		list = append(list, s.modules[name])
	}
	return list
}

func (s *ModuleSet) Len() int {
	s.lck.RLock()
	defer s.lck.RUnlock()

	return len(s.modules)
}

func (s *ModuleSet) NamesInOrder() []string {
	s.lck.Lock()
	defer s.lck.Unlock()
	return s.namesInOrder()
}

func (s *ModuleSet) namesInOrder() []string {
	if s.orderedNames == nil {
		s.orderedNames = sortModuleNames(s.modules)
	}
	return s.orderedNames
}

func (s *ModuleSet) Has(name string) bool {
	s.lck.RLock()
	defer s.lck.RUnlock()
	_, ok := s.modules[name]
	return ok
}

func sortModuleNames(modules map[string]*Module) []string {
	// Get modules array.
	mods := make([]*Module, 0, len(modules))
	for _, mod := range modules {
		mods = append(mods, mod)
	}
	// Sort by order and name
	sort.Slice(mods, func(i, j int) bool {
		if mods[i].Order != mods[j].Order {
			return mods[i].Order < mods[j].Order
		}
		return mods[i].Name < mods[j].Name
	})
	// return names array.
	names := make([]string, 0, len(modules))
	for _, mod := range mods {
		names = append(names, mod.Name)
	}
	return names
}
