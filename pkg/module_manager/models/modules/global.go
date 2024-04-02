package modules

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	log "github.com/sirupsen/logrus"

	"github.com/flant/addon-operator/pkg/hook/types"
	"github.com/flant/addon-operator/pkg/module_manager/models/hooks"
	"github.com/flant/addon-operator/pkg/module_manager/models/hooks/kind"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/addon-operator/pkg/values/validation"
	"github.com/flant/addon-operator/sdk"
	"github.com/flant/shell-operator/pkg/hook/binding_context"
	sh_op_types "github.com/flant/shell-operator/pkg/hook/types"
	utils_file "github.com/flant/shell-operator/pkg/utils/file"
)

// GlobalModule is an ephemeral container for global hooks
type GlobalModule struct {
	hooksDir string

	// probably we can use HookStorage here, but we have to add generics then
	byBinding map[sh_op_types.BindingType][]*hooks.GlobalHook
	byName    map[string]*hooks.GlobalHook

	valuesStorage *ValuesStorage

	enabledByHookC chan *EnabledPatchReport

	// dependency
	dc *hooks.HookExecutionDependencyContainer
}

// EnabledReportChannel returns channel with dynamic modules enabling by global hooks
func (gm *GlobalModule) EnabledReportChannel() chan *EnabledPatchReport {
	return gm.enabledByHookC
}

// NewGlobalModule build ephemeral global container for global hooks and values
func NewGlobalModule(hooksDir string, staticValues utils.Values, dc *hooks.HookExecutionDependencyContainer,
	configBytes, valuesBytes []byte,
) (*GlobalModule, error) {
	valuesStorage, err := NewValuesStorage("global", staticValues, configBytes, valuesBytes)
	if err != nil {
		return nil, fmt.Errorf("new values storage: %w", err)
	}

	return &GlobalModule{
		hooksDir:       hooksDir,
		byBinding:      make(map[sh_op_types.BindingType][]*hooks.GlobalHook),
		byName:         make(map[string]*hooks.GlobalHook),
		valuesStorage:  valuesStorage,
		dc:             dc,
		enabledByHookC: make(chan *EnabledPatchReport, 10),
	}, nil
}

// RegisterHooks finds and registers global hooks
func (gm *GlobalModule) RegisterHooks() ([]*hooks.GlobalHook, error) {
	log.Debugf("Search and register global hooks")

	hks, err := gm.searchAndRegisterHooks()
	if err != nil {
		return nil, err
	}

	return hks, nil
}

// GetHookByName ...
func (gm *GlobalModule) GetHookByName(name string) *hooks.GlobalHook {
	return gm.byName[name]
}

// GetHooks returns module hooks, they could be filtered by BindingType optionally
func (gm *GlobalModule) GetHooks(bt ...sh_op_types.BindingType) []*hooks.GlobalHook {
	if len(bt) > 0 {
		t := bt[0]
		res, ok := gm.byBinding[t]
		if !ok {
			return []*hooks.GlobalHook{}
		}
		sort.Slice(res, func(i, j int) bool {
			return res[i].Order(t) < res[j].Order(t)
		})

		return res
	}

	// return all hooks
	res := make([]*hooks.GlobalHook, 0, len(gm.byName))
	for _, h := range gm.byName {
		res = append(res, h)
	}

	sort.Slice(res, func(i, j int) bool {
		return res[i].GetName() < res[j].GetName()
	})

	return res
}

// RunHookByName runs some specified hook by its name
func (gm *GlobalModule) RunHookByName(hookName string, binding sh_op_types.BindingType, bindingContext []binding_context.BindingContext, logLabels map[string]string) (string, string, error) {
	globalHook := gm.byName[hookName]

	beforeValues := gm.valuesStorage.GetValues(false)
	beforeChecksum := beforeValues.Checksum()

	// Update kubernetes snapshots just before execute a hook
	if binding == sh_op_types.OnKubernetesEvent || binding == sh_op_types.Schedule {
		bindingContext = globalHook.GetHookController().UpdateSnapshots(bindingContext)
	}

	if binding == types.BeforeAll || binding == types.AfterAll {
		snapshots := globalHook.GetHookController().KubernetesSnapshots()
		newBindingContext := make([]binding_context.BindingContext, 0)
		for _, bc := range bindingContext {
			bc.Snapshots = snapshots
			bc.Metadata.IncludeAllSnapshots = true
			newBindingContext = append(newBindingContext, bc)
		}
		bindingContext = newBindingContext
	}

	err := gm.executeHook(globalHook, binding, bindingContext, logLabels)
	if err != nil {
		return "", "", err
	}

	afterValues := gm.valuesStorage.GetValues(false)
	afterChecksum := afterValues.Checksum()

	return beforeChecksum, afterChecksum, nil
}

// GetName ...
func (gm *GlobalModule) GetName() string {
	return utils.GlobalValuesKey
}

func (gm *GlobalModule) executeHook(h *hooks.GlobalHook, bindingType sh_op_types.BindingType, bc []binding_context.BindingContext, logLabels map[string]string) error {
	// Convert bindingContext for version
	// versionedContextList := ConvertBindingContextList(h.Config.Version, bindingContext)
	logEntry := log.WithFields(utils.LabelsToLogFields(logLabels))

	for _, info := range h.GetHookController().SnapshotsInfo() {
		logEntry.Debugf("snapshot info: %s", info)
	}

	prefixedConfigValues := gm.valuesStorage.GetConfigValues(true)
	prefixedValues := gm.valuesStorage.GetValues(true)

	hookResult, err := h.Execute(h.GetConfigVersion(), bc, "global", prefixedConfigValues, prefixedValues, logLabels)
	if hookResult != nil && hookResult.Usage != nil {
		metricLabels := map[string]string{
			"hook":       h.GetName(),
			"binding":    string(bindingType),
			"queue":      logLabels["queue"],
			"activation": logLabels["event.type"],
		}
		// usage metrics
		gm.dc.MetricStorage.HistogramObserve("{PREFIX}global_hook_run_sys_cpu_seconds", hookResult.Usage.Sys.Seconds(), metricLabels, nil)
		gm.dc.MetricStorage.HistogramObserve("{PREFIX}global_hook_run_user_cpu_seconds", hookResult.Usage.User.Seconds(), metricLabels, nil)
		gm.dc.MetricStorage.GaugeSet("{PREFIX}global_hook_run_max_rss_bytes", float64(hookResult.Usage.MaxRss)*1024, metricLabels)
	}
	if err != nil {
		return fmt.Errorf("global hook '%s' failed: %s", h.GetName(), err)
	}

	// Apply metric operations
	err = gm.dc.HookMetricsStorage.SendBatch(hookResult.Metrics, map[string]string{
		"hook": h.GetName(),
	})
	if err != nil {
		return err
	}

	if len(hookResult.ObjectPatcherOperations) > 0 {
		err = gm.dc.KubeObjectPatcher.ExecuteOperations(hookResult.ObjectPatcherOperations)
		if err != nil {
			return err
		}
	}

	configValuesPatch, has := hookResult.Patches[utils.ConfigMapPatch]
	if has && configValuesPatch != nil {
		// Apply patch to get intermediate updated values.
		configValuesPatchResult, err := gm.handlePatch(prefixedConfigValues, *configValuesPatch)
		if err != nil {
			return fmt.Errorf("global hook '%s': kube config global values update error: %s", h.GetName(), err)
		}

		if configValuesPatchResult != nil && configValuesPatchResult.ValuesChanged {
			logEntry.Debugf("Global hook '%s': validate global config values before update", h.GetName())
			// Validate merged static and new values.
			// TODO: probably, we have to replace with with some transaction method on valuesStorage
			newValues, validationErr := gm.valuesStorage.GenerateNewConfigValues(configValuesPatchResult.Values, true)
			if validationErr != nil {
				return fmt.Errorf("cannot apply config values patch for global values: %w", validationErr)
			}

			err := gm.dc.KubeConfigManager.DeprecatedSaveConfigValues(utils.GlobalValuesKey, configValuesPatchResult.Values)
			if err != nil {
				logEntry.Debugf("Global hook '%s' kube config global values stay unchanged:\n%s", h.GetName(), gm.valuesStorage.GetConfigValues(false).DebugString())
				return fmt.Errorf("global hook '%s': set kube config failed: %s", h.GetName(), err)
			}

			gm.valuesStorage.SaveConfigValues(newValues)

			logEntry.Debugf("Global hook '%s': kube config global values updated", h.GetName())
			logEntry.Debugf("New kube config global values:\n%s\n", gm.valuesStorage.GetConfigValues(false).DebugString())
		}

		// Apply patches for *Enabled keys.
		err = gm.applyEnabledPatches(*configValuesPatch)
		if err != nil {
			return fmt.Errorf("apply enabled patches from global config patch: %v", err)
		}
	}

	valuesPatch, has := hookResult.Patches[utils.MemoryValuesPatch]
	if has && valuesPatch != nil {
		// Apply patch to get intermediate updated values.
		valuesPatchResult, err := gm.handlePatch(prefixedValues, *valuesPatch)
		if err != nil {
			return fmt.Errorf("global hook '%s': dynamic global values update error: %s", h.GetName(), err)
		}

		// MemoryValuesPatch from global hook can contains patches for *Enabled keys
		// and no patches for 'global' section — valuesPatchResult will be nil in this case.
		if valuesPatchResult != nil && valuesPatchResult.ValuesChanged {
			logEntry.Debugf("Global hook '%s': validate global values before update", h.GetName())
			validationErr := gm.valuesStorage.validateValues(valuesPatchResult.Values)
			if validationErr != nil {
				return fmt.Errorf("cannot apply values patch for global values: %w", validationErr)
			}

			gm.valuesStorage.appendValuesPatch(valuesPatchResult.ValuesPatch)
			err = gm.valuesStorage.CommitValues()
			if err != nil {
				return fmt.Errorf("error on commit values: %w", err)
			}

			logEntry.Debugf("Global hook '%s': kube global values updated", h.GetName())
			logEntry.Debugf("New global values:\n%s", gm.valuesStorage.GetValues(false).DebugString())
		}

		// Apply patches for *Enabled keys.
		err = gm.applyEnabledPatches(*valuesPatch)
		if err != nil {
			return fmt.Errorf("apply enabled patches from global values patch: %v", err)
		}
	}

	return nil
}

type EnabledPatchReport struct {
	Patch utils.ValuesPatch
	Done  chan error
}

// applyEnabledPatches apply patches for enabled modules
func (gm *GlobalModule) applyEnabledPatches(valuesPatch utils.ValuesPatch) error {
	enabledPatch := utils.EnabledFromValuesPatch(valuesPatch)
	if len(enabledPatch.Operations) == 0 {
		return nil
	}

	report := &EnabledPatchReport{
		Patch: valuesPatch,
		Done:  make(chan error),
	}

	gm.enabledByHookC <- report

	err := <-report.Done

	return err
}

func (gm *GlobalModule) GetValues(withPrefix bool) utils.Values {
	return gm.valuesStorage.GetValues(withPrefix)
}

func (gm *GlobalModule) GetConfigValues(withPrefix bool) utils.Values {
	return gm.valuesStorage.GetConfigValues(withPrefix)
}

func (gm *GlobalModule) GenerateNewConfigValues(kubeConfigValues utils.Values, validate bool) (utils.Values, error) {
	return gm.valuesStorage.GenerateNewConfigValues(kubeConfigValues, validate)
}

func (gm *GlobalModule) SaveConfigValues(configV utils.Values) {
	gm.valuesStorage.SaveConfigValues(configV)
}

// SetEnabledModules inject enabledModules to the global values
// enabledModules are injected as a patch, to recalculate on every global values change
func (gm *GlobalModule) SetEnabledModules(enabledModules []string) {
	if len(enabledModules) == 0 {
		return
	}

	data, _ := json.Marshal(enabledModules)
	gm.valuesStorage.appendValuesPatch(utils.ValuesPatch{Operations: []*utils.ValuesPatchOperation{
		{
			Op:    "add",
			Path:  "/global/enabledModules",
			Value: data,
		},
	}})

	_ = gm.valuesStorage.calculateResultValues()
}

func (gm *GlobalModule) GetValuesPatches() []utils.ValuesPatch {
	return gm.valuesStorage.getValuesPatches()
}

type globalValuesPatchResult struct {
	// Global values with the root "global" key.
	Values utils.Values
	// Original values patch argument.
	ValuesPatch utils.ValuesPatch
	// Whether values changed after applying patch.
	ValuesChanged bool
}

// ToDO: work in progress
// handlePatch do simple checks of patches and apply them to passed Values.
func (gm *GlobalModule) handlePatch(currentValues utils.Values, valuesPatch utils.ValuesPatch) (*globalValuesPatchResult, error) {
	if err := utils.ValidateHookValuesPatch(valuesPatch, utils.GlobalValuesKey); err != nil {
		return nil, fmt.Errorf("merge global values failed: %s", err)
	}

	// Get patches for global section
	globalValuesPatch := utils.FilterValuesPatch(valuesPatch, utils.GlobalValuesKey)
	if len(globalValuesPatch.Operations) == 0 {
		// No patches for 'global' section
		return nil, nil
	}

	// Apply new patches in Strict mode. Hook should not return 'remove' with nonexistent path.
	newValues, valuesChanged, err := utils.ApplyValuesPatch(currentValues, globalValuesPatch, utils.Strict)
	if err != nil {
		return nil, fmt.Errorf("merge global values failed: %s", err)
	}

	switch v := newValues[utils.GlobalValuesKey].(type) {
	case utils.Values:
		newValues = v
	case map[string]interface{}:
		newValues = utils.Values(v)
	default:
		return nil, fmt.Errorf("unknown global values type: %T", v)
	}

	result := &globalValuesPatchResult{
		Values:        newValues,
		ValuesChanged: valuesChanged,
		ValuesPatch:   globalValuesPatch,
	}

	return result, nil
}

func (gm *GlobalModule) searchAndRegisterHooks() ([]*hooks.GlobalHook, error) {
	hks, err := gm.searchGlobalHooks()
	if err != nil {
		return nil, fmt.Errorf("search module hooks failed: %w", err)
	}

	log.Debugf("Found %d global hooks", len(hks))
	if log.GetLevel() == log.DebugLevel {
		for _, h := range hks {
			log.Debugf("  GlobalHook: Name=%s, Path=%s", h.GetName(), h.GetPath())
		}
	}

	for _, globalHook := range hks {
		hookLogEntry := log.WithField("hook", globalHook.GetName()).
			WithField("hook.type", "global")

		// TODO: we could make multierr here and return all config errors at once
		err := globalHook.InitializeHookConfig()
		if err != nil {
			return nil, fmt.Errorf("global hook --config invalid: %w", err)
		}

		// Add hook info as log labels
		for _, kubeCfg := range globalHook.GetHookConfig().OnKubernetesEvents {
			kubeCfg.Monitor.Metadata.LogLabels["hook"] = globalHook.GetName()
			kubeCfg.Monitor.Metadata.LogLabels["hook.type"] = "global"
			kubeCfg.Monitor.Metadata.MetricLabels = map[string]string{
				"hook":    globalHook.GetName(),
				"binding": kubeCfg.BindingName,
				"module":  "", // empty "module" label for label set consistency with module hooks
				"queue":   kubeCfg.Queue,
				"kind":    kubeCfg.Monitor.Kind,
			}
		}

		// register module hook in indexes
		gm.byName[globalHook.GetName()] = globalHook
		for _, binding := range globalHook.GetHookConfig().Bindings() {
			gm.byBinding[binding] = append(gm.byBinding[binding], globalHook)
		}

		hookLogEntry.Debugf("Module hook from '%s'. Bindings: %s", globalHook.GetPath(), globalHook.GetConfigDescription())
	}

	return hks, nil
}

// searchGlobalHooks recursively find all executables in hooksDir. Absent hooksDir is not an error.
func (gm *GlobalModule) searchGlobalHooks() (hks []*hooks.GlobalHook, err error) {
	if gm.hooksDir == "" {
		log.Warnf("Global hooks directory path is empty! No global hooks to load.")
		return nil, nil
	}

	shellHooks, err := gm.searchGlobalShellHooks(gm.hooksDir)
	if err != nil {
		return nil, err
	}

	goHooks, err := gm.searchGlobalGoHooks()
	if err != nil {
		return nil, err
	}

	hks = make([]*hooks.GlobalHook, 0, len(shellHooks)+len(goHooks))

	for _, sh := range shellHooks {
		gh := hooks.NewGlobalHook(sh)
		hks = append(hks, gh)
	}

	for _, gh := range goHooks {
		glh := hooks.NewGlobalHook(gh)
		hks = append(hks, glh)
	}

	log.Debugf("Search global hooks: %d shell, %d golang", len(shellHooks), len(goHooks))

	return hks, nil
}

// searchGlobalHooks recursively find all executables in hooksDir. Absent hooksDir is not an error.
func (gm *GlobalModule) searchGlobalShellHooks(hooksDir string) (hks []*kind.ShellHook, err error) {
	if _, err := os.Stat(hooksDir); os.IsNotExist(err) {
		return nil, nil
	}

	hooksSubDir := filepath.Join(hooksDir, "hooks")
	if _, err := os.Stat(hooksSubDir); !os.IsNotExist(err) {
		hooksDir = hooksSubDir
	}
	hooksRelativePaths, err := utils_file.RecursiveGetExecutablePaths(hooksDir)
	if err != nil {
		return nil, err
	}

	hks = make([]*kind.ShellHook, 0)

	// sort hooks by path
	sort.Strings(hooksRelativePaths)
	log.Debugf("  Hook paths: %+v", hooksRelativePaths)

	for _, hookPath := range hooksRelativePaths {
		hookName, err := filepath.Rel(hooksDir, hookPath)
		if err != nil {
			return nil, err
		}

		globalHook := kind.NewShellHook(hookName, hookPath)

		hks = append(hks, globalHook)
	}

	count := "no"
	if len(hks) > 0 {
		count = strconv.Itoa(len(hks))
	}
	log.Infof("Found %s global shell hooks in '%s'", count, hooksDir)

	return
}

func (gm *GlobalModule) searchGlobalGoHooks() ([]*kind.GoHook, error) {
	// find global hooks in go hooks registry
	goHooks := sdk.Registry().GetGlobalHooks()

	count := "no"
	if len(goHooks) > 0 {
		count = strconv.Itoa(len(goHooks))
	}
	log.Infof("Found %s global Go hooks", count)

	return goHooks, nil
}

func (gm *GlobalModule) GetSchemaStorage() *validation.SchemaStorage {
	return gm.valuesStorage.schemaStorage
}
