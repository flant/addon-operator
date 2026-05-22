package addon_operator

import (
	"testing"

	"github.com/flant/addon-operator/pkg/app"
)

// TestDedupConsumersOptedIn_NilCfg tolerates a nil *app.Config: the operator
// must not panic on this gate before NewAddonOperator has resolved its
// config (e.g. tests that call it directly).
func TestDedupConsumersOptedIn_NilCfg(t *testing.T) {
	if dedupConsumersOptedIn(nil) {
		t.Error("dedupConsumersOptedIn(nil) = true, want false (nil cfg means no opt-in)")
	}
}

// TestDedupConsumersOptedIn_DefaultsAreOff pins the contract that the
// SharedStoreManager is NOT constructed by default. Constructing it is
// expensive (one extra dynamic client + run-loop goroutine + RESTMapper
// derivation), so any silent default-on regression here would be a
// material change in addon-operator's startup footprint.
func TestDedupConsumersOptedIn_DefaultsAreOff(t *testing.T) {
	cfg := app.NewConfig()
	if dedupConsumersOptedIn(cfg) {
		t.Error("dedupConsumersOptedIn(NewConfig()) = true, want false (no consumer opted in by default)")
	}
}

// TestDedupConsumersOptedIn_HelmResourcesCacheOptsIn is the positive
// counterpart: the moment HelmResourcesCache is on, the manager must be
// constructed. Future consumers are expected to extend dedupConsumersOptedIn
// with their own toggle (see the function-level doc) and add a sibling
// test to this file pinning that they participate in the gate.
func TestDedupConsumersOptedIn_HelmResourcesCacheOptsIn(t *testing.T) {
	cfg := app.NewConfig()
	cfg.DedupClient.HelmResourcesCache = true
	if !dedupConsumersOptedIn(cfg) {
		t.Error("dedupConsumersOptedIn: HelmResourcesCache=true must opt the manager in")
	}
}

// TestDedupConsumersOptedIn_EnabledAloneDoesNotOptIn pins the new
// independence rule: --dedup-client-enabled controls shell-operator's
// hook-side DedupClient; it must NOT cause addon-operator to also build
// its private SharedStoreManager. The gate is specifically for
// addon-operator-owned consumers.
func TestDedupConsumersOptedIn_EnabledAloneDoesNotOptIn(t *testing.T) {
	cfg := app.NewConfig()
	cfg.DedupClient.Enabled = true
	if dedupConsumersOptedIn(cfg) {
		t.Error("dedupConsumersOptedIn: Enabled alone must not opt addon-operator's SharedStoreManager in")
	}
}
