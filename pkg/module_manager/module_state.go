package module_manager

import (
	"fmt"
	"sync"
)

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
	LastModuleErr        error
	hookErrors           map[string]error
	hookErrorsLock       sync.Mutex
	synchronizationState *SynchronizationState
}

func NewModuleState() *ModuleState {
	return &ModuleState{
		Phase:                Startup,
		hookErrors:           make(map[string]error),
		synchronizationState: NewSynchronizationState(),
	}
}

func (s *ModuleState) Synchronization() *SynchronizationState {
	return s.synchronizationState
}

// SetLastHookErr saves error from hook.
func (s *ModuleState) SetLastHookErr(hookName string, err error) {
	s.hookErrorsLock.Lock()
	defer s.hookErrorsLock.Unlock()

	s.hookErrors[hookName] = err
}

// GetLastHookErr returns hook error.
func (s *ModuleState) GetLastHookErr() error {
	s.hookErrorsLock.Lock()
	defer s.hookErrorsLock.Unlock()

	for name, err := range s.hookErrors {
		if err != nil {
			return fmt.Errorf("%s: %v", name, err)
		}
	}

	return nil
}
