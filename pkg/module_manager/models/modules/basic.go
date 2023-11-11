package modules

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/flant/addon-operator/pkg/module_manager/models/hooks"
	"github.com/flant/addon-operator/pkg/module_manager/models/hooks/kind"

	sh_app "github.com/flant/shell-operator/pkg/app"
	"github.com/flant/shell-operator/pkg/executor"
	"github.com/gofrs/uuid/v5"
	"github.com/kennygrant/sanitize"

	"github.com/hashicorp/go-multierror"

	"github.com/flant/addon-operator/pkg/hook/types"
	"github.com/flant/shell-operator/pkg/hook/binding_context"
	"github.com/flant/shell-operator/pkg/utils/measure"

	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/addon-operator/sdk"
	sh_op_types "github.com/flant/shell-operator/pkg/hook/types"
	utils_file "github.com/flant/shell-operator/pkg/utils/file"
	log "github.com/sirupsen/logrus"
)

type BasicModule struct {
	// required
	Name string
	// required
	Order uint32
	// required
	Path string

	valuesStorage *ValuesStorage

	state *moduleState

	hooks *HooksStorage

	// dependency
	dc *hooks.HookExecutionDependencyContainer
}

func NewBasicModule(name, path string, order uint32, staticValues utils.Values, validator validator) *BasicModule {
	return &BasicModule{
		Name:          name,
		Order:         order,
		Path:          path,
		valuesStorage: NewValuesStorage(name, staticValues, validator),
		state: &moduleState{
			Phase:                Startup,
			hookErrors:           make(map[string]error),
			synchronizationState: NewSynchronizationState(),
		},
		hooks: newHooksStorage(),
	}
}

func (bm *BasicModule) WithDependencies(dep *hooks.HookExecutionDependencyContainer) {
	bm.dc = dep
}

// Deprecated: remove this
func (bm *BasicModule) GetBaseModule() *BasicModule {
	return bm
}
func (bm *BasicModule) GetOrder() uint32 {
	return bm.Order
}

func (bm *BasicModule) GetName() string {
	return bm.Name
}

func (bm *BasicModule) GetPath() string {
	return bm.Path
}

func (bm *BasicModule) GetHooks(bt ...sh_op_types.BindingType) []*hooks.ModuleHook {
	return bm.hooks.getHooks(bt...)
}

func (bm *BasicModule) DeregisterHooks() {
	bm.hooks.clean()
}

func (bm *BasicModule) ResetState() {
	bm.state = &moduleState{
		Phase:                Startup,
		hookErrors:           make(map[string]error),
		synchronizationState: NewSynchronizationState(),
	}
}

//func (bm *BasicModule) UpdateConfigValues(newValues utils.Values) {
//	// TODO: merge with values?
//	bm.configValues = newValues
//}

func (bm *BasicModule) RegisterHooks(logger *log.Entry) ([]*hooks.ModuleHook, error) {
	if bm.hooks.registered {
		logger.Debugf("Module hooks already registered")
		return nil, nil
	}

	logger.Debugf("Search and register hooks")

	hks, err := bm.searchAndRegisterHooks(logger)
	if err != nil {
		return nil, err
	}

	bm.hooks.registered = true

	return hks, nil
}

//// ConfigValues returns raw values from ConfigMap:
//// - global section
//// - module section
//func (m *Module) ConfigValues() utils.Values {
//	return mergeLayers(
//		// Global values from ConfigMap with defaults from schema.
//		m.moduleManager.GlobalConfigValues(),
//		// Init module section.
//		utils.Values{m.ValuesKey(): map[string]interface{}{}},
//		// Merge overrides from ConfigMap.
//		m.moduleManager.ModuleConfigValues(m.Name),
//	)
//}

//func (bm *BasicModule) GetConfigValues() utils.Values {
//	return bm.configValues
//	// TODO: was merged here
//}

// // Values returns effective values for module hook or helm chart:
// //
// // global section: static + config + defaults + patches from hooks
// //
// // module section: static + config + defaults + patches from hooks
//// TODO: patch
//func (bm *BasicModule) GetValues() utils.Values {
//	return bm.values
//}

// there was
//func (m *Module) Values() (utils.Values, error) {
//	var err error
//
//	globalValues, err := m.moduleManager.GlobalValues()
//	if err != nil {
//		return nil, fmt.Errorf("construct module values: %s", err)
//	}
//
//	// Apply global and module values defaults before applying patches.
//	res := mergeLayers(
//		// Global values with patches and defaults.
//		globalValues,
//		// Init module section.
//		utils.Values{m.ValuesKey(): map[string]interface{}{}},
//		// Merge static values from various values.yaml files.
//		m.CommonStaticConfig.GetValues(),
//		m.StaticConfig.GetValues(),
//		// Apply config values defaults before ConfigMap overrides.
//		&applyDefaultsForModule{
//			m.ValuesKey(),
//			validation.ConfigValuesSchema,
//			m.moduleManager.ValuesValidator,
//		},
//		// Merge overrides from ConfigMap.
//		m.moduleManager.ModuleConfigValues(m.Name),
//		// Apply dynamic values defaults before patches.
//		&applyDefaultsForModule{
//			m.ValuesKey(),
//			validation.ValuesSchema,
//			m.moduleManager.ValuesValidator,
//		},
//	)

func (bm *BasicModule) searchModuleHooks() ([]*hooks.ModuleHook, error) {
	shellHooks, err := bm.searchModuleShellHooks()
	if err != nil {
		return nil, err
	}

	goHooks := bm.searchModuleGoHooks()

	mHooks := make([]*hooks.ModuleHook, 0, len(shellHooks)+len(goHooks))

	for _, sh := range shellHooks {
		mh := hooks.NewModuleHook(sh)
		mHooks = append(mHooks, mh)
	}

	for _, gh := range goHooks {
		mh := hooks.NewModuleHook(gh)
		mHooks = append(mHooks, mh)
	}

	sort.SliceStable(mHooks, func(i, j int) bool {
		return mHooks[i].GetPath() < mHooks[j].GetPath()
	})

	return mHooks, nil
}

func (bm *BasicModule) searchModuleShellHooks() (hks []*kind.ShellHook, err error) {
	hooksDir := filepath.Join(bm.Path, "hooks")
	if _, err := os.Stat(hooksDir); os.IsNotExist(err) {
		return nil, nil
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
		hookName, err := filepath.Rel(filepath.Dir(bm.Path), hookPath)
		if err != nil {
			return nil, err
		}

		shHook := kind.NewShellHook(hookName, hookPath)

		hks = append(hks, shHook)
	}

	return
}

func (bm *BasicModule) searchModuleGoHooks() (hks []*kind.GoHook) {
	// find module hooks in go hooks registry
	goHooks := sdk.Registry().GetModuleHooks(bm.Name)
	hks = make([]*kind.GoHook, 0, len(goHooks))

	for _, h := range goHooks {
		hks = append(hks, h)
	}

	return hks
}

func (bm *BasicModule) searchAndRegisterHooks(logger *log.Entry) ([]*hooks.ModuleHook, error) {
	hks, err := bm.searchModuleHooks()
	if err != nil {
		return nil, fmt.Errorf("search module hooks failed: %w", err)
	}

	logger.Debugf("Found %d hooks", len(hks))
	if log.GetLevel() == log.DebugLevel {
		for _, h := range hks {
			logger.Debugf("  ModuleHook: Name=%s, Path=%s", h.GetName(), h.GetPath())
		}
	}

	for _, moduleHook := range hks {
		hookLogEntry := logger.WithField("hook", moduleHook.GetName()).
			WithField("hook.type", "module")

		// TODO: we could make multierr here and return all config errors at once
		err := moduleHook.InitializeHookConfig()
		if err != nil {
			return nil, fmt.Errorf("module hook --config invalid: %w", err)
		}

		// Add hook info as log labels
		for _, kubeCfg := range moduleHook.GetHookConfig().OnKubernetesEvents {
			kubeCfg.Monitor.Metadata.LogLabels["module"] = bm.Name
			kubeCfg.Monitor.Metadata.LogLabels["hook"] = moduleHook.GetName()
			kubeCfg.Monitor.Metadata.LogLabels["hook.type"] = "module"
			kubeCfg.Monitor.Metadata.MetricLabels = map[string]string{
				"hook":    moduleHook.GetName(),
				"binding": kubeCfg.BindingName,
				"module":  bm.Name,
				"queue":   kubeCfg.Queue,
				"kind":    kubeCfg.Monitor.Kind,
			}
		}

		// register module hook in indexes
		bm.hooks.AddHook(moduleHook)

		hookLogEntry.Debugf("Module hook from '%s'. Bindings: %s", moduleHook.GetPath(), moduleHook.GetConfigDescription())
	}

	return hks, nil
}

func (bm *BasicModule) GetPhase() ModuleRunPhase {
	return bm.state.Phase
}

func (bm *BasicModule) SetPhase(phase ModuleRunPhase) {
	bm.state.Phase = phase
}

func (bm *BasicModule) SetError(err error) {
	bm.state.lastModuleErr = err
}

func (bm *BasicModule) SetStateEnabled(e bool) {
	bm.state.Enabled = e
}

func (bm *BasicModule) SaveHookError(hookName string, err error) {
	bm.state.hookErrorsLock.Lock()
	defer bm.state.hookErrorsLock.Unlock()

	if err == nil {
		delete(bm.state.hookErrors, hookName)
		return
	}

	bm.state.hookErrors[hookName] = err
}

// RunHooksByBinding gets all hooks for binding, for each hook it creates a BindingContext,
// sets KubernetesSnapshots and runs the hook.
func (bm *BasicModule) RunHooksByBinding(binding sh_op_types.BindingType, logLabels map[string]string) error {
	var err error
	moduleHooks := bm.GetHooks(binding)

	for _, moduleHook := range moduleHooks {
		// TODO: baga - тут будут блокироваться все хуки
		err = moduleHook.RateLimitWait(context.Background())
		if err != nil {
			// This could happen when the Context is
			// canceled, or the expected wait time exceeds the Context's Deadline.
			// The best we can do without proper context usage is to repeat the task.
			return err
		}

		bc := binding_context.BindingContext{
			Binding: string(binding),
		}
		// Update kubernetes snapshots just before execute a hook
		if binding == types.BeforeHelm || binding == types.AfterHelm || binding == types.AfterDeleteHelm {
			bc.Snapshots = moduleHook.GetHookController().KubernetesSnapshots()
			bc.Metadata.IncludeAllSnapshots = true
		}
		bc.Metadata.BindingType = binding

		metricLabels := map[string]string{
			"module":     bm.Name,
			"hook":       moduleHook.GetName(),
			"binding":    string(binding),
			"queue":      "main", // AfterHelm,BeforeHelm hooks always handle in main queue
			"activation": logLabels["event.type"],
		}

		func() {
			defer measure.Duration(func(d time.Duration) {
				bm.dc.MetricStorage.HistogramObserve("{PREFIX}module_hook_run_seconds", d.Seconds(), metricLabels, nil)
			})()
			err = bm.executeHook(moduleHook, binding, []binding_context.BindingContext{bc}, logLabels, metricLabels)
		}()
		if err != nil {
			return err
		}
	}

	return nil
}

func (bm *BasicModule) RunHookByName(hookName string, binding sh_op_types.BindingType, bindingContext []binding_context.BindingContext, logLabels map[string]string) (string, string, error) {
	values := bm.valuesStorage.GetValues()

	valuesChecksum := values.Checksum()

	moduleHook := bm.hooks.getHookByName(hookName)

	// Update kubernetes snapshots just before execute a hook
	// Note: BeforeHelm and AfterHelm are run by RunHookByBinding
	if binding == sh_op_types.OnKubernetesEvent || binding == sh_op_types.Schedule {
		bindingContext = moduleHook.GetHookController().UpdateSnapshots(bindingContext)
	}

	metricLabels := map[string]string{
		"module":     bm.Name,
		"hook":       hookName,
		"binding":    string(binding),
		"queue":      logLabels["queue"],
		"activation": logLabels["event.type"],
	}

	err := bm.executeHook(moduleHook, binding, bindingContext, logLabels, metricLabels)
	if err != nil {
		return "", "", err
	}

	newValuesChecksum := bm.valuesStorage.GetValues().Checksum()

	return valuesChecksum, newValuesChecksum, nil

}
func (bm *BasicModule) RunEnabledScript(tmpDir string, precedingEnabledModules []string, logLabels map[string]string) (bool, error) {
	// Copy labels and set 'module' label.
	logLabels = utils.MergeLabels(logLabels)
	logLabels["module"] = bm.Name

	logEntry := log.WithFields(utils.LabelsToLogFields(logLabels))
	enabledScriptPath := filepath.Join(bm.Path, "enabled")

	f, err := os.Stat(enabledScriptPath)
	if os.IsNotExist(err) {
		logEntry.Debugf("MODULE '%s' is ENABLED. Enabled script is not exist!", bm.Name)
		return true, nil
	} else if err != nil {
		logEntry.Errorf("Cannot stat enabled script '%s': %s", enabledScriptPath, err)
		return false, err
	}

	if !utils_file.IsFileExecutable(f) {
		logEntry.Errorf("Found non-executable enabled script '%s'", enabledScriptPath)
		return false, fmt.Errorf("non-executable enable script")
	}

	configValuesPath, err := bm.prepareConfigValuesJsonFile(tmpDir)
	if err != nil {
		logEntry.Errorf("Prepare CONFIG_VALUES_PATH file for '%s': %s", enabledScriptPath, err)
		return false, err
	}
	defer func() {
		if sh_app.DebugKeepTmpFiles == "yes" {
			return
		}
		err := os.Remove(configValuesPath)
		if err != nil {
			log.WithField("module", bm.Name).
				Errorf("Remove tmp file '%s': %s", configValuesPath, err)
		}
	}()

	valuesPath, err := bm.prepareValuesJsonFileForEnabledScript(tmpDir, precedingEnabledModules)
	if err != nil {
		logEntry.Errorf("Prepare VALUES_PATH file for '%s': %s", enabledScriptPath, err)
		return false, err
	}
	defer func() {
		if sh_app.DebugKeepTmpFiles == "yes" {
			return
		}
		err := os.Remove(valuesPath)
		if err != nil {
			log.WithField("module", bm.Name).
				Errorf("Remove tmp file '%s': %s", configValuesPath, err)
		}
	}()

	enabledResultFilePath, err := bm.prepareModuleEnabledResultFile(tmpDir)
	if err != nil {
		logEntry.Errorf("Prepare MODULE_ENABLED_RESULT file for '%s': %s", enabledScriptPath, err)
		return false, err
	}
	defer func() {
		if sh_app.DebugKeepTmpFiles == "yes" {
			return
		}
		err := os.Remove(enabledResultFilePath)
		if err != nil {
			log.WithField("module", bm.Name).
				Errorf("Remove tmp file '%s': %s", configValuesPath, err)
		}
	}()

	logEntry.Debugf("Execute enabled script '%s', preceding modules: %v", enabledScriptPath, precedingEnabledModules)

	envs := make([]string, 0)
	envs = append(envs, os.Environ()...)
	envs = append(envs, fmt.Sprintf("CONFIG_VALUES_PATH=%s", configValuesPath))
	envs = append(envs, fmt.Sprintf("VALUES_PATH=%s", valuesPath))
	envs = append(envs, fmt.Sprintf("MODULE_ENABLED_RESULT=%s", enabledResultFilePath))

	cmd := executor.MakeCommand("", enabledScriptPath, []string{}, envs)

	usage, err := executor.RunAndLogLines(cmd, logLabels)
	if usage != nil {
		// usage metrics
		metricLabels := map[string]string{
			"module":     bm.Name,
			"hook":       "enabled",
			"binding":    "enabled",
			"queue":      logLabels["queue"],
			"activation": logLabels["event.type"],
		}
		bm.dc.MetricStorage.HistogramObserve("{PREFIX}module_hook_run_sys_cpu_seconds", usage.Sys.Seconds(), metricLabels, nil)
		bm.dc.MetricStorage.HistogramObserve("{PREFIX}module_hook_run_user_cpu_seconds", usage.User.Seconds(), metricLabels, nil)
		bm.dc.MetricStorage.GaugeSet("{PREFIX}module_hook_run_max_rss_bytes", float64(usage.MaxRss)*1024, metricLabels)
	}
	if err != nil {
		logEntry.Errorf("Fail to run enabled script '%s': %s", enabledScriptPath, err)
		return false, err
	}

	moduleEnabled, err := bm.readModuleEnabledResult(enabledResultFilePath)
	if err != nil {
		logEntry.Errorf("Read enabled result from '%s': %s", enabledScriptPath, err)
		return false, fmt.Errorf("bad enabled result")
	}

	result := "Disabled"
	if moduleEnabled {
		result = "Enabled"
	}
	logEntry.Infof("Enabled script run successful, result '%v', module '%s'", moduleEnabled, result)
	return moduleEnabled, nil
}

func (bm *BasicModule) prepareValuesJsonFileForEnabledScript(tmpdir string, precedingEnabledModules []string) (string, error) {
	values, err := bm.valuesForEnabledScript(precedingEnabledModules)
	if err != nil {
		return "", err
	}
	return bm.prepareValuesJsonFileWith(tmpdir, values)
}

func (bm *BasicModule) prepareModuleEnabledResultFile(tmpdir string) (string, error) {
	path := filepath.Join(tmpdir, fmt.Sprintf("%s.module-enabled-result", bm.Name))
	if err := utils.CreateEmptyWritableFile(path); err != nil {
		return "", err
	}
	return path, nil
}

func (bm *BasicModule) readModuleEnabledResult(filePath string) (bool, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return false, fmt.Errorf("cannot read %s: %s", filePath, err)
	}

	value := strings.TrimSpace(string(data))

	if value == "true" {
		return true, nil
	} else if value == "false" {
		return false, nil
	}

	return false, fmt.Errorf("expected 'true' or 'false', got '%s'", value)
}

// VALUES_PATH
func (bm *BasicModule) prepareValuesJsonFileWith(tmpdir string, values utils.Values) (string, error) {
	data, err := values.JsonBytes()
	if err != nil {
		return "", err
	}

	path := filepath.Join(tmpdir, fmt.Sprintf("%s.module-values-%s.json", bm.SafeName(), uuid.Must(uuid.NewV4()).String()))
	err = utils.DumpData(path, data)
	if err != nil {
		return "", err
	}

	log.Debugf("Prepared module %s hook values:\n%s", bm.Name, values.DebugString())

	return path, nil
}

// ValuesForEnabledScript returns effective values for enabled script.
// There is enabledModules key in global section with previously enabled modules.
func (bm *BasicModule) valuesForEnabledScript(precedingEnabledModules []string) (utils.Values, error) {
	res := bm.valuesStorage.GetValues()

	// TODO: ??????
	res = mergeLayers(
		utils.Values{},
		res,
		utils.Values{
			"global": map[string]interface{}{
				"enabledModules": precedingEnabledModules,
			},
		},
	)
	return res, nil
}

func (bm *BasicModule) SafeName() string {
	return sanitize.BaseName(bm.Name)
}

// CONFIG_VALUES_PATH
func (bm *BasicModule) prepareConfigValuesJsonFile(tmpDir string) (string, error) {
	data, err := bm.valuesStorage.GetConfigValues().JsonBytes()
	if err != nil {
		return "", err
	}

	path := filepath.Join(tmpDir, fmt.Sprintf("%s.module-config-values-%s.json", bm.SafeName(), uuid.Must(uuid.NewV4()).String()))
	err = utils.DumpData(path, data)
	if err != nil {
		return "", err
	}

	log.Debugf("Prepared module %s hook config values:\n%s", bm.Name, bm.valuesStorage.GetConfigValues().DebugString())

	return path, nil
}

// instead on ModuleHook.Run
func (bm *BasicModule) executeHook(h *hooks.ModuleHook, bindingType sh_op_types.BindingType, context []binding_context.BindingContext, logLabels map[string]string, metricLabels map[string]string) error {
	logLabels = utils.MergeLabels(logLabels, map[string]string{
		"hook":      h.GetName(),
		"hook.type": "module",
		"binding":   string(bindingType),
	})

	logEntry := log.WithFields(utils.LabelsToLogFields(logLabels))

	logStartLevel := log.InfoLevel
	// Use Debug when run as a separate task for Kubernetes or Schedule hooks, as task start is already logged.
	// TODO log this message by callers.
	if bindingType == sh_op_types.OnKubernetesEvent || bindingType == sh_op_types.Schedule {
		logStartLevel = log.DebugLevel
	}
	logEntry.Logf(logStartLevel, "Module hook start %s/%s", bm.Name, h.GetName())

	for _, info := range h.GetHookController().SnapshotsInfo() {
		logEntry.Debugf("snapshot info: %s", info)
	}

	// TODO(yalosev): add some description here
	// why we have to add a module name key at top level
	hookConfigValues := utils.Values{
		bm.moduleNameForValues(): bm.valuesStorage.GetConfigValues(),
	}
	hookValues := utils.Values{
		bm.moduleNameForValues(): bm.valuesStorage.GetValues(),
	}

	hookResult, err := h.Execute(h.GetConfigVersion(), context, bm.SafeName(), hookConfigValues, hookValues, logLabels)
	if hookResult != nil && hookResult.Usage != nil {
		bm.dc.MetricStorage.HistogramObserve("{PREFIX}module_hook_run_sys_cpu_seconds", hookResult.Usage.Sys.Seconds(), metricLabels, nil)
		bm.dc.MetricStorage.HistogramObserve("{PREFIX}module_hook_run_user_cpu_seconds", hookResult.Usage.User.Seconds(), metricLabels, nil)
		bm.dc.MetricStorage.GaugeSet("{PREFIX}module_hook_run_max_rss_bytes", float64(hookResult.Usage.MaxRss)*1024, metricLabels)
	}
	if err != nil {
		// we have to check if there are some status patches to apply
		if hookResult != nil && len(hookResult.ObjectPatcherOperations) > 0 {
			statusPatchesErr := bm.dc.KubeObjectPatcher.ExecuteOperations(hookResult.ObjectPatcherOperations)
			if statusPatchesErr != nil {
				return fmt.Errorf("module hook '%s' failed: %s, update status operation failed: %s", h.GetName(), err, statusPatchesErr)
			}
		}
		return fmt.Errorf("module hook '%s' failed: %s", h.GetName(), err)
	}

	if len(hookResult.ObjectPatcherOperations) > 0 {
		err = bm.dc.KubeObjectPatcher.ExecuteOperations(hookResult.ObjectPatcherOperations)
		if err != nil {
			return err
		}
	}

	// Apply metric operations
	err = bm.dc.HookMetricsStorage.SendBatch(hookResult.Metrics, map[string]string{
		"hook":   h.GetName(),
		"module": bm.Name,
	})
	if err != nil {
		return err
	}

	// Apply binding actions. (Only Go hook for now).
	if h.GetKind() == kind.HookKindGo {
		err = h.ApplyBindingActions(hookResult.BindingActions)
		if err != nil {
			return err
		}
	}

	configValuesPatch, has := hookResult.Patches[utils.ConfigMapPatch]
	if has && configValuesPatch != nil {
		configValues := bm.valuesStorage.GetConfigValues()

		// Apply patch to get intermediate updated values.
		configValuesPatchResult, err := bm.handleModuleValuesPatch(configValues, *configValuesPatch)
		if err != nil {
			return fmt.Errorf("module hook '%s': kube module config values update error: %s", h.GetName(), err)
		}

		if configValuesPatchResult.ValuesChanged {
			logEntry.Debugf("Module hook '%s': validate module config values before update", h.GetName())
			// Validate merged static and new values.
			validationErr := bm.valuesStorage.PreCommitConfigValues(configValuesPatchResult.Values)
			if validationErr != nil {
				return multierror.Append(
					fmt.Errorf("cannot apply config values patch for module values"),
					validationErr,
				)
			}

			err := bm.dc.KubeConfigManager.SaveConfigValues(bm.Name, configValuesPatchResult.Values)
			if err != nil {
				logEntry.Debugf("Module hook '%s' kube module config values stay unchanged:\n%s", h.GetName(), bm.valuesStorage.GetConfigValues().DebugString())
				return fmt.Errorf("module hook '%s': set kube module config failed: %s", h.GetName(), err)
			}

			bm.valuesStorage.CommitConfigValues()

			logEntry.Debugf("Module hook '%s': kube module '%s' config values updated:\n%s", h.GetName(), bm.Name, bm.valuesStorage.GetConfigValues().DebugString())
		}
	}

	valuesPatch, has := hookResult.Patches[utils.MemoryValuesPatch]
	if has && valuesPatch != nil {
		currentValues := bm.valuesStorage.GetValues()

		// Apply patch to get intermediate updated values.
		valuesPatchResult, err := bm.handleModuleValuesPatch(currentValues, *valuesPatch)
		if err != nil {
			return fmt.Errorf("module hook '%s': dynamic module values update error: %s", h.GetName(), err)
		}
		if valuesPatchResult.ValuesChanged {
			logEntry.Debugf("Module hook '%s': validate module values before update", h.GetName())
			// Validate schema for updated module values
			validationErr := bm.valuesStorage.PreCommitValues(valuesPatchResult.Values)
			if validationErr != nil {
				return multierror.Append(
					fmt.Errorf("cannot apply values patch for module values"),
					validationErr,
				)
			}

			// Save patch set if everything is ok.
			bm.valuesStorage.AppendValuesPatch(valuesPatchResult.ValuesPatch)
			bm.valuesStorage.CommitValues()

			logEntry.Debugf("Module hook '%s': dynamic module '%s' values updated:\n%s", h.GetName(), bm.Name, bm.valuesStorage.GetValues().DebugString())
		}
	}

	logEntry.Debugf("Module hook success %s/%s", bm.Name, h.GetName())

	return nil
}

type moduleValuesMergeResult struct {
	// global values with root ModuleValuesKey key
	Values          utils.Values
	ModuleValuesKey string
	ValuesPatch     utils.ValuesPatch
	ValuesChanged   bool
}

// moduleNameForValues returns module name as camelCase
// Example:
//
//	my-super-module -> mySuperModule
func (bm *BasicModule) moduleNameForValues() string {
	return utils.ModuleNameToValuesKey(bm.Name)
}

func (bm *BasicModule) handleModuleValuesPatch(currentValues utils.Values, valuesPatch utils.ValuesPatch) (*moduleValuesMergeResult, error) {
	moduleValuesKey := bm.moduleNameForValues()

	if err := utils.ValidateHookValuesPatch(valuesPatch, moduleValuesKey); err != nil {
		return nil, fmt.Errorf("merge module '%s' values failed: %s", bm.Name, err)
	}

	// module values doesn't contain moduleName key as top level one
	// but patches are still have, we have to use temporary map
	// TODO: maybe we have to change ApplyValuesPatch function
	tmpValues := utils.Values{
		moduleValuesKey: currentValues,
	}

	// Apply new patches in Strict mode. Hook should not return 'remove' with nonexistent path.
	newValues, valuesChanged, err := utils.ApplyValuesPatch(tmpValues, valuesPatch, utils.Strict)
	if err != nil {
		return nil, fmt.Errorf("merge module '%s' values failed: %s", bm.Name, err)
	}

	switch v := newValues[moduleValuesKey].(type) {
	case utils.Values:
		newValues = v
	case map[string]interface{}:
		newValues = utils.Values(v)
	default:
		return nil, fmt.Errorf("unknown module values type: %T", v)
	}

	result := &moduleValuesMergeResult{
		ModuleValuesKey: moduleValuesKey,
		Values:          newValues,
		ValuesChanged:   valuesChanged,
		ValuesPatch:     valuesPatch,
	}

	return result, nil
}

func (bm *BasicModule) ValidateAndSaveConfigValues(v utils.Values) error {
	return bm.valuesStorage.PreCommitConfigValues(v)
}

func (bm *BasicModule) ConfigValuesHaveChanges() bool {
	return bm.valuesStorage.dirtyConfigValuesHasDiff()
}

func (bm *BasicModule) CommitConfigValuesChange() {
	bm.valuesStorage.CommitConfigValues()
}

func (bm *BasicModule) GetValues() utils.Values {
	return bm.valuesStorage.GetValues()
}

func (bm *BasicModule) GetConfigValues() utils.Values {
	return bm.valuesStorage.GetConfigValues()
}

// Synchronization xxx
// TODO: don't like this honestly, i think we can remake it
func (bm *BasicModule) Synchronization() *SynchronizationState {
	return bm.state.synchronizationState
}

// SynchronizationNeeded is true if module has at least one kubernetes hook
// with executeHookOnSynchronization.
// TODO: dont skip
func (bm *BasicModule) SynchronizationNeeded() bool {
	for _, modHook := range bm.hooks.byName {
		if modHook.SynchronizationNeeded() {
			return true
		}
	}
	return false
}

// HasKubernetesHooks is true if module has at least one kubernetes hook.
func (bm *BasicModule) HasKubernetesHooks() bool {
	hks := bm.hooks.getHooks(sh_op_types.OnKubernetesEvent)
	return len(hks) > 0
}

func (bm *BasicModule) GetHookByName(name string) *hooks.ModuleHook {
	return bm.hooks.getHookByName(name)
}

func (bm *BasicModule) GetLastHookError() error {
	bm.state.hookErrorsLock.RLock()
	defer bm.state.hookErrorsLock.RUnlock()

	for name, err := range bm.state.hookErrors {
		if err != nil {
			return fmt.Errorf("%s: %v", name, err)
		}
	}

	return nil
}

func (bm *BasicModule) GetModuleError() error {
	return bm.state.lastModuleErr
}

type ModuleRunPhase string

const (
	// Startup - module is just enabled.
	Startup ModuleRunPhase = "Startup"
	// OnStartupDone - onStartup hooks are done.
	OnStartupDone ModuleRunPhase = "OnStartupDone"
	// QueueSynchronizationTasks - should queue Synchronization tasks.
	QueueSynchronizationTasks ModuleRunPhase = "QueueSynchronizationTasks"
	// WaitForSynchronization - some Synchronization tasks are in queues, should wait for them to finish.
	WaitForSynchronization ModuleRunPhase = "WaitForSynchronization"
	// EnableScheduleBindings - enable schedule binding after Synchronization.
	EnableScheduleBindings ModuleRunPhase = "EnableScheduleBindings"
	// CanRunHelm - module is ready to run its Helm chart.
	CanRunHelm ModuleRunPhase = "CanRunHelm"
)

type moduleState struct {
	Enabled              bool
	Phase                ModuleRunPhase
	lastModuleErr        error
	hookErrors           map[string]error
	hookErrorsLock       sync.RWMutex
	synchronizationState *SynchronizationState
}

//
//// StaticAndNewValues returns global values defined in
//// various values.yaml files and in a ConfigMap and module values
//// defined in various values.yaml files merged with newValues.
//// Deprecated: think about it
//// TODO(yalosev): old style stuff, have to redo
//func (bm *BasicModule) StaticAndNewValues(newValues utils.Values) utils.Values {
//	return mergeLayers(
//		// Global values from values.yaml and ConfigMap with defaults from schema.
//		m.moduleManager.GlobalStaticAndConfigValues(),
//		// Init module section.
//		utils.Values{m.ValuesKey(): map[string]interface{}{}},
//		// Merge static values from various values.yaml files.
//		m.CommonStaticConfig.GetValues(),
//		m.StaticConfig.GetValues(),
//		// Apply config values defaults before overrides.
//		&applyDefaultsForModule{
//			m.ValuesKey(),
//			validation.ConfigValuesSchema,
//			m.moduleManager.ValuesValidator,
//		},
//		// Merge overrides from newValues.
//		newValues,
//	)
//}
