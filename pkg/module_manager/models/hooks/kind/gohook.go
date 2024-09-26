package kind

import (
	"context"

	log "github.com/sirupsen/logrus"

	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/pkg/module_manager/go_hook/metrics"
	"github.com/flant/addon-operator/pkg/utils"
	sh_hook "github.com/flant/shell-operator/pkg/hook"
	"github.com/flant/shell-operator/pkg/hook/binding_context"
	"github.com/flant/shell-operator/pkg/hook/config"
	"github.com/flant/shell-operator/pkg/hook/controller"
	"github.com/flant/shell-operator/pkg/kube/object_patch"
)

type GoHook struct {
	basicHook sh_hook.Hook

	config        *go_hook.HookConfig
	reconcileFunc ReconcileFunc
}

// NewGoHook creates a new go hook
func NewGoHook(config *go_hook.HookConfig, f ReconcileFunc) *GoHook {
	return &GoHook{
		config:        config,
		reconcileFunc: f,
	}
}

// BackportHookConfig passes config for shell-operator to make HookController and GetConfigDescription workable.
func (h *GoHook) BackportHookConfig(cfg *config.HookConfig) {
	h.basicHook.Config = cfg
	h.basicHook.RateLimiter = sh_hook.CreateRateLimiter(cfg)
}

// Run start ReconcileFunc
func (h *GoHook) Run(input *go_hook.HookInput) error {
	return h.reconcileFunc(input)
}

// AddMetadata add hook metadata, name and path which are resolved by SDK registry
func (h *GoHook) AddMetadata(meta *go_hook.HookMetadata) {
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
func (h *GoHook) Execute(_ string, bContext []binding_context.BindingContext, _ string, configValues, values utils.Values, logLabels map[string]string) (result *HookResult, err error) {
	// Values are patched in-place, so an error can occur.
	patchableValues, err := go_hook.NewPatchableValues(values)
	if err != nil {
		return nil, err
	}

	patchableConfigValues, err := go_hook.NewPatchableValues(configValues)
	if err != nil {
		return nil, err
	}

	bindingActions := new([]go_hook.BindingAction)

	logEntry := log.WithFields(utils.LabelsToLogFields(logLabels)).
		WithField("output", "gohook")

	formattedSnapshots := make(go_hook.Snapshots, len(bContext))
	for _, bc := range bContext {
		for snapBindingName, snaps := range bc.Snapshots {
			for _, snapshot := range snaps {
				goSnapshot := snapshot.FilterResult
				formattedSnapshots[snapBindingName] = append(formattedSnapshots[snapBindingName], goSnapshot)
			}
		}
	}

	metricsCollector := metrics.NewCollector(h.GetName())
	patchCollector := object_patch.NewPatchCollector()

	err = h.Run(&go_hook.HookInput{
		Snapshots:        formattedSnapshots,
		Values:           patchableValues,
		ConfigValues:     patchableConfigValues,
		PatchCollector:   patchCollector,
		LogEntry:         logEntry,
		MetricsCollector: metricsCollector,
		BindingActions:   bindingActions,
	})
	if err != nil {
		// on error we have to check if status collector has any status patches to apply
		// return non-nil HookResult if there are status patches
		if statusPatches := object_patch.GetPatchStatusOperationsOnHookError(patchCollector.Operations()); len(statusPatches) > 0 {
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
func (h *GoHook) GetConfig() *go_hook.HookConfig {
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
type ReconcileFunc func(input *go_hook.HookInput) error
