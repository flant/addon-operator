package addon_operator

import (
	"reflect"
	"testing"

	hrm "github.com/flant/addon-operator/pkg/helm_resources_manager"
)

// TestParseExtraLabels_Defaults pins the contract that the default
// app.ExtraLabels value ("heritage=addon-operator") yields the equivalent
// map[string]string for the dedup-backed lister. Drift here would silently
// break absent-resource detection: the dedup path needs the equality
// requirements rendered as MatchingLabels.
func TestParseExtraLabels_Defaults(t *testing.T) {
	got, err := hrm.ParseExtraLabels("heritage=addon-operator")
	if err != nil {
		t.Fatalf("ParseExtraLabels: %v", err)
	}
	want := map[string]string{"heritage": "addon-operator"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestParseExtraLabels_MultiKey covers the case where users extend
// app.ExtraLabels with extra equality requirements (a documented, supported
// configuration). The dedup-backed lister applies all of them as
// MatchingLabels at list time.
func TestParseExtraLabels_MultiKey(t *testing.T) {
	got, err := hrm.ParseExtraLabels("heritage=addon-operator,tier=control-plane")
	if err != nil {
		t.Fatalf("ParseExtraLabels: %v", err)
	}
	want := map[string]string{
		"heritage": "addon-operator",
		"tier":     "control-plane",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestParseExtraLabels_RejectsSetBased pins the explicit guard: set-based
// requirements (e.g. "k in (a,b)") cannot be honoured by the dedup-backed
// lister's MatchingLabels path, and silently dropping them would convert
// foreign objects into false-positive existence matches. We require the
// operator to fail loudly at startup instead.
func TestParseExtraLabels_RejectsSetBased(t *testing.T) {
	if _, err := hrm.ParseExtraLabels("heritage in (addon-operator)"); err == nil {
		t.Error("expected error for set-based requirement, got nil")
	}
}

// TestParseExtraLabels_Empty allows an empty raw string (the env value is
// unset) to map to a nil map so the lister opts out of list-time filtering
// instead of throwing.
func TestParseExtraLabels_Empty(t *testing.T) {
	got, err := hrm.ParseExtraLabels("")
	if err != nil {
		t.Fatalf("ParseExtraLabels: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil for empty input", got)
	}
}

// TestParseExtraLabels_RejectsMalformed makes sure parser errors propagate.
// Operator startup converts these into a hard failure, so a silent
// fall-through here would be a real foot-gun.
func TestParseExtraLabels_RejectsMalformed(t *testing.T) {
	if _, err := hrm.ParseExtraLabels("===nope==="); err == nil {
		t.Error("expected error for malformed selector, got nil")
	}
}
