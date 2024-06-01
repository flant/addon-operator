package dynamically_enabled

import (
	"context"
	"sync"

	log "github.com/sirupsen/logrus"

	"github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders"
	"github.com/flant/addon-operator/pkg/module_manager/scheduler/node"
)

const (
	Name extenders.ExtenderName = "DynamicallyEnabled"
)

type Extender struct {
	notifyCh      chan extenders.ExtenderEvent
	l             sync.RWMutex
	modulesStatus map[string]bool
}

type DynamicExtenderEvent struct {
	DynamicExtenderUpdated bool
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
		status, found := e.modulesStatus[moduleName]
		if !found || (found && status != value) {
			e.modulesStatus[moduleName] = value
			e.sendNotify()
		}
	case "remove":
		if _, found := e.modulesStatus[moduleName]; found {
			delete(e.modulesStatus, moduleName)
			e.sendNotify()
		}
	default:
		log.Warnf("Unknown patch operation: %s", operation)
	}
	e.l.Unlock()
}

func (e *Extender) sendNotify() {
	e.notifyCh <- extenders.ExtenderEvent{
		ExtenderName: Name,
		EncapsulatedEvent: DynamicExtenderEvent{
			DynamicExtenderUpdated: true,
		},
	}
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
	return true
}

func (e *Extender) SetNotifyChannel(_ context.Context, ch chan extenders.ExtenderEvent) {
	e.notifyCh = ch
}

func (e *Extender) Order() {
}
