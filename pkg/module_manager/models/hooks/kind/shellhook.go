package kind

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/gofrs/uuid/v5"

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

type ShellHook struct {
	sh_hook.Hook
}

// NewShellHook new hook, which runs via the OS interpreter like bash/python/etc
func NewShellHook(name, path string, keepTemporaryHookFiles bool, logProxyHookJSON bool, logger *log.Logger) *ShellHook {
	return &ShellHook{
		Hook: sh_hook.Hook{
			Name:                   name,
			Path:                   path,
			KeepTemporaryHookFiles: keepTemporaryHookFiles,
			LogProxyHookJSON:       logProxyHookJSON,
			Logger:                 logger,
		},
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
func (sh *ShellHook) Execute(configVersion string, bContext []bindingcontext.BindingContext, moduleSafeName string, configValues, values utils.Values, logLabels map[string]string) (result *HookResult, err error) {
	result = &HookResult{
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
					Errorf("Remove tmp file '%s': %s", f, err)
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

	sh.Logger.Info("WELL IT's LOGPROXYHOOKJSON", slog.Bool("value", sh.LogProxyHookJSON))
	sh.Logger.Info("WELL IT's LOGPROXYHOOKJSON HOOK", slog.Bool("value", sh.Hook.LogProxyHookJSON))
	sh.Logger.Info("WELL IT's GLOBALVAR", slog.Bool("value", shapp.LogProxyHookJSON))

	cmd := executor.NewExecutor(
		"",
		sh.GetPath(),
		[]string{},
		envs).
		WithLogProxyHookJSON(sh.Hook.LogProxyHookJSON).
		WithLogProxyHookJSONKey(sh.LogProxyHookJSONKey).
		WithLogger(sh.Logger.Named("executor"))

	usage, err := cmd.RunAndLogLines(logLabels)
	result.Usage = usage
	if err != nil {
		return result, err
	}

	result.Patches[utils.ConfigMapPatch], err = utils.ValuesPatchFromFile(configValuesPatchPath)
	if err != nil {
		return result, fmt.Errorf("got bad json patch for config values: %s", err)
	}

	result.Patches[utils.MemoryValuesPatch], err = utils.ValuesPatchFromFile(valuesPatchPath)
	if err != nil {
		return result, fmt.Errorf("got bad json patch for values: %s", err)
	}

	result.Metrics, err = metricoperation.MetricOperationsFromFile(metricsPath)
	if err != nil {
		return result, fmt.Errorf("got bad metrics: %s", err)
	}

	kubernetesPatchBytes, err := os.ReadFile(kubernetesPatchPath)
	if err != nil {
		return result, fmt.Errorf("can't read kubernetes patch file: %s", err)
	}

	result.ObjectPatcherOperations, err = objectpatch.ParseOperations(kubernetesPatchBytes)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (sh *ShellHook) getConfig() (configOutput []byte, err error) {
	envs := make([]string, 0)
	envs = append(envs, os.Environ()...)
	args := []string{"--config"}

	cmd := executor.NewExecutor(
		"",
		sh.Path,
		args,
		envs).
		WithLogProxyHookJSON(sh.Hook.LogProxyHookJSON).
		WithLogProxyHookJSONKey(sh.LogProxyHookJSONKey).
		WithLogger(sh.Logger.Named("executor")).
		WithCMDStdout(nil)

	sh.Hook.Logger.Debugf("Executing hook in: '%s'", strings.Join(args, " "))

	output, err := cmd.Output()
	if err != nil {
		sh.Hook.Logger.Debugf("Hook '%s' config failed: %v, output:\n%s", sh.Name, err, string(output))
		return nil, err
	}

	sh.Hook.Logger.Debugf("Hook '%s' config output:\n%s", sh.Name, string(output))

	return output, nil
}

// GetConfig returns config via executing the hook with `--config` param
func (sh *ShellHook) GetConfig() ([]byte, error) {
	return sh.getConfig()
}

// PrepareTmpFilesForHookRun creates temporary files for hook and returns environment variables with paths
func (sh *ShellHook) prepareTmpFilesForHookRun(bindingContext []byte, moduleSafeName string, configValues, values utils.Values) (tmpFiles map[string]string, err error) {
	tmpFiles = make(map[string]string)

	tmpFiles["CONFIG_VALUES_PATH"], err = sh.prepareConfigValuesJsonFile(moduleSafeName, configValues)
	if err != nil {
		return
	}

	tmpFiles["VALUES_PATH"], err = sh.prepareValuesJsonFile(moduleSafeName, values)
	if err != nil {
		return
	}

	tmpFiles["BINDING_CONTEXT_PATH"], err = sh.prepareBindingContextJsonFile(moduleSafeName, bindingContext)
	if err != nil {
		return
	}

	tmpFiles["CONFIG_VALUES_JSON_PATCH_PATH"], err = sh.prepareConfigValuesJsonPatchFile()
	if err != nil {
		return
	}

	tmpFiles["VALUES_JSON_PATCH_PATH"], err = sh.prepareValuesJsonPatchFile()
	if err != nil {
		return
	}

	tmpFiles["METRICS_PATH"], err = sh.prepareMetricsFile()
	if err != nil {
		return
	}

	tmpFiles["KUBERNETES_PATCH_PATH"], err = sh.prepareKubernetesPatchFile()
	if err != nil {
		return
	}

	return
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

	sh.Hook.Logger.Debugf("Prepared module %s hook config values:\n%s", moduleSafeName, configValues.DebugString())

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

	sh.Hook.Logger.Debugf("Prepared module %s hook values:\n%s", moduleSafeName, values.DebugString())

	return path, nil
}
