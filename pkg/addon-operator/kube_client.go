package addon_operator

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/deckhouse/deckhouse/pkg/log"
	metricsstorage "github.com/deckhouse/deckhouse/pkg/metrics-storage"
	"github.com/ldmonster/kubeclient"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"

	"github.com/flant/addon-operator/pkg"
	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm_resources_manager"
	klient "github.com/flant/kube-client/client"
	"github.com/flant/shell-operator/pkg/metric"
)

// DefaultHelmMonitorKubeClientMetricLabels are labels that indicates go client metrics producer.
// Important! These labels should be consistent with similar labels in ShellOperator!
var DefaultHelmMonitorKubeClientMetricLabels = map[string]string{pkg.MetricKeyComponent: "helm_monitor"}

// defaultKubeClient initializes a Kubernetes client
func defaultKubeClient(metricStorage metricsstorage.Storage, metricLabels map[string]string, logger *log.Logger) *klient.Client {
	client := klient.New(klient.WithLogger(logger))
	client.WithContextName(app.KubeContext)
	client.WithConfigPath(app.KubeConfig)
	client.WithMetricStorage(metric.NewMetricsAdapter(metricStorage, logger.Named("kube-client-metrics-adapter")))
	client.WithMetricLabels(metricLabels)
	client.WithMetricPrefix("nelm_resource_manager_")

	return client
}

// InitDefaultHelmResourcesManager constructs the helm-resources manager.
//
// sharedMgr is the addon-operator-owned *kubeclient.SharedStoreManager
// (created in NewAddonOperator when at least one dedup-cache consumer
// opted in). It is optional: when nil, the manager falls back to its
// own cr_cache.Cache regardless of useDedupCache, so library consumers
// who never set up a SharedStoreManager get the legacy behaviour without
// extra wiring.
//
// useDedupCache is the user-facing toggle (--dedup-client-helm-resources-cache).
// The cache path is only chosen when both useDedupCache=true AND a
// SharedStoreManager is available; this matches the semantics documented
// on app.Config.DedupClient.HelmResourcesCache and keeps the selection
// logic in one place.
func InitDefaultHelmResourcesManager(
	ctx context.Context,
	namespace string,
	metricStorage metricsstorage.Storage,
	sharedMgr *kubeclient.SharedStoreManager,
	useDedupCache bool,
	dedupReconstructLRUSize int,
	logger *log.Logger,
) (helm_resources_manager.HelmResourcesManager, error) {
	kubeClient := defaultKubeClient(metricStorage, DefaultHelmMonitorKubeClientMetricLabels, logger.Named("helm-monitor-kube-client"))
	kubeClient.WithRateLimiterSettings(app.HelmMonitorKubeClientQps, app.HelmMonitorKubeClientBurst)

	if err := kubeClient.Init(); err != nil {
		return nil, fmt.Errorf("initialize Kubernetes client for Helm resources manager: %s\n", err)
	}

	managerLogger := logger.Named("helm-resource-manager")

	mgr, err := newHelmResourcesManager(ctx, kubeClient, sharedMgr, useDedupCache, dedupReconstructLRUSize, managerLogger)
	if err != nil {
		return nil, fmt.Errorf("initialize Helm resources manager: %s\n", err)
	}

	mgr.WithDefaultNamespace(namespace)

	return mgr, nil
}

// newHelmResourcesManager picks between the legacy cr_cache path and the
// shared-dedup-store path. Extracted so the selection logic is testable in
// isolation and the call site reads top-down.
//
// The dedup path is taken iff useDedupCache is true AND the operator
// successfully constructed its SharedStoreManager (sharedMgr != nil).
// A `useDedupCache=true && sharedMgr=nil` combination is treated as a
// programmer error in addon-operator's own setup (NewAddonOperator should
// have constructed the manager when dedupConsumersOptedIn returned true)
// and surfaces as an explicit error rather than a silent fallback — that
// way the failure mode is loud and stays loud across binary restarts.
//
// The label match for the dedup-backed list path is parsed from
// app.ExtraLabels — same source the legacy cache uses for its watch-level
// label selector, so the absent-resource check considers exactly the same
// objects either way.
func newHelmResourcesManager(
	ctx context.Context,
	kubeClient *klient.Client,
	sharedMgr *kubeclient.SharedStoreManager,
	useDedupCache bool,
	dedupReconstructLRUSize int,
	logger *log.Logger,
) (helm_resources_manager.HelmResourcesManager, error) {
	if !useDedupCache {
		return helm_resources_manager.NewHelmResourcesManager(ctx, kubeClient, logger)
	}
	if sharedMgr == nil {
		return nil, fmt.Errorf("dedup-client-helm-resources-cache=true but no SharedStoreManager was constructed; " +
			"this is an addon-operator bootstrap bug — NewAddonOperator must build the manager before Setup()")
	}

	labelMatch, err := parseHelmResourcesLabelSelector(app.ExtraLabels)
	if err != nil {
		return nil, fmt.Errorf("parse helm resources label selector: %w", err)
	}

	// Per-consumer DedupClient: shares the SharedStoreManager's underlying
	// DedupStore + informer map with any other addon-operator consumer
	// that will be added later, but keeps an independent LRU here so hot
	// reads don't evict each other. ReconstructLRUSize is reused from the
	// existing DedupClient settings — when zero, the LRU is disabled and
	// reads go through the dedup store directly on every Get.
	clientOpts := []kubeclient.Option{}
	if dedupReconstructLRUSize > 0 {
		clientOpts = append(clientOpts, kubeclient.WithReconstructionCache(dedupReconstructLRUSize))
	}
	dedupClient := sharedMgr.NewClient(clientOpts...)

	logger.Info("helm resources manager: using shared dedup store via SharedStoreManager.NewClient",
		slog.Any("label_match", labelMatch),
		slog.Int("reconstruct_lru_size", dedupReconstructLRUSize))

	lister := helm_resources_manager.NewDedupClientLister(dedupClient, labelMatch)
	return helm_resources_manager.NewHelmResourcesManagerWithLister(ctx, kubeClient, lister, logger)
}

// parseHelmResourcesLabelSelector turns app.ExtraLabels (a comma-separated
// "k=v,k=v" string, default "heritage=addon-operator") into the equality-only
// map[string]string the dedup-backed lister expects. Non-equality
// requirements (e.g. "k in (a,b)") are intentionally rejected — the
// dedup-cache list path can only honour MatchingLabels-style equality, and
// silently dropping requirements would mask false-positive absent-resource
// detections.
func parseHelmResourcesLabelSelector(raw string) (map[string]string, error) {
	if raw == "" {
		return nil, nil
	}
	sel, err := metav1.ParseToLabelSelector(raw)
	if err != nil {
		return nil, fmt.Errorf("parse %q: %w", raw, err)
	}
	if len(sel.MatchExpressions) > 0 {
		return nil, fmt.Errorf("dedup-client-backed helm resources cache does not support set-based requirements in %q", raw)
	}
	out := make(map[string]string, len(sel.MatchLabels))
	for k, v := range sel.MatchLabels {
		out[k] = v
	}
	// Validate the combined selector parses back as a labels.Selector for
	// consistency with the legacy cache; the lister itself uses the map
	// form via cr_client.MatchingLabels.
	if _, err := k8slabels.Parse(raw); err != nil {
		return nil, fmt.Errorf("validate %q as labels.Selector: %w", raw, err)
	}
	return out, nil
}
