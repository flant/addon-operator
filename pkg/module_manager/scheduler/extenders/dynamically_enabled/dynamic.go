package dynamically_enabled

import (
	"sync"

	log "github.com/sirupsen/logrus"

	"github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders"
	"github.com/flant/addon-operator/pkg/module_manager/scheduler/node"
)

const (
	Name extenders.ExtenderName = "DynamicallyEnabled"
)

type Extender struct {
	l             sync.RWMutex
	modulesStatus map[string]bool
}

func NewExtender() *Extender {
	e := &Extender{
		modulesStatus: make(map[string]bool),
	}
	return e
}

func (e *Extender) Dump() map[string]bool {
	e.l.Lock()
	defer e.l.Unlock()
	return e.modulesStatus
}

func (e *Extender) UpdateStatus(moduleName, operation string, value bool) {
	e.l.Lock()
	switch operation {
	case "add":
		e.modulesStatus[moduleName] = value
	case "remove":
		delete(e.modulesStatus, moduleName)
	default:
		log.Warnf("Unknown patch operation: %s", operation)
	}
	e.l.Unlock()
}

func (e *Extender) Name() extenders.ExtenderName {
	return Name
}

func (e *Extender) Filter(module node.ModuleInterface) (*bool, error) {
	e.l.RLock()
	defer e.l.RUnlock()

	if val, found := e.modulesStatus[module.GetName()]; found {
		return &val, nil
	}

	return nil, nil
}

func (e *Extender) IsNotifier() bool {
	return false
}

func (e *Extender) Order() {
}
