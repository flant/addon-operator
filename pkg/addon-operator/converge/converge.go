package converge

import (
	"sync/atomic"
	"time"

	"github.com/flant/addon-operator/pkg/hook/types"
	"github.com/flant/addon-operator/pkg/task"
	sh_task "github.com/flant/shell-operator/pkg/task"
)

type ConvergeState struct {
	Phase phase

	FirstRunPhase firstRunPhase
	FirstRunDoneC chan struct{}
	StartedAt     int64
	Activation    string
	CRDsEnsured   bool
}

type phase struct {
	value atomic.Value
}

func (p *phase) Load() ConvergePhase {
	if phase, ok := p.value.Load().(ConvergePhase); ok {
		return phase
	}

	return ""
}

func (p *phase) Store(v ConvergePhase) {
	p.value.Store(v)
}

type firstRunPhase struct {
	value atomic.Value
}

func (p *firstRunPhase) Load() firstConvergePhase {
	if phase, ok := p.value.Load().(firstConvergePhase); ok {
		return phase
	}

	return FirstNotStarted
}

func (p *firstRunPhase) Store(v firstConvergePhase) {
	p.value.Store(v)
}

type ConvergePhase string

const (
	StandBy                 ConvergePhase = "StandBy"
	RunBeforeAll            ConvergePhase = "RunBeforeAll"
	WaitBeforeAll           ConvergePhase = "WaitBeforeAll"
	WaitDeleteAndRunModules ConvergePhase = "WaitDeleteAndRunModules"
	WaitAfterAll            ConvergePhase = "WaitAfterAll"
)

type firstConvergePhase int

const (
	FirstNotStarted firstConvergePhase = iota
	FirstStarted
	FirstDone
)

func NewConvergeState() *ConvergeState {
	cs := &ConvergeState{
		FirstRunDoneC: make(chan struct{}),
	}
	cs.Phase.Store(StandBy)
	cs.FirstRunPhase.Store(FirstNotStarted)
	return cs
}

func (cs *ConvergeState) SetFirstRunPhase(ph firstConvergePhase) {
	cs.FirstRunPhase.Store(ph)
	if ph == FirstDone {
		close(cs.FirstRunDoneC)
	}
}

const ConvergeEventProp = "converge.event"

type ConvergeEvent string

const (
	// OperatorStartup is a first converge during startup.
	OperatorStartup ConvergeEvent = "OperatorStartup"
	// GlobalValuesChanged is a converge initiated by changing values in the global hook.
	GlobalValuesChanged ConvergeEvent = "GlobalValuesChanged"
	// ReloadAllModules is a converge queued to the main queue after the graph's state change
	ReloadAllModules ConvergeEvent = "ReloadAllModules"
)

func IsConvergeTask(t sh_task.Task) bool {
	taskType := t.GetType()
	hm := task.HookMetadataAccessor(t)

	switch taskType {
	case task.ModuleDelete, task.ConvergeModules, task.ModuleEnsureCRDs:
		return true
	case task.ModuleRun, task.ParallelModuleRun:
		return hm.IsReloadAll
	case task.GlobalHookRun:
		switch hm.BindingType {
		case types.BeforeAll, types.AfterAll:
			return true
		}
	case task.ModuleHookRun:
		if hm.IsSynchronization() {
			return true
		}
	}
	return false
}

func IsFirstConvergeTask(t sh_task.Task) bool {
	taskType := t.GetType()
	switch taskType {
	case task.ModulePurge, task.DiscoverHelmReleases, task.GlobalHookEnableKubernetesBindings, task.GlobalHookEnableScheduleBindings:
		return true
	}
	return false
}

func NewConvergeModulesTask(description string, convergeEvent ConvergeEvent, logLabels map[string]string) sh_task.Task {
	convergeTask := sh_task.NewTask(task.ConvergeModules).
		WithLogLabels(logLabels).
		WithQueueName("main").
		WithMetadata(task.HookMetadata{
			EventDescription: description,
		}).
		WithQueuedAt(time.Now())
	convergeTask.SetProp(ConvergeEventProp, convergeEvent)
	return convergeTask
}

func NewApplyKubeConfigValuesTask(description string, logLabels map[string]string, globalValuesChanged bool) sh_task.Task {
	convergeTask := sh_task.NewTask(task.ApplyKubeConfigValues).
		WithLogLabels(logLabels).
		WithQueueName("main").
		WithMetadata(task.HookMetadata{
			EventDescription:    description,
			GlobalValuesChanged: globalValuesChanged,
		}).
		WithQueuedAt(time.Now())
	return convergeTask
}
