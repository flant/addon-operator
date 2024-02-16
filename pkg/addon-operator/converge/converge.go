package converge

import (
	"time"

	"github.com/flant/addon-operator/pkg/hook/types"
	"github.com/flant/addon-operator/pkg/task"
	sh_task "github.com/flant/shell-operator/pkg/task"
)

type ConvergeState struct {
	Phase         ConvergePhase
	FirstRunPhase firstConvergePhase
	FirstRunDoneC chan struct{}
	StartedAt     int64
	Activation    string
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
	return &ConvergeState{
		Phase:         StandBy,
		FirstRunPhase: FirstNotStarted,
		FirstRunDoneC: make(chan struct{}),
	}
}

func (cs *ConvergeState) SetFirstRunPhase(ph firstConvergePhase) {
	cs.FirstRunPhase = ph
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
	// KubeConfigChanged is a converge started after changing ConfigMap.
	KubeConfigChanged ConvergeEvent = "KubeConfigChanged"
	// ReloadAllModules is a converge queued to the
	ReloadAllModules ConvergeEvent = "ReloadAllModules"
)

func IsConvergeTask(t sh_task.Task) bool {
	taskType := t.GetType()
	hm := task.HookMetadataAccessor(t)

	switch taskType {
	case task.ModuleDelete, task.ConvergeModules:
		return true
	case task.ModuleRun:
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
