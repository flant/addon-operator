package addon_operator

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/deckhouse/deckhouse/pkg/log"
	klient "github.com/flant/kube-client/client"
	"github.com/ldmonster/kubeclient"

	"github.com/flant/addon-operator/pkg/app"
)

// dedupConsumersOptedIn reports whether at least one internal addon-operator
// component requested a dedup-store-backed cache. The dedup-store manager is
// expensive (one extra dynamic client + run-loop goroutine per process), so
// the operator only constructs it when somebody actually consumes it. Today
// the sole consumer is helm_resources_manager via
// app.Config.DedupClient.HelmResourcesCache; new consumers should be added
// here as `|| cfg.<NewToggle>` so the manager construction stays gated by
// real usage.
func dedupConsumersOptedIn(cfg *app.Config) bool {
	if cfg == nil {
		return false
	}
	return cfg.DedupClient.HelmResourcesCache
}

// newDedupSharedStoreManager constructs the addon-operator-private
// *kubeclient.SharedStoreManager. The manager owns a single DedupStore +
// informer map + indexer that every addon-operator component opting into a
// dedup-backed cache shares; each component additionally gets its own
// per-client LRU when it calls mgr.NewClient (controlled by the optional
// kubeclient.WithReconstructionCache option at NewClient time).
//
// The manager is intentionally separate from shell-operator's hook-side
// dedup client (op.engine.DedupClient): at the time of writing
// shell-operator constructs the latter via kubeclient.New, not via
// SharedStoreManager, so the two stores would not be physically shared
// even if we passed shell-operator's client through. Once shell-operator
// switches to SharedStoreManager itself the two paths can be merged by
// passing this manager into AssembleCommonOperatorFromConfig.
//
// Returns (nil, error) only when the kube client is missing prerequisites
// the manager cannot fall back from (no rest.Config). RESTMapper failures
// fall through to kubeclient's default in-memory mapper; the operator logs
// a warning and continues, mirroring shell-operator's initDedupClient
// contract.
func newDedupSharedStoreManager(kubeClient *klient.Client, cfg *app.Config, logger *log.Logger) (*kubeclient.SharedStoreManager, error) {
	if kubeClient == nil {
		return nil, fmt.Errorf("main kube client is nil")
	}
	restCfg := kubeClient.RestConfig()
	if restCfg == nil {
		return nil, fmt.Errorf("main kube client has no rest.Config (was it Init()-ed?)")
	}

	opts := []kubeclient.Option{}

	mapper, mapperErr := kubeClient.ToRESTMapper()
	if mapperErr != nil {
		// Same reasoning as shell-operator's initDedupClient: a missing
		// RESTMapper is not fatal — kubeclient's fallback resolves GVK→GVR
		// using its own in-memory mapper. Log loudly so operators can see
		// the degraded path before they hit the first failed List.
		logger.Warn("could not derive RESTMapper from main kube client; "+
			"dedup shared store manager will fall back to the default in-memory mapper",
			log.Err(mapperErr))
	} else if mapper != nil {
		opts = append(opts, kubeclient.WithRESTMapper(mapper))
	}

	mgr, err := kubeclient.NewSharedStoreManager(restCfg, opts...)
	if err != nil {
		return nil, fmt.Errorf("create kubeclient shared store manager: %w", err)
	}
	logger.Info("dedup shared store manager constructed",
		slog.Bool("rest_mapper", mapper != nil),
		slog.Bool("helm_resources_consumer", cfg.DedupClient.HelmResourcesCache))
	return mgr, nil
}

// startDedupSharedStoreManager spins up the dedup manager's run loop in a
// dedicated goroutine. The Start method blocks until ctx is cancelled, so
// keeping it on a goroutine is required to let the operator continue with
// its own setup. Errors from the run loop are logged at error level but do
// not bring the operator down — the legacy code paths still work without
// the dedup cache (resources_monitor falls back to the cr_cache lister
// path), and a hard failure here would convert a non-critical degradation
// into an unrecoverable boot loop.
//
// Calling this when dedupSharedStoreMgr is nil is a safe no-op so the
// caller does not have to gate.
func (op *AddonOperator) startDedupSharedStoreManager(ctx context.Context) {
	if op.dedupSharedStoreMgr == nil {
		return
	}
	logger := op.Logger.Named("dedup-shared-store")
	go func() {
		logger.Debug("dedup shared store manager run loop starting")
		if err := op.dedupSharedStoreMgr.Start(ctx); err != nil && ctx.Err() == nil {
			logger.Error("dedup shared store manager exited with error", log.Err(err))
			return
		}
		logger.Debug("dedup shared store manager run loop exited")
	}()
}
