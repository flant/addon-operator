package kind

import (
	"context"
	"errors"

	"github.com/davecgh/go-spew/spew"
	"github.com/deckhouse/deckhouse/pkg/log"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	gohook "github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/pkg/module_manager/go_hook/metrics"
	"github.com/flant/addon-operator/pkg/utils"
	sh_hook "github.com/flant/shell-operator/pkg/hook"
	bindingcontext "github.com/flant/shell-operator/pkg/hook/binding_context"
	"github.com/flant/shell-operator/pkg/hook/config"
	"github.com/flant/shell-operator/pkg/hook/controller"
	htypes "github.com/flant/shell-operator/pkg/hook/types"
	objectpatch "github.com/flant/shell-operator/pkg/kube/object_patch"
	kubeeventsmanager "github.com/flant/shell-operator/pkg/kube_events_manager"
	eventtypes "github.com/flant/shell-operator/pkg/kube_events_manager/types"
	schdulertypes "github.com/flant/shell-operator/pkg/schedule_manager/types"
)

const (
	defaultHookGroupName = "main"
	defaultHookQueueName = "main"
)

var _ gohook.HookConfigLoader = (*GoHook)(nil)

type GoHook struct {
	basicHook sh_hook.Hook

	config        *gohook.HookConfig
	reconcileFunc ReconcileFunc
}

// NewGoHook creates a new go hook
func NewGoHook(config *gohook.HookConfig, f ReconcileFunc) *GoHook {
	logger := config.Logger
	if logger == nil {
		logger = log.NewLogger(log.Options{}).Named("auto-hook-logger")
	}

	return &GoHook{
		config:        config,
		reconcileFunc: f,
		basicHook: sh_hook.Hook{
			Logger: logger,
		},
	}
}

// BackportHookConfig passes config for shell-operator to make HookController and GetConfigDescription workable.
func (h *GoHook) BackportHookConfig(cfg *config.HookConfig) {
	h.basicHook.Config = cfg
	h.basicHook.RateLimiter = sh_hook.CreateRateLimiter(cfg)
}

// Run start ReconcileFunc
func (h *GoHook) Run(input *gohook.HookInput) error {
	return h.reconcileFunc(input)
}

// AddMetadata add hook metadata, name and path which are resolved by SDK registry
func (h *GoHook) AddMetadata(meta *gohook.HookMetadata) {
	h.basicHook.Name = meta.Name
	h.basicHook.Path = meta.Path
}

// WithHookController sets dependency "hook controller" for shell-operator
func (h *GoHook) WithHookController(hookController *controller.HookController) {
	h.basicHook.HookController = hookController
}

// GetHookController returns HookController
func (h *GoHook) GetHookController() *controller.HookController {
	return h.basicHook.HookController
}

// GetBasicHook returns hook for shell-operator
// Deprecated: don't use it for production purposes. You don't need such a low level for working with hooks
func (h *GoHook) GetBasicHook() sh_hook.Hook {
	return h.basicHook
}

// WithTmpDir injects temp directory from operator
func (h *GoHook) WithTmpDir(tmpDir string) {
	h.basicHook.TmpDir = tmpDir
}

// RateLimitWait runs query rate limiter pause
func (h *GoHook) RateLimitWait(ctx context.Context) error {
	return h.basicHook.RateLimitWait(ctx)
}

// GetHookConfigDescription get part of hook config for logging/debugging
func (h *GoHook) GetHookConfigDescription() string {
	return h.basicHook.GetConfigDescription()
}

// Execute runs the hook and return the result of the execution
func (h *GoHook) Execute(_ string, bContext []bindingcontext.BindingContext, _ string, configValues, values utils.Values, logLabels map[string]string) (result *HookResult, err error) {
	// Values are patched in-place, so an error can occur.
	patchableValues, err := gohook.NewPatchableValues(values)
	if err != nil {
		return nil, err
	}

	patchableConfigValues, err := gohook.NewPatchableValues(configValues)
	if err != nil {
		return nil, err
	}

	bindingActions := new([]gohook.BindingAction)

	logEntry := utils.EnrichLoggerWithLabels(h.basicHook.Logger, logLabels).
		With("output", "gohook")

	formattedSnapshots := make(gohook.Snapshots, len(bContext))
	for _, bc := range bContext {
		for snapBindingName, snaps := range bc.Snapshots {
			for _, snapshot := range snaps {
				goSnapshot := snapshot.FilterResult
				formattedSnapshots[snapBindingName] = append(formattedSnapshots[snapBindingName], goSnapshot)
			}
		}
	}

	metricsCollector := metrics.NewCollector(h.GetName())
	patchCollector := objectpatch.NewPatchCollector()

	err = h.Run(&gohook.HookInput{
		Snapshots:        formattedSnapshots,
		Values:           patchableValues,
		ConfigValues:     patchableConfigValues,
		PatchCollector:   patchCollector,
		Logger:           logEntry,
		MetricsCollector: metricsCollector,
		BindingActions:   bindingActions,
	})
	if err != nil {
		// on error we have to check if status collector has any status patches to apply
		// return non-nil HookResult if there are status patches
		if statusPatches := objectpatch.GetPatchStatusOperationsOnHookError(patchCollector.Operations()); len(statusPatches) > 0 {
			return &HookResult{
				ObjectPatcherOperations: statusPatches,
			}, err
		}
		return nil, err
	}

	result = &HookResult{
		Patches: map[utils.ValuesPatchType]*utils.ValuesPatch{
			utils.MemoryValuesPatch: {Operations: patchableValues.GetPatches()},
			utils.ConfigMapPatch:    {Operations: patchableConfigValues.GetPatches()},
		},
		Metrics:                 metricsCollector.CollectedMetrics(),
		ObjectPatcherOperations: patchCollector.Operations(),
		BindingActions:          *bindingActions,
	}

	return result, nil
}

// GetConfig returns hook config, which was set by user, while defining the hook
func (h *GoHook) GetConfig() *gohook.HookConfig {
	return h.config
}

// GetName returns the hook's name
func (h *GoHook) GetName() string {
	return h.basicHook.Name
}

// GetPath returns hook's path on the filesystem
func (h *GoHook) GetPath() string {
	return h.basicHook.Path
}

// GetKind returns kind of the hook
func (h *GoHook) GetKind() HookKind {
	return HookKindGo
}

// ReconcileFunc function which holds the main logic of the hook
type ReconcileFunc func(input *gohook.HookInput) error

// LoadAndValidateShellConfig loads shell hook config from bytes and validate it. Returns multierror.
func (h *GoHook) GetConfigForModule(_ string) (*config.HookConfig, error) {
	ghcfg, err := newHookConfigFromGoConfig(h.config)
	if err != nil {
		return nil, err
	}

	return &ghcfg, nil
}

func (h *GoHook) GetOnStartup() *float64 {
	if h.config.OnStartup != nil {
		return &h.config.OnStartup.Order
	}

	return nil
}

func (h *GoHook) GetBeforeAll() *float64 {
	if h.config.OnBeforeAll != nil {
		return &h.config.OnBeforeAll.Order
	}

	if h.config.OnBeforeHelm != nil {
		return &h.config.OnBeforeHelm.Order
	}

	return nil
}

func (h *GoHook) GetAfterAll() *float64 {
	if h.config.OnAfterAll != nil {
		return &h.config.OnAfterAll.Order
	}

	if h.config.OnAfterHelm != nil {
		return &h.config.OnAfterHelm.Order
	}

	return nil
}

func (h *GoHook) GetAfterDeleteHelm() *float64 {
	if h.config.OnAfterDeleteHelm != nil {
		return &h.config.OnAfterDeleteHelm.Order
	}

	return nil
}

func newHookConfigFromGoConfig(input *gohook.HookConfig) (config.HookConfig, error) {
	c := config.HookConfig{
		Version:            "v1",
		Schedules:          []htypes.ScheduleConfig{},
		OnKubernetesEvents: []htypes.OnKubernetesEventConfig{},
	}

	if input.Settings != nil {
		c.Settings = &htypes.Settings{
			ExecutionMinInterval: input.Settings.ExecutionMinInterval,
			ExecutionBurst:       input.Settings.ExecutionBurst,
		}
	}

	if input.OnStartup != nil {
		c.OnStartup = &htypes.OnStartupConfig{}
		c.OnStartup.BindingName = string(htypes.OnStartup)
		c.OnStartup.Order = input.OnStartup.Order
	}

	/*** A HUGE copy paste from shell-operator’s hook_config.ConvertAndCheckV1   ***/
	// WARNING no checks and defaults!
	for i, kubeCfg := range input.Kubernetes {
		monitor := &kubeeventsmanager.MonitorConfig{}
		monitor.Metadata.DebugName = config.MonitorDebugName(kubeCfg.Name, i)
		monitor.Metadata.MonitorId = config.MonitorConfigID()
		monitor.Metadata.LogLabels = map[string]string{}
		monitor.Metadata.MetricLabels = map[string]string{}
		monitor.ApiVersion = kubeCfg.ApiVersion
		monitor.Kind = kubeCfg.Kind
		monitor.WithNameSelector(kubeCfg.NameSelector)
		monitor.WithFieldSelector(kubeCfg.FieldSelector)
		monitor.WithNamespaceSelector(kubeCfg.NamespaceSelector)
		monitor.WithLabelSelector(kubeCfg.LabelSelector)
		if kubeCfg.FilterFunc == nil {
			return config.HookConfig{}, errors.New(`"FilterFunc" in KubernetesConfig cannot be nil`)
		}
		filterFunc := kubeCfg.FilterFunc
		monitor.FilterFunc = func(obj *unstructured.Unstructured) (interface{}, error) {
			return filterFunc(obj)
		}
		if gohook.BoolDeref(kubeCfg.ExecuteHookOnEvents, true) {
			monitor.WithEventTypes(nil)
		} else {
			monitor.WithEventTypes([]eventtypes.WatchEventType{})
		}

		kubeConfig := htypes.OnKubernetesEventConfig{}
		kubeConfig.Monitor = monitor
		kubeConfig.AllowFailure = input.AllowFailure
		if kubeCfg.Name == "" {
			return c, spew.Errorf(`"name" is a required field in binding: %v`, kubeCfg)
		}
		kubeConfig.BindingName = kubeCfg.Name
		if input.Queue == "" {
			kubeConfig.Queue = defaultHookQueueName
		} else {
			kubeConfig.Queue = input.Queue
		}
		kubeConfig.Group = defaultHookGroupName

		kubeConfig.ExecuteHookOnSynchronization = gohook.BoolDeref(kubeCfg.ExecuteHookOnSynchronization, true)
		kubeConfig.WaitForSynchronization = gohook.BoolDeref(kubeCfg.WaitForSynchronization, true)

		kubeConfig.KeepFullObjectsInMemory = false
		kubeConfig.Monitor.KeepFullObjectsInMemory = false

		c.OnKubernetesEvents = append(c.OnKubernetesEvents, kubeConfig)
	}

	// schedule bindings with includeSnapshotsFrom
	// are depend on kubernetes bindings.
	c.Schedules = []htypes.ScheduleConfig{}
	for _, inSch := range input.Schedule {
		res := htypes.ScheduleConfig{}

		if inSch.Name == "" {
			return c, spew.Errorf(`"name" is a required field in binding: %v`, inSch)
		}
		res.BindingName = inSch.Name

		res.AllowFailure = input.AllowFailure
		res.ScheduleEntry = schdulertypes.ScheduleEntry{
			Crontab: inSch.Crontab,
			Id:      config.ScheduleID(),
		}

		if input.Queue == "" {
			res.Queue = "main"
		} else {
			res.Queue = input.Queue
		}
		res.Group = "main"

		c.Schedules = append(c.Schedules, res)
	}

	// Update IncludeSnapshotsFrom for every binding with a group.
	// Merge binding's IncludeSnapshotsFrom with snapshots list calculated for group.
	groupSnapshots := make(map[string][]string)
	for _, kubeCfg := range c.OnKubernetesEvents {
		if kubeCfg.Group == "" {
			continue
		}
		if _, ok := groupSnapshots[kubeCfg.Group]; !ok {
			groupSnapshots[kubeCfg.Group] = make([]string, 0)
		}
		groupSnapshots[kubeCfg.Group] = append(groupSnapshots[kubeCfg.Group], kubeCfg.BindingName)
	}
	newKubeEvents := make([]htypes.OnKubernetesEventConfig, 0)
	for _, cfg := range c.OnKubernetesEvents {
		if snapshots, ok := groupSnapshots[cfg.Group]; ok {
			cfg.IncludeSnapshotsFrom = config.MergeArrays(cfg.IncludeSnapshotsFrom, snapshots)
		}
		newKubeEvents = append(newKubeEvents, cfg)
	}
	c.OnKubernetesEvents = newKubeEvents
	newSchedules := make([]htypes.ScheduleConfig, 0)
	for _, cfg := range c.Schedules {
		if snapshots, ok := groupSnapshots[cfg.Group]; ok {
			cfg.IncludeSnapshotsFrom = config.MergeArrays(cfg.IncludeSnapshotsFrom, snapshots)
		}
		newSchedules = append(newSchedules, cfg)
	}
	c.Schedules = newSchedules

	/*** END Copy Paste ***/

	return c, nil
}
