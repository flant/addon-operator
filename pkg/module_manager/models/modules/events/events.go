package events

// ModuleEventType type of the event
type ModuleEventType int

const (
	ModuleRegistered ModuleEventType = iota
	ModuleEnabled
	ModuleDisabled

	FirstConvergeDone
)

// ModuleEvent event model for hooks
type ModuleEvent struct {
	ModuleName string
	EventType  ModuleEventType
}
