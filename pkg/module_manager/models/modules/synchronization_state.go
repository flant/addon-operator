package modules

import (
	"fmt"
	"sync"

	log "github.com/deckhouse/deckhouse/go_lib/log"

	"github.com/flant/addon-operator/pkg/task"
)

// kubernetesBindingSynchronizationState is a state of the single Synchronization task
// for one kubernetes binding.
type kubernetesBindingSynchronizationState struct {
	HookName    string
	BindingName string
	Queued      bool
	Done        bool
}

func (k *kubernetesBindingSynchronizationState) String() string {
	return fmt.Sprintf("queue=%v done=%v", k.Queued, k.Done)
}

// SynchronizationState stores state to track synchronization
// tasks for kubernetes bindings either for all global hooks
// or for module's hooks.
type SynchronizationState struct {
	state map[string]*kubernetesBindingSynchronizationState
	m     sync.RWMutex
}

func NewSynchronizationState() *SynchronizationState {
	return &SynchronizationState{
		state: make(map[string]*kubernetesBindingSynchronizationState),
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

// IsCompleted returns true if all states are in done status.
func (s *SynchronizationState) IsCompleted() bool {
	s.m.RLock()
	defer s.m.RUnlock()
	done := true
	for _, state := range s.state {
		if !state.Done {
			done = false
			log.Debugf("Synchronization isn't done for %s/%s", state.HookName, state.BindingName)
			break
		}
	}
	return done
}

func (s *SynchronizationState) QueuedForBinding(metadata task.HookMetadata) {
	s.m.Lock()
	defer s.m.Unlock()
	var state *kubernetesBindingSynchronizationState
	state, ok := s.state[metadata.KubernetesBindingId]
	if !ok {
		state = &kubernetesBindingSynchronizationState{
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
	var state *kubernetesBindingSynchronizationState
	state, ok := s.state[id]
	if !ok {
		state = &kubernetesBindingSynchronizationState{}
		s.state[id] = state
	}
	log.Debugf("Synchronization done for %s/%s", state.HookName, state.BindingName)
	state.Done = true
}

func (s *SynchronizationState) DebugDumpState(logEntry *log.Logger) {
	s.m.RLock()
	defer s.m.RUnlock()
	for id, state := range s.state {
		logEntry.Debugf("%s/%s: queued=%v done=%v id=%s", state.HookName, state.BindingName, state.Queued, state.Done, id)
	}
}
