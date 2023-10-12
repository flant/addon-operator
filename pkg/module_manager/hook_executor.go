package module_manager

import (
	"fmt"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/pkg/module_manager/go_hook/metrics"
	"github.com/flant/addon-operator/pkg/utils"
	sh_app "github.com/flant/shell-operator/pkg/app"
	"github.com/flant/shell-operator/pkg/executor"
	. "github.com/flant/shell-operator/pkg/hook/binding_context"
	"github.com/flant/shell-operator/pkg/kube/object_patch"
	metric_operation "github.com/flant/shell-operator/pkg/metric_storage/operation"
)

type HookExecutor struct {
	Hook                  Hook
	Context               []BindingContext
	ConfigVersion         string
	ConfigValuesPath      string
	ValuesPath            string
	ContextPath           string
	ConfigValuesPatchPath string
	ValuesPatchPath       string
	MetricsPath           string
	ObjectPatcher         *object_patch.ObjectPatcher
	KubernetesPatchPath   string
	LogLabels             map[string]string
	Helm                  *helm.ClientFactory
}

func NewHookExecutor(h Hook, context []BindingContext, configVersion string, objectPatcher *object_patch.ObjectPatcher) *HookExecutor {
	return &HookExecutor{
		Hook:          h,
		Context:       context,
		ConfigVersion: configVersion,
		LogLabels:     map[string]string{},
		ObjectPatcher: objectPatcher,
	}
}

func (e *HookExecutor) WithLogLabels(logLabels map[string]string) {
	e.LogLabels = logLabels
}

func (e *HookExecutor) WithHelm(helm *helm.ClientFactory) {
	e.Helm = helm
}

type HookResult struct {
	Usage                   *executor.CmdUsage
	Patches                 map[utils.ValuesPatchType]*utils.ValuesPatch
	Metrics                 []metric_operation.MetricOperation
	ObjectPatcherOperations []object_patch.Operation
	BindingActions          []go_hook.BindingAction
}

func (e *HookExecutor) Run() (result *HookResult, err error) {
	if e.Hook.GetGoHook() != nil {
		return e.RunGoHook()
	}

	result = &HookResult{
		Patches: make(map[utils.ValuesPatchType]*utils.ValuesPatch),
	}

	versionedContextList := ConvertBindingContextList(e.ConfigVersion, e.Context)
	bindingContextBytes, err := versionedContextList.Json()
	if err != nil {
		return nil, err
	}

	tmpFiles, err := e.Hook.PrepareTmpFilesForHookRun(bindingContextBytes)
	if err != nil {
		return nil, err
	}
	// Remove tmp files after execution
	defer func() {
		if sh_app.DebugKeepTmpFiles == "yes" {
			return
		}
		for _, f := range tmpFiles {
			err := os.Remove(f)
			if err != nil {
				log.WithField("hook", e.Hook.GetName()).
					Errorf("Remove tmp file '%s': %s", f, err)
			}
		}
	}()
	e.ConfigValuesPatchPath = tmpFiles["CONFIG_VALUES_JSON_PATCH_PATH"]
	e.ValuesPatchPath = tmpFiles["VALUES_JSON_PATCH_PATH"]
	e.MetricsPath = tmpFiles["METRICS_PATH"]
	e.KubernetesPatchPath = tmpFiles["KUBERNETES_PATCH_PATH"]

	envs := make([]string, 0)
	envs = append(envs, os.Environ()...)
	for envName, filePath := range tmpFiles {
		envs = append(envs, fmt.Sprintf("%s=%s", envName, filePath))
	}

	cmd := executor.MakeCommand("", e.Hook.GetPath(), []string{}, envs)

	usage, err := executor.RunAndLogLines(cmd, e.LogLabels)
	result.Usage = usage
	if err != nil {
		return result, err
	}

	result.Patches[utils.ConfigMapPatch], err = utils.ValuesPatchFromFile(e.ConfigValuesPatchPath)
	if err != nil {
		return result, fmt.Errorf("got bad json patch for config values: %s", err)
	}

	result.Patches[utils.MemoryValuesPatch], err = utils.ValuesPatchFromFile(e.ValuesPatchPath)
	if err != nil {
		return result, fmt.Errorf("got bad json patch for values: %s", err)
	}

	result.Metrics, err = metric_operation.MetricOperationsFromFile(e.MetricsPath)
	if err != nil {
		return result, fmt.Errorf("got bad metrics: %s", err)
	}

	kubernetesPatchBytes, err := os.ReadFile(e.KubernetesPatchPath)
	if err != nil {
		return result, fmt.Errorf("can't read kubernetes patch file: %s", err)
	}

	result.ObjectPatcherOperations, err = object_patch.ParseOperations(kubernetesPatchBytes)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (e *HookExecutor) RunGoHook() (result *HookResult, err error) {
	goHook := e.Hook.GetGoHook()
	if goHook == nil {
		return
	}

	// Values are patched in-place, so an error can occur.
	values, err := e.Hook.GetValues()
	if err != nil {
		return nil, err
	}

	patchableValues, err := go_hook.NewPatchableValues(values)
	if err != nil {
		return nil, err
	}

	patchableConfigValues, err := go_hook.NewPatchableValues(e.Hook.GetConfigValues())
	if err != nil {
		return nil, err
	}

	bindingActions := new([]go_hook.BindingAction)

	logEntry := log.WithFields(utils.LabelsToLogFields(e.LogLabels)).
		WithField("output", "gohook")

	formattedSnapshots := make(go_hook.Snapshots, len(e.Context))
	for _, context := range e.Context {
		for snapBindingName, snaps := range context.Snapshots {
			for _, snapshot := range snaps {
				goSnapshot := snapshot.FilterResult
				formattedSnapshots[snapBindingName] = append(formattedSnapshots[snapBindingName], goSnapshot)
			}
		}
	}

	metricsCollector := metrics.NewCollector(e.Hook.GetName())
	patchCollector := object_patch.NewPatchCollector()

	err = goHook.Run(&go_hook.HookInput{
		Snapshots:        formattedSnapshots,
		Values:           patchableValues,
		ConfigValues:     patchableConfigValues,
		PatchCollector:   patchCollector,
		LogEntry:         logEntry,
		MetricsCollector: metricsCollector,
		BindingActions:   bindingActions,
	})
	if err != nil {
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

func (e *HookExecutor) Config() (configOutput []byte, err error) {
	// Config() is called directly for go hooks
	if e.Hook.GetGoHook() != nil {
		return nil, nil
	}

	envs := make([]string, 0)
	envs = append(envs, os.Environ()...)

	cmd := executor.MakeCommand("", e.Hook.GetPath(), []string{"--config"}, envs)

	log.Debugf("Executing hook in %s: '%s'", cmd.Dir, strings.Join(cmd.Args, " "))
	cmd.Stdout = nil

	output, err := executor.Output(cmd)
	if err != nil {
		log.Debugf("Hook '%s' config failed: %v, output:\n%s", e.Hook.GetName(), err, string(output))
		return nil, err
	}

	log.Debugf("Hook '%s' config output:\n%s", e.Hook.GetName(), string(output))

	return output, nil
}
