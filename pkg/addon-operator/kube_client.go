package addon_operator

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/deckhouse/deckhouse/pkg/log"
	metricsstorage "github.com/deckhouse/deckhouse/pkg/metrics-storage"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/flant/addon-operator/pkg"
	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm_resources_manager"
	klient "github.com/flant/kube-client/client"
	"github.com/flant/shell-operator/pkg/kube/dedupclient"
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
// dedupClient is optional: when non-nil AND app.Config.DedupClient.HelmResourcesCache
// is true, the manager skips its own cr_cache.Cache and reads through the
// shared dedup client instead. Otherwise the legacy label-filtered cache is
// kept.
//
// The cache choice is intentionally made here (the binary entry point) so
// library consumers that build their own AddonOperator can pass a different
// dedupClient (or nil) without touching helm_resources_manager internals.
func InitDefaultHelmResourcesManager(
	ctx context.Context,
	namespace string,
	metricStorage metricsstorage.Storage,
	dedupClient *dedupclient.Client,
	useDedupCache bool,
	logger *log.Logger,
) (helm_resources_manager.HelmResourcesManager, error) {
	kubeClient := defaultKubeClient(metricStorage, DefaultHelmMonitorKubeClientMetricLabels, logger.Named("helm-monitor-kube-client"))
	kubeClient.WithRateLimiterSettings(app.HelmMonitorKubeClientQps, app.HelmMonitorKubeClientBurst)

	if err := kubeClient.Init(); err != nil {
		return nil, fmt.Errorf("initialize Kubernetes client for Helm resources manager: %s\n", err)
	}

	managerLogger := logger.Named("helm-resource-manager")

	mgr, err := newHelmResourcesManager(ctx, kubeClient, dedupClient, useDedupCache, managerLogger)
	if err != nil {
		return nil, fmt.Errorf("initialize Helm resources manager: %s\n", err)
	}

	mgr.WithDefaultNamespace(namespace)

	return mgr, nil
}

// newHelmResourcesManager picks between the legacy and the dedup-backed
// constructor. Extracted so the selection logic is testable in isolation
// and the call site reads top-down. The dedup path is only taken when:
//
//  1. the runtime DedupClient was actually constructed (Enabled=true);
//  2. the operator opted in to swap the helm-resources cache via
//     --dedup-client-helm-resources-cache (app.Config.DedupClient.HelmResourcesCache).
//
// The label match for the dedup-backed list path is parsed from
// app.ExtraLabels — same source the legacy cache uses for its watch-level
// label selector, so the absent-resource check considers exactly the same
// objects either way.
func newHelmResourcesManager(
	ctx context.Context,
	kubeClient *klient.Client,
	dedupClient *dedupclient.Client,
	useDedupCache bool,
	logger *log.Logger,
) (helm_resources_manager.HelmResourcesManager, error) {
	if !useDedupCache || dedupClient == nil {
		if useDedupCache && dedupClient == nil {
			// Operator asked for the dedup cache but the dedup client
			// was not constructed (--dedup-client-enabled=false). Log
			// loudly and fall through to the legacy cache so the
			// operator still boots; otherwise we'd convert a cache-tier
			// misconfiguration into a hard startup failure.
			logger.Warn("dedup-client-helm-resources-cache requested but dedup client not initialised; falling back to dedicated cr_cache",
				slog.String("hint", "set --dedup-client-enabled=true to take the dedup path"))
		}
		return helm_resources_manager.NewHelmResourcesManager(ctx, kubeClient, logger)
	}

	labelMatch, err := parseHelmResourcesLabelSelector(app.ExtraLabels)
	if err != nil {
		return nil, fmt.Errorf("parse helm resources label selector: %w", err)
	}

	logger.Info("helm resources manager: using shared DedupClient cache",
		slog.Any("label_match", labelMatch))

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
