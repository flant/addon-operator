package modules

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/deckhouse/deckhouse/pkg/log"

	"github.com/flant/addon-operator/pkg"
)

type TaskMetadata interface {
	GetKubernetesBindingID() string
	GetHookName() string
	GetBinding() string
}

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
			log.Debug("Synchronization isn't done",
				slog.String("hook", state.HookName),
				slog.String(pkg.LogKeyBinding, state.BindingName))
			break
		}
	}
	return done
}

// QueuedForBinding marks a Kubernetes binding as queued for synchronization
func (s *SynchronizationState) QueuedForBinding(metadata TaskMetadata) {
	s.m.Lock()
	defer s.m.Unlock()
	var state *kubernetesBindingSynchronizationState
	state, ok := s.state[metadata.GetKubernetesBindingID()]
	if !ok {
		state = &kubernetesBindingSynchronizationState{
			HookName:    metadata.GetHookName(),
			BindingName: metadata.GetBinding(),
		}
		s.state[metadata.GetKubernetesBindingID()] = state
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
	log.Debug("Synchronization done",
		slog.String("hook", state.HookName),
		slog.String(pkg.LogKeyBinding, state.BindingName))
	state.Done = true
}

func (s *SynchronizationState) DebugDumpState(logEntry *log.Logger) {
	s.m.RLock()
	defer s.m.RUnlock()
	for id, state := range s.state {
		logEntry.Debug(fmt.Sprintf("%s/%s: queued=%v done=%v id=%s", state.HookName, state.BindingName, state.Queued, state.Done, id))
	}
}
