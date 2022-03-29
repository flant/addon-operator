// +build !release

package kube_config_manager

import "github.com/flant/addon-operator/pkg/utils"

type MockKubeConfigManager struct {
	KubeConfigManager
}

func (kcm MockKubeConfigManager) SaveGlobalConfigValues(values utils.Values) error {
	return nil
}

func (kcm MockKubeConfigManager) SaveModuleConfigValues(moduleName string, values utils.Values) error {
	return nil
}
