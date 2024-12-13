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
	kemtypes "github.com/flant/shell-operator/pkg/kube_events_manager/types"

	"github.com/gofrs/uuid/v5"

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

var _ gohook.HookConfigLoader = (*BatchHook)(nil)

type BatchHook struct {
	sh_hook.Hook
	// hook ID in batch
	ID     uint
	config *sdkhook.HookConfig
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
func (h *BatchHook) BackportHookConfig(cfg *config.HookConfig) {
	h.Config = cfg
	h.RateLimiter = sh_hook.CreateRateLimiter(cfg)
}

// WithHookController sets dependency "hook controller" for shell-operator
func (h *BatchHook) WithHookController(hookController *controller.HookController) {
	h.HookController = hookController
}

// WithTmpDir injects temp directory from operator
func (h *BatchHook) WithTmpDir(tmpDir string) {
	h.TmpDir = tmpDir
}

// GetPath returns hook's path on the filesystem
func (h *BatchHook) GetPath() string {
	return h.Path
}

// GetHookController returns HookController
func (h *BatchHook) GetHookController() *controller.HookController {
	return h.HookController
}

// GetName returns the hook's name
func (h *BatchHook) GetName() string {
	return h.Name
}

// GetKind returns kind of the hook
func (h *BatchHook) GetKind() HookKind {
	return HookKindShell
}

// GetHookConfigDescription get part of hook config for logging/debugging
func (h *BatchHook) GetHookConfigDescription() string {
	return h.Hook.GetConfigDescription()
}

// Execute runs the hook via the OS interpreter and returns the result of the execution
func (h *BatchHook) Execute(configVersion string, bContext []bindingcontext.BindingContext, moduleSafeName string, configValues, values utils.Values, logLabels map[string]string) (result *HookResult, err error) {
	result = &HookResult{
		Patches: make(map[utils.ValuesPatchType]*utils.ValuesPatch),
	}

	versionedContextList := bindingcontext.ConvertBindingContextList(configVersion, bContext)
	bindingContextBytes, err := versionedContextList.Json()
	if err != nil {
		return nil, err
	}

	// tmp files has uuid in name and create only in tmp folder (because of RO filesystem)
	tmpFiles, err := h.prepareTmpFilesForHookRun(bindingContextBytes, moduleSafeName, configValues, values)
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
				h.Hook.Logger.With("hook", h.GetName()).
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
	args = append(args, "hook", "run", strconv.Itoa(int(h.ID)))
	envs = append(envs, os.Environ()...)
	for envName, filePath := range tmpFiles {
		envs = append(envs, fmt.Sprintf("%s=%s", envName, filePath))
	}

	cmd := executor.NewExecutor(
		"",
		h.GetPath(),
		args,
		envs).
		WithLogProxyHookJSON(shapp.LogProxyHookJSON).
		WithLogProxyHookJSONKey(h.LogProxyHookJSONKey).
		WithLogger(h.Logger.Named("executor"))

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

func (h *BatchHook) getConfig() ([]sdkhook.HookConfig, error) {
	return GetBatchHookConfig(h.Path)
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
func (h *BatchHook) GetConfig() ([]sdkhook.HookConfig, error) {
	return h.getConfig()
}

// LoadAndValidateShellConfig loads shell hook config from bytes and validate it. Returns multierror.
func (h *BatchHook) LoadAndValidate(_ string) (*config.HookConfig, error) {
	cfg, err := h.GetConfig()
	if err != nil {
		return nil, err
	}

	h.config = &cfg[h.ID]

	hcv1 := remapHookConfigV1FromHookConfig(h.config)

	hookconfig := new(config.HookConfig)
	hookconfig.Version = h.config.ConfigVersion

	err = hcv1.ConvertAndCheck(hookconfig)
	if err != nil {
		return nil, fmt.Errorf("convert and check from hook config v1: %w", err)
	}

	return hookconfig, nil
}

func (h *BatchHook) LoadOnStartup() (*float64, error) {
	res := float64(*h.config.OnStartup)
	return &res, nil
}

func (h *BatchHook) LoadBeforeAll() (*float64, error) {
	res := float64(*h.config.OnBeforeHelm)
	return &res, nil
}

func (h *BatchHook) LoadAfterAll() (*float64, error) {
	res := float64(*h.config.OnAfterHelm)
	return &res, nil
}

func (h *BatchHook) LoadAfterDeleteHelm() (*float64, error) {
	res := float64(*h.config.OnAfterDeleteHelm)
	return &res, nil
}

// PrepareTmpFilesForHookRun creates temporary files for hook and returns environment variables with paths
func (h *BatchHook) prepareTmpFilesForHookRun(bindingContext []byte, moduleSafeName string, configValues, values utils.Values) (tmpFiles map[string]string, err error) {
	tmpFiles = make(map[string]string)

	tmpFiles["CONFIG_VALUES_PATH"], err = h.prepareConfigValuesJsonFile(moduleSafeName, configValues)
	if err != nil {
		return
	}

	tmpFiles["VALUES_PATH"], err = h.prepareValuesJsonFile(moduleSafeName, values)
	if err != nil {
		return
	}

	tmpFiles["BINDING_CONTEXT_PATH"], err = h.prepareBindingContextJsonFile(moduleSafeName, bindingContext)
	if err != nil {
		return
	}

	tmpFiles["CONFIG_VALUES_JSON_PATCH_PATH"], err = h.prepareConfigValuesJsonPatchFile()
	if err != nil {
		return
	}

	tmpFiles["VALUES_JSON_PATCH_PATH"], err = h.prepareValuesJsonPatchFile()
	if err != nil {
		return
	}

	tmpFiles["METRICS_PATH"], err = h.prepareMetricsFile()
	if err != nil {
		return
	}

	tmpFiles["KUBERNETES_PATCH_PATH"], err = h.prepareKubernetesPatchFile()
	if err != nil {
		return
	}

	return
}

// METRICS_PATH
func (h *BatchHook) prepareMetricsFile() (string, error) {
	path := filepath.Join(h.TmpDir, fmt.Sprintf("%s.module-hook-metrics-%s.json", h.SafeName(), uuid.Must(uuid.NewV4()).String()))
	if err := utils.CreateEmptyWritableFile(path); err != nil {
		return "", err
	}
	return path, nil
}

// BINDING_CONTEXT_PATH
func (h *BatchHook) prepareBindingContextJsonFile(moduleSafeName string, bindingContext []byte) (string, error) {
	path := filepath.Join(h.TmpDir, fmt.Sprintf("%s.module-hook-%s-binding-context-%s.json", moduleSafeName, h.SafeName(), uuid.Must(uuid.NewV4()).String()))
	err := utils.DumpData(path, bindingContext)
	if err != nil {
		return "", err
	}

	return path, nil
}

// CONFIG_VALUES_JSON_PATCH_PATH
func (h *BatchHook) prepareConfigValuesJsonPatchFile() (string, error) {
	path := filepath.Join(h.TmpDir, fmt.Sprintf("%s.module-hook-config-values-%s.json-patch", h.SafeName(), uuid.Must(uuid.NewV4()).String()))
	if err := utils.CreateEmptyWritableFile(path); err != nil {
		return "", err
	}
	return path, nil
}

// VALUES_JSON_PATCH_PATH
func (h *BatchHook) prepareValuesJsonPatchFile() (string, error) {
	path := filepath.Join(h.TmpDir, fmt.Sprintf("%s.module-hook-values-%s.json-patch", h.SafeName(), uuid.Must(uuid.NewV4()).String()))
	if err := utils.CreateEmptyWritableFile(path); err != nil {
		return "", err
	}
	return path, nil
}

// KUBERNETES PATCH PATH
func (h *BatchHook) prepareKubernetesPatchFile() (string, error) {
	path := filepath.Join(h.TmpDir, fmt.Sprintf("%s-object-patch-%s", h.SafeName(), uuid.Must(uuid.NewV4()).String()))
	if err := utils.CreateEmptyWritableFile(path); err != nil {
		return "", err
	}
	return path, nil
}

// CONFIG_VALUES_PATH
func (h *BatchHook) prepareConfigValuesJsonFile(moduleSafeName string, configValues utils.Values) (string, error) {
	data, err := configValues.JsonBytes()
	if err != nil {
		return "", err
	}

	path := filepath.Join(h.TmpDir, fmt.Sprintf("%s.module-config-values-%s.json", moduleSafeName, uuid.Must(uuid.NewV4()).String()))
	err = utils.DumpData(path, data)
	if err != nil {
		return "", err
	}

	h.Hook.Logger.Debugf("Prepared module %s hook config values:\n%s", moduleSafeName, configValues.DebugString())

	return path, nil
}

func (h *BatchHook) prepareValuesJsonFile(moduleSafeName string, values utils.Values) (string, error) {
	data, err := values.JsonBytes()
	if err != nil {
		return "", err
	}

	path := filepath.Join(h.TmpDir, fmt.Sprintf("%s.module-values-%s.json", moduleSafeName, uuid.Must(uuid.NewV4()).String()))
	err = utils.DumpData(path, data)
	if err != nil {
		return "", err
	}

	h.Hook.Logger.Debugf("Prepared module %s hook values:\n%s", moduleSafeName, values.DebugString())

	return path, nil
}

func remapHookConfigV1FromHookConfig(hcfg *sdkhook.HookConfig) *config.HookConfigV1 {
	hcv1 := &config.HookConfigV1{
		ConfigVersion: hcfg.ConfigVersion,
	}

	if len(hcfg.Schedule) > 0 {
		hcv1.Schedule = make([]config.ScheduleConfigV1, 0, len(hcfg.Schedule))
	}

	if len(hcfg.Kubernetes) > 0 {
		hcv1.OnKubernetesEvent = make([]config.OnKubernetesEventConfigV1, 0, len(hcfg.Kubernetes))
	}

	if hcfg.OnStartup != nil {
		hcv1.OnStartup = float64(*hcfg.OnStartup)
	}

	if hcfg.Settings != nil {
		hcv1.Settings = &config.SettingsV1{
			ExecutionMinInterval: hcfg.Settings.ExecutionMinInterval.String(),
			ExecutionBurst:       strconv.Itoa(hcfg.Settings.ExecutionBurst),
		}
	}

	for _, sch := range hcfg.Schedule {
		hcv1.Schedule = append(hcv1.Schedule, config.ScheduleConfigV1{
			Name:    sch.Name,
			Crontab: sch.Crontab,
		})
	}

	for _, kube := range hcfg.Kubernetes {
		newShCfg := config.OnKubernetesEventConfigV1{
			ApiVersion:                   kube.APIVersion,
			Kind:                         kube.Kind,
			Name:                         kube.Name,
			LabelSelector:                kube.LabelSelector,
			JqFilter:                     kube.JqFilter,
			ExecuteHookOnSynchronization: "true",
			WaitForSynchronization:       "true",
			// permanently false
			KeepFullObjectsInMemory: "false",
			ResynchronizationPeriod: kube.ResynchronizationPeriod,
			IncludeSnapshotsFrom:    kube.IncludeSnapshotsFrom,
			Queue:                   kube.Queue,
			// TODO: make default constants public to use here
			// like go hooks apply default
			Group: "main",
		}

		if kube.NameSelector != nil {
			newShCfg.NameSelector = &config.KubeNameSelectorV1{
				MatchNames: kube.NameSelector.MatchNames,
			}
		}

		if kube.NamespaceSelector != nil {
			newShCfg.Namespace = &config.KubeNamespaceSelectorV1{
				NameSelector:  (*kemtypes.NameSelector)(kube.NamespaceSelector.NameSelector),
				LabelSelector: kube.NamespaceSelector.LabelSelector,
			}
		}

		if kube.FieldSelector != nil {
			fs := &config.KubeFieldSelectorV1{
				MatchExpressions: make([]kemtypes.FieldSelectorRequirement, 0, len(kube.FieldSelector.MatchExpressions)),
			}

			for _, expr := range kube.FieldSelector.MatchExpressions {
				fs.MatchExpressions = append(fs.MatchExpressions, kemtypes.FieldSelectorRequirement(expr))
			}

			newShCfg.FieldSelector = fs
		}

		if kube.KeepFullObjectsInMemory != nil {
			newShCfg.KeepFullObjectsInMemory = strconv.FormatBool(*kube.KeepFullObjectsInMemory)
		}

		// *bool --> ExecuteHookOnEvents: [All events] || empty array or nothing
		if kube.ExecuteHookOnEvents != nil && !*kube.ExecuteHookOnEvents {
			newShCfg.ExecuteHookOnEvents = make([]kemtypes.WatchEventType, 0, 1)
		}

		if kube.ExecuteHookOnSynchronization != nil {
			newShCfg.ExecuteHookOnSynchronization = strconv.FormatBool(*kube.ExecuteHookOnSynchronization)
		}

		if kube.WaitForSynchronization != nil {
			newShCfg.WaitForSynchronization = strconv.FormatBool(*kube.WaitForSynchronization)
		}

		if kube.KeepFullObjectsInMemory != nil {
			newShCfg.KeepFullObjectsInMemory = strconv.FormatBool(*kube.KeepFullObjectsInMemory)
		}

		if kube.AllowFailure != nil {
			newShCfg.AllowFailure = *kube.AllowFailure
		}

		hcv1.OnKubernetesEvent = append(hcv1.OnKubernetesEvent, newShCfg)
	}

	return hcv1
}
