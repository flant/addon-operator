package helm_resources_manager

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/deckhouse/deckhouse/pkg/log"
	"k8s.io/apimachinery/pkg/labels"
	cr_cache "sigs.k8s.io/controller-runtime/pkg/cache"

	"github.com/flant/addon-operator/pkg/app"
	. "github.com/flant/addon-operator/pkg/helm_resources_manager/types"
	klient "github.com/flant/kube-client/client"
	"github.com/flant/kube-client/manifest"
)

type HelmResourcesManager interface {
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

	cache cr_cache.Cache

	kubeClient *klient.Client

	eventCh chan ReleaseStatusEvent

	logger *log.Logger

	l        sync.RWMutex
	monitors map[string]*ResourcesMonitor
}

var _ HelmResourcesManager = &helmResourcesManager{}

func NewHelmResourcesManager(ctx context.Context, kclient *klient.Client, logger *log.Logger) (HelmResourcesManager, error) {
	//nolint:govet
	cctx, cancel := context.WithCancel(ctx)
	if kclient == nil {
		//nolint:govet
		return nil, fmt.Errorf("kube client not set")
	}

	cfg := kclient.RestConfig()
	defaultLabelSelector, err := labels.Parse(app.ExtraLabels)
	if err != nil {
		return nil, err
	}
	cache, err := cr_cache.New(cfg, cr_cache.Options{
		DefaultLabelSelector: defaultLabelSelector,
	})
	if err != nil {
		return nil, err
	}

	go cache.Start(cctx)
	log.Debug("Helm resource manager: cache's been started")
	if synced := cache.WaitForCacheSync(cctx); !synced {
		return nil, fmt.Errorf("Couldn't sync helm resource informer cache")
	}
	log.Debug("Helm resourcer manager: cache has been synced")

	return &helmResourcesManager{
		eventCh:    make(chan ReleaseStatusEvent),
		monitors:   make(map[string]*ResourcesMonitor),
		ctx:        cctx,
		cancel:     cancel,
		kubeClient: kclient,
		cache:      cache,
		logger:     logger,
	}, nil
}

func (hm *helmResourcesManager) WithDefaultNamespace(namespace string) {
	hm.Namespace = namespace
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
	log.Debug("Start helm resources monitor for module",
		slog.String("module", moduleName))
	hm.StopMonitor(moduleName)

	cfg := &ResourceMonitorConfig{
		ModuleName:       moduleName,
		Manifests:        manifests,
		DefaultNamespace: defaultNamespace,
		HelmStatusGetter: lastReleaseStatus,
		AbsentCb:         hm.absentResourcesCallback,
		KubeClient:       hm.kubeClient,
		Cache:            hm.cache,

		Logger: hm.logger.Named("resource-monitor"),
	}

	rm := NewResourcesMonitor(hm.ctx, cfg)

	hm.l.Lock()
	hm.monitors[moduleName] = rm
	hm.l.Unlock()
	rm.Start()
}

func (hm *helmResourcesManager) absentResourcesCallback(moduleName string, unexpectedStatus bool, absent []manifest.Manifest, defaultNs string) {
	log.Debug("Detect absent resources for module",
		slog.String("module", moduleName))
	for _, m := range absent {
		log.Debug("absent module",
			slog.String("namespace", m.Namespace(defaultNs)),
			slog.String("kind", m.Kind()),
			slog.String("module", m.Name()))
	}
	hm.eventCh <- ReleaseStatusEvent{
		ModuleName:       moduleName,
		Absent:           absent,
		UnexpectedStatus: unexpectedStatus,
	}
}

func (hm *helmResourcesManager) StopMonitors() {
	hm.l.Lock()
	for moduleName, monitor := range hm.monitors {
		monitor.Stop()
		delete(hm.monitors, moduleName)
	}
	hm.l.Unlock()
}

func (hm *helmResourcesManager) PauseMonitors() {
	hm.l.RLock()
	for _, monitor := range hm.monitors {
		monitor.Pause()
	}
	hm.l.RUnlock()
}

func (hm *helmResourcesManager) ResumeMonitors() {
	hm.l.RLock()
	for _, monitor := range hm.monitors {
		monitor.Resume()
	}
	hm.l.RUnlock()
}

func (hm *helmResourcesManager) StopMonitor(moduleName string) {
	hm.l.Lock()
	if monitor, ok := hm.monitors[moduleName]; ok {
		monitor.Stop()
		delete(hm.monitors, moduleName)
	}
	hm.l.Unlock()
}

func (hm *helmResourcesManager) PauseMonitor(moduleName string) {
	hm.l.RLock()
	if monitor, ok := hm.monitors[moduleName]; ok {
		monitor.Pause()
	}
	hm.l.RUnlock()
}

func (hm *helmResourcesManager) ResumeMonitor(moduleName string) {
	hm.l.RLock()
	if monitor, ok := hm.monitors[moduleName]; ok {
		monitor.Resume()
	}
	hm.l.RUnlock()
}

func (hm *helmResourcesManager) HasMonitor(moduleName string) bool {
	hm.l.RLock()
	_, ok := hm.monitors[moduleName]
	hm.l.RUnlock()
	return ok
}

func (hm *helmResourcesManager) AbsentResources(moduleName string) ([]manifest.Manifest, error) {
	hm.l.RLock()
	defer hm.l.RUnlock()
	if monitor, ok := hm.monitors[moduleName]; ok {
		return monitor.AbsentResources()
	}
	return nil, nil
}

func (hm *helmResourcesManager) GetMonitor(moduleName string) *ResourcesMonitor {
	hm.l.RLock()
	defer hm.l.RUnlock()
	return hm.monitors[moduleName]
}

func (hm *helmResourcesManager) GetAbsentResources(manifests []manifest.Manifest, defaultNamespace string) ([]manifest.Manifest, error) {
	cfg := &ResourceMonitorConfig{
		Manifests:        manifests,
		DefaultNamespace: defaultNamespace,
		KubeClient:       hm.kubeClient,
		Cache:            hm.cache,

		Logger: hm.logger.Named("resource-monitor"),
	}

	rm := NewResourcesMonitor(hm.ctx, cfg)

	return rm.AbsentResources()
}
