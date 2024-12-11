package kind

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/deckhouse/deckhouse/pkg/log"
	sdkhook "github.com/deckhouse/module-sdk/pkg/hook"
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

type BatchHook struct {
	sh_hook.Hook
	// hook ID in batch
	ID uint
}

// NewBatchHook new hook, which runs via the OS interpreter like bash/python/etc
func NewBatchHook(name, path string, id uint, keepTemporaryHookFiles bool, logProxyHookJSON bool, logger *log.Logger) *BatchHook {
	return &BatchHook{
		Hook: sh_hook.Hook{
			Name:                   name,
			Path:                   path,
			KeepTemporaryHookFiles: keepTemporaryHookFiles,
			LogProxyHookJSON:       logProxyHookJSON,
			Logger:                 logger,
		},
		ID: id,
	}
}

// BackportHookConfig for shell-operator to make HookController and GetConfigDescription workable.
func (sh *BatchHook) BackportHookConfig(cfg *config.HookConfig) {
	sh.Config = cfg
	sh.RateLimiter = sh_hook.CreateRateLimiter(cfg)
}

// WithHookController sets dependency "hook controller" for shell-operator
func (sh *BatchHook) WithHookController(hookController *controller.HookController) {
	sh.HookController = hookController
}

// WithTmpDir injects temp directory from operator
func (sh *BatchHook) WithTmpDir(tmpDir string) {
	sh.TmpDir = tmpDir
}

// GetPath returns hook's path on the filesystem
func (sh *BatchHook) GetPath() string {
	return sh.Path
}

// GetHookController returns HookController
func (sh *BatchHook) GetHookController() *controller.HookController {
	return sh.HookController
}

// GetName returns the hook's name
func (sh *BatchHook) GetName() string {
	return sh.Name
}

// GetKind returns kind of the hook
func (sh *BatchHook) GetKind() HookKind {
	return HookKindShell
}

// GetHookConfigDescription get part of hook config for logging/debugging
func (sh *BatchHook) GetHookConfigDescription() string {
	return sh.Hook.GetConfigDescription()
}

// Execute runs the hook via the OS interpreter and returns the result of the execution
func (sh *BatchHook) Execute(configVersion string, bContext []bindingcontext.BindingContext, moduleSafeName string, configValues, values utils.Values, logLabels map[string]string) (result *HookResult, err error) {
	result = &HookResult{
		Patches: make(map[utils.ValuesPatchType]*utils.ValuesPatch),
	}

	versionedContextList := bindingcontext.ConvertBindingContextList(configVersion, bContext)
	bindingContextBytes, err := versionedContextList.Json()
	if err != nil {
		return nil, err
	}

	// tmp files has uuid in name and create only in tmp folder (because of RO filesystem)
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
	args := make([]string, 0)
	args = append(args, "hook", "run", strconv.Itoa(int(sh.ID)))
	envs = append(envs, os.Environ()...)
	for envName, filePath := range tmpFiles {
		envs = append(envs, fmt.Sprintf("%s=%s", envName, filePath))
	}

	cmd := executor.NewExecutor(
		"",
		sh.GetPath(),
		args,
		envs).
		WithLogProxyHookJSON(shapp.LogProxyHookJSON).
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

func (sh *BatchHook) getConfig() ([]sdkhook.HookConfig, error) {
	return GetBatchHookConfig(sh.Path)
}

func GetBatchHookConfig(hookPath string) ([]sdkhook.HookConfig, error) {
	args := []string{"hook", "config"}
	o, err := exec.Command(hookPath, args...).Output()
	if err != nil {
		return nil, fmt.Errorf("exec file '%s': %w", hookPath, err)
	}

	cfgs := make([]sdkhook.HookConfig, 0)
	buf := bytes.NewReader(o)
	err = json.NewDecoder(buf).Decode(&cfgs)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	return cfgs, nil
}

// GetConfig returns config via executing the hook with `--config` param
func (sh *BatchHook) GetConfig() ([]sdkhook.HookConfig, error) {
	return sh.getConfig()
}

// PrepareTmpFilesForHookRun creates temporary files for hook and returns environment variables with paths
func (sh *BatchHook) prepareTmpFilesForHookRun(bindingContext []byte, moduleSafeName string, configValues, values utils.Values) (tmpFiles map[string]string, err error) {
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
func (sh *BatchHook) prepareMetricsFile() (string, error) {
	path := filepath.Join(sh.TmpDir, fmt.Sprintf("%s.module-hook-metrics-%s.json", sh.SafeName(), uuid.Must(uuid.NewV4()).String()))
	if err := utils.CreateEmptyWritableFile(path); err != nil {
		return "", err
	}
	return path, nil
}

// BINDING_CONTEXT_PATH
func (sh *BatchHook) prepareBindingContextJsonFile(moduleSafeName string, bindingContext []byte) (string, error) {
	path := filepath.Join(sh.TmpDir, fmt.Sprintf("%s.module-hook-%s-binding-context-%s.json", moduleSafeName, sh.SafeName(), uuid.Must(uuid.NewV4()).String()))
	err := utils.DumpData(path, bindingContext)
	if err != nil {
		return "", err
	}

	return path, nil
}

// CONFIG_VALUES_JSON_PATCH_PATH
func (sh *BatchHook) prepareConfigValuesJsonPatchFile() (string, error) {
	path := filepath.Join(sh.TmpDir, fmt.Sprintf("%s.module-hook-config-values-%s.json-patch", sh.SafeName(), uuid.Must(uuid.NewV4()).String()))
	if err := utils.CreateEmptyWritableFile(path); err != nil {
		return "", err
	}
	return path, nil
}

// VALUES_JSON_PATCH_PATH
func (sh *BatchHook) prepareValuesJsonPatchFile() (string, error) {
	path := filepath.Join(sh.TmpDir, fmt.Sprintf("%s.module-hook-values-%s.json-patch", sh.SafeName(), uuid.Must(uuid.NewV4()).String()))
	if err := utils.CreateEmptyWritableFile(path); err != nil {
		return "", err
	}
	return path, nil
}

// KUBERNETES PATCH PATH
func (sh *BatchHook) prepareKubernetesPatchFile() (string, error) {
	path := filepath.Join(sh.TmpDir, fmt.Sprintf("%s-object-patch-%s", sh.SafeName(), uuid.Must(uuid.NewV4()).String()))
	if err := utils.CreateEmptyWritableFile(path); err != nil {
		return "", err
	}
	return path, nil
}

// CONFIG_VALUES_PATH
func (sh *BatchHook) prepareConfigValuesJsonFile(moduleSafeName string, configValues utils.Values) (string, error) {
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

func (sh *BatchHook) prepareValuesJsonFile(moduleSafeName string, values utils.Values) (string, error) {
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
