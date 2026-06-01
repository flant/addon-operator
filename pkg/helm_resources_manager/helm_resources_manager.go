package helm_resources_manager

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/deckhouse/deckhouse/pkg/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"

	"github.com/flant/addon-operator/pkg"
	"github.com/flant/addon-operator/pkg/app"
	. "github.com/flant/addon-operator/pkg/helm_resources_manager/types"
	"github.com/flant/kube-client/manifest"
	"github.com/flant/shell-operator/pkg/kube/dedupclient"
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
	KubeClient() *dedupclient.Client
}

type helmResourcesManager struct {
	ctx    context.Context
	cancel context.CancelFunc

	Namespace string

	// lister is the cache-shape adapter consumed by every spawned
	// ResourcesMonitor. NewHelmResourcesManager wraps a dedicated
	// label-filtered cr_cache.Cache; NewHelmResourcesManagerWithLister lets
	// callers inject an alternative (e.g. a DedupClient-backed lister).
	lister ResourceNameLister

	kubeClient *dedupclient.Client

	eventCh chan ReleaseStatusEvent

	logger *log.Logger

	l        sync.RWMutex
	monitors map[string]*ResourcesMonitor
}

var _ HelmResourcesManager = &helmResourcesManager{}

// NewHelmResourcesManager constructs a helm resources manager that lists
// module resources through the supplied singleton deduplicated kube client.
// No separate cache/connection is created: the manager reads through the
// dedup client's controller-runtime cache, re-applying the heritage label
// parsed from app.ExtraLabels (typically heritage=addon-operator) at List
// time so absent-resource detection only considers operator-managed objects.
func NewHelmResourcesManager(ctx context.Context, kclient *dedupclient.Client, logger *log.Logger) (HelmResourcesManager, error) {
	if kclient == nil {
		return nil, fmt.Errorf("kube client not set")
	}

	labelMatch, err := ParseExtraLabels(app.ExtraLabels)
	if err != nil {
		return nil, err
	}

	lister := NewDedupClientLister(kclient, labelMatch)

	return NewHelmResourcesManagerWithLister(ctx, kclient, lister, logger)
}

// NewHelmResourcesManagerWithLister constructs a helm resources manager
// whose monitors read through the supplied ResourceNameLister. The lister's
// lifecycle (cache startup, sync, shutdown) is the caller's responsibility —
// for the dedup-client path the *dedupclient.Client already runs its own
// Start/Shutdown, so this manager simply piggybacks on it.
//
// Library consumers that want a non-default lister (e.g. an in-memory
// fake for tests, or a user-supplied controller-runtime client) reach this
// constructor directly. addon-operator itself routes through
// InitDefaultHelmResourcesManager.
func NewHelmResourcesManagerWithLister(ctx context.Context, kclient *dedupclient.Client, lister ResourceNameLister, logger *log.Logger) (HelmResourcesManager, error) {
	if kclient == nil {
		return nil, fmt.Errorf("kube client not set")
	}
	if lister == nil {
		return nil, fmt.Errorf("resource name lister not set")
	}

	cctx, cancel := context.WithCancel(ctx)
	return &helmResourcesManager{
		eventCh:    make(chan ReleaseStatusEvent),
		monitors:   make(map[string]*ResourcesMonitor),
		ctx:        cctx,
		cancel:     cancel,
		kubeClient: kclient,
		lister:     lister,
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
		slog.String(pkg.LogKeyModule, moduleName))
	hm.StopMonitor(moduleName)

	cfg := &ResourceMonitorConfig{
		ModuleName:       moduleName,
		Manifests:        manifests,
		DefaultNamespace: defaultNamespace,
		HelmStatusGetter: lastReleaseStatus,
		AbsentCb:         hm.absentResourcesCallback,
		KubeClient:       hm.kubeClient,
		Lister:           hm.lister,

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
		slog.String(pkg.LogKeyModule, moduleName))

	for _, m := range absent {
		log.Debug("absent module",
			slog.String(pkg.LogKeyNamespace, m.Namespace(defaultNs)),
			slog.String(pkg.LogKeyKind, m.Kind()),
			slog.String(pkg.LogKeyModule, m.Name()))
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
		Lister:           hm.lister,

		Logger: hm.logger.Named("resource-monitor"),
	}

	rm := NewResourcesMonitor(hm.ctx, cfg)

	return rm.AbsentResources()
}

func (hm *helmResourcesManager) KubeClient() *dedupclient.Client {
	return hm.kubeClient
}

// ParseExtraLabels turns app.ExtraLabels (a comma-separated "k=v,k=v"
// string, default "heritage=addon-operator") into the equality-only
// map[string]string the dedup-backed lister applies at List time.
// Set-based requirements are rejected: the dedup-cache list path can only
// honour MatchingLabels-style equality, and silently dropping requirements
// would mask false-positive absent-resource detections.
func ParseExtraLabels(raw string) (map[string]string, error) {
	if raw == "" {
		return nil, nil
	}
	sel, err := metav1.ParseToLabelSelector(raw)
	if err != nil {
		return nil, fmt.Errorf("parse %q: %w", raw, err)
	}
	if len(sel.MatchExpressions) > 0 {
		return nil, fmt.Errorf("helm resources cache does not support set-based requirements in %q", raw)
	}
	out := make(map[string]string, len(sel.MatchLabels))
	for k, v := range sel.MatchLabels {
		out[k] = v
	}
	if _, err := k8slabels.Parse(raw); err != nil {
		return nil, fmt.Errorf("validate %q as labels.Selector: %w", raw, err)
	}
	return out, nil
}
