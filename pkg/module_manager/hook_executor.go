package module_manager

import (
	"fmt"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"

	sh_app "github.com/flant/shell-operator/pkg/app"
	"github.com/flant/shell-operator/pkg/executor"
	. "github.com/flant/shell-operator/pkg/hook/binding_context"
	metric_operation "github.com/flant/shell-operator/pkg/metric_storage/operation"

	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/addon-operator/sdk"
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
	LogLabels             map[string]string
}

func NewHookExecutor(h Hook, context []BindingContext, configVersion string) *HookExecutor {
	return &HookExecutor{
		Hook:          h,
		Context:       context,
		ConfigVersion: configVersion,
		LogLabels:     map[string]string{},
	}
}

func (e *HookExecutor) WithLogLabels(logLabels map[string]string) {
	e.LogLabels = logLabels
}

func (e *HookExecutor) Run() (patches map[utils.ValuesPatchType]*utils.ValuesPatch, metrics []metric_operation.MetricOperation, err error) {
	if e.Hook.GetGoHook() != nil {
		return e.RunGoHook()
	}

	patches = make(map[utils.ValuesPatchType]*utils.ValuesPatch)

	versionedContextList := ConvertBindingContextList(e.ConfigVersion, e.Context)
	bindingContextBytes, err := versionedContextList.Json()
	if err != nil {
		return nil, nil, err
	}

	tmpFiles, err := e.Hook.PrepareTmpFilesForHookRun(bindingContextBytes)
	if err != nil {
		return nil, nil, err
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

	envs := []string{}
	envs = append(envs, os.Environ()...)
	for envName, filePath := range tmpFiles {
		envs = append(envs, fmt.Sprintf("%s=%s", envName, filePath))
	}
	envs = append(envs, helm.NewClient().CommandEnv()...)

	cmd := executor.MakeCommand("", e.Hook.GetPath(), []string{}, envs)

	err = executor.RunAndLogLines(cmd, e.LogLabels)
	if err != nil {
		return nil, nil, err
	}

	patches[utils.ConfigMapPatch], err = utils.ValuesPatchFromFile(e.ConfigValuesPatchPath)
	if err != nil {
		return nil, nil, fmt.Errorf("got bad json patch for config values: %s", err)
	}

	patches[utils.MemoryValuesPatch], err = utils.ValuesPatchFromFile(e.ValuesPatchPath)
	if err != nil {
		return nil, nil, fmt.Errorf("got bad json patch for values: %s", err)
	}

	metrics, err = metric_operation.MetricOperationsFromFile(e.MetricsPath)
	if err != nil {
		return nil, nil, fmt.Errorf("got bad metrics: %s", err)
	}

	return patches, metrics, nil
}

func (e *HookExecutor) RunGoHook() (patches map[utils.ValuesPatchType]*utils.ValuesPatch, metrics []metric_operation.MetricOperation, err error) {
	goHook := e.Hook.GetGoHook()
	if goHook == nil {
		return
	}

	// prepare hook input
	input := &sdk.HookInput{
		BindingContexts: e.Context,
		ConfigValues:    e.Hook.GetConfigValues(),
		LogLabels:       e.LogLabels,
	}

	// Values are patched in-place, so an error can occur.
	input.Values, err = e.Hook.GetValues()
	if err != nil {
		return nil, nil, err
	}

	output, err := goHook.Run(input)
	if err != nil {
		return nil, nil, err
	}

	patches = map[utils.ValuesPatchType]*utils.ValuesPatch{
		utils.ConfigMapPatch:    output.ConfigValuesPatches,
		utils.MemoryValuesPatch: output.MemoryValuesPatches,
	}

	return patches, output.Metrics, output.Error
}

func (e *HookExecutor) Config() (configOutput []byte, err error) {
	// Config() is called directly for go hooks
	if e.Hook.GetGoHook() != nil {
		return nil, nil
	}

	envs := []string{}
	envs = append(envs, os.Environ()...)
	envs = append(envs, helm.NewClient().CommandEnv()...)

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
