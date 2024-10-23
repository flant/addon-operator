package helm_resources_manager

import (
	"context"

	. "github.com/flant/addon-operator/pkg/helm_resources_manager/types"
	klient "github.com/flant/kube-client/client"
	"github.com/flant/kube-client/manifest"
	log "github.com/flant/shell-operator/pkg/unilogger"
)

type HelmResourcesManager interface {
	WithContext(ctx context.Context)
	WithKubeClient(client *klient.Client)
	WithDefaultNamespace(namespace string)
	Stop()
	StopMonitors()
	PauseMonitors()
	ResumeMonitors()
	StartMonitor(moduleName string, manifests []manifest.Manifest, defaultNamespace string, LastReleaseStatus func(releaseName string) (revision string, status string, err error))
	HasMonitor(moduleName string) bool
	StopMonitor(moduleName string)
	PauseMonitor(moduleName string)
	ResumeMonitor(moduleName string)
	AbsentResources(moduleName string) ([]manifest.Manifest, error)
	GetMonitor(moduleName string) *ResourcesMonitor
	GetAbsentResources(templates []manifest.Manifest, defaultNamespace string) ([]manifest.Manifest, error)
	Ch() chan ReleaseStatusEvent
}

type helmResourcesManager struct {
	ctx    context.Context
	cancel context.CancelFunc

	Namespace string

	kubeClient *klient.Client

	monitors map[string]*ResourcesMonitor

	eventCh chan ReleaseStatusEvent

	logger *log.Logger
}

var _ HelmResourcesManager = &helmResourcesManager{}

func NewHelmResourcesManager(logger *log.Logger) HelmResourcesManager {
	return &helmResourcesManager{
		eventCh:  make(chan ReleaseStatusEvent),
		monitors: make(map[string]*ResourcesMonitor),
		logger:   logger,
	}
}

func (hm *helmResourcesManager) WithKubeClient(client *klient.Client) {
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

func (hm *helmResourcesManager) Ch() chan ReleaseStatusEvent {
	return hm.eventCh
}

func (hm *helmResourcesManager) StartMonitor(moduleName string, manifests []manifest.Manifest, defaultNamespace string, lastReleaseStatus func(releaseName string) (revision string, status string, err error)) {
	log.Debugf("Start helm resources monitor for '%s'", moduleName)
	hm.StopMonitor(moduleName)

	rm := NewResourcesMonitor(hm.logger.Named("resource-monitor"))
	rm.WithKubeClient(hm.kubeClient)
	rm.WithContext(hm.ctx)
	rm.WithModuleName(moduleName)
	rm.WithManifests(manifests)
	rm.WithDefaultNamespace(defaultNamespace)
	rm.WithStatusGetter(lastReleaseStatus)
	rm.WithAbsentCb(hm.absentResourcesCallback)

	hm.monitors[moduleName] = rm
	rm.Start()
}

func (hm *helmResourcesManager) absentResourcesCallback(moduleName string, unexpectedStatus bool, absent []manifest.Manifest, defaultNs string) {
	log.Debugf("Detect absent resources for %s", moduleName)
	for _, m := range absent {
		log.Debugf("%s/%s/%s", m.Namespace(defaultNs), m.Kind(), m.Name())
	}
	hm.eventCh <- ReleaseStatusEvent{
		ModuleName:       moduleName,
		Absent:           absent,
		UnexpectedStatus: unexpectedStatus,
	}
}

func (hm *helmResourcesManager) StopMonitors() {
	for moduleName := range hm.monitors {
		hm.StopMonitor(moduleName)
	}
}

func (hm *helmResourcesManager) PauseMonitors() {
	for _, monitor := range hm.monitors {
		monitor.Pause()
	}
}

func (hm *helmResourcesManager) ResumeMonitors() {
	for _, monitor := range hm.monitors {
		monitor.Resume()
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

func (hm *helmResourcesManager) GetMonitor(moduleName string) *ResourcesMonitor {
	return hm.monitors[moduleName]
}

func (hm *helmResourcesManager) GetAbsentResources(manifests []manifest.Manifest, defaultNamespace string) ([]manifest.Manifest, error) {
	rm := NewResourcesMonitor(hm.logger.Named("resource-monitor"))
	rm.WithKubeClient(hm.kubeClient)
	rm.WithManifests(manifests)
	rm.WithDefaultNamespace(defaultNamespace)
	return rm.AbsentResources()
}
