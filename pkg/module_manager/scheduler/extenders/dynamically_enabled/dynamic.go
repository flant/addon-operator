package dynamically_enabled

import (
	"context"
	"sync"

	log "github.com/deckhouse/deckhouse/go_lib/log"
	"github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders"
)

const (
	Name extenders.ExtenderName = "DynamicallyEnabled"
)

type Extender struct {
	notifyCh      chan extenders.ExtenderEvent
	l             sync.RWMutex
	modulesStatus map[string]bool
}

type DynamicExtenderEvent struct{}

func NewExtender() *Extender {
	e := &Extender{
		modulesStatus: make(map[string]bool),
	}
	return e
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
	if e.notifyCh != nil {
		e.notifyCh <- extenders.ExtenderEvent{
			ExtenderName:      Name,
			EncapsulatedEvent: DynamicExtenderEvent{},
		}
	}
}

func (e *Extender) Name() extenders.ExtenderName {
	return Name
}

func (e *Extender) Filter(moduleName string, _ map[string]string) (*bool, error) {
	e.l.RLock()
	defer e.l.RUnlock()

	if val, found := e.modulesStatus[moduleName]; found {
		return &val, nil
	}

	return nil, nil
}

func (e *Extender) IsTerminator() bool {
	return false
}

func (e *Extender) SetNotifyChannel(_ context.Context, ch chan extenders.ExtenderEvent) {
	e.notifyCh = ch
}
