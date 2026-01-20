package task

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/deckhouse/deckhouse/pkg/log"

	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
	bindingcontext "github.com/flant/shell-operator/pkg/hook/binding_context"
	"github.com/flant/shell-operator/pkg/hook/task_metadata"
	"github.com/flant/shell-operator/pkg/hook/types"
	"github.com/flant/shell-operator/pkg/task"
)

// HookMetadata is a metadata for addon-operator tasks
type HookMetadata struct {
	EventDescription    string // event name for informative queue dump
	HookName            string
	ModuleName          string
	ParallelRunMetadata *ParallelRunMetadata
	Binding             string // binding name from configuration
	BindingType         types.BindingType
	BindingContext      []bindingcontext.BindingContext
	AllowFailure        bool // Task considered as 'ok' if hook failed. False by default. Can be true for some schedule hooks.
	Critical            bool

	DoModuleStartup bool // Execute onStartup and kubernetes@Synchronization hooks for module
	IsReloadAll     bool // ModuleRun task is a part of 'Reload all modules' process.

	ValuesChecksum           string // checksum of global values before first afterAll hook execution
	GlobalValuesChanged      bool   // global values changed flag
	LastAfterAllHook         bool   // true if task is a last afterAll hook in sequence
	ReloadAllOnValuesChanges bool   // whether to run DiscoverModules process if hook change global values

	KubernetesBindingId      string   // Unique id for kubernetes bindings
	WaitForSynchronization   bool     // kubernetes.Synchronization task should be waited
	MonitorIDs               []string // an array of monitor IDs to unlock Kubernetes events after Synchronization.
	ExecuteOnSynchronization bool     // A flag to skip hook execution in Synchronization tasks.
}

// ParallelRunMetadata is metadata for a parallel task
type ParallelRunMetadata struct {
	// channelId of the parallelTaskChannel to communicate between parallel ModuleRun and ParallelModuleRun tasks
	ChannelId string
	// context with cancel to stop ParallelModuleRun task
	Context context.Context
	CancelF func()

	// map of modules, taking part in a parallel run
	l       sync.Mutex
	modules map[string]ParallelRunModuleMetadata
}

// ParallelRunModuleMetadata is metadata for a parallel module
type ParallelRunModuleMetadata struct {
	DoModuleStartup bool
}

func (pm *ParallelRunMetadata) GetModulesMetadata() map[string]ParallelRunModuleMetadata {
	pm.l.Lock()
	defer pm.l.Unlock()
	return pm.modules
}

func (pm *ParallelRunMetadata) SetModuleMetadata(moduleName string, metadata ParallelRunModuleMetadata) {
	if pm.modules == nil {
		pm.modules = make(map[string]ParallelRunModuleMetadata)
	}
	pm.l.Lock()
	pm.modules[moduleName] = metadata
	pm.l.Unlock()
}

func (pm *ParallelRunMetadata) DeleteModuleMetadata(moduleName string) {
	if pm.modules == nil {
		return
	}
	pm.l.Lock()
	delete(pm.modules, moduleName)
	pm.l.Unlock()
}

func (pm *ParallelRunMetadata) ListModules() []string {
	pm.l.Lock()
	defer pm.l.Unlock()
	result := make([]string, 0, len(pm.modules))
	for module := range pm.modules {
		result = append(result, module)
	}
	return result
}

var (
	_ task_metadata.HookNameAccessor       = HookMetadata{}
	_ task_metadata.BindingContextAccessor = HookMetadata{}
	_ task_metadata.MonitorIDAccessor      = HookMetadata{}
	_ task_metadata.BindingContextSetter   = HookMetadata{}
	_ task_metadata.MonitorIDSetter        = HookMetadata{}
	_ task.MetadataDescriptionGetter       = HookMetadata{}
	_ modules.TaskMetadata                 = HookMetadata{}
)

func HookMetadataAccessor(t task.Task) HookMetadata {
	taskMeta := t.GetMetadata()
	if taskMeta == nil {
		log.Error("Possible Bug! task metadata is nil")
		return HookMetadata{}
	}

	meta, ok := taskMeta.(HookMetadata)
	if !ok {
		log.Error("Possible Bug! task metadata is not of type ModuleHookMetadata",
			slog.String("type", string(t.GetType())),
			slog.String("got", fmt.Sprintf("%T", meta)))
		return HookMetadata{}
	}

	return meta
}

func (hm HookMetadata) GetDescription() string {
	bindingsMap := make(map[string]struct{})

	for _, bc := range hm.BindingContext {
		bindingsMap[bc.Binding] = struct{}{}
	}

	bindings := make([]string, 0, len(bindingsMap))

	for bindingName := range bindingsMap {
		bindings = append(bindings, bindingName)
	}
	sort.Strings(bindings)
	bindingNames := ""
	if len(bindings) > 0 {
		bindingNames = ":" + strings.Join(bindings, ",")
	}
	if len(hm.BindingContext) > 1 {
		bindingNames = fmt.Sprintf("%s in %d contexts", bindingNames, len(hm.BindingContext))
	}

	if hm.ModuleName == "" {
		// global hook
		return fmt.Sprintf("%s:%s%s:%s", string(hm.BindingType), hm.HookName, bindingNames, hm.EventDescription)
	}

	if hm.HookName == "" {
		// module run
		osh := ""
		if hm.DoModuleStartup {
			osh = ":doStartup"
		}
		return fmt.Sprintf("%s%s%s:%s", hm.ModuleName, osh, bindingNames, hm.EventDescription)
	}

	// module hook
	return fmt.Sprintf("%s:%s%s:%s", string(hm.BindingType), hm.HookName, bindingNames, hm.EventDescription)
}

func (hm HookMetadata) GetKubernetesBindingID() string { return hm.KubernetesBindingId }
func (hm HookMetadata) GetBinding() string             { return hm.Binding }

func (hm HookMetadata) GetHookName() string {
	return hm.HookName
}

func (hm HookMetadata) GetBindingContext() []bindingcontext.BindingContext {
	return hm.BindingContext
}

func (hm HookMetadata) SetBindingContext(context []bindingcontext.BindingContext) interface{} {
	hm.BindingContext = context
	return hm
}

func (hm HookMetadata) GetMonitorIDs() []string {
	return hm.MonitorIDs
}

func (hm HookMetadata) SetMonitorIDs(monitorIDs []string) interface{} {
	hm.MonitorIDs = monitorIDs
	return hm
}

func (hm HookMetadata) IsSynchronization() bool {
	return len(hm.BindingContext) > 0 && hm.BindingContext[0].IsSynchronization()
}
