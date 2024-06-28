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
	Filter(moduleName string, logLabels map[string]string) (*bool, error)
}

type NotificationExtender interface {
	// SetNotifyChannel sets output channel for an extender's events, to notify when module state could be changed during the runtime
	SetNotifyChannel(context.Context, chan ExtenderEvent)
}

// Hail to enabled scripts
type ResettableExtender interface {
	// Reset resets the extender's cache
	Reset()
}

// Type of extenders that can only disable an enabled module if some requirement isn't met.
// By design, it makes sense to run terminators in the end of filtering because terminators can't be overridden by other extenders.
// For example, enabled scripts extender.
type TerminatingExtender interface {
	// Just a signature to match extenders
	IsTerminator()
}
