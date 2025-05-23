package modules

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/gofrs/uuid/v5"
	"github.com/hashicorp/go-multierror"
	"github.com/kennygrant/sanitize"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/flant/addon-operator/pkg"
	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/hook/types"
	environmentmanager "github.com/flant/addon-operator/pkg/module_manager/environment_manager"
	"github.com/flant/addon-operator/pkg/module_manager/models/hooks"
	"github.com/flant/addon-operator/pkg/module_manager/models/hooks/kind"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/addon-operator/pkg/values/validation"
	"github.com/flant/addon-operator/sdk"
	shapp "github.com/flant/shell-operator/pkg/app"
	"github.com/flant/shell-operator/pkg/executor"
	bindingcontext "github.com/flant/shell-operator/pkg/hook/binding_context"
	sh_op_types "github.com/flant/shell-operator/pkg/hook/types"
	utils_file "github.com/flant/shell-operator/pkg/utils/file"
	"github.com/flant/shell-operator/pkg/utils/measure"
)

// BasicModule is a basic representation of the Module, which addon-operator works with
// any Module has the next parameters:
//   - name of the module
//   - order of the module execution
//   - path of the module on a filesystem
//   - values storage - config and calculated values for the module
//   - hooks of the module
//   - current module state
type BasicModule struct {
	// required
	Name string
	// required
	Order uint32
	// required
	Path string

	crdsExist     bool
	crdFilesPaths []string

	valuesStorage *ValuesStorage

	hooks *HooksStorage

	// dependency
	dc *hooks.HookExecutionDependencyContainer

	keepTemporaryHookFiles bool

	logger *log.Logger

	l     sync.RWMutex
	state *moduleState
}

// TODO: add options WithLogger
// NewBasicModule creates new BasicModule
// staticValues - are values from modules/values.yaml and /modules/<module-name>/values.yaml, they could not be changed during the runtime
func NewBasicModule(name, path string, order uint32, staticValues utils.Values, configBytes, valuesBytes []byte, opts ...ModuleOption) (*BasicModule, error) {
	valuesStorage, err := NewValuesStorage(name, staticValues, configBytes, valuesBytes)
	if err != nil {
		return nil, fmt.Errorf("new values storage: %w", err)
	}

	crdsFromPath := getCRDsFromPath(path, app.CRDsFilters)
	bmodule := &BasicModule{
		Name:          name,
		Order:         order,
		Path:          path,
		crdsExist:     len(crdsFromPath) > 0,
		crdFilesPaths: crdsFromPath,
		valuesStorage: valuesStorage,
		state: &moduleState{
			Phase:                Startup,
			hookErrors:           make(map[string]error),
			synchronizationState: NewSynchronizationState(),
		},
		hooks:                  newHooksStorage(),
		keepTemporaryHookFiles: shapp.DebugKeepTmpFiles,
	}

	for _, opt := range opts {
		opt.Apply(bmodule)
	}

	if bmodule.logger == nil {
		bmodule.logger = log.NewLogger(log.Options{}).Named("basic-module").Named(name)
	}

	return bmodule, nil
}

func (bm *BasicModule) WithLogger(logger *log.Logger) {
	bm.logger = logger
}

// getCRDsFromPath scan path/crds directory and store yaml file in slice
// if file name do not start with `_` or `doc-` prefix
func getCRDsFromPath(path string, crdsFilters string) []string {
	var crdFilesPaths []string
	err := filepath.Walk(
		filepath.Join(path, "crds"),
		func(path string, _ os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if !matchPrefix(path, crdsFilters) && filepath.Ext(path) == ".yaml" {
				crdFilesPaths = append(crdFilesPaths, path)
			}

			return nil
		})
	if err != nil {
		return nil
	}

	return crdFilesPaths
}

func matchPrefix(path string, crdsFilters string) bool {
	filters := strings.Split(crdsFilters, ",")
	for _, filter := range filters {
		if strings.HasPrefix(filepath.Base(path), strings.TrimSpace(filter)) {
			return true
		}
	}

	return false
}

// WithDependencies inject module dependencies
func (bm *BasicModule) WithDependencies(dep *hooks.HookExecutionDependencyContainer) {
	bm.dc = dep
}

// GetOrder returns the module order
func (bm *BasicModule) GetOrder() uint32 {
	return bm.Order
}

// GetName returns the module name
func (bm *BasicModule) GetName() string {
	return bm.Name
}

// GetPath returns the module path on a filesystem
func (bm *BasicModule) GetPath() string {
	return bm.Path
}

// GetHooks returns module hooks, they could be filtered by BindingType optionally
func (bm *BasicModule) GetHooks(bt ...sh_op_types.BindingType) []*hooks.ModuleHook {
	return bm.hooks.getHooks(bt...)
}

// DeregisterHooks clean up all module hooks
func (bm *BasicModule) DeregisterHooks() {
	bm.hooks.clean()
}

// HooksControllersReady returns controllersReady status of the hook storage
func (bm *BasicModule) HooksControllersReady() bool {
	return bm.hooks.controllersReady
}

// SetHooksControllersReady sets controllersReady status of the hook storage to true
func (bm *BasicModule) SetHooksControllersReady() {
	bm.hooks.controllersReady = true
}

// ResetState drops the module state
func (bm *BasicModule) ResetState() {
	bm.l.Lock()
	var maintenanceState MaintenanceState

	if bm.state.maintenanceState == Unmanaged {
		maintenanceState = Unmanaged
	}

	bm.state = &moduleState{
		Phase:                Startup,
		hookErrors:           make(map[string]error),
		synchronizationState: NewSynchronizationState(),
		maintenanceState:     maintenanceState,
	}
	bm.l.Unlock()
}

// RegisterHooks searches and registers all module hooks from a filesystem or GoHook Registry
func (bm *BasicModule) RegisterHooks(logger *log.Logger) ([]*hooks.ModuleHook, error) {
	if bm.hooks.registered {
		logger.Debug("Module hooks already registered")
		return nil, nil
	}

	hks, err := bm.searchModuleHooks()
	if err != nil {
		return nil, fmt.Errorf("search module hooks failed: %w", err)
	}

	logger.Debug("Found hooks", slog.Int("count", len(hks)))
	if logger.GetLevel() == log.LevelDebug {
		for _, h := range hks {
			logger.Debug("ModuleHook",
				slog.String("name", h.GetName()),
				slog.String("path", h.GetPath()))
		}
	}

	logger.Debug("Register hooks")

	if err := bm.registerHooks(hks, logger); err != nil {
		return nil, fmt.Errorf("register hooks: %w", err)
	}

	bm.hooks.registered = true

	return hks, nil
}

func (bm *BasicModule) searchModuleHooks() ([]*hooks.ModuleHook, error) {
	shellHooks, err := bm.searchModuleShellHooks()
	if err != nil {
		return nil, fmt.Errorf("search module shell hooks: %w", err)
	}

	goHooks := bm.searchModuleGoHooks()

	batchHooks, err := bm.searchModuleBatchHooks()
	if err != nil {
		return nil, fmt.Errorf("search module batch hooks: %w", err)
	}

	if len(shellHooks)+len(batchHooks) > 0 {
		if err := bm.AssembleEnvironmentForModule(environmentmanager.ShellHookEnvironment); err != nil {
			return nil, fmt.Errorf("Assemble %q module's environment: %w", bm.GetName(), err)
		}
	}

	mHooks := make([]*hooks.ModuleHook, 0, len(shellHooks)+len(goHooks))

	for _, sh := range shellHooks {
		mh := hooks.NewModuleHook(sh)
		mHooks = append(mHooks, mh)
	}

	for _, gh := range goHooks {
		mh := hooks.NewModuleHook(gh)
		mHooks = append(mHooks, mh)
	}

	for _, bh := range batchHooks {
		mh := hooks.NewModuleHook(bh)
		mHooks = append(mHooks, mh)
	}

	sort.SliceStable(mHooks, func(i, j int) bool {
		return mHooks[i].GetPath() < mHooks[j].GetPath()
	})

	return mHooks, nil
}

func (bm *BasicModule) searchModuleShellHooks() ([]*kind.ShellHook, error) {
	hooksDir := filepath.Join(bm.Path, "hooks")
	if _, err := os.Stat(hooksDir); os.IsNotExist(err) {
		return nil, nil
	}

	hooksRelativePaths, err := utils_file.RecursiveGetExecutablePaths(hooksDir, hooksExcludedDir...)
	if err != nil {
		return nil, err
	}

	hks := make([]*kind.ShellHook, 0)

	// sort hooks by path
	sort.Strings(hooksRelativePaths)
	bm.logger.Debug("Hook paths",
		slog.Any("paths", hooksRelativePaths))

	var (
		checkPythonEnv           sync.Once
		discoveredPythonVenvPath string
	)

	for _, hookPath := range hooksRelativePaths {
		options := make([]kind.ShellHookOption, 0, 1)

		if filepath.Ext(hookPath) == ".py" {
			checkPythonEnv.Do(func() {
				f, err := os.Stat(filepath.Join(bm.Path, kind.PythonVenvPath, kind.PythonBinaryPath))
				if err == nil {
					if !f.IsDir() && f.Mode()&0o111 != 0 {
						discoveredPythonVenvPath = filepath.Join(bm.Path, kind.PythonVenvPath)
					}
				}
			})
			options = append(options, kind.WithPythonVenv(discoveredPythonVenvPath))
		}

		hookName, err := filepath.Rel(filepath.Dir(bm.Path), hookPath)
		if err != nil {
			return nil, fmt.Errorf("could not get hook name: %w", err)
		}

		if filepath.Ext(hookPath) == "" {
			_, err := kind.GetBatchHookConfig(bm.safeName(), hookPath)
			if err == nil {
				continue
			}

			bm.logger.Warn("get batch hook config", slog.String("hook_file_path", hookPath), log.Err(err))
		}

		shHook := kind.NewShellHook(hookName, hookPath, bm.safeName(), bm.keepTemporaryHookFiles, shapp.LogProxyHookJSON, bm.logger.Named("shell-hook"), options...)

		hks = append(hks, shHook)
	}

	return hks, nil
}

func (bm *BasicModule) searchModuleBatchHooks() ([]*kind.BatchHook, error) {
	hooksDir := filepath.Join(bm.Path, "hooks")
	if _, err := os.Stat(hooksDir); os.IsNotExist(err) {
		return nil, nil
	}

	hooksRelativePaths, err := RecursiveGetBatchHookExecutablePaths(bm.safeName(), hooksDir, bm.logger, hooksExcludedDir...)
	if err != nil {
		return nil, err
	}

	hks := make([]*kind.BatchHook, 0)

	// sort hooks by path
	sort.Strings(hooksRelativePaths)
	bm.logger.Debug("sorted paths", slog.Any("paths", hooksRelativePaths))

	for _, hookPath := range hooksRelativePaths {
		hookName, err := filepath.Rel(filepath.Dir(bm.Path), hookPath)
		if err != nil {
			return nil, fmt.Errorf("could not get hook name: %w", err)
		}

		sdkcfgs, err := kind.GetBatchHookConfig(bm.safeName(), hookPath)
		if err != nil {
			return nil, fmt.Errorf("getting sdk config for '%s': %w", hookName, err)
		}

		for idx, cfg := range sdkcfgs {
			nestedHookName := fmt.Sprintf("%s:%s:%d", hookName, cfg.Metadata.Name, idx)
			shHook := kind.NewBatchHook(nestedHookName, hookPath, bm.safeName(), uint(idx), bm.keepTemporaryHookFiles, shapp.LogProxyHookJSON, bm.logger.Named("batch-hook"))

			hks = append(hks, shHook)
		}
	}

	return hks, nil
}

func RecursiveGetBatchHookExecutablePaths(moduleName, dir string, logger *log.Logger, excludedDirs ...string) ([]string, error) {
	paths := make([]string, 0)
	excludedDirs = append(excludedDirs, "lib")
	err := filepath.Walk(dir, func(path string, f os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if f.IsDir() {
			// Skip hidden and lib directories inside initial directory
			if strings.HasPrefix(f.Name(), ".") || slices.Contains(excludedDirs, f.Name()) {
				return filepath.SkipDir
			}

			return nil
		}

		if err := isExecutableBatchHookFile(moduleName, path, f); err != nil {
			if errors.Is(err, ErrFileNoExecutablePermissions) {
				logger.Warn("file is skipped", slog.String("path", path), log.Err(err))

				return nil
			}

			logger.Debug("file is skipped", slog.String("path", path), log.Err(err))

			return nil
		}

		paths = append(paths, path)

		return nil
	})
	if err != nil {
		return nil, err
	}

	return paths, nil
}

var (
	ErrFileHasWrongExtension       = errors.New("file has wrong extension")
	ErrFileIsNotBatchHook          = errors.New("file is not batch hook")
	ErrFileNoExecutablePermissions = errors.New("no executable permissions, chmod +x is required to run this hook")

	// the lisf of subdirectories to exclude when searching for a module's hooks
	hooksExcludedDir = []string{"venv"}
)

func isExecutableBatchHookFile(moduleName, path string, f os.FileInfo) error {
	switch filepath.Ext(f.Name()) {
	// ignore any extension and hidden files
	case "":
		return IsFileBatchHook(moduleName, path, f)
	// ignore all with extensions
	default:
		return ErrFileHasWrongExtension
	}
}

var compiledHooksFound = regexp.MustCompile(`Found ([1-9]|[1-9]\d|[1-9]\d\d|[1-9]\d\d\d) items`)

func IsFileBatchHook(moduleName, path string, f os.FileInfo) error {
	if f.Mode()&0o111 == 0 {
		return ErrFileNoExecutablePermissions
	}

	// TODO: check binary another way
	args := []string{"hook", "list"}

	cmd := executor.NewExecutor(
		"",
		path,
		args,
		[]string{}).
		WithChroot(utils.GetModuleChrootPath(moduleName))

	o, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("exec file '%s': %w", path, err)
	}

	if compiledHooksFound.Match(o) {
		return nil
	}

	return ErrFileIsNotBatchHook
}

func (bm *BasicModule) searchModuleGoHooks() []*kind.GoHook {
	// find module hooks in go hooks registry
	return sdk.Registry().GetModuleHooks(bm.GetName())
}

func (bm *BasicModule) registerHooks(hks []*hooks.ModuleHook, logger *log.Logger) error {
	for _, moduleHook := range hks {
		hookLogEntry := logger.With("hook", moduleHook.GetName()).
			With("hook.type", "module")

		// TODO: we could make multierr here and return all config errors at once
		err := moduleHook.InitializeHookConfig()
		if err != nil {
			return fmt.Errorf("`%s` module hook `%s` --config invalid: %w", bm.GetName(), moduleHook.GetName(), err)
		}

		bm.logger.Debug("module hook config print", slog.String("module_name", bm.GetName()), slog.String("hook_name", moduleHook.GetName()), slog.Any("config", moduleHook.GetHookConfig().V1))

		// Add hook info as log labels
		for _, kubeCfg := range moduleHook.GetHookConfig().OnKubernetesEvents {
			kubeCfg.Monitor.Metadata.LogLabels["module"] = bm.GetName()
			kubeCfg.Monitor.Metadata.LogLabels["hook"] = moduleHook.GetName()
			kubeCfg.Monitor.Metadata.LogLabels["hook.type"] = "module"
			kubeCfg.Monitor.Metadata.MetricLabels = map[string]string{
				"hook":               moduleHook.GetName(),
				pkg.MetricKeyBinding: kubeCfg.BindingName,
				"module":             bm.GetName(),
				"queue":              kubeCfg.Queue,
				"kind":               kubeCfg.Monitor.Kind,
			}
		}

		// register module hook in indexes
		bm.hooks.AddHook(moduleHook)

		hookLogEntry.Debug("Module hook",
			slog.String("path", moduleHook.GetPath()),
			slog.String("bindings", moduleHook.GetConfigDescription()))
	}

	return nil
}

// GetPhase ...
func (bm *BasicModule) GetPhase() ModuleRunPhase {
	bm.l.RLock()
	defer bm.l.RUnlock()
	return bm.state.Phase
}

// SetPhase ...
func (bm *BasicModule) SetPhase(phase ModuleRunPhase) {
	bm.l.Lock()
	bm.state.Phase = phase
	bm.l.Unlock()
}

// SetError ...
func (bm *BasicModule) SetError(err error) {
	bm.l.Lock()
	bm.state.lastModuleErr = err
	bm.l.Unlock()
}

// SetStateEnabled ...
func (bm *BasicModule) SetStateEnabled(e bool) {
	bm.l.Lock()
	bm.state.Enabled = e
	bm.l.Unlock()
}

// SaveHookError ...
func (bm *BasicModule) SaveHookError(hookName string, err error) {
	bm.l.Lock()
	bm.state.hookErrors[hookName] = err
	bm.l.Unlock()
}

func (bm *BasicModule) SetMaintenanceState(state utils.Maintenance) {
	bm.l.Lock()
	switch state {
	case utils.NoResourceReconciliation:
		if bm.state.maintenanceState == Managed {
			bm.state.maintenanceState = Unmanaged
		}
	case utils.Managed:
		if bm.state.maintenanceState != Managed {
			bm.state.maintenanceState = Managed
		}
	}
	bm.l.Unlock()
}

func (bm *BasicModule) SetUnmanaged() {
	bm.l.Lock()
	if bm.state.maintenanceState == Managed {
		bm.state.maintenanceState = Unmanaged
	}
	bm.l.Unlock()
}

func (bm *BasicModule) GetMaintenanceState() MaintenanceState {
	bm.l.RLock()
	defer bm.l.RUnlock()

	return bm.state.maintenanceState
}

// RunHooksByBinding gets all hooks for binding, for each hook it creates a BindingContext,
// sets KubernetesSnapshots and runs the hook.
func (bm *BasicModule) RunHooksByBinding(ctx context.Context, binding sh_op_types.BindingType, logLabels map[string]string) error {
	var err error
	moduleHooks := bm.GetHooks(binding)

	for _, moduleHook := range moduleHooks {
		// TODO: This looks like a bug. It will block all hooks of the module
		err = moduleHook.RateLimitWait(context.Background())
		if err != nil {
			// This could happen when the Context is
			// canceled, or the expected wait time exceeds the Context's Deadline.
			// The best we can do without proper context usage is to repeat the task.
			return err
		}

		bc := bindingcontext.BindingContext{
			Binding: string(binding),
		}
		// Update kubernetes snapshots just before execute a hook
		if binding == types.BeforeHelm || binding == types.AfterHelm || binding == types.AfterDeleteHelm {
			bc.Snapshots = moduleHook.GetHookController().KubernetesSnapshots()
			bc.Metadata.IncludeAllSnapshots = true
		}
		bc.Metadata.BindingType = binding

		metricLabels := map[string]string{
			"module":                bm.GetName(),
			"hook":                  moduleHook.GetName(),
			pkg.MetricKeyBinding:    string(binding),
			"queue":                 "main", // AfterHelm,BeforeHelm hooks always handle in main queue
			pkg.MetricKeyActivation: logLabels[pkg.LogKeyEventType],
		}

		func() {
			defer measure.Duration(func(d time.Duration) {
				bm.dc.MetricStorage.HistogramObserve("{PREFIX}module_hook_run_seconds", d.Seconds(), metricLabels, nil)
			})()
			err = bm.executeHook(ctx, moduleHook, binding, []bindingcontext.BindingContext{bc}, logLabels, metricLabels)
		}()
		if err != nil {
			return err
		}
	}

	return nil
}

// RunHookByName runs some specified hook by its name
func (bm *BasicModule) RunHookByName(ctx context.Context, hookName string, binding sh_op_types.BindingType, bindingContext []bindingcontext.BindingContext, logLabels map[string]string) (string, string, error) {
	values := bm.valuesStorage.GetValues(false)
	valuesChecksum := values.Checksum()

	moduleHook := bm.hooks.getHookByName(hookName)

	// Update kubernetes snapshots just before execute a hook
	// Note: BeforeHelm and AfterHelm are run by RunHookByBinding
	if binding == sh_op_types.OnKubernetesEvent || binding == sh_op_types.Schedule {
		bindingContext = moduleHook.GetHookController().UpdateSnapshots(bindingContext)
	}

	metricLabels := map[string]string{
		"module":                bm.GetName(),
		"hook":                  hookName,
		pkg.MetricKeyBinding:    string(binding),
		"queue":                 logLabels["queue"],
		pkg.MetricKeyActivation: logLabels[pkg.LogKeyEventType],
	}

	err := bm.executeHook(ctx, moduleHook, binding, bindingContext, logLabels, metricLabels)
	if err != nil {
		return "", "", err
	}

	newValuesChecksum := bm.valuesStorage.GetValues(false).Checksum()

	return valuesChecksum, newValuesChecksum, nil
}

// RunEnabledScript execute enabled script
func (bm *BasicModule) RunEnabledScript(ctx context.Context, tmpDir string, precedingEnabledModules []string, logLabels map[string]string) (bool, error) {
	// Copy labels and set 'module' label.
	logLabels = utils.MergeLabels(logLabels)
	logLabels["module"] = bm.GetName()

	logEntry := utils.EnrichLoggerWithLabels(bm.logger, logLabels)
	enabledScriptPath := filepath.Join(bm.Path, "enabled")
	configValuesPath, err := bm.prepareConfigValuesJsonFile(tmpDir)
	if err != nil {
		logEntry.Error("Prepare CONFIG_VALUES_PATH file",
			slog.String("path", enabledScriptPath),
			log.Err(err))
		return false, err
	}
	defer func() {
		if bm.keepTemporaryHookFiles {
			return
		}
		err := os.Remove(configValuesPath)
		if err != nil {
			bm.logger.With("module", bm.GetName()).
				Error("Remove tmp file",
					slog.String("path", enabledScriptPath),
					log.Err(err))
		}
	}()

	valuesPath, err := bm.prepareValuesJsonFileForEnabledScript(tmpDir, precedingEnabledModules)
	if err != nil {
		logEntry.Error("Prepare VALUES_PATH file",
			slog.String("path", enabledScriptPath),
			log.Err(err))
		return false, err
	}
	defer func() {
		if bm.keepTemporaryHookFiles {
			return
		}
		err := os.Remove(valuesPath)
		if err != nil {
			bm.logger.With("module", bm.GetName()).
				Error("Remove tmp file",
					slog.String("path", configValuesPath),
					log.Err(err))
		}
	}()

	enabledResultFilePath, err := bm.prepareModuleEnabledResultFile(tmpDir)
	if err != nil {
		logEntry.Error("Prepare MODULE_ENABLED_RESULT file",
			slog.String("path", enabledScriptPath),
			log.Err(err))
		return false, err
	}
	defer func() {
		if bm.keepTemporaryHookFiles {
			return
		}
		err := os.Remove(enabledResultFilePath)
		if err != nil {
			bm.logger.With("module", bm.GetName()).
				Error("Remove tmp file",
					slog.String("path", configValuesPath),
					log.Err(err))
		}
	}()

	logEntry.Debug("Execute enabled script",
		slog.String("path", enabledScriptPath),
		slog.Any("modules", precedingEnabledModules))

	envs := make([]string, 0)
	envs = append(envs, os.Environ()...)
	envs = append(envs, fmt.Sprintf("CONFIG_VALUES_PATH=%s", configValuesPath))
	envs = append(envs, fmt.Sprintf("VALUES_PATH=%s", valuesPath))
	envs = append(envs, fmt.Sprintf("MODULE_ENABLED_RESULT=%s", enabledResultFilePath))

	if err := bm.AssembleEnvironmentForModule(environmentmanager.EnabledScriptEnvironment); err != nil {
		return false, fmt.Errorf("Assemble %q module's environment: %w", bm.GetName(), err)
	}

	cmd := executor.NewExecutor(
		"",
		enabledScriptPath,
		[]string{},
		envs).
		WithLogger(bm.logger.Named("executor")).
		WithCMDStdout(nil).
		WithChroot(utils.GetModuleChrootPath(bm.GetName()))

	usage, err := cmd.RunAndLogLines(ctx, logLabels)
	if usage != nil {
		// usage metrics
		metricLabels := map[string]string{
			"module":                bm.GetName(),
			"hook":                  "enabled",
			pkg.MetricKeyBinding:    "enabled",
			"queue":                 logLabels["queue"],
			pkg.MetricKeyActivation: logLabels[pkg.LogKeyEventType],
		}
		bm.dc.MetricStorage.HistogramObserve("{PREFIX}module_hook_run_sys_cpu_seconds", usage.Sys.Seconds(), metricLabels, nil)
		bm.dc.MetricStorage.HistogramObserve("{PREFIX}module_hook_run_user_cpu_seconds", usage.User.Seconds(), metricLabels, nil)
		bm.dc.MetricStorage.GaugeSet("{PREFIX}module_hook_run_max_rss_bytes", float64(usage.MaxRss)*1024, metricLabels)
	}
	if err != nil {
		logEntry.Error("Fail to run enabled script",
			slog.String("path", enabledScriptPath),
			log.Err(err))
		return false, err
	}

	moduleEnabled, err := bm.readModuleEnabledResult(enabledResultFilePath)
	if err != nil {
		logEntry.Error("Read enabled result",
			slog.String("path", enabledScriptPath),
			log.Err(err))
		return false, fmt.Errorf("bad enabled result")
	}

	result := "Disabled"
	if moduleEnabled {
		result = "Enabled"
	}
	logEntry.Info("Enabled script run successful",
		slog.Bool("result", moduleEnabled),
		slog.String("status", result))
	bm.l.Lock()
	bm.state.enabledScriptResult = &moduleEnabled
	bm.l.Unlock()
	return moduleEnabled, nil
}

func (bm *BasicModule) prepareValuesJsonFileForEnabledScript(tmpdir string, precedingEnabledModules []string) (string, error) {
	values := bm.valuesForEnabledScript(precedingEnabledModules)
	return bm.prepareValuesJsonFileWith(tmpdir, values)
}

func (bm *BasicModule) prepareModuleEnabledResultFile(tmpdir string) (string, error) {
	path := filepath.Join(tmpdir, fmt.Sprintf("%s.module-enabled-result", bm.GetName()))
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

	switch value {
	case "true":
		return true, nil
	case "false":
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

	path := filepath.Join(tmpdir, fmt.Sprintf("%s.module-values-%s.json", bm.safeName(), uuid.Must(uuid.NewV4()).String()))
	err = utils.DumpData(path, data)
	if err != nil {
		return "", err
	}

	bm.logger.Debug("Prepared module hook values",
		slog.String("module", bm.GetName()),
		slog.String("values", values.DebugString()))

	return path, nil
}

// ValuesForEnabledScript returns effective values for enabled script.
// There is enabledModules key in global section with previously enabled modules.
func (bm *BasicModule) valuesForEnabledScript(precedingEnabledModules []string) utils.Values {
	res := bm.valuesStorage.GetValues(true)

	res = mergeLayers(
		utils.Values{},
		res,
		bm.dc.GlobalValuesGetter.GetValues(true),
		utils.Values{
			"global": map[string]interface{}{
				"enabledModules": precedingEnabledModules,
			},
		},
	)
	return res
}

func (bm *BasicModule) safeName() string {
	return sanitize.BaseName(bm.GetName())
}

// CONFIG_VALUES_PATH
func (bm *BasicModule) prepareConfigValuesJsonFile(tmpDir string) (string, error) {
	v := utils.Values{
		"global":                 bm.dc.GlobalValuesGetter.GetConfigValues(false),
		bm.moduleNameForValues(): bm.GetConfigValues(false),
	}

	data, err := v.JsonBytes()
	if err != nil {
		return "", err
	}

	path := filepath.Join(tmpDir, fmt.Sprintf("%s.module-config-values-%s.json", bm.safeName(), uuid.Must(uuid.NewV4()).String()))
	err = utils.DumpData(path, data)
	if err != nil {
		return "", err
	}

	bm.logger.Debug("Prepared module hook config values",
		slog.String("module", bm.GetName()),
		slog.String("values", v.DebugString()))

	return path, nil
}

// instead on ModuleHook.Run
func (bm *BasicModule) executeHook(ctx context.Context, h *hooks.ModuleHook, bindingType sh_op_types.BindingType, bctx []bindingcontext.BindingContext, logLabels map[string]string, metricLabels map[string]string) error {
	ctx, span := otel.Tracer("bm-"+bm.GetName()).Start(ctx, "executeHook")
	defer span.End()

	span.SetAttributes(
		attribute.String("module_name", bm.GetName()),
		attribute.String("hook_name", h.GetName()),
	)

	logLabels = utils.MergeLabels(logLabels, map[string]string{
		"hook":            h.GetName(),
		"hook.type":       "module",
		pkg.LogKeyBinding: string(bindingType),
	})

	logEntry := utils.EnrichLoggerWithLabels(bm.logger, logLabels)

	logStartLevel := log.LevelInfo
	// Use Debug when run as a separate task for Kubernetes or Schedule hooks, as task start is already logged.
	// TODO log this message by callers.
	if bindingType == sh_op_types.OnKubernetesEvent || bindingType == sh_op_types.Schedule {
		logStartLevel = log.LevelDebug
	}
	logEntry.Log(ctx, logStartLevel.Level(), "Module hook start", slog.String(bm.GetName(), h.GetName()))

	for _, info := range h.GetHookController().SnapshotsInfo() {
		logEntry.Debug("snapshot info",
			slog.String("value", info))
	}

	prefixedConfigValues := bm.valuesStorage.GetConfigValues(true)
	prefixedValues := bm.valuesStorage.GetValues(true)
	valuesModuleName := bm.moduleNameForValues()
	configValues := prefixedConfigValues.GetKeySection(valuesModuleName)
	values := prefixedValues.GetKeySection(valuesModuleName)

	// we have to add a module name key at top level
	// because all hooks are living with an old scheme
	hookConfigValues := utils.Values{
		utils.GlobalValuesKey:    bm.dc.GlobalValuesGetter.GetConfigValues(false),
		bm.moduleNameForValues(): configValues,
	}
	hookValues := utils.Values{
		utils.GlobalValuesKey:    bm.dc.GlobalValuesGetter.GetValues(false),
		bm.moduleNameForValues(): values,
	}

	hookResult, err := h.Execute(ctx, h.GetConfigVersion(), bctx, bm.safeName(), hookConfigValues, hookValues, logLabels)
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
		"module": bm.GetName(),
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
		// Apply patch to get intermediate updated values.
		configValuesPatchResult, err := bm.handleModuleValuesPatch(prefixedConfigValues, *configValuesPatch)
		if err != nil {
			return fmt.Errorf("module hook '%s': kube module config values update error: %s", h.GetName(), err)
		}

		if configValuesPatchResult.ValuesChanged {
			logEntry.Debug("Module hook: validate module config values before update",
				slog.String("module", h.GetName()))
			// Validate merged static and new values.
			newValues, validationErr := bm.valuesStorage.GenerateNewConfigValues(configValuesPatchResult.Values, true)
			if validationErr != nil {
				return multierror.Append(
					fmt.Errorf("cannot apply config values patch for module values"),
					validationErr,
				)
			}

			err := bm.dc.KubeConfigManager.SaveConfigValues(bm.GetName(), configValuesPatchResult.Values)
			if err != nil {
				logEntry.Debug("Module hook kube module config values stay unchanged",
					slog.String("module", h.GetName()),
					slog.String("values", bm.valuesStorage.GetConfigValues(false).DebugString()))
				return fmt.Errorf("module hook '%s': set kube module config failed: %s", h.GetName(), err)
			}

			bm.valuesStorage.SaveConfigValues(newValues)

			logEntry.Debug("Module hook: kube module config values updated",
				slog.String("hook", h.GetName()),
				slog.String("module", bm.GetName()),
				slog.String("values", bm.valuesStorage.GetConfigValues(false).DebugString()))
		}
	}

	valuesPatch, has := hookResult.Patches[utils.MemoryValuesPatch]
	if has && valuesPatch != nil {
		// Apply patch to get intermediate updated values.
		valuesPatchResult, err := bm.handleModuleValuesPatch(prefixedValues, *valuesPatch)
		if err != nil {
			return fmt.Errorf("module hook '%s': dynamic module values update error: %s", h.GetName(), err)
		}
		if valuesPatchResult.ValuesChanged {
			logEntry.Debug("Module hook: validate module values before update",
				slog.String("module", h.GetName()))

			// Validate schema for updated module values
			validationErr := bm.valuesStorage.validateValues(valuesPatchResult.Values)
			if validationErr != nil {
				return multierror.Append(
					fmt.Errorf("cannot apply values patch for module values"),
					validationErr,
				)
			}

			// Save patch set if everything is ok.
			bm.valuesStorage.appendValuesPatch(valuesPatchResult.ValuesPatch)
			err = bm.valuesStorage.CommitValues()
			if err != nil {
				return fmt.Errorf("error on commit values: %w", err)
			}

			logEntry.Debug("Module hook: dynamic module values updated",
				slog.String("hook", h.GetName()),
				slog.String("module", bm.GetName()),
				slog.String("values", bm.valuesStorage.GetValues(false).DebugString()))
		}
	}

	logEntry.Debug("Module hook success",
		slog.String("module", bm.GetName()),
		slog.String("hook", h.GetName()))

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
	return utils.ModuleNameToValuesKey(bm.GetName())
}

func (bm *BasicModule) handleModuleValuesPatch(currentValues utils.Values, valuesPatch utils.ValuesPatch) (*moduleValuesMergeResult, error) {
	moduleValuesKey := bm.moduleNameForValues()

	if err := utils.ValidateHookValuesPatch(valuesPatch, moduleValuesKey); err != nil {
		return nil, fmt.Errorf("merge module '%s' values failed: %s", bm.GetName(), err)
	}

	// Apply new patches in Strict mode. Hook should not return 'remove' with nonexistent path.
	newValues, valuesChanged, err := utils.ApplyValuesPatch(currentValues, valuesPatch, utils.Strict)
	if err != nil {
		return nil, fmt.Errorf("merge module '%s' values failed: %s", bm.GetName(), err)
	}

	switch v := newValues[moduleValuesKey].(type) {
	case utils.Values:
		newValues = v
	case map[string]interface{}:
		newValues = v
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

func (bm *BasicModule) GenerateNewConfigValues(kubeConfigValues utils.Values, validate bool) (utils.Values, error) {
	return bm.valuesStorage.GenerateNewConfigValues(kubeConfigValues, validate)
}

func (bm *BasicModule) SaveConfigValues(configV utils.Values) {
	bm.valuesStorage.SaveConfigValues(configV)
}

func (bm *BasicModule) GetValues(withPrefix bool) utils.Values {
	return bm.valuesStorage.GetValues(withPrefix)
}

func (bm *BasicModule) GetConfigValues(withPrefix bool) utils.Values {
	return bm.valuesStorage.GetConfigValues(withPrefix)
}

// Synchronization xxx
// TODO: don't like this honestly, i think we can remake it
func (bm *BasicModule) Synchronization() *SynchronizationState {
	bm.l.RLock()
	defer bm.l.RUnlock()
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

// GetHookByName returns hook by its name
func (bm *BasicModule) GetHookByName(name string) *hooks.ModuleHook {
	return bm.hooks.getHookByName(name)
}

// GetValuesPatches returns patches for debug output
func (bm *BasicModule) GetValuesPatches() []utils.ValuesPatch {
	return bm.valuesStorage.getValuesPatches()
}

// GetHookErrorsSummary get hooks errors summary report
func (bm *BasicModule) GetHookErrorsSummary() string {
	bm.l.RLock()
	defer bm.l.RUnlock()

	hooksState := make([]string, 0, len(bm.state.hookErrors))
	for name, err := range bm.state.hookErrors {
		errorMsg := fmt.Sprint(err)
		if err == nil {
			errorMsg = "ok"
		}

		hooksState = append(hooksState, fmt.Sprintf("%s: %s", name, errorMsg))
	}

	sort.Strings(hooksState)
	return strings.Join(hooksState, "\n")
}

// GetEnabledScriptResult returns a bool pointer to the enabled script results
func (bm *BasicModule) GetEnabledScriptResult() *bool {
	bm.l.RLock()
	defer bm.l.RUnlock()
	return bm.state.enabledScriptResult
}

// GetLastHookError get error of the last executed hook
func (bm *BasicModule) GetLastHookError() error {
	bm.l.RLock()
	defer bm.l.RUnlock()

	for name, err := range bm.state.hookErrors {
		if err != nil {
			return fmt.Errorf("%s: %v", name, err)
		}
	}

	return nil
}

func (bm *BasicModule) GetModuleError() error {
	bm.l.RLock()
	defer bm.l.RUnlock()
	return bm.state.lastModuleErr
}

func (bm *BasicModule) GetValuesStorage() *ValuesStorage {
	return bm.valuesStorage
}

// GetSchemaStorage returns current schema storage of the basic module
func (bm *BasicModule) GetSchemaStorage() *validation.SchemaStorage {
	return bm.valuesStorage.schemaStorage
}

// ApplyNewSchemaStorage updates schema storage of the basic module
func (bm *BasicModule) ApplyNewSchemaStorage(schema *validation.SchemaStorage) error {
	return bm.valuesStorage.applyNewSchemaStorage(schema)
}

func (bm *BasicModule) Validate() error {
	valuesKey := utils.ModuleNameToValuesKey(bm.GetName())
	restoredName := utils.ModuleNameFromValuesKey(valuesKey)

	bm.logger.Info("Validating module",
		slog.String("module", bm.GetName()),
		slog.String("path", bm.GetPath()))

	if bm.GetName() != restoredName {
		return fmt.Errorf("'%s' name should be in kebab-case and be restorable from camelCase: consider renaming to '%s'", bm.GetName(), restoredName)
	}

	err := bm.ValidateValues()
	if err != nil {
		return fmt.Errorf("validate values: %w", err)
	}

	err = bm.ValidateConfigValues()
	if err != nil {
		return fmt.Errorf("validate config values: %w", err)
	}

	return nil
}

func (bm *BasicModule) DisassembleEnvironmentForModule() error {
	if bm.dc.EnvironmentManager != nil {
		return bm.dc.EnvironmentManager.DisassembleEnvironmentForModule(bm.GetName(), bm.Path, environmentmanager.NoEnvironment)
	}

	return nil
}

func (bm *BasicModule) AssembleEnvironmentForModule(targetEnvironment environmentmanager.Environment) error {
	if bm.dc.EnvironmentManager != nil {
		return bm.dc.EnvironmentManager.AssembleEnvironmentForModule(bm.GetName(), bm.Path, targetEnvironment)
	}

	return nil
}

func (bm *BasicModule) ValidateValues() error {
	return bm.valuesStorage.validateValues(bm.GetValues(false))
}

func (bm *BasicModule) ValidateConfigValues() error {
	return bm.valuesStorage.validateConfigValues(bm.GetConfigValues(false))
}

func (bm *BasicModule) GetCRDFilesPaths() []string {
	return bm.crdFilesPaths
}

func (bm *BasicModule) CRDExist() bool {
	return bm.crdsExist
}

type ModuleRunPhase string

const (
	// Startup - module is just enabled.
	Startup ModuleRunPhase = "Startup"
	// OnStartupDone - onStartup hooks have completed execution.
	OnStartupDone ModuleRunPhase = "OnStartupDone"
	// QueueSynchronizationTasks - synchronization tasks should be queued.
	QueueSynchronizationTasks ModuleRunPhase = "QueueSynchronizationTasks"
	// WaitForSynchronization - synchronization tasks are in queues, waiting for them to complete.
	WaitForSynchronization ModuleRunPhase = "WaitForSynchronization"
	// EnableScheduleBindings - enable schedule bindings after synchronization is complete.
	EnableScheduleBindings ModuleRunPhase = "EnableScheduleBindings"
	// CanRunHelm - module is ready to run its Helm chart.
	CanRunHelm ModuleRunPhase = "CanRunHelm"
	// HooksDisabled - module has its hooks disabled (before update or deletion).
	HooksDisabled ModuleRunPhase = "HooksDisabled"

	// Ready - all phases are complete.
	// This is the final phase, which indicates that the ModuleRun task completed without errors.
	// Note: This only confirms the module's own execution succeeded, not the status of any resources it created.
	// Custom readiness checks can be implemented separately if needed.
	Ready ModuleRunPhase = "Ready"
)

type MaintenanceState int

const (
	// Module runs in a normal mode
	Managed MaintenanceState = iota
	// All consequent helm runs are inhibited (heritage labels are removed and resource informer is stopped)
	Unmanaged = 1
)

type moduleState struct {
	Enabled              bool
	maintenanceState     MaintenanceState
	Phase                ModuleRunPhase
	lastModuleErr        error
	hookErrors           map[string]error
	synchronizationState *SynchronizationState
	enabledScriptResult  *bool
}
