package addon_operator

import (
	"testing"

	"github.com/deckhouse/deckhouse/pkg/log"

	"github.com/flant/addon-operator/pkg/app"
)

// TestResolveConfig_WithConfig_IgnoresEnv pins the central contract of
// WithConfig: when a library caller supplies a *app.Config, environment
// variables MUST NOT override its values.
func TestResolveConfig_WithConfig_IgnoresEnv(t *testing.T) {
	// Set env vars that would normally take effect on a fresh config.
	t.Setenv("ADDON_OPERATOR_MODULES_DIR", "/from/env")
	t.Setenv("ADDON_OPERATOR_NAMESPACE", "ns-from-env")
	t.Setenv("ADDON_OPERATOR_LISTEN_PORT", "9999")
	t.Setenv("HELM_HISTORY_MAX", "777")
	t.Setenv("LOG_LEVEL", "debug")

	// Caller-supplied config with explicit values that differ from env.
	in := app.NewConfig()
	in.App.ModulesDir = "/from/caller"
	in.App.Namespace = "ns-from-caller"
	in.App.ListenPort = "1111"
	in.Helm.HistoryMax = 3
	in.Log.Level = "error"

	out := resolveConfig(in, log.NewNop())

	if out != in {
		t.Fatalf("resolveConfig must return the supplied *Config pointer unchanged when WithConfig is used")
	}
	if out.App.ModulesDir != "/from/caller" {
		t.Errorf("ModulesDir: got %q, env must NOT override caller value", out.App.ModulesDir)
	}
	if out.App.Namespace != "ns-from-caller" {
		t.Errorf("Namespace: got %q, env must NOT override caller value", out.App.Namespace)
	}
	if out.App.ListenPort != "1111" {
		t.Errorf("ListenPort: got %q, env must NOT override caller value", out.App.ListenPort)
	}
	if out.Helm.HistoryMax != 3 {
		t.Errorf("Helm.HistoryMax: got %d, env must NOT override caller value", out.Helm.HistoryMax)
	}
	if out.Log.Level != "error" {
		t.Errorf("Log.Level: got %q, env must NOT override caller value", out.Log.Level)
	}
}

// TestResolveConfig_NoConfig_AppliesEnv ensures the CLI / binary path still
// works: with no caller config, defaults are taken from NewConfig and then
// overlaid with environment variables.
func TestResolveConfig_NoConfig_AppliesEnv(t *testing.T) {
	t.Setenv("ADDON_OPERATOR_MODULES_DIR", "/from/env")
	t.Setenv("ADDON_OPERATOR_NAMESPACE", "ns-from-env")
	t.Setenv("LOG_LEVEL", "debug")

	out := resolveConfig(nil, log.NewNop())

	if out == nil {
		t.Fatal("resolveConfig(nil) returned nil")
	}
	if out.App.ModulesDir != "/from/env" {
		t.Errorf("ModulesDir: got %q, want /from/env", out.App.ModulesDir)
	}
	if out.App.Namespace != "ns-from-env" {
		t.Errorf("Namespace: got %q, want ns-from-env", out.App.Namespace)
	}
	if out.Log.Level != "debug" {
		t.Errorf("Log.Level: got %q, want debug", out.Log.Level)
	}
}

// TestResolveConfig_NoConfig_NoEnv returns plain defaults from NewConfig.
func TestResolveConfig_NoConfig_NoEnv(t *testing.T) {
	defaults := app.NewConfig()

	out := resolveConfig(nil, log.NewNop())

	if out == nil {
		t.Fatal("resolveConfig(nil) returned nil")
	}
	if out.App.ListenAddress != defaults.App.ListenAddress {
		t.Errorf("ListenAddress: got %q, want %q", out.App.ListenAddress, defaults.App.ListenAddress)
	}
	if out.App.ListenPort != defaults.App.ListenPort {
		t.Errorf("ListenPort: got %q, want %q", out.App.ListenPort, defaults.App.ListenPort)
	}
	if out.Helm.HistoryMax != defaults.Helm.HistoryMax {
		t.Errorf("Helm.HistoryMax: got %d, want %d", out.Helm.HistoryMax, defaults.Helm.HistoryMax)
	}
	if out.Log.Level != defaults.Log.Level {
		t.Errorf("Log.Level: got %q, want %q", out.Log.Level, defaults.Log.Level)
	}
}

// TestWithConfig_StoresConfigPointer ensures WithConfig wires the same
// *app.Config into AddonOperator unchanged, so resolveConfig's pass-through
// behavior is reachable through the public Option API.
func TestWithConfig_StoresConfigPointer(t *testing.T) {
	cfg := app.NewConfig()
	cfg.App.Namespace = "library-mode"

	ao := &AddonOperator{}
	WithConfig(cfg)(ao)

	if ao.config != cfg {
		t.Fatal("WithConfig did not store the supplied *Config on the operator")
	}
	if ao.config.App.Namespace != "library-mode" {
		t.Errorf("Namespace: got %q, want library-mode", ao.config.App.Namespace)
	}
}
