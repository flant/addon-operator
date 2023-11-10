package modules

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/flant/addon-operator/pkg/hook/types"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/shell-operator/pkg/hook/binding_context"

	"github.com/flant/addon-operator/pkg/module_manager/models/hooks"
	"github.com/flant/addon-operator/pkg/module_manager/models/hooks/kind"
	"github.com/flant/addon-operator/sdk"
	sh_op_types "github.com/flant/shell-operator/pkg/hook/types"
	utils_file "github.com/flant/shell-operator/pkg/utils/file"
	log "github.com/sirupsen/logrus"
)

// GlobalModule is an ephemeral container for global hooks
type GlobalModule struct {
	hooksDir string

	// probably we can use HookStorage here, but we have to add generics then
	byBinding map[sh_op_types.BindingType][]*hooks.GlobalHook
	byName    map[string]*hooks.GlobalHook

	valuesStorage *ValuesStorage

	// dependency
	// DEPRECATED: move to values storage
	valuesValidator validator
	dc              *hooks.HookExecutionDependencyContainer
}

func NewGlobalModule(hooksDir string, staticValues utils.Values, validator validator, dc *hooks.HookExecutionDependencyContainer) *GlobalModule {
	return &GlobalModule{
		hooksDir:      hooksDir,
		byBinding:     make(map[sh_op_types.BindingType][]*hooks.GlobalHook),
		byName:        make(map[string]*hooks.GlobalHook),
		valuesStorage: NewValuesStorage("global", staticValues, validator),
		dc:            dc,
	}
}

func (gm *GlobalModule) RegisterHooks() ([]*hooks.GlobalHook, error) {
	log.Debugf("Search and register global hooks")

	hks, err := gm.searchAndRegisterHooks()
	if err != nil {
		return nil, err
	}

	return hks, nil
}

func (gm *GlobalModule) GetHookByName(name string) *hooks.GlobalHook {
	return gm.byName[name]
}

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

//func (gm *GlobalModule) RunHooksByBinding(binding sh_op_types.BindingType, logLabels map[string]string) error {
//
//}

func (gm *GlobalModule) RunHookByName(hookName string, binding sh_op_types.BindingType, bindingContext []binding_context.BindingContext, logLabels map[string]string) (string, string, error) {
	globalHook := gm.byName[hookName]

	beforeValues := gm.valuesStorage.GetValues()
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

	afterValues := gm.valuesStorage.GetValues()
	afterChecksum := afterValues.Checksum()

	return beforeChecksum, afterChecksum, nil
}

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

	configValues := gm.valuesStorage.GetConfigValues()
	values := gm.valuesStorage.GetValues()

	hookResult, err := h.Execute(h.GetConfigVersion(), bc, "global", configValues, values, logLabels)
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
		configValuesPatchResult, err := gm.handlePatch(configValues, *configValuesPatch)
		if err != nil {
			return fmt.Errorf("global hook '%s': kube config global values update error: %s", h.GetName(), err)
		}

		if configValuesPatchResult != nil && configValuesPatchResult.ValuesChanged {
			logEntry.Debugf("Global hook '%s': validate global config values before update", h.GetName())
			// Validate merged static and new values.
			validationErr := gm.valuesStorage.PreCommitConfigValues(gm.GetName(), configValuesPatchResult.Values)
			//mergedValues := h.moduleManager.GlobalStaticAndNewValues(configValuesPatchResult.Values)
			//validationErr := h.moduleManager.ValuesValidator.ValidateGlobalConfigValues(mergedValues)
			if validationErr != nil {
				return fmt.Errorf("cannot apply config values patch for global values: %w", validationErr)
			}

			err := gm.dc.KubeConfigManager.SaveConfigValues(utils.GlobalValuesKey, configValuesPatchResult.Values)
			if err != nil {
				logEntry.Debugf("Global hook '%s' kube config global values stay unchanged:\n%s", h.GetName(), gm.valuesStorage.GetConfigValues().DebugString())
				return fmt.Errorf("global hook '%s': set kube config failed: %s", h.GetName(), err)
			}

			gm.valuesStorage.CommitConfigValues()

			//h.moduleManager.UpdateGlobalConfigValues(configValuesPatchResult.Values)
			// TODO(yalosev): save patches - UpdateGlobalDynamicValuesPatches

			logEntry.Debugf("Global hook '%s': kube config global values updated", h.GetName())
			logEntry.Debugf("New kube config global values:\n%s\n", gm.valuesStorage.GetConfigValues().DebugString())
		}
		// TODO(yalosev): Don't know what to do with this yet
		// Apply patches for *Enabled keys.
		err = h.ApplyEnabledPatches(*configValuesPatch)
		if err != nil {
			return fmt.Errorf("apply enabled patches from global config patch: %v", err)
		}
	}

	valuesPatch, has := hookResult.Patches[utils.MemoryValuesPatch]
	if has && valuesPatch != nil {
		// Apply patch to get intermediate updated values.
		valuesPatchResult, err := gm.handlePatch(values, *valuesPatch)
		if err != nil {
			return fmt.Errorf("global hook '%s': dynamic global values update error: %s", h.GetName(), err)
		}

		// MemoryValuesPatch from global hook can contains patches for *Enabled keys
		// and no patches for 'global' section — valuesPatchResult will be nil in this case.
		if valuesPatchResult != nil && valuesPatchResult.ValuesChanged {
			logEntry.Debugf("Global hook '%s': validate global values before update", h.GetName())
			validationErr := gm.valuesStorage.PreCommitValues(gm.GetName(), valuesPatchResult.Values)
			if validationErr != nil {
				return fmt.Errorf("cannot apply values patch for global values: %w", validationErr)
			}

			gm.valuesStorage.CommitValues()

			gm.valuesStorage.AppendValuesPatch(valuesPatchResult.ValuesPatch)

			logEntry.Debugf("Global hook '%s': kube global values updated", h.GetName())
			logEntry.Debugf("New global values:\n%s", gm.valuesStorage.GetValues().DebugString())
		}
		// TODO(yalosev): do something here
		// Apply patches for *Enabled keys.
		err = h.ApplyEnabledPatches(*valuesPatch)
		if err != nil {
			return fmt.Errorf("apply enabled patches from global values patch: %v", err)
		}
	}

	return nil
}

// TODO(yalosev): change name, because we don't save values here, just store them as dirty
func (gm *GlobalModule) ValidateAndSaveConfigValues(v utils.Values) error {
	return gm.valuesStorage.PreCommitConfigValues(gm.GetName(), v)
}

func (gm *GlobalModule) ConfigValuesHaveChanges() bool {
	return gm.valuesStorage.dirtyConfigValuesHasDiff()
}

func (gm *GlobalModule) CommitConfigValuesChange() {
	gm.valuesStorage.CommitConfigValues()
}
func (gm *GlobalModule) GetValues() utils.Values {
	return gm.valuesStorage.GetValues()
}

func (gm *GlobalModule) GetConfigValues() utils.Values {
	return gm.valuesStorage.GetConfigValues()
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

	// global values doesn't contain global key as top level one
	// but patches are still have, we have to use temporary map
	// TODO: maybe we have to change ApplyValuesPatch function
	tmpValues := utils.Values{
		utils.GlobalValuesKey: currentValues,
	}

	// Apply new patches in Strict mode. Hook should not return 'remove' with nonexistent path.
	newValues, valuesChanged, err := utils.ApplyValuesPatch(tmpValues, globalValuesPatch, utils.Strict)
	if err != nil {
		return nil, fmt.Errorf("merge global values failed: %s", err)
	}

	newValues = newValues[utils.GlobalValuesKey].(map[string]interface{})

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
		gh := hooks.NewGlobalHook(sh, gm.dc)
		hks = append(hks, gh)
	}

	for _, gh := range goHooks {
		glh := hooks.NewGlobalHook(gh, gm.dc)
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
