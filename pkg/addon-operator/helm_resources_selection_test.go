package addon_operator

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/deckhouse/deckhouse/pkg/log"
)

// TestParseHelmResourcesLabelSelector_Defaults pins the contract that the
// default app.ExtraLabels value ("heritage=addon-operator") yields the
// equivalent map[string]string for the dedup-backed lister. Drift here
// would silently break absent-resource detection: the legacy cache does the
// match at watch level via labels.Selector, the dedup path needs the same
// equality requirements rendered as MatchingLabels.
func TestParseHelmResourcesLabelSelector_Defaults(t *testing.T) {
	got, err := parseHelmResourcesLabelSelector("heritage=addon-operator")
	if err != nil {
		t.Fatalf("parseHelmResourcesLabelSelector: %v", err)
	}
	want := map[string]string{"heritage": "addon-operator"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestParseHelmResourcesLabelSelector_MultiKey covers the case where users
// extend app.ExtraLabels with extra equality requirements (a documented,
// supported configuration). The dedup-backed lister applies all of them as
// MatchingLabels at list time.
func TestParseHelmResourcesLabelSelector_MultiKey(t *testing.T) {
	got, err := parseHelmResourcesLabelSelector("heritage=addon-operator,tier=control-plane")
	if err != nil {
		t.Fatalf("parseHelmResourcesLabelSelector: %v", err)
	}
	want := map[string]string{
		"heritage": "addon-operator",
		"tier":     "control-plane",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestParseHelmResourcesLabelSelector_RejectsSetBased pins the explicit
// guard documented in parseHelmResourcesLabelSelector: set-based
// requirements (e.g. "k in (a,b)") cannot be honoured by the dedup-backed
// lister's MatchingLabels path, and silently dropping them would convert
// foreign objects into false-positive existence matches. We require the
// operator to fail loudly at startup instead.
func TestParseHelmResourcesLabelSelector_RejectsSetBased(t *testing.T) {
	if _, err := parseHelmResourcesLabelSelector("heritage in (addon-operator)"); err == nil {
		t.Error("expected error for set-based requirement, got nil")
	}
}

// TestParseHelmResourcesLabelSelector_Empty allows an empty raw string
// (the env value is unset) to map to a nil map so the lister opts out of
// list-time filtering instead of throwing — matches the legacy cr_cache
// behaviour where an empty selector means "no filter".
func TestParseHelmResourcesLabelSelector_Empty(t *testing.T) {
	got, err := parseHelmResourcesLabelSelector("")
	if err != nil {
		t.Fatalf("parseHelmResourcesLabelSelector: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil for empty input", got)
	}
}

// TestParseHelmResourcesLabelSelector_RejectsMalformed makes sure parser
// errors propagate. Operator startup converts these into a hard failure
// (newHelmResourcesManager returns the error from this function), so a
// silent fall-through here would be a real foot-gun.
func TestParseHelmResourcesLabelSelector_RejectsMalformed(t *testing.T) {
	if _, err := parseHelmResourcesLabelSelector("===nope==="); err == nil {
		t.Error("expected error for malformed selector, got nil")
	}
}

// TestNewHelmResourcesManager_DedupCacheRequiresSharedManager pins the
// "loud-on-bug" contract introduced when option (b) decoupled
// HelmResourcesCache from --dedup-client-enabled. Until this change a
// useDedupCache=true caller could silently fall back to the cr_cache path
// when the dedup client was not initialised; that masked configuration
// errors and made memory diagnostics ambiguous (the operator was running
// the legacy cache while logs/flags suggested the dedup one). The new
// contract is: when the operator declares it wants the dedup cache but
// addon-operator failed to construct its SharedStoreManager, we surface
// the failure as an error from newHelmResourcesManager so it can never be
// confused for a successfully wired dedup cache. A nil kubeClient is fine
// here — the function fails fast on the manager check before touching it.
func TestNewHelmResourcesManager_DedupCacheRequiresSharedManager(t *testing.T) {
	_, err := newHelmResourcesManager(
		context.Background(),
		nil,
		nil,
		true,
		0,
		log.NewNop(),
	)
	if err == nil {
		t.Fatal("expected error when useDedupCache=true and sharedMgr=nil, got nil")
	}
	if !strings.Contains(err.Error(), "SharedStoreManager") {
		t.Errorf("error must mention SharedStoreManager (so on-call can grep it), got: %v", err)
	}
}
