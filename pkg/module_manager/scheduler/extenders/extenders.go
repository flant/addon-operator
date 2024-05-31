package extenders

import (
	"context"

	"github.com/flant/addon-operator/pkg/module_manager/scheduler/node"
)

type ExtenderEvent struct {
	ExtenderName      ExtenderName
	EncapsulatedEvent interface{}
}

type ExtenderName string

type Extender interface {
	// Name returns the extender's name
	Name() ExtenderName
	// Filter returns the result of applying the extender
	Filter(module node.ModuleInterface) (*bool, error)
	// Dump returns the extender's status of all modules
	Dump() map[string]bool

	// not implemented
	Order()
}

type NotificationExtender interface {
	// SetNotifyChannel set output channel for events, when module state could be changed during the runtime
	SetNotifyChannel(context.Context, chan ExtenderEvent)
}

// Hail to enabled scripts
type ResettableExtender interface {
	// Reset resets the extender's cache
	Reset()
}
