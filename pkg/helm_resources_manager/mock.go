// +build !release

package helm_resources_manager

import (
	"context"

	klient "github.com/flant/kube-client/client"
	"github.com/flant/kube-client/manifest"

	. "github.com/flant/addon-operator/pkg/helm_resources_manager/types"
)

type MockHelmResourcesManager struct {
	MonitorsNames []string
}

func (h *MockHelmResourcesManager) WithContext(ctx context.Context) {

}
func (h *MockHelmResourcesManager) WithKubeClient(client klient.Client) {

}
func (h *MockHelmResourcesManager) WithDefaultNamespace(namespace string) {

}
func (h *MockHelmResourcesManager) Stop() {

}
func (h *MockHelmResourcesManager) StopMonitors() {

}
func (h *MockHelmResourcesManager) PauseMonitors() {

}
func (h *MockHelmResourcesManager) ResumeMonitors() {

}
func (h *MockHelmResourcesManager) StartMonitor(moduleName string, manifests []manifest.Manifest, defaultNamespace string) {

}
func (h *MockHelmResourcesManager) HasMonitor(moduleName string) bool {
	return false
}
func (h *MockHelmResourcesManager) StopMonitor(moduleName string)  {}
func (h *MockHelmResourcesManager) PauseMonitor(moduleName string) {}
func (h *MockHelmResourcesManager) ResumeMonitor(moduleName string) {
}
func (h *MockHelmResourcesManager) AbsentResources(moduleName string) ([]manifest.Manifest, error) {
	return nil, nil
}
func (h *MockHelmResourcesManager) GetMonitor(moduleName string) *ResourcesMonitor {
	return nil
}
func (h *MockHelmResourcesManager) GetAbsentResources(templates []manifest.Manifest, defaultNamespace string) ([]manifest.Manifest, error) {
	return nil, nil
}
func (h *MockHelmResourcesManager) Ch() chan AbsentResourcesEvent {
	return nil
}
