package app

import (
	"fmt"
	"os"
	"strconv"
	"time"

	env "github.com/caarlos0/env/v11"
)

type AppSettings struct {
	ModulesDir     string `env:"MODULES_DIR"`
	GlobalHooksDir string `env:"GLOBAL_HOOKS_DIR"`
	TempDir        string `env:"TMP_DIR"`
	Namespace      string `env:"NAMESPACE"`
	ListenAddress  string `env:"LISTEN_ADDRESS"`
	ListenPort     string `env:"LISTEN_PORT"`
	ConfigMapName  string `env:"CONFIG_MAP"`

	PrometheusMetricsPrefix string `env:"PROMETHEUS_METRICS_PREFIX"`

	UnnumberedModuleOrder int `env:"UNNUMBERED_MODULES_ORDER"`

	ShellChrootDir string `env:"SHELL_CHROOT_DIR"`

	StrictModeEnabled bool   `env:"STRICT_CHECK_VALUES_MODE_ENABLED"`
	AppliedExtenders  string `env:"APPLIED_MODULE_EXTENDERS"`
	ExtraLabels       string `env:"CRD_EXTRA_LABELS"`
	CRDsFilters       string `env:"CRD_FILTER_PREFIXES"`
}

type HelmSettings struct {
	HistoryMax    int32         `env:"HISTORY_MAX"`
	Timeout       time.Duration `env:"TIMEOUT"`
	IgnoreRelease string        `env:"IGNORE_RELEASE"`

	MonitorKubeClientQps   float32 `env:"MONITOR_KUBE_CLIENT_QPS"`
	MonitorKubeClientBurst int     `env:"MONITOR_KUBE_CLIENT_BURST"`
}

type AdmissionSettings struct {
	ListenPort string `env:"ADMISSION_SERVER_LISTEN_PORT"`
	CertsDir   string `env:"ADMISSION_SERVER_CERTS_DIR"`
	Enabled    bool   `env:"ADMISSION_SERVER_ENABLED"`
}

type KubeSettings struct {
	Context     string  `env:"CONTEXT"`
	Config      string  `env:"CONFIG"`
	Server      string  `env:"SERVER"`
	ClientQPS   float32 `env:"CLIENT_QPS"`
	ClientBurst int     `env:"CLIENT_BURST"`
}

type ObjectPatcherSettings struct {
	KubeClientQPS     float32       `env:"KUBE_CLIENT_QPS"`
	KubeClientBurst   int           `env:"KUBE_CLIENT_BURST"`
	KubeClientTimeout time.Duration `env:"KUBE_CLIENT_TIMEOUT"`
}

type DebugSettings struct {
	UnixSocket     string `env:"UNIX_SOCKET"`
	HTTPServerAddr string `env:"HTTP_SERVER_ADDR"`
	KeepTmpFiles   bool   `env:"KEEP_TMP_FILES"`
	KubernetesAPI  bool   `env:"KUBERNETES_API"`
}

type LogSettings struct {
	Level         string `env:"LEVEL"`
	Type          string `env:"TYPE"`
	NoTime        bool   `env:"NO_TIME"`
	ProxyHookJSON bool   `env:"PROXY_HOOK_JSON"`
}

// Config is the single source of truth for addon-operator configuration.
// Populate it in stages: NewConfig sets hardcoded defaults,
// ParseEnv overrides with environment variables, BindFlags registers CLI flags
// whose defaults are the current cfg values so that an explicit flag always wins.
// Priority: CLI flags > env vars > hardcoded defaults.
type Config struct {
	App           AppSettings           `envPrefix:"ADDON_OPERATOR_"`
	Helm          HelmSettings          `envPrefix:"HELM_"`
	Admission     AdmissionSettings     `envPrefix:"ADDON_OPERATOR_"`
	Kube          KubeSettings          `envPrefix:"KUBE_"`
	ObjectPatcher ObjectPatcherSettings `envPrefix:"OBJECT_PATCHER_"`
	Debug         DebugSettings         `envPrefix:"DEBUG_"`
	Log           LogSettings           `envPrefix:"LOG_"`
}

func NewConfig() *Config {
	helmMonitorQPS, _ := strconv.ParseFloat(HelmMonitorKubeClientQpsDefault, 32)
	helmMonitorBurst, _ := strconv.Atoi(HelmMonitorKubeClientBurstDefault)

	return &Config{
		App: AppSettings{
			ModulesDir:              ModulesDir,
			GlobalHooksDir:          GlobalHooksDir,
			TempDir:                 DefaultTempDir,
			ListenAddress:           ListenAddress,
			ListenPort:              ListenPort,
			ConfigMapName:           ConfigMapName,
			PrometheusMetricsPrefix: DefaultPrometheusMetricsPrefix,
			UnnumberedModuleOrder:   UnnumberedModuleOrder,
			ExtraLabels:             ExtraLabels,
			CRDsFilters:             CRDsFilters,
		},
		Helm: HelmSettings{
			HistoryMax:             Helm3HistoryMax,
			Timeout:                Helm3Timeout,
			MonitorKubeClientQps:   float32(helmMonitorQPS),
			MonitorKubeClientBurst: helmMonitorBurst,
		},
		Admission: AdmissionSettings{
			ListenPort: AdmissionServerListenPort,
		},
		Kube: KubeSettings{
			ClientQPS:   5,
			ClientBurst: 10,
		},
		ObjectPatcher: ObjectPatcherSettings{
			KubeClientQPS:     5,
			KubeClientBurst:   10,
			KubeClientTimeout: 10 * time.Second,
		},
		Debug: DebugSettings{
			UnixSocket: DefaultDebugUnixSocket,
		},
		Log: LogSettings{
			Level: "info",
			Type:  "text",
		},
	}
}

// ParseEnv overrides cfg fields with values from environment variables.
// Fields whose env var is not set retain their current values, so hardcoded
// defaults from NewConfig are preserved when no env var is present.
//
// Both addon-operator's own variables (ADDON_OPERATOR_*, HELM_*, shared
// KUBE_*, OBJECT_PATCHER_*, DEBUG_*, LOG_*) and the shell-operator-side
// variables that have no ADDON_OPERATOR_* counterpart (SHELL_OPERATOR_*
// HOOKS_DIR / TMP_DIR / LISTEN_ADDRESS / LISTEN_PORT / PROMETHEUS_METRICS_PREFIX
// / NAMESPACE) are recognized. ADDON_OPERATOR_* wins when both are set
// because it is applied last and overlays whatever shell-operator envs put
// in cfg.
//
// IMPORTANT: ParseEnv must only be invoked on the binary/CLI path, never on a
// caller-supplied *Config (see addon_operator.resolveConfig). Library users
// who pass WithConfig rely on env vars being ignored.
func ParseEnv(cfg *Config) error {
	applyShellOperatorEnv(cfg)

	if err := env.ParseWithOptions(cfg, env.Options{}); err != nil {
		return fmt.Errorf("parse config from environment: %w", err)
	}

	return nil
}

// applyShellOperatorEnv copies the six SHELL_OPERATOR_* variables that
// shell-operator's own *shapp.Config would otherwise read but addon-operator
// silently dropped, into the corresponding addon-operator fields. The mapping
// is intentionally explicit (no struct-tag magic) so reviewers can see
// exactly which shell-operator envs are honored.
//
// Precedence is achieved by call order in ParseEnv: this runs first, the
// ADDON_OPERATOR_* / shared-env overlay runs second and wins on conflict.
func applyShellOperatorEnv(cfg *Config) {
	if v, ok := os.LookupEnv("SHELL_OPERATOR_HOOKS_DIR"); ok {
		cfg.App.GlobalHooksDir = v
	}

	if v, ok := os.LookupEnv("SHELL_OPERATOR_TMP_DIR"); ok {
		cfg.App.TempDir = v
	}

	if v, ok := os.LookupEnv("SHELL_OPERATOR_LISTEN_ADDRESS"); ok {
		cfg.App.ListenAddress = v
	}

	if v, ok := os.LookupEnv("SHELL_OPERATOR_LISTEN_PORT"); ok {
		cfg.App.ListenPort = v
	}

	if v, ok := os.LookupEnv("SHELL_OPERATOR_PROMETHEUS_METRICS_PREFIX"); ok {
		cfg.App.PrometheusMetricsPrefix = v
	}

	if v, ok := os.LookupEnv("SHELL_OPERATOR_NAMESPACE"); ok {
		cfg.App.Namespace = v
	}
}

// ApplyConfig copies Config values into package-level variables for backward
// compatibility with code that reads them directly (e.g. module_manager) and
// for debug sub-commands that bind their --debug-unix-socket flag to the
// package-level DebugUnixSocket global (see pkg/app/debug_flag.go).
//
// A nil cfg is a no-op so callers don't need to guard. The function is
// idempotent and safe to call multiple times — bindDebugFlags invokes it so
// debug sub-commands see env/default-merged values, and NewAddonOperator
// re-invokes it once flags have been parsed so an explicit --flag wins.
//
// This mirrors shell-operator's app.ApplyConfig contract (nil-safe,
// idempotent, called from bindDebugFlags) so addon-operator's outer programs
// and debug sub-commands behave the same way.
func ApplyConfig(cfg *Config) {
	if cfg == nil {
		return
	}

	ModulesDir = cfg.App.ModulesDir
	GlobalHooksDir = cfg.App.GlobalHooksDir
	TempDir = cfg.App.TempDir
	Namespace = cfg.App.Namespace
	ListenAddress = cfg.App.ListenAddress
	ListenPort = cfg.App.ListenPort
	ConfigMapName = cfg.App.ConfigMapName
	PrometheusMetricsPrefix = cfg.App.PrometheusMetricsPrefix
	UnnumberedModuleOrder = cfg.App.UnnumberedModuleOrder
	ShellChrootDir = cfg.App.ShellChrootDir
	StrictModeEnabled = cfg.App.StrictModeEnabled
	AppliedExtenders = cfg.App.AppliedExtenders
	ExtraLabels = cfg.App.ExtraLabels
	CRDsFilters = cfg.App.CRDsFilters

	Helm3HistoryMax = cfg.Helm.HistoryMax
	Helm3Timeout = cfg.Helm.Timeout
	HelmIgnoreRelease = cfg.Helm.IgnoreRelease
	HelmMonitorKubeClientQps = cfg.Helm.MonitorKubeClientQps
	HelmMonitorKubeClientBurst = cfg.Helm.MonitorKubeClientBurst

	AdmissionServerListenPort = cfg.Admission.ListenPort
	AdmissionServerCertsDir = cfg.Admission.CertsDir
	AdmissionServerEnabled = cfg.Admission.Enabled

	KubeContext = cfg.Kube.Context
	KubeConfig = cfg.Kube.Config
	KubeServer = cfg.Kube.Server
	KubeClientQPS = cfg.Kube.ClientQPS
	KubeClientBurst = cfg.Kube.ClientBurst

	ObjectPatcherKubeClientQPS = cfg.ObjectPatcher.KubeClientQPS
	ObjectPatcherKubeClientBurst = cfg.ObjectPatcher.KubeClientBurst
	ObjectPatcherKubeClientTimeout = cfg.ObjectPatcher.KubeClientTimeout

	DebugUnixSocket = cfg.Debug.UnixSocket
	DebugHTTPServerAddr = cfg.Debug.HTTPServerAddr
	DebugKeepTmpFiles = cfg.Debug.KeepTmpFiles
	DebugKubernetesAPI = cfg.Debug.KubernetesAPI

	LogLevel = cfg.Log.Level
	LogType = cfg.Log.Type
	LogNoTime = cfg.Log.NoTime
	LogProxyHookJSON = cfg.Log.ProxyHookJSON
}
