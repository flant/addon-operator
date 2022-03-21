package module_manager

import (
	log "github.com/sirupsen/logrus"
	"sync"

	"github.com/flant/addon-operator/pkg/task"
)

// KubernetesBindingSynchronizationState is a state of the single Synchronization task.
type KubernetesBindingSynchronizationState struct {
	HookName    string
	BindingName string
	Queued      bool
	Done        bool
}

// SynchronizationState can be used to track synchronization tasks for global or for module.
type SynchronizationState struct {
	state map[string]*KubernetesBindingSynchronizationState
	m     sync.RWMutex
}

func NewSynchronizationState() *SynchronizationState {
	return &SynchronizationState{
		state: make(map[string]*KubernetesBindingSynchronizationState),
	}
}

func (s *SynchronizationState) HasQueued() bool {
	s.m.RLock()
	defer s.m.RUnlock()
	queued := false
	for _, state := range s.state {
		if state.Queued {
			queued = true
			break
		}
	}
	return queued
}

// IsComplete returns true if all states are in done status.
func (s *SynchronizationState) IsComplete() bool {
	s.m.RLock()
	defer s.m.RUnlock()
	done := true
	for _, state := range s.state {
		if !state.Done {
			done = false
		}
	}
	return done
}

func (s *SynchronizationState) QueuedForBinding(metadata task.HookMetadata) {
	s.m.Lock()
	defer s.m.Unlock()
	var state *KubernetesBindingSynchronizationState
	state, ok := s.state[metadata.KubernetesBindingId]
	if !ok {
		state = &KubernetesBindingSynchronizationState{
			HookName:    metadata.HookName,
			BindingName: metadata.Binding,
		}
		s.state[metadata.KubernetesBindingId] = state
	}
	state.Queued = true
}

func (s *SynchronizationState) DoneForBinding(id string) {
	s.m.Lock()
	defer s.m.Unlock()
	var state *KubernetesBindingSynchronizationState
	state, ok := s.state[id]
	if !ok {
		state = &KubernetesBindingSynchronizationState{}
		s.state[id] = state
	}
	state.Done = true
}

func (s *SynchronizationState) DebugDumpState(logEntry *log.Entry) {
	s.m.RLock()
	defer s.m.RUnlock()
	for id, state := range s.state {
		logEntry.Debugf("%s/%s: queued=%v done=%v id=%s", state.HookName, state.BindingName, state.Queued, state.Done, id)
	}
}
