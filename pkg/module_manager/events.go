package module_manager

type ModuleEventType int

const (
	ModuleRegistered ModuleEventType = iota
	ModuleEnabled
	ModuleDisabled
)

type ModuleEvent struct {
	ModuleName string
	EventType  ModuleEventType
}
