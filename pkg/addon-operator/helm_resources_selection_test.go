package addon_operator

import (
	"reflect"
	"testing"
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
