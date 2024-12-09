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
	// IsTerminator marks extender that can only disable an enabled module if some requirement isn't met.
	// By design, terminators can't be overridden by other extenders.
	IsTerminator() bool
}

type NotificationExtender interface {
	// SetNotifyChannel sets output channel for an extender's events, to notify when module state could be changed during the runtime
	SetNotifyChannel(context.Context, chan ExtenderEvent)
}

type TopologicalExtender interface {
	// GetTopologicalHints returns the list of vertices that should be connected to the specified vertex
	GetTopologicalHints(string) []string
}

// Hail to enabled scripts
type StatefulExtender interface {
	// SetModulesStateHelper sets a helper function to get the list of enabled modules according to the latest vertex state buffer
	SetModulesStateHelper(func() []string)
}
