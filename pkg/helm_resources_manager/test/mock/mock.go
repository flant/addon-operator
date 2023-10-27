package mock

import (
	"context"

	. "github.com/flant/addon-operator/pkg/helm_resources_manager"
	. "github.com/flant/addon-operator/pkg/helm_resources_manager/types"
	klient "github.com/flant/kube-client/client"
	"github.com/flant/kube-client/manifest"
)

type MockHelmResourcesManager struct {
	MonitorsNames []string
}

func (h *MockHelmResourcesManager) WithContext(_ context.Context) {}

func (h *MockHelmResourcesManager) WithKubeClient(_ *klient.Client) {}

func (h *MockHelmResourcesManager) WithDefaultNamespace(_ string) {}

func (h *MockHelmResourcesManager) Stop() {}

func (h *MockHelmResourcesManager) StopMonitors() {}

func (h *MockHelmResourcesManager) PauseMonitors() {}

func (h *MockHelmResourcesManager) ResumeMonitors() {}

func (h *MockHelmResourcesManager) StartMonitor(_ string, _ []manifest.Manifest, _ string) {
}

func (h *MockHelmResourcesManager) HasMonitor(_ string) bool {
	return false
}
func (h *MockHelmResourcesManager) StopMonitor(_ string)   {}
func (h *MockHelmResourcesManager) PauseMonitor(_ string)  {}
func (h *MockHelmResourcesManager) ResumeMonitor(_ string) {}

func (h *MockHelmResourcesManager) AbsentResources(_ string) ([]manifest.Manifest, error) {
	return nil, nil
}

func (h *MockHelmResourcesManager) GetMonitor(_ string) *ResourcesMonitor {
	return nil
}

func (h *MockHelmResourcesManager) GetAbsentResources(_ []manifest.Manifest, _ string) ([]manifest.Manifest, error) {
	return nil, nil
}

func (h *MockHelmResourcesManager) Ch() chan AbsentResourcesEvent {
	return nil
}
