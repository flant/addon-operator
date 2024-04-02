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
	// returns the extender's name
	Name() ExtenderName
	// returns the result of applying the extender
	Filter(module node.ModuleInterface) (*bool, error)
	// returns true if the extender's purpose is to check if a module should be disabled for some reason
	IsShutter() bool
	// dumps the extender's status for the sake of debug
	Dump() map[string]bool
	// returns true if the extender has a notification channel
	IsNotifier() bool
	// sets the extender's notification channel and starts its loop
	SetNotifyChannel(context.Context, chan ExtenderEvent)
	// resets the exender's cache
	Reset()

	// not implemented
	Order()
}

const (
	StaticExtender             ExtenderName = "Static"
	DynamicallyEnabledExtender ExtenderName = "DynamicallyEnabled"
	KubeConfigExtender         ExtenderName = "KubeConfig"
	ScriptEnabledExtender      ExtenderName = "ScriptEnabled"
)
