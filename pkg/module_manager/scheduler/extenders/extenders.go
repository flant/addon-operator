package extenders

import (
	"context"
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
	Filter(moduleName string) (*bool, error)

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
