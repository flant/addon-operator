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

type HookResult struct {
	Usage   *executor.CmdUsage
	Patches map[utils.ValuesPatchType]*utils.ValuesPatch
	Metrics []metric_operation.MetricOperation
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

	envs := []string{}
	envs = append(envs, os.Environ()...)
	for envName, filePath := range tmpFiles {
		envs = append(envs, fmt.Sprintf("%s=%s", envName, filePath))
	}
	envs = append(envs, helm.NewClient().CommandEnv()...)

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

	return result, nil
}

func (e *HookExecutor) RunGoHook() (result *HookResult, err error) {
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
		return nil, err
	}

	output, err := goHook.Run(input)
	if err != nil {
		return nil, err
	}

	result = &HookResult{
		Patches: map[utils.ValuesPatchType]*utils.ValuesPatch{
			utils.ConfigMapPatch:    output.ConfigValuesPatches,
			utils.MemoryValuesPatch: output.MemoryValuesPatches,
		},
		Metrics: output.Metrics,
	}

	return result, output.Error
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
