package events

type ModuleEventType int

const (
	ModuleRegistered ModuleEventType = iota
	ModulePurged
	ModuleEnabled
	ModuleDisabled
)

type ModuleEvent struct {
	ModuleName string
	EventType  ModuleEventType
}
