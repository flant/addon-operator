package app

import (
	"reflect"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// newTestCmd builds a minimal root + start cobra pair and calls BindFlags on it.
func newTestCmd(cfg *Config) (*cobra.Command, *cobra.Command) {
	root := &cobra.Command{Use: "addon-operator"}
	start := &cobra.Command{Use: "start"}
	BindFlags(cfg, root, start)
	root.AddCommand(start)
	return root, start
}

// parseFlags parses args against the start sub-command.
//
// We must set RunE on the start command (not root.Commands()[0]) because
// bindDebugFlags adds a "debug-options" sub-command to root inside BindFlags
// — so root.Commands() actually contains [debug-options, start]. Setting
// RunE on the right command is also what makes start "Runnable", which is a
// precondition for cobra to invoke PreRunE (used by bindDedupClientFlags to
// apply its late-merge of CLI vs env-derived []string values).
func parseFlags(t *testing.T, cfg *Config, args ...string) {
	t.Helper()
	root, start := newTestCmd(cfg)
	root.SetArgs(append([]string{"start"}, args...))
	start.RunE = func(_ *cobra.Command, _ []string) error { return nil }
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestNewConfig_Defaults(t *testing.T) {
	cfg := NewConfig()

	if cfg.App.ModulesDir != ModulesDir {
		t.Errorf("ModulesDir: got %q, want %q", cfg.App.ModulesDir, ModulesDir)
	}
	if cfg.App.GlobalHooksDir != GlobalHooksDir {
		t.Errorf("GlobalHooksDir: got %q, want %q", cfg.App.GlobalHooksDir, GlobalHooksDir)
	}
	if cfg.App.TempDir != DefaultTempDir {
		t.Errorf("TempDir: got %q, want %q", cfg.App.TempDir, DefaultTempDir)
	}
	if cfg.App.ListenAddress != ListenAddress {
		t.Errorf("ListenAddress: got %q, want %q", cfg.App.ListenAddress, ListenAddress)
	}
	if cfg.App.ListenPort != ListenPort {
		t.Errorf("ListenPort: got %q, want %q", cfg.App.ListenPort, ListenPort)
	}
	if cfg.App.ConfigMapName != ConfigMapName {
		t.Errorf("ConfigMapName: got %q, want %q", cfg.App.ConfigMapName, ConfigMapName)
	}
	if cfg.App.PrometheusMetricsPrefix != DefaultPrometheusMetricsPrefix {
		t.Errorf("PrometheusMetricsPrefix: got %q, want %q", cfg.App.PrometheusMetricsPrefix, DefaultPrometheusMetricsPrefix)
	}
	if cfg.App.UnnumberedModuleOrder != UnnumberedModuleOrder {
		t.Errorf("UnnumberedModuleOrder: got %d, want %d", cfg.App.UnnumberedModuleOrder, UnnumberedModuleOrder)
	}
	if cfg.App.ExtraLabels != ExtraLabels {
		t.Errorf("ExtraLabels: got %q, want %q", cfg.App.ExtraLabels, ExtraLabels)
	}
	if cfg.App.CRDsFilters != CRDsFilters {
		t.Errorf("CRDsFilters: got %q, want %q", cfg.App.CRDsFilters, CRDsFilters)
	}
	if cfg.Helm.HistoryMax != Helm3HistoryMax {
		t.Errorf("Helm.HistoryMax: got %d, want %d", cfg.Helm.HistoryMax, Helm3HistoryMax)
	}
	if cfg.Helm.Timeout != Helm3Timeout {
		t.Errorf("Helm.Timeout: got %v, want %v", cfg.Helm.Timeout, Helm3Timeout)
	}
	if cfg.Admission.ListenPort != AdmissionServerListenPort {
		t.Errorf("Admission.ListenPort: got %q, want %q", cfg.Admission.ListenPort, AdmissionServerListenPort)
	}
	if cfg.Admission.Enabled {
		t.Error("Admission.Enabled: expected false by default")
	}
	if cfg.Log.Level != "info" {
		t.Errorf("Log.Level: got %q, want %q", cfg.Log.Level, "info")
	}
	if cfg.Log.Type != "text" {
		t.Errorf("Log.Type: got %q, want %q", cfg.Log.Type, "text")
	}
	if cfg.Debug.UnixSocket != DefaultDebugUnixSocket {
		t.Errorf("Debug.UnixSocket: got %q, want %q", cfg.Debug.UnixSocket, DefaultDebugUnixSocket)
	}
}

func TestParseEnv_OverridesDefaults(t *testing.T) {
	t.Setenv("ADDON_OPERATOR_MODULES_DIR", "/custom/modules")
	t.Setenv("ADDON_OPERATOR_NAMESPACE", "my-ns")
	t.Setenv("ADDON_OPERATOR_LISTEN_PORT", "9999")
	t.Setenv("HELM_HISTORY_MAX", "5")
	t.Setenv("ADDON_OPERATOR_ADMISSION_SERVER_ENABLED", "true")
	t.Setenv("LOG_LEVEL", "debug")

	cfg := NewConfig()
	if err := ParseEnv(cfg); err != nil {
		t.Fatalf("ParseEnv: %v", err)
	}

	if cfg.App.ModulesDir != "/custom/modules" {
		t.Errorf("ModulesDir: got %q, want /custom/modules", cfg.App.ModulesDir)
	}
	if cfg.App.Namespace != "my-ns" {
		t.Errorf("Namespace: got %q, want my-ns", cfg.App.Namespace)
	}
	if cfg.App.ListenPort != "9999" {
		t.Errorf("ListenPort: got %q, want 9999", cfg.App.ListenPort)
	}
	if cfg.Helm.HistoryMax != 5 {
		t.Errorf("Helm.HistoryMax: got %d, want 5", cfg.Helm.HistoryMax)
	}
	if !cfg.Admission.Enabled {
		t.Error("Admission.Enabled: expected true")
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("Log.Level: got %q, want debug", cfg.Log.Level)
	}
}

// TestParseEnv_AppliesShellOperatorEnv ensures the six SHELL_OPERATOR_*
// variables that shell-operator's own *app.Config would parse but used to be
// silently dropped by addon-operator are now folded into the addon-operator
// config (and therefore also flow into the *shapp.Config sent to the
// embedded engine).
func TestParseEnv_AppliesShellOperatorEnv(t *testing.T) {
	t.Setenv("SHELL_OPERATOR_HOOKS_DIR", "/from/shell-op/hooks")
	t.Setenv("SHELL_OPERATOR_TMP_DIR", "/from/shell-op/tmp")
	t.Setenv("SHELL_OPERATOR_LISTEN_ADDRESS", "10.0.0.1")
	t.Setenv("SHELL_OPERATOR_LISTEN_PORT", "9700")
	t.Setenv("SHELL_OPERATOR_PROMETHEUS_METRICS_PREFIX", "shop_")
	t.Setenv("SHELL_OPERATOR_NAMESPACE", "shop-ns")

	cfg := NewConfig()
	if err := ParseEnv(cfg); err != nil {
		t.Fatalf("ParseEnv: %v", err)
	}

	if cfg.App.GlobalHooksDir != "/from/shell-op/hooks" {
		t.Errorf("GlobalHooksDir: got %q, want /from/shell-op/hooks", cfg.App.GlobalHooksDir)
	}
	if cfg.App.TempDir != "/from/shell-op/tmp" {
		t.Errorf("TempDir: got %q, want /from/shell-op/tmp", cfg.App.TempDir)
	}
	if cfg.App.ListenAddress != "10.0.0.1" {
		t.Errorf("ListenAddress: got %q, want 10.0.0.1", cfg.App.ListenAddress)
	}
	if cfg.App.ListenPort != "9700" {
		t.Errorf("ListenPort: got %q, want 9700", cfg.App.ListenPort)
	}
	if cfg.App.PrometheusMetricsPrefix != "shop_" {
		t.Errorf("PrometheusMetricsPrefix: got %q, want shop_", cfg.App.PrometheusMetricsPrefix)
	}
	if cfg.App.Namespace != "shop-ns" {
		t.Errorf("Namespace: got %q, want shop-ns", cfg.App.Namespace)
	}
}

// TestParseEnv_AddonOperatorEnvBeatsShellOperatorEnv pins the precedence
// rule: when both ADDON_OPERATOR_* (or shared) and SHELL_OPERATOR_* are set,
// the addon-operator-side variable wins.
func TestParseEnv_AddonOperatorEnvBeatsShellOperatorEnv(t *testing.T) {
	// SHELL_OPERATOR_* — should be overridden by the addon-operator equivalents below.
	t.Setenv("SHELL_OPERATOR_HOOKS_DIR", "/shop/hooks")
	t.Setenv("SHELL_OPERATOR_TMP_DIR", "/shop/tmp")
	t.Setenv("SHELL_OPERATOR_LISTEN_ADDRESS", "10.0.0.1")
	t.Setenv("SHELL_OPERATOR_LISTEN_PORT", "9700")
	t.Setenv("SHELL_OPERATOR_PROMETHEUS_METRICS_PREFIX", "shop_")
	t.Setenv("SHELL_OPERATOR_NAMESPACE", "shop-ns")

	// ADDON_OPERATOR_* / shared — must win.
	t.Setenv("ADDON_OPERATOR_GLOBAL_HOOKS_DIR", "/addon/hooks")
	t.Setenv("ADDON_OPERATOR_TMP_DIR", "/addon/tmp")
	t.Setenv("ADDON_OPERATOR_LISTEN_ADDRESS", "127.0.0.1")
	t.Setenv("ADDON_OPERATOR_LISTEN_PORT", "9650")
	t.Setenv("ADDON_OPERATOR_PROMETHEUS_METRICS_PREFIX", "ao_")
	t.Setenv("ADDON_OPERATOR_NAMESPACE", "ao-ns")

	cfg := NewConfig()
	if err := ParseEnv(cfg); err != nil {
		t.Fatalf("ParseEnv: %v", err)
	}

	checks := map[string]struct{ got, want string }{
		"GlobalHooksDir":          {cfg.App.GlobalHooksDir, "/addon/hooks"},
		"TempDir":                 {cfg.App.TempDir, "/addon/tmp"},
		"ListenAddress":           {cfg.App.ListenAddress, "127.0.0.1"},
		"ListenPort":              {cfg.App.ListenPort, "9650"},
		"PrometheusMetricsPrefix": {cfg.App.PrometheusMetricsPrefix, "ao_"},
		"Namespace":               {cfg.App.Namespace, "ao-ns"},
	}
	for name, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: got %q, want %q (ADDON_OPERATOR_* must beat SHELL_OPERATOR_*)", name, c.got, c.want)
		}
	}
}

func TestBindFlags_AppFlags(t *testing.T) {
	cfg := NewConfig()
	parseFlags(t, cfg,
		"--modules-dir=/my/mods",
		"--global-hooks-dir=/my/hooks",
		"--tmp-dir=/my/tmp",
		"--namespace=test-ns",
		"--prometheus-listen-address=127.0.0.1",
		"--prometheus-listen-port=9700",
		"--prometheus-metrics-prefix=myprefix_",
		"--config-map=my-cm",
		"--shell-chroot-dir=/chroot",
		"--strict-check-values-mode-enabled=true",
		"--applied-module-extenders=ext1,ext2",
		"--crd-extra-labels=foo=bar",
		"--crd-filters=doc-",
		"--unnumbered-modules-order=5",
	)

	if cfg.App.ModulesDir != "/my/mods" {
		t.Errorf("ModulesDir: got %q", cfg.App.ModulesDir)
	}
	if cfg.App.GlobalHooksDir != "/my/hooks" {
		t.Errorf("GlobalHooksDir: got %q", cfg.App.GlobalHooksDir)
	}
	if cfg.App.TempDir != "/my/tmp" {
		t.Errorf("TempDir: got %q", cfg.App.TempDir)
	}
	if cfg.App.Namespace != "test-ns" {
		t.Errorf("Namespace: got %q", cfg.App.Namespace)
	}
	if cfg.App.ListenAddress != "127.0.0.1" {
		t.Errorf("ListenAddress: got %q", cfg.App.ListenAddress)
	}
	if cfg.App.ListenPort != "9700" {
		t.Errorf("ListenPort: got %q", cfg.App.ListenPort)
	}
	if cfg.App.PrometheusMetricsPrefix != "myprefix_" {
		t.Errorf("PrometheusMetricsPrefix: got %q", cfg.App.PrometheusMetricsPrefix)
	}
	if cfg.App.ConfigMapName != "my-cm" {
		t.Errorf("ConfigMapName: got %q", cfg.App.ConfigMapName)
	}
	if cfg.App.ShellChrootDir != "/chroot" {
		t.Errorf("ShellChrootDir: got %q", cfg.App.ShellChrootDir)
	}
	if !cfg.App.StrictModeEnabled {
		t.Error("StrictModeEnabled: expected true")
	}
	if cfg.App.AppliedExtenders != "ext1,ext2" {
		t.Errorf("AppliedExtenders: got %q", cfg.App.AppliedExtenders)
	}
	if cfg.App.ExtraLabels != "foo=bar" {
		t.Errorf("ExtraLabels: got %q", cfg.App.ExtraLabels)
	}
	if cfg.App.CRDsFilters != "doc-" {
		t.Errorf("CRDsFilters: got %q", cfg.App.CRDsFilters)
	}
	if cfg.App.UnnumberedModuleOrder != 5 {
		t.Errorf("UnnumberedModuleOrder: got %d", cfg.App.UnnumberedModuleOrder)
	}
}

func TestBindFlags_HelmFlags(t *testing.T) {
	cfg := NewConfig()
	parseFlags(t, cfg,
		"--helm-history-max=7",
		"--helm-timeout=5m",
		"--helm-ignore-release=my-release",
		"--helm-monitor-kube-client-qps=3.5",
		"--helm-monitor-kube-client-burst=20",
	)

	if cfg.Helm.HistoryMax != 7 {
		t.Errorf("Helm.HistoryMax: got %d, want 7", cfg.Helm.HistoryMax)
	}
	if cfg.Helm.Timeout != 5*time.Minute {
		t.Errorf("Helm.Timeout: got %v, want 5m", cfg.Helm.Timeout)
	}
	if cfg.Helm.IgnoreRelease != "my-release" {
		t.Errorf("Helm.IgnoreRelease: got %q", cfg.Helm.IgnoreRelease)
	}
	if cfg.Helm.MonitorKubeClientQps != 3.5 {
		t.Errorf("Helm.MonitorKubeClientQps: got %v, want 3.5", cfg.Helm.MonitorKubeClientQps)
	}
	if cfg.Helm.MonitorKubeClientBurst != 20 {
		t.Errorf("Helm.MonitorKubeClientBurst: got %d, want 20", cfg.Helm.MonitorKubeClientBurst)
	}
}

func TestBindFlags_AdmissionFlags(t *testing.T) {
	cfg := NewConfig()
	parseFlags(t, cfg,
		"--admission-server-listen-port=9700",
		"--admission-server-certs-dir=/certs",
		"--admission-server-enabled=true",
	)

	if cfg.Admission.ListenPort != "9700" {
		t.Errorf("Admission.ListenPort: got %q", cfg.Admission.ListenPort)
	}
	if cfg.Admission.CertsDir != "/certs" {
		t.Errorf("Admission.CertsDir: got %q", cfg.Admission.CertsDir)
	}
	if !cfg.Admission.Enabled {
		t.Error("Admission.Enabled: expected true")
	}
}

func TestBindFlags_KubeFlags(t *testing.T) {
	cfg := NewConfig()
	parseFlags(t, cfg,
		"--kube-context=my-ctx",
		"--kube-config=/home/user/.kube/config",
		"--kube-server=https://10.0.0.1:6443",
		"--kube-client-qps=20",
		"--kube-client-burst=40",
		"--object-patcher-kube-client-qps=8",
		"--object-patcher-kube-client-burst=16",
		"--object-patcher-kube-client-timeout=30s",
	)

	if cfg.Kube.Context != "my-ctx" {
		t.Errorf("Kube.Context: got %q", cfg.Kube.Context)
	}
	if cfg.Kube.Config != "/home/user/.kube/config" {
		t.Errorf("Kube.Config: got %q", cfg.Kube.Config)
	}
	if cfg.Kube.Server != "https://10.0.0.1:6443" {
		t.Errorf("Kube.Server: got %q", cfg.Kube.Server)
	}
	if cfg.Kube.ClientQPS != 20 {
		t.Errorf("Kube.ClientQPS: got %v", cfg.Kube.ClientQPS)
	}
	if cfg.Kube.ClientBurst != 40 {
		t.Errorf("Kube.ClientBurst: got %d", cfg.Kube.ClientBurst)
	}
	if cfg.ObjectPatcher.KubeClientQPS != 8 {
		t.Errorf("ObjectPatcher.KubeClientQPS: got %v", cfg.ObjectPatcher.KubeClientQPS)
	}
	if cfg.ObjectPatcher.KubeClientBurst != 16 {
		t.Errorf("ObjectPatcher.KubeClientBurst: got %d", cfg.ObjectPatcher.KubeClientBurst)
	}
	if cfg.ObjectPatcher.KubeClientTimeout != 30*time.Second {
		t.Errorf("ObjectPatcher.KubeClientTimeout: got %v", cfg.ObjectPatcher.KubeClientTimeout)
	}
}

func TestBindFlags_LogFlags(t *testing.T) {
	cfg := NewConfig()
	parseFlags(t, cfg,
		"--log-level=debug",
		"--log-type=json",
		"--log-no-time=true",
		"--log-proxy-hook-json=true",
	)

	if cfg.Log.Level != "debug" {
		t.Errorf("Log.Level: got %q", cfg.Log.Level)
	}
	if cfg.Log.Type != "json" {
		t.Errorf("Log.Type: got %q", cfg.Log.Type)
	}
	if !cfg.Log.NoTime {
		t.Error("Log.NoTime: expected true")
	}
	if !cfg.Log.ProxyHookJSON {
		t.Error("Log.ProxyHookJSON: expected true")
	}
}

// TestDebugUnixSocket_DefaultIsBakedIn pins the package-level default so
// debug sub-commands that bind to &DebugUnixSocket get a sensible value
// even before any ApplyConfig call.
func TestDebugUnixSocket_DefaultIsBakedIn(t *testing.T) {
	if DebugUnixSocket != DefaultDebugUnixSocket {
		t.Errorf("DebugUnixSocket default: got %q, want %q", DebugUnixSocket, DefaultDebugUnixSocket)
	}
}

// TestApplyConfig_NilIsNoop documents that ApplyConfig tolerates a nil cfg
// so callers don't need to guard at every call site. Mirrors the shell-operator
// app.ApplyConfig contract.
func TestApplyConfig_NilIsNoop(t *testing.T) {
	prev := DebugUnixSocket
	t.Cleanup(func() { DebugUnixSocket = prev })

	DebugUnixSocket = "/run/sentinel.socket"
	ApplyConfig(nil)

	if DebugUnixSocket != "/run/sentinel.socket" {
		t.Errorf("ApplyConfig(nil) altered globals: DebugUnixSocket=%q", DebugUnixSocket)
	}
}

// TestBindDebugFlags_SyncsDebugUnixSocketGlobal guarantees that registering
// debug flags propagates cfg.Debug.UnixSocket to the package-level
// DebugUnixSocket global. Debug sub-commands (global, module, ...) bind their
// own --debug-unix-socket flag to that global via DefineDebugUnixSocketFlag,
// so this is what makes them pick up env/default-merged values without going
// through the start command path.
func TestBindDebugFlags_SyncsDebugUnixSocketGlobal(t *testing.T) {
	prev := DebugUnixSocket
	t.Cleanup(func() { DebugUnixSocket = prev })

	DebugUnixSocket = "/stale/value.socket"

	cfg := NewConfig()
	cfg.Debug.UnixSocket = "/synced/from/cfg.socket"

	root := &cobra.Command{Use: "addon-operator"}
	start := &cobra.Command{Use: "start"}
	BindFlags(cfg, root, start)

	if DebugUnixSocket != "/synced/from/cfg.socket" {
		t.Errorf("BindFlags should sync DebugUnixSocket global from cfg: got %q, want %q",
			DebugUnixSocket, "/synced/from/cfg.socket")
	}
}

func TestBindFlags_DebugFlags(t *testing.T) {
	cfg := NewConfig()
	parseFlags(t, cfg,
		"--debug-unix-socket=/tmp/my.sock",
		"--debug-http-addr=:9100",
		"--debug-keep-tmp-files=true",
		"--debug-kubernetes-api=true",
	)

	if cfg.Debug.UnixSocket != "/tmp/my.sock" {
		t.Errorf("Debug.UnixSocket: got %q", cfg.Debug.UnixSocket)
	}
	if cfg.Debug.HTTPServerAddr != ":9100" {
		t.Errorf("Debug.HTTPServerAddr: got %q", cfg.Debug.HTTPServerAddr)
	}
	if !cfg.Debug.KeepTmpFiles {
		t.Error("Debug.KeepTmpFiles: expected true")
	}
	if !cfg.Debug.KubernetesAPI {
		t.Error("Debug.KubernetesAPI: expected true")
	}
}

func TestBindFlags_FlagOverridesEnv(t *testing.T) {
	t.Setenv("ADDON_OPERATOR_NAMESPACE", "from-env")

	cfg := NewConfig()
	if err := ParseEnv(cfg); err != nil {
		t.Fatalf("ParseEnv: %v", err)
	}

	// env sets namespace to "from-env"; the flag should win
	parseFlags(t, cfg, "--namespace=from-flag")

	if cfg.App.Namespace != "from-flag" {
		t.Errorf("Namespace: got %q, want from-flag (flag should override env)", cfg.App.Namespace)
	}
}

func TestApplyConfig_CopiesAllFields(t *testing.T) {
	cfg := NewConfig()
	cfg.App.ModulesDir = "/applied/modules"
	cfg.App.Namespace = "applied-ns"
	cfg.App.ListenAddress = "1.2.3.4"
	cfg.App.ListenPort = "1234"
	cfg.App.ConfigMapName = "applied-cm"
	cfg.App.PrometheusMetricsPrefix = "applied_"
	cfg.App.UnnumberedModuleOrder = 99
	cfg.App.ShellChrootDir = "/applied/chroot"
	cfg.App.StrictModeEnabled = true
	cfg.App.AppliedExtenders = "e1"
	cfg.App.ExtraLabels = "k=v"
	cfg.App.CRDsFilters = "x-"
	cfg.Helm.HistoryMax = 3
	cfg.Helm.Timeout = 7 * time.Minute
	cfg.Helm.IgnoreRelease = "skip-me"
	cfg.Helm.MonitorKubeClientQps = 2.0
	cfg.Helm.MonitorKubeClientBurst = 4
	cfg.Admission.ListenPort = "8888"
	cfg.Admission.CertsDir = "/certs"
	cfg.Admission.Enabled = true
	cfg.Kube.Context = "kctx"
	cfg.Kube.Config = "/kube/cfg"
	cfg.Kube.Server = "https://k8s"
	cfg.Kube.ClientQPS = 11
	cfg.Kube.ClientBurst = 22
	cfg.ObjectPatcher.KubeClientQPS = 3
	cfg.ObjectPatcher.KubeClientBurst = 6
	cfg.ObjectPatcher.KubeClientTimeout = 15 * time.Second
	cfg.Debug.UnixSocket = "/dbg.sock"
	cfg.Debug.HTTPServerAddr = ":9200"
	cfg.Debug.KeepTmpFiles = true
	cfg.Debug.KubernetesAPI = true
	cfg.Log.Level = "error"
	cfg.Log.Type = "color"
	cfg.Log.NoTime = true
	cfg.Log.ProxyHookJSON = true

	ApplyConfig(cfg)

	checks := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"ModulesDir", ModulesDir, "/applied/modules"},
		{"Namespace", Namespace, "applied-ns"},
		{"ListenAddress", ListenAddress, "1.2.3.4"},
		{"ListenPort", ListenPort, "1234"},
		{"ConfigMapName", ConfigMapName, "applied-cm"},
		{"PrometheusMetricsPrefix", PrometheusMetricsPrefix, "applied_"},
		{"UnnumberedModuleOrder", UnnumberedModuleOrder, 99},
		{"ShellChrootDir", ShellChrootDir, "/applied/chroot"},
		{"StrictModeEnabled", StrictModeEnabled, true},
		{"AppliedExtenders", AppliedExtenders, "e1"},
		{"ExtraLabels", ExtraLabels, "k=v"},
		{"CRDsFilters", CRDsFilters, "x-"},
		{"Helm3HistoryMax", Helm3HistoryMax, int32(3)},
		{"Helm3Timeout", Helm3Timeout, 7 * time.Minute},
		{"HelmIgnoreRelease", HelmIgnoreRelease, "skip-me"},
		{"HelmMonitorKubeClientQps", HelmMonitorKubeClientQps, float32(2.0)},
		{"HelmMonitorKubeClientBurst", HelmMonitorKubeClientBurst, 4},
		{"AdmissionServerListenPort", AdmissionServerListenPort, "8888"},
		{"AdmissionServerCertsDir", AdmissionServerCertsDir, "/certs"},
		{"AdmissionServerEnabled", AdmissionServerEnabled, true},
		{"KubeContext", KubeContext, "kctx"},
		{"KubeConfig", KubeConfig, "/kube/cfg"},
		{"KubeServer", KubeServer, "https://k8s"},
		{"KubeClientQPS", KubeClientQPS, float32(11)},
		{"KubeClientBurst", KubeClientBurst, 22},
		{"ObjectPatcherKubeClientQPS", ObjectPatcherKubeClientQPS, float32(3)},
		{"ObjectPatcherKubeClientBurst", ObjectPatcherKubeClientBurst, 6},
		{"ObjectPatcherKubeClientTimeout", ObjectPatcherKubeClientTimeout, 15 * time.Second},
		{"DebugUnixSocket", DebugUnixSocket, "/dbg.sock"},
		{"DebugHTTPServerAddr", DebugHTTPServerAddr, ":9200"},
		{"DebugKeepTmpFiles", DebugKeepTmpFiles, true},
		{"DebugKubernetesAPI", DebugKubernetesAPI, true},
		{"LogLevel", LogLevel, "error"},
		{"LogType", LogType, "color"},
		{"LogNoTime", LogNoTime, true},
		{"LogProxyHookJSON", LogProxyHookJSON, true},
	}

	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("ApplyConfig %s: got %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestBindFlags_AllFlagsRegistered(t *testing.T) {
	expected := []string{
		"modules-dir", "unnumbered-modules-order", "global-hooks-dir", "tmp-dir",
		"namespace", "prometheus-listen-address", "prometheus-listen-port",
		"prometheus-metrics-prefix", "config-map", "shell-chroot-dir",
		"strict-check-values-mode-enabled", "applied-module-extenders",
		"crd-extra-labels", "crd-filters",
		"helm-history-max", "helm-timeout", "helm-ignore-release",
		"helm-monitor-kube-client-qps", "helm-monitor-kube-client-burst",
		"admission-server-listen-port", "admission-server-certs-dir", "admission-server-enabled",
		"kube-context", "kube-config", "kube-server",
		"kube-client-qps", "kube-client-burst",
		"object-patcher-kube-client-qps", "object-patcher-kube-client-burst",
		"object-patcher-kube-client-timeout",
		"dedup-client-enabled", "dedup-client-namespace", "dedup-client-watch-gvk",
		"dedup-client-reconstruct-lru-size", "dedup-client-gc-interval",
		"log-level", "log-type", "log-no-time", "log-proxy-hook-json",
		"debug-unix-socket", "debug-http-addr", "debug-keep-tmp-files", "debug-kubernetes-api",
	}

	cfg := NewConfig()
	_, start := newTestCmd(cfg)

	for _, name := range expected {
		if start.Flags().Lookup(name) == nil {
			t.Errorf("flag --%s not registered on start command", name)
		}
	}
}

// TestBindFlags_DedupClient_Defaults verifies that with no env vars and no
// CLI flags, the DedupClient block is fully zero-valued — i.e. the feature
// is opt-in and addon-operator's existing in-cluster behavior is unchanged
// for users who never touch the new knobs.
func TestBindFlags_DedupClient_Defaults(t *testing.T) {
	cfg := NewConfig()
	parseFlags(t, cfg)

	if cfg.DedupClient.Enabled {
		t.Error("DedupClient.Enabled: must default to false (opt-in feature)")
	}
	if len(cfg.DedupClient.Namespaces) != 0 {
		t.Errorf("DedupClient.Namespaces: got %v, want empty by default", cfg.DedupClient.Namespaces)
	}
	if len(cfg.DedupClient.WatchGVKs) != 0 {
		t.Errorf("DedupClient.WatchGVKs: got %v, want empty by default", cfg.DedupClient.WatchGVKs)
	}
	if cfg.DedupClient.ReconstructLRUSize != 0 {
		t.Errorf("DedupClient.ReconstructLRUSize: got %d, want 0", cfg.DedupClient.ReconstructLRUSize)
	}
	if cfg.DedupClient.GCInterval != 0 {
		t.Errorf("DedupClient.GCInterval: got %v, want 0", cfg.DedupClient.GCInterval)
	}
}

// TestBindFlags_DedupClient_FlagOverridesEnv pins the precedence rule: an
// explicit --dedup-client-* flag invocation always replaces the env-derived
// default, even for repeatable []string fields. This mirrors shell-operator's
// own bindDedupClientFlags semantics so both operators behave identically.
func TestBindFlags_DedupClient_FlagOverridesEnv(t *testing.T) {
	t.Setenv("DEDUP_CLIENT_ENABLED", "true")
	t.Setenv("DEDUP_CLIENT_NAMESPACES", "from-env-1,from-env-2")
	t.Setenv("DEDUP_CLIENT_WATCH_GVKS", "/v1/Pod,apps/v1/Deployment")
	t.Setenv("DEDUP_CLIENT_RECONSTRUCT_LRU_SIZE", "1024")
	t.Setenv("DEDUP_CLIENT_GC_INTERVAL", "1m")

	cfg := NewConfig()
	if err := ParseEnv(cfg); err != nil {
		t.Fatalf("ParseEnv: %v", err)
	}

	parseFlags(t, cfg,
		"--dedup-client-enabled=false",
		"--dedup-client-namespace=cli-ns-1",
		"--dedup-client-namespace=cli-ns-2",
		"--dedup-client-watch-gvk=/v1/ConfigMap",
		"--dedup-client-reconstruct-lru-size=2048",
		"--dedup-client-gc-interval=30s",
	)

	if cfg.DedupClient.Enabled {
		t.Error("Enabled: --dedup-client-enabled=false must override env value true")
	}
	wantNS := []string{"cli-ns-1", "cli-ns-2"}
	if !reflect.DeepEqual(cfg.DedupClient.Namespaces, wantNS) {
		t.Errorf("Namespaces: got %v, want %v (CLI must replace env)", cfg.DedupClient.Namespaces, wantNS)
	}
	wantGVKs := []string{"/v1/ConfigMap"}
	if !reflect.DeepEqual(cfg.DedupClient.WatchGVKs, wantGVKs) {
		t.Errorf("WatchGVKs: got %v, want %v (CLI must replace env)", cfg.DedupClient.WatchGVKs, wantGVKs)
	}
	if cfg.DedupClient.ReconstructLRUSize != 2048 {
		t.Errorf("ReconstructLRUSize: got %d, want 2048", cfg.DedupClient.ReconstructLRUSize)
	}
	if cfg.DedupClient.GCInterval != 30*time.Second {
		t.Errorf("GCInterval: got %v, want 30s", cfg.DedupClient.GCInterval)
	}
}

// TestBindFlags_DedupClient_EnvKeptWhenNoCLI ensures the env-derived []string
// values survive when the user does not pass the corresponding CLI flag.
// Without the late-merge in bindDedupClientFlags, pflag's default would
// silently set Namespaces/WatchGVKs to nil because their CLI default is nil.
func TestBindFlags_DedupClient_EnvKeptWhenNoCLI(t *testing.T) {
	t.Setenv("DEDUP_CLIENT_NAMESPACES", "kube-system,default")
	t.Setenv("DEDUP_CLIENT_WATCH_GVKS", "/v1/Pod,apps/v1/Deployment")

	cfg := NewConfig()
	if err := ParseEnv(cfg); err != nil {
		t.Fatalf("ParseEnv: %v", err)
	}

	parseFlags(t, cfg)

	wantNS := []string{"kube-system", "default"}
	if !reflect.DeepEqual(cfg.DedupClient.Namespaces, wantNS) {
		t.Errorf("Namespaces: got %v, want %v (env must be kept when no CLI override)",
			cfg.DedupClient.Namespaces, wantNS)
	}
	wantGVKs := []string{"/v1/Pod", "apps/v1/Deployment"}
	if !reflect.DeepEqual(cfg.DedupClient.WatchGVKs, wantGVKs) {
		t.Errorf("WatchGVKs: got %v, want %v (env must be kept when no CLI override)",
			cfg.DedupClient.WatchGVKs, wantGVKs)
	}
}

func TestBindFlags_DebugOptionsSubcommandRegistered(t *testing.T) {
	cfg := NewConfig()
	root, _ := newTestCmd(cfg)

	var found bool
	for _, sub := range root.Commands() {
		if sub.Use == "debug-options" {
			found = true
			if !sub.Hidden {
				t.Error("debug-options command should be hidden")
			}
			break
		}
	}
	if !found {
		t.Error("debug-options subcommand not registered on root command")
	}
}
