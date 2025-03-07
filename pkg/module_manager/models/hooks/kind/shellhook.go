package kind

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/go-openapi/spec"
	"github.com/gofrs/uuid/v5"
	"gopkg.in/yaml.v3"

	gohook "github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/pkg/utils"
	shapp "github.com/flant/shell-operator/pkg/app"
	"github.com/flant/shell-operator/pkg/executor"
	sh_hook "github.com/flant/shell-operator/pkg/hook"
	bindingcontext "github.com/flant/shell-operator/pkg/hook/binding_context"
	"github.com/flant/shell-operator/pkg/hook/config"
	"github.com/flant/shell-operator/pkg/hook/controller"
	objectpatch "github.com/flant/shell-operator/pkg/kube/object_patch"
	metricoperation "github.com/flant/shell-operator/pkg/metric_storage/operation"
)

var _ gohook.HookConfigLoader = (*ShellHook)(nil)

type ShellHook struct {
	moduleName string
	sh_hook.Hook

	ScheduleConfig *HookScheduleConfig
}

// NewShellHook new hook, which runs via the OS interpreter like bash/python/etc
func NewShellHook(name, path, moduleName string, keepTemporaryHookFiles bool, logProxyHookJSON bool, logger *log.Logger) *ShellHook {
	return &ShellHook{
		moduleName: moduleName,
		Hook: sh_hook.Hook{
			Name:                   name,
			Path:                   path,
			KeepTemporaryHookFiles: keepTemporaryHookFiles,
			LogProxyHookJSON:       logProxyHookJSON,
			Logger:                 logger,
		},
		ScheduleConfig: &HookScheduleConfig{},
	}
}

// BackportHookConfig for shell-operator to make HookController and GetConfigDescription workable.
func (sh *ShellHook) BackportHookConfig(cfg *config.HookConfig) {
	sh.Config = cfg
	sh.RateLimiter = sh_hook.CreateRateLimiter(cfg)
}

// WithHookController sets dependency "hook controller" for shell-operator
func (sh *ShellHook) WithHookController(hookController *controller.HookController) {
	sh.HookController = hookController
}

// WithTmpDir injects temp directory from operator
func (sh *ShellHook) WithTmpDir(tmpDir string) {
	sh.TmpDir = tmpDir
}

// GetPath returns hook's path on the filesystem
func (sh *ShellHook) GetPath() string {
	return sh.Path
}

// GetHookController returns HookController
func (sh *ShellHook) GetHookController() *controller.HookController {
	return sh.HookController
}

// GetName returns the hook's name
func (sh *ShellHook) GetName() string {
	return sh.Name
}

// GetKind returns kind of the hook
func (sh *ShellHook) GetKind() HookKind {
	return HookKindShell
}

// GetHookConfigDescription get part of hook config for logging/debugging
func (sh *ShellHook) GetHookConfigDescription() string {
	return sh.Hook.GetConfigDescription()
}

// Execute runs the hook via the OS interpreter and returns the result of the execution
func (sh *ShellHook) Execute(configVersion string, bContext []bindingcontext.BindingContext, moduleSafeName string, configValues, values utils.Values, logLabels map[string]string) (*HookResult, error) {
	result := &HookResult{
		Patches: make(map[utils.ValuesPatchType]*utils.ValuesPatch),
	}

	versionedContextList := bindingcontext.ConvertBindingContextList(configVersion, bContext)
	bindingContextBytes, err := versionedContextList.Json()
	if err != nil {
		return nil, err
	}

	tmpFiles, err := sh.prepareTmpFilesForHookRun(bindingContextBytes, moduleSafeName, configValues, values)
	if err != nil {
		return nil, err
	}
	// Remove tmp files after execution
	defer func() {
		if shapp.DebugKeepTmpFilesVar == "yes" {
			return
		}
		for _, f := range tmpFiles {
			err := os.Remove(f)
			if err != nil {
				sh.Hook.Logger.With("hook", sh.GetName()).
					Error("Remove tmp file",
						slog.String("file", f),
						log.Err(err))
			}
		}
	}()
	configValuesPatchPath := tmpFiles["CONFIG_VALUES_JSON_PATCH_PATH"]
	valuesPatchPath := tmpFiles["VALUES_JSON_PATCH_PATH"]
	metricsPath := tmpFiles["METRICS_PATH"]
	kubernetesPatchPath := tmpFiles["KUBERNETES_PATCH_PATH"]

	envs := make([]string, 0)
	envs = append(envs, os.Environ()...)
	for envName, filePath := range tmpFiles {
		envs = append(envs, fmt.Sprintf("%s=%s", envName, filePath))
	}

	cmd := executor.NewExecutor(
		"",
		sh.GetPath(),
		[]string{},
		envs).
		WithLogProxyHookJSON(shapp.LogProxyHookJSON).
		WithLogProxyHookJSONKey(sh.LogProxyHookJSONKey).
		WithLogger(sh.Logger.Named("executor")).
		WithChroot(utils.GetModuleChrootPath(sh.moduleName))

	usage, err := cmd.RunAndLogLines(logLabels)
	result.Usage = usage
	if err != nil {
		return result, err
	}

	result.Patches[utils.ConfigMapPatch], err = utils.ValuesPatchFromFile(configValuesPatchPath)
	if err != nil {
		return result, fmt.Errorf("got bad json patch for config values: %w", err)
	}

	result.Patches[utils.MemoryValuesPatch], err = utils.ValuesPatchFromFile(valuesPatchPath)
	if err != nil {
		return result, fmt.Errorf("got bad json patch for values: %w", err)
	}

	result.Metrics, err = metricoperation.MetricOperationsFromFile(metricsPath)
	if err != nil {
		return result, fmt.Errorf("got bad metrics: %w", err)
	}

	kubernetesPatchBytes, err := os.ReadFile(kubernetesPatchPath)
	if err != nil {
		return result, fmt.Errorf("can't read kubernetes patch file: %w", err)
	}

	result.ObjectPatcherOperations, err = objectpatch.ParseOperations(kubernetesPatchBytes)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (sh *ShellHook) getConfig() ([]byte, error) {
	envs := make([]string, 0)
	envs = append(envs, os.Environ()...)
	args := []string{"--config"}

	cmd := executor.NewExecutor(
		"",
		sh.Path,
		args,
		envs).
		WithLogProxyHookJSON(shapp.LogProxyHookJSON).
		WithLogProxyHookJSONKey(sh.LogProxyHookJSONKey).
		WithLogger(sh.Logger.Named("executor")).
		WithCMDStdout(nil).
		WithChroot(utils.GetModuleChrootPath(sh.moduleName))

	sh.Hook.Logger.Debug("Executing hook",
		slog.String("args", strings.Join(args, " ")))

	output, err := cmd.Output()
	if err != nil {
		sh.Hook.Logger.Debug("Hook config failed",
			slog.String("hook", sh.Name),
			log.Err(err),
			slog.String("output", string(output)))
		return nil, err
	}

	sh.Hook.Logger.Debug("Hook config output",
		slog.String("hook", sh.Name),
		slog.String("output", string(output)))

	return output, nil
}

// GetConfig returns config via executing the hook with `--config` param
func (sh *ShellHook) GetConfig() ([]byte, error) {
	return sh.getConfig()
}

// LoadAndValidateShellConfig loads shell hook config from bytes and validate it. Returns multierror.
func (sh *ShellHook) GetConfigForModule(moduleKind string) (*config.HookConfig, error) {
	cfgData, err := sh.GetConfig()
	if err != nil {
		return nil, err
	}

	vu := config.NewDefaultVersionedUntyped()
	err = vu.Load(cfgData)
	if err != nil {
		return nil, err
	}

	var validationFunc func(version string) *spec.Schema
	switch moduleKind {
	case "global":
		{
			validationFunc = getGlobalHookConfigSchema
		}
	case "embedded":
		{
			validationFunc = getModuleHookConfigSchema
		}
	}

	err = config.ValidateConfig(vu.Obj, validationFunc(vu.Version), "")
	if err != nil {
		return nil, err
	}

	cfg := new(config.HookConfig)
	cfg.Version = vu.Version

	err = cfg.ConvertAndCheck(cfgData)
	if err != nil {
		return nil, err
	}

	sh.Config = cfg
	sh.ScheduleConfig = &HookScheduleConfig{}
	err = yaml.Unmarshal(cfgData, sh.ScheduleConfig)
	if err != nil {
		return nil, fmt.Errorf("unmarshal schedule yaml hook config: %s", err)
	}

	return cfg, nil
}

func (sh *ShellHook) GetOnStartup() *float64 {
	if sh.Config != nil {
		if sh.Config.OnStartup != nil {
			return &sh.Config.OnStartup.Order
		}
	}

	return nil
}

func (sh *ShellHook) GetBeforeAll() *float64 {
	res := ConvertFloatForBinding(sh.ScheduleConfig.BeforeAll)
	if res != nil {
		return res
	}

	res = ConvertFloatForBinding(sh.ScheduleConfig.BeforeHelm)
	if res != nil {
		return res
	}

	return nil
}

func (sh *ShellHook) GetAfterAll() *float64 {
	res := ConvertFloatForBinding(sh.ScheduleConfig.AfterAll)
	if res != nil {
		return res
	}

	res = ConvertFloatForBinding(sh.ScheduleConfig.AfterHelm)
	if res != nil {
		return res
	}

	return nil
}

func (sh *ShellHook) GetAfterDeleteHelm() *float64 {
	res := ConvertFloatForBinding(sh.ScheduleConfig.AfterDeleteHelm)
	if res != nil {
		return res
	}

	return nil
}

type HookScheduleConfig struct {
	// global module
	BeforeAll *uint `json:"beforeAll" yaml:"beforeAll"`
	AfterAll  *uint `json:"afterAll" yaml:"afterAll"`
	// embedded module
	BeforeHelm      *uint `json:"beforeHelm" yaml:"beforeHelm"`
	AfterHelm       *uint `json:"afterHelm" yaml:"afterHelm"`
	AfterDeleteHelm *uint `json:"afterDeleteHelm" yaml:"afterDeleteHelm"`
}

func ConvertFloatForBinding(value *uint) *float64 {
	if value == nil {
		return nil
	}

	res := float64(*value)

	return &res
}

func getGlobalHookConfigSchema(version string) *spec.Schema {
	globalHookVersion := "global-hook-" + version
	if _, ok := config.Schemas[globalHookVersion]; !ok {
		schema := config.Schemas[version]
		switch version {
		case "v1":
			// add beforeAll and afterAll properties
			schema += `
  beforeAll:
    type: integer
    example: 10    
  afterAll:
    type: integer
    example: 10    
`
		case "v0":
			// add beforeAll and afterAll properties
			schema += `
  beforeAll:
    type: integer
    example: 10    
  afterAll:
    type: integer
    example: 10    
`
		}
		config.Schemas[globalHookVersion] = schema
	}

	return config.GetSchema(globalHookVersion)
}

func getModuleHookConfigSchema(version string) *spec.Schema {
	globalHookVersion := "module-hook-" + version
	if _, ok := config.Schemas[globalHookVersion]; !ok {
		schema := config.Schemas[version]
		switch version {
		case "v1":
			// add beforeHelm, afterHelm and afterDeleteHelm properties
			schema += `
  beforeHelm:
    type: integer
    example: 10    
  afterHelm:
    type: integer
    example: 10    
  afterDeleteHelm:
    type: integer
    example: 10   
`
		case "v0":
			// add beforeHelm, afterHelm and afterDeleteHelm properties
			schema += `
  beforeHelm:
    type: integer
    example: 10    
  afterHelm:
    type: integer
    example: 10    
  afterDeleteHelm:
    type: integer
    example: 10    
`
		}
		config.Schemas[globalHookVersion] = schema
	}

	return config.GetSchema(globalHookVersion)
}

// PrepareTmpFilesForHookRun creates temporary files for hook and returns environment variables with paths
func (sh *ShellHook) prepareTmpFilesForHookRun(bindingContext []byte, moduleSafeName string, configValues, values utils.Values) (map[string]string, error) {
	var err error
	tmpFiles := make(map[string]string)

	tmpFiles["CONFIG_VALUES_PATH"], err = sh.prepareConfigValuesJsonFile(moduleSafeName, configValues)
	if err != nil {
		return tmpFiles, err
	}

	tmpFiles["VALUES_PATH"], err = sh.prepareValuesJsonFile(moduleSafeName, values)
	if err != nil {
		return tmpFiles, err
	}

	tmpFiles["BINDING_CONTEXT_PATH"], err = sh.prepareBindingContextJsonFile(moduleSafeName, bindingContext)
	if err != nil {
		return tmpFiles, err
	}

	tmpFiles["CONFIG_VALUES_JSON_PATCH_PATH"], err = sh.prepareConfigValuesJsonPatchFile()
	if err != nil {
		return tmpFiles, err
	}

	tmpFiles["VALUES_JSON_PATCH_PATH"], err = sh.prepareValuesJsonPatchFile()
	if err != nil {
		return tmpFiles, err
	}

	tmpFiles["METRICS_PATH"], err = sh.prepareMetricsFile()
	if err != nil {
		return tmpFiles, err
	}

	tmpFiles["KUBERNETES_PATCH_PATH"], err = sh.prepareKubernetesPatchFile()
	if err != nil {
		return tmpFiles, err
	}

	return tmpFiles, err
}

// METRICS_PATH
func (sh *ShellHook) prepareMetricsFile() (string, error) {
	path := filepath.Join(sh.TmpDir, fmt.Sprintf("%s.module-hook-metrics-%s.json", sh.SafeName(), uuid.Must(uuid.NewV4()).String()))
	if err := utils.CreateEmptyWritableFile(path); err != nil {
		return "", err
	}
	return path, nil
}

// BINDING_CONTEXT_PATH
func (sh *ShellHook) prepareBindingContextJsonFile(moduleSafeName string, bindingContext []byte) (string, error) {
	path := filepath.Join(sh.TmpDir, fmt.Sprintf("%s.module-hook-%s-binding-context-%s.json", moduleSafeName, sh.SafeName(), uuid.Must(uuid.NewV4()).String()))
	err := utils.DumpData(path, bindingContext)
	if err != nil {
		return "", err
	}

	return path, nil
}

// CONFIG_VALUES_JSON_PATCH_PATH
func (sh *ShellHook) prepareConfigValuesJsonPatchFile() (string, error) {
	path := filepath.Join(sh.TmpDir, fmt.Sprintf("%s.module-hook-config-values-%s.json-patch", sh.SafeName(), uuid.Must(uuid.NewV4()).String()))
	if err := utils.CreateEmptyWritableFile(path); err != nil {
		return "", err
	}
	return path, nil
}

// VALUES_JSON_PATCH_PATH
func (sh *ShellHook) prepareValuesJsonPatchFile() (string, error) {
	path := filepath.Join(sh.TmpDir, fmt.Sprintf("%s.module-hook-values-%s.json-patch", sh.SafeName(), uuid.Must(uuid.NewV4()).String()))
	if err := utils.CreateEmptyWritableFile(path); err != nil {
		return "", err
	}
	return path, nil
}

// KUBERNETES PATCH PATH
func (sh *ShellHook) prepareKubernetesPatchFile() (string, error) {
	path := filepath.Join(sh.TmpDir, fmt.Sprintf("%s-object-patch-%s", sh.SafeName(), uuid.Must(uuid.NewV4()).String()))
	if err := utils.CreateEmptyWritableFile(path); err != nil {
		return "", err
	}
	return path, nil
}

// CONFIG_VALUES_PATH
func (sh *ShellHook) prepareConfigValuesJsonFile(moduleSafeName string, configValues utils.Values) (string, error) {
	data, err := configValues.JsonBytes()
	if err != nil {
		return "", err
	}

	path := filepath.Join(sh.TmpDir, fmt.Sprintf("%s.module-config-values-%s.json", moduleSafeName, uuid.Must(uuid.NewV4()).String()))
	err = utils.DumpData(path, data)
	if err != nil {
		return "", err
	}

	sh.Hook.Logger.Debug("Prepared module hook config values",
		slog.String("module", moduleSafeName),
		slog.String("values", configValues.DebugString()))

	return path, nil
}

func (sh *ShellHook) prepareValuesJsonFile(moduleSafeName string, values utils.Values) (string, error) {
	data, err := values.JsonBytes()
	if err != nil {
		return "", err
	}

	path := filepath.Join(sh.TmpDir, fmt.Sprintf("%s.module-values-%s.json", moduleSafeName, uuid.Must(uuid.NewV4()).String()))
	err = utils.DumpData(path, data)
	if err != nil {
		return "", err
	}

	sh.Hook.Logger.Debug("Prepared module hook values",
		slog.String("module", moduleSafeName),
		slog.String("values", values.DebugString()))

	return path, nil
}
