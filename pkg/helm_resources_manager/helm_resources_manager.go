package helm_resources_manager

import (
	"context"

	log "github.com/sirupsen/logrus"

	"github.com/flant/shell-operator/pkg/kube"
	"github.com/flant/shell-operator/pkg/utils/manifest"

	. "github.com/flant/addon-operator/pkg/helm_resources_manager/types"
)

type HelmResourcesManager interface {
	WithContext(ctx context.Context)
	WithKubeClient(client kube.KubernetesClient)
	WithDefaultNamespace(namespace string)
	Stop()
	StopMonitors()
	StartMonitor(moduleName string, manifests []manifest.Manifest, defaultNamespace string)
	HasMonitor(moduleName string) bool
	StopMonitor(moduleName string)
	PauseMonitor(moduleName string)
	ResumeMonitor(moduleName string)
	AbsentResources(moduleName string) ([]manifest.Manifest, error)
	GetAbsentResources(templates []manifest.Manifest, defaultNamespace string) ([]manifest.Manifest, error)
	Ch() chan AbsentResourcesEvent
}

type helmResourcesManager struct {
	ctx    context.Context
	cancel context.CancelFunc

	Namespace string

	kubeClient kube.KubernetesClient

	monitors map[string]*ResourcesMonitor

	eventCh chan AbsentResourcesEvent
}

var _ HelmResourcesManager = &helmResourcesManager{}

func NewHelmResourcesManager() HelmResourcesManager {
	return &helmResourcesManager{
		eventCh:  make(chan AbsentResourcesEvent),
		monitors: make(map[string]*ResourcesMonitor),
	}
}

func (hm *helmResourcesManager) WithKubeClient(client kube.KubernetesClient) {
	hm.kubeClient = client
}

func (hm *helmResourcesManager) WithDefaultNamespace(namespace string) {
	hm.Namespace = namespace
}

func (hm *helmResourcesManager) WithContext(ctx context.Context) {
	hm.ctx, hm.cancel = context.WithCancel(ctx)
}

func (hm *helmResourcesManager) Stop() {
	if hm.cancel != nil {
		hm.cancel()
	}
}

func (hm *helmResourcesManager) Ch() chan AbsentResourcesEvent {
	return hm.eventCh
}

func (hm *helmResourcesManager) StartMonitor(moduleName string, manifests []manifest.Manifest, defaultNamespace string) {
	hm.StopMonitor(moduleName)

	rm := NewResourcesMonitor()
	rm.WithKubeClient(hm.kubeClient)
	rm.WithContext(hm.ctx)
	rm.WithModuleName(moduleName)
	rm.WithManifests(manifests)
	rm.WithDefaultNamespace(defaultNamespace)
	rm.WithAbsentCb(hm.absentResourcesCallback)

	hm.monitors[moduleName] = rm
	rm.Start()
}

func (hm *helmResourcesManager) absentResourcesCallback(moduleName string, absent []manifest.Manifest, defaultNs string) {
	log.Debugf("Detect absent resources for %s", moduleName)
	for _, m := range absent {
		log.Debugf("%s/%s/%s", m.Namespace(defaultNs), m.Kind(), m.Name())
	}
	hm.eventCh <- AbsentResourcesEvent{
		ModuleName: moduleName,
		Absent:     absent,
	}
}

func (hm *helmResourcesManager) StopMonitors() {
	for moduleName := range hm.monitors {
		hm.StopMonitor(moduleName)
	}
}

func (hm *helmResourcesManager) StopMonitor(moduleName string) {
	if monitor, ok := hm.monitors[moduleName]; ok {
		monitor.Stop()
		delete(hm.monitors, moduleName)
	}
}

func (hm *helmResourcesManager) PauseMonitor(moduleName string) {
	if monitor, ok := hm.monitors[moduleName]; ok {
		monitor.Pause()
	}
}

func (hm *helmResourcesManager) ResumeMonitor(moduleName string) {
	if monitor, ok := hm.monitors[moduleName]; ok {
		monitor.Resume()
	}
}

func (hm *helmResourcesManager) HasMonitor(moduleName string) bool {
	_, ok := hm.monitors[moduleName]
	return ok
}

func (hm *helmResourcesManager) AbsentResources(moduleName string) ([]manifest.Manifest, error) {
	if monitor, ok := hm.monitors[moduleName]; ok {
		return monitor.AbsentResources()
	}
	return nil, nil
}

func (hm *helmResourcesManager) GetAbsentResources(manifests []manifest.Manifest, defaultNamespace string) ([]manifest.Manifest, error) {
	rm := NewResourcesMonitor()
	rm.WithKubeClient(hm.kubeClient)
	rm.WithManifests(manifests)
	rm.WithDefaultNamespace(defaultNamespace)
	return rm.AbsentResources()
}
