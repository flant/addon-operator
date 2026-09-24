package kube_config_manager

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// ponytail: kube-client.Client hides the fake clientset's WatchList opt-out, so client-go 0.35
	// informers wait forever for the initial-events bookmark. Drop once kube-client forwards
	// IsWatchListSemanticsUnSupported.
	os.Setenv("KUBE_FEATURE_WatchListClient", "false")
	os.Exit(m.Run())
}
