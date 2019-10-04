package module_manager

import (
	"fmt"
	"os"
	"strings"

	"github.com/romana/rlog"

	"github.com/flant/shell-operator/pkg/executor"

	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/utils"
)

type HookExecutor struct {
	Hook Hook
	Context interface{}
	ConfigValuesPath string
	ValuesPath string
	ContextPath string
	ConfigValuesPatchPath string
	ValuesPatchPath string
}

func NewHookExecutor(h Hook, context interface{}) *HookExecutor {
	return &HookExecutor{
		Hook: h,
		Context: context,
	}
}

func (e *HookExecutor) Run() (patches map[utils.ValuesPatchType]*utils.ValuesPatch, err error) {
	patches = make(map[utils.ValuesPatchType]*utils.ValuesPatch)

	tmpFiles, err := e.Hook.PrepareTmpFilesForHookRun(e.Context)
	if err != nil {
		return nil, err
	}
	e.ConfigValuesPatchPath = tmpFiles["CONFIG_VALUES_JSON_PATCH_PATH"]
	e.ValuesPatchPath = tmpFiles["VALUES_JSON_PATCH_PATH"]

	envs := []string{}
	envs = append(envs, os.Environ()...)
	for envName, filePath := range tmpFiles {
		envs = append(envs, fmt.Sprintf("%s=%s", envName, filePath))
	}
	envs = append(envs, helm.Client.CommandEnv()...)

	cmd := executor.MakeCommand("", e.Hook.GetPath(), []string{}, envs)

	err = executor.Run(cmd)
	if err != nil {
		return nil, fmt.Errorf("%s FAILED: %s", e.Hook.GetName(), err)
	}

	patches[utils.ConfigMapPatch], err = utils.ValuesPatchFromFile(e.ConfigValuesPatchPath)
	if err != nil {
		return nil, fmt.Errorf("got bad config values json patch from hook %s: %s", e.Hook.GetName(), err)
	}

	patches[utils.MemoryValuesPatch], err = utils.ValuesPatchFromFile(e.ValuesPatchPath)
	if err != nil {
		return nil, fmt.Errorf("got bad values json patch from hook %s: %s", e.Hook.GetName(), err)
	}

	return patches, nil
}

func (e *HookExecutor) Config() (configOutput []byte, err error) {
	envs := []string{}
	envs = append(envs, os.Environ()...)
	envs = append(envs, helm.Client.CommandEnv()...)

	cmd := executor.MakeCommand("", e.Hook.GetPath(), []string{"--config"}, envs)

	rlog.Debugf("Executing hook in %s: '%s'", cmd.Dir, strings.Join(cmd.Args, " "))
	cmd.Stdout = nil

	output, err := executor.Output(cmd)
	if err != nil {
		rlog.Errorf("Hook '%s' config failed: %v, output:\n%s",  e.Hook.GetName(), err, string(output))
		return nil, fmt.Errorf("%s FAILED: %s", e.Hook.GetName(), err)
	}

	rlog.Debugf("Hook '%s' config output:\n%s", e.Hook.GetName(), string(output))

	return output, nil
}

