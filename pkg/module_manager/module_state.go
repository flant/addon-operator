package module_manager

type ModuleRunPhase string

const (
	// Startup - module is just enabled.
	Startup ModuleRunPhase = "Startup"
	// OnStartupDone - onStartup hooks are done.
	OnStartupDone ModuleRunPhase = "OnStartupDone"
	// QueueSynchronizationTasks - should queue Synchronization tasks.
	QueueSynchronizationTasks ModuleRunPhase = "QueueSynchronizationTasks"
	// WaitForSynchronization - some Synchronization tasks are in queues, should wait for them to finish.
	WaitForSynchronization ModuleRunPhase = "WaitForSynchronization"
	// EnableScheduleBindings - enable schedule binding after Synchronization.
	EnableScheduleBindings ModuleRunPhase = "EnableScheduleBindings"
	// CanRunHelm - module is ready to run its Helm chart.
	CanRunHelm ModuleRunPhase = "CanRunHelm"
)

type ModuleState struct {
	Enabled              bool
	Phase                ModuleRunPhase
	synchronizationState *SynchronizationState
}

func NewModuleState() *ModuleState {
	return &ModuleState{
		Phase:                Startup,
		synchronizationState: NewSynchronizationState(),
	}
}

func (s *ModuleState) Synchronization() *SynchronizationState {
	return s.synchronizationState
}
