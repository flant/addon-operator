package module_manager

import (
	"fmt"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"

	sh_app "github.com/flant/shell-operator/pkg/app"
	"github.com/flant/shell-operator/pkg/executor"
	. "github.com/flant/shell-operator/pkg/hook/binding_context"
	"github.com/flant/shell-operator/pkg/metrics_storage"

	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/utils"
)

type HookExecutor struct {
	Hook                  Hook
	Context               BindingContextList
	ConfigValuesPath      string
	ValuesPath            string
	ContextPath           string
	ConfigValuesPatchPath string
	ValuesPatchPath       string
	MetricsPath           string
	LogLabels             map[string]string
}

func NewHookExecutor(h Hook, context BindingContextList) *HookExecutor {
	return &HookExecutor{
		Hook:      h,
		Context:   context,
		LogLabels: map[string]string{},
	}
}

func (e *HookExecutor) WithLogLabels(logLabels map[string]string) {
	e.LogLabels = logLabels
}

func (e *HookExecutor) Run() (patches map[utils.ValuesPatchType]*utils.ValuesPatch, metrics []metrics_storage.MetricOperation, err error) {
	patches = make(map[utils.ValuesPatchType]*utils.ValuesPatch)

	bindingContextBytes, err := e.Context.Json()
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

	metrics, err = metrics_storage.MetricOperationsFromFile(e.MetricsPath)
	if err != nil {
		return nil, nil, fmt.Errorf("got bad metrics: %s", err)
	}

	return patches, metrics, nil
}

func (e *HookExecutor) Config() (configOutput []byte, err error) {
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
