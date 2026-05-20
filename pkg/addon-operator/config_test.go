package addon_operator

import (
	"reflect"
	"testing"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"

	"github.com/flant/addon-operator/pkg/app"
	shapp "github.com/flant/shell-operator/pkg/app"
)

// TestResolveConfig_WithConfig_IgnoresEnv pins the central contract of
// WithConfig: when a library caller supplies a *app.Config, environment
// variables MUST NOT override its values. This covers ADDON_OPERATOR_*,
// shared envs (KUBE_*, LOG_*, ...) AND the shell-operator-side
// SHELL_OPERATOR_* variables that ParseEnv now also recognizes.
func TestResolveConfig_WithConfig_IgnoresEnv(t *testing.T) {
	// ADDON_OPERATOR_* / shared envs.
	t.Setenv("ADDON_OPERATOR_MODULES_DIR", "/from/env")
	t.Setenv("ADDON_OPERATOR_NAMESPACE", "ns-from-env")
	t.Setenv("ADDON_OPERATOR_LISTEN_PORT", "9999")
	t.Setenv("HELM_HISTORY_MAX", "777")
	t.Setenv("LOG_LEVEL", "debug")

	// SHELL_OPERATOR_* envs — must also be ignored when WithConfig is used.
	t.Setenv("SHELL_OPERATOR_HOOKS_DIR", "/shop/hooks")
	t.Setenv("SHELL_OPERATOR_NAMESPACE", "shop-ns")
	t.Setenv("SHELL_OPERATOR_LISTEN_PORT", "9700")

	// Caller-supplied config with explicit values that differ from every env.
	in := app.NewConfig()
	in.App.ModulesDir = "/from/caller"
	in.App.GlobalHooksDir = "/caller/global-hooks"
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
	if out.App.GlobalHooksDir != "/caller/global-hooks" {
		t.Errorf("GlobalHooksDir: got %q, SHELL_OPERATOR_HOOKS_DIR must NOT override caller value", out.App.GlobalHooksDir)
	}
	if out.App.Namespace != "ns-from-caller" {
		t.Errorf("Namespace: got %q, neither ADDON_OPERATOR_NAMESPACE nor SHELL_OPERATOR_NAMESPACE must override caller value", out.App.Namespace)
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

// TestShellOperatorConfig_NilIn returns nil for a nil input so the engine
// can fall back to its in-cluster defaults.
func TestShellOperatorConfig_NilIn(t *testing.T) {
	if got := shellOperatorConfig(nil); got != nil {
		t.Errorf("shellOperatorConfig(nil) = %+v, want nil", got)
	}
}

// TestShellOperatorConfig_MapsAllRelevantFields pins the bridge between
// addon-operator's *app.Config and shell-operator's *shapp.Config. Any future
// change to the mapping (added/removed/renamed field) must be reflected here
// so the contract "addon-operator config drives shell-operator" stays
// truthful.
func TestShellOperatorConfig_MapsAllRelevantFields(t *testing.T) {
	in := app.NewConfig()
	in.App.GlobalHooksDir = "/global-hooks"
	in.App.TempDir = "/tmp/addon"
	in.App.ListenAddress = "1.2.3.4"
	in.App.ListenPort = "9700"
	in.App.PrometheusMetricsPrefix = "ao_"
	in.App.Namespace = "addon-ns"

	in.Kube.Context = "kctx"
	in.Kube.Config = "/kube/cfg"
	in.Kube.Server = "https://k8s"
	in.Kube.ClientQPS = 11
	in.Kube.ClientBurst = 22

	in.ObjectPatcher.KubeClientQPS = 3
	in.ObjectPatcher.KubeClientBurst = 6
	in.ObjectPatcher.KubeClientTimeout = 15 * time.Second

	in.Debug.UnixSocket = "/dbg.sock"
	in.Debug.HTTPServerAddr = ":9200"
	in.Debug.KeepTmpFiles = true
	in.Debug.KubernetesAPI = true

	in.Log.Level = "error"
	in.Log.Type = "color"
	in.Log.NoTime = true
	in.Log.ProxyHookJSON = true

	out := shellOperatorConfig(in)
	if out == nil {
		t.Fatal("shellOperatorConfig returned nil for a non-nil input")
	}

	// App
	if out.App.HooksDir != "/global-hooks" {
		t.Errorf("App.HooksDir: got %q, want /global-hooks (must come from App.GlobalHooksDir)", out.App.HooksDir)
	}
	if out.App.TempDir != "/tmp/addon" {
		t.Errorf("App.TempDir: got %q, want /tmp/addon", out.App.TempDir)
	}
	if out.App.ListenAddress != "1.2.3.4" {
		t.Errorf("App.ListenAddress: got %q, want 1.2.3.4", out.App.ListenAddress)
	}
	if out.App.ListenPort != "9700" {
		t.Errorf("App.ListenPort: got %q, want 9700", out.App.ListenPort)
	}
	if out.App.PrometheusMetricsPrefix != "ao_" {
		t.Errorf("App.PrometheusMetricsPrefix: got %q, want ao_", out.App.PrometheusMetricsPrefix)
	}
	if out.App.Namespace != "addon-ns" {
		t.Errorf("App.Namespace: got %q, want addon-ns", out.App.Namespace)
	}

	// Kube
	if out.Kube.Context != "kctx" {
		t.Errorf("Kube.Context: got %q", out.Kube.Context)
	}
	if out.Kube.Config != "/kube/cfg" {
		t.Errorf("Kube.Config: got %q", out.Kube.Config)
	}
	if out.Kube.Server != "https://k8s" {
		t.Errorf("Kube.Server: got %q", out.Kube.Server)
	}
	if out.Kube.ClientQPS != 11 {
		t.Errorf("Kube.ClientQPS: got %v", out.Kube.ClientQPS)
	}
	if out.Kube.ClientBurst != 22 {
		t.Errorf("Kube.ClientBurst: got %d", out.Kube.ClientBurst)
	}

	// ObjectPatcher
	if out.ObjectPatcher.KubeClientQPS != 3 {
		t.Errorf("ObjectPatcher.KubeClientQPS: got %v", out.ObjectPatcher.KubeClientQPS)
	}
	if out.ObjectPatcher.KubeClientBurst != 6 {
		t.Errorf("ObjectPatcher.KubeClientBurst: got %d", out.ObjectPatcher.KubeClientBurst)
	}
	if out.ObjectPatcher.KubeClientTimeout != 15*time.Second {
		t.Errorf("ObjectPatcher.KubeClientTimeout: got %v", out.ObjectPatcher.KubeClientTimeout)
	}

	// Debug
	if out.Debug.UnixSocket != "/dbg.sock" {
		t.Errorf("Debug.UnixSocket: got %q", out.Debug.UnixSocket)
	}
	if out.Debug.HTTPServerAddr != ":9200" {
		t.Errorf("Debug.HTTPServerAddr: got %q", out.Debug.HTTPServerAddr)
	}
	if !out.Debug.KeepTempFiles {
		t.Error("Debug.KeepTempFiles: expected true (must come from Debug.KeepTmpFiles)")
	}
	if !out.Debug.KubernetesAPI {
		t.Error("Debug.KubernetesAPI: expected true")
	}

	// Log
	if out.Log.Level != "error" {
		t.Errorf("Log.Level: got %q", out.Log.Level)
	}
	if out.Log.Type != "color" {
		t.Errorf("Log.Type: got %q", out.Log.Type)
	}
	if !out.Log.NoTime {
		t.Error("Log.NoTime: expected true")
	}
	if !out.Log.ProxyHookJSON {
		t.Error("Log.ProxyHookJSON: expected true")
	}

	// Admission/Conversion are intentionally left empty: addon-operator runs
	// its own admission server and does not delegate to shell-operator's
	// webhook implementation.
	if !reflect.DeepEqual(out.Admission, shapp.AdmissionSettings{}) {
		t.Errorf("Admission must be zero-valued, got %+v", out.Admission)
	}
	if !reflect.DeepEqual(out.Conversion, shapp.ConversionSettings{}) {
		t.Errorf("Conversion must be zero-valued, got %+v", out.Conversion)
	}
}
