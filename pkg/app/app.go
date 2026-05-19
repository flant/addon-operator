package app

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	shapp "github.com/flant/shell-operator/pkg/app"
)

var (
	AppName        = "addon-operator"
	AppDescription = ""
	Version        = "dev"

	ListenAddress = "0.0.0.0"
	ListenPort    = "9650"

	DefaultPrometheusMetricsPrefix = "addon_operator_"
	PrometheusMetricsPrefix        = DefaultPrometheusMetricsPrefix

	Helm3HistoryMax   int32 = 10
	Helm3Timeout            = 20 * time.Minute
	HelmIgnoreRelease       = ""

	HelmMonitorKubeClientQpsDefault   = "5"
	HelmMonitorKubeClientQps          float32
	HelmMonitorKubeClientBurstDefault = "10"
	HelmMonitorKubeClientBurst        int

	Namespace     = ""
	ConfigMapName = "addon-operator"

	GlobalHooksDir = "global-hooks"
	ModulesDir     = "modules"
	TempDir        = ""
	ShellChrootDir = ""

	UnnumberedModuleOrder = 1

	AdmissionServerListenPort = "9651"
	AdmissionServerCertsDir   = ""
	AdmissionServerEnabled    = false

	// StrictModeEnabled fail with error if MODULES_DIR/values.yaml does not exist
	StrictModeEnabled = false

	// AppliedExtenders defines the list and the order of applied module extenders
	AppliedExtenders = ""

	// ExtraLabels defines strings for CRDs label selector
	ExtraLabels = "heritage=addon-operator"
	// CRDsFilters defines filters for CRD files, example `doc-,_`
	CRDsFilters = "doc-,_"

	// NumberOfParallelQueues defines the number of precreated parallel queues for parallel execution
	NumberOfParallelQueues   = 20
	ParallelQueuePrefix      = "parallel_queue"
	ParallelQueueNamePattern = ParallelQueuePrefix + "_%d"

	// Kube client settings (previously from shell-operator globals)
	KubeContext     string
	KubeConfig      string
	KubeServer      string
	KubeClientQPS   float32
	KubeClientBurst int

	ObjectPatcherKubeClientQPS     float32
	ObjectPatcherKubeClientBurst   int
	ObjectPatcherKubeClientTimeout time.Duration

	// Debug settings (previously from shell-operator globals)
	DebugUnixSocket     string
	DebugHTTPServerAddr string
	DebugKeepTmpFiles   bool
	DebugKubernetesAPI  bool

	// Log settings (previously from shell-operator globals)
	LogLevel         string
	LogType          string
	LogNoTime        bool
	LogProxyHookJSON bool
)

const (
	DefaultTempDir         = "/tmp/addon-operator"
	DefaultDebugUnixSocket = "/var/run/addon-operator/debug.socket"
)

// BindFlags registers all operator CLI flags on cmd, using the current cfg
// values (already merged with env vars and hardcoded defaults) as flag defaults.
// An explicit CLI flag always wins.
func BindFlags(cfg *Config, rootCmd *cobra.Command, cmd *cobra.Command) {
	bindAppFlags(cfg, cmd)
	bindHelmFlags(cfg, cmd)
	bindAdmissionFlags(cfg, cmd)
	bindKubeFlags(cfg, cmd)
	bindLogFlags(cfg, cmd)
	bindDebugFlags(cfg, rootCmd, cmd)
}

func bindAppFlags(cfg *Config, cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVar(&cfg.App.ModulesDir, "modules-dir", cfg.App.ModulesDir, "Paths where to search for module directories. Can be set with $MODULES_DIR.")
	f.IntVar(&cfg.App.UnnumberedModuleOrder, "unnumbered-modules-order", cfg.App.UnnumberedModuleOrder, "Default order for modules without numbered prefix in name. Can be set with $UNNUMBERED_MODULES_ORDER.")
	f.StringVar(&cfg.App.GlobalHooksDir, "global-hooks-dir", cfg.App.GlobalHooksDir, "A path where to search for global hook files (and OpenAPI schemas). Can be set with $GLOBAL_HOOKS_DIR.")
	f.StringVar(&cfg.App.TempDir, "tmp-dir", cfg.App.TempDir, "A path to store temporary files with data for hooks. Can be set with $ADDON_OPERATOR_TMP_DIR.")
	f.StringVar(&cfg.App.Namespace, "namespace", cfg.App.Namespace, "Namespace of addon-operator. Can be set with $ADDON_OPERATOR_NAMESPACE.")
	f.StringVar(&cfg.App.ListenAddress, "prometheus-listen-address", cfg.App.ListenAddress, "Address to use to serve metrics to Prometheus. Can be set with $ADDON_OPERATOR_LISTEN_ADDRESS.")
	f.StringVar(&cfg.App.ListenPort, "prometheus-listen-port", cfg.App.ListenPort, "Port to use to serve metrics to Prometheus. Can be set with $ADDON_OPERATOR_LISTEN_PORT.")
	f.StringVar(&cfg.App.PrometheusMetricsPrefix, "prometheus-metrics-prefix", cfg.App.PrometheusMetricsPrefix, "Prefix for Prometheus metrics. Can be set with $ADDON_OPERATOR_PROMETHEUS_METRICS_PREFIX.")
	f.StringVar(&cfg.App.ConfigMapName, "config-map", cfg.App.ConfigMapName, "Name of a ConfigMap to store values. Can be set with $ADDON_OPERATOR_CONFIG_MAP.")
	f.StringVar(&cfg.App.ShellChrootDir, "shell-chroot-dir", cfg.App.ShellChrootDir, "Defines the path where shell scripts (shell hooks and enabled scripts) will be chrooted to. Can be set with $ADDON_OPERATOR_SHELL_CHROOT_DIR.")
	f.BoolVar(&cfg.App.StrictModeEnabled, "strict-check-values-mode-enabled", cfg.App.StrictModeEnabled, "Flag to enable strict-check-values mode. Can be set with $STRICT_CHECK_VALUES_MODE_ENABLED.")
	f.StringVar(&cfg.App.AppliedExtenders, "applied-module-extenders", cfg.App.AppliedExtenders, "Flag to define which module extenders to apply. Can be set with $ADDON_OPERATOR_APPLIED_MODULE_EXTENDERS.")
	f.StringVar(&cfg.App.ExtraLabels, "crd-extra-labels", cfg.App.ExtraLabels, "String with CRDs label selectors, like `heritage=addon-operator`. Can be set with $ADDON_OPERATOR_CRD_EXTRA_LABELS.")
	f.StringVar(&cfg.App.CRDsFilters, "crd-filters", cfg.App.CRDsFilters, "String of filters for the CRD, separated by commas. Can be set with $ADDON_OPERATOR_CRD_FILTER_PREFIXES.")
}

func bindHelmFlags(cfg *Config, cmd *cobra.Command) {
	f := cmd.Flags()
	f.Int32Var(&cfg.Helm.HistoryMax, "helm-history-max", cfg.Helm.HistoryMax, "Helm: limit the maximum number of revisions saved per release. Use 0 for no limit. Can be set with $HELM_HISTORY_MAX.")
	f.DurationVar(&cfg.Helm.Timeout, "helm-timeout", cfg.Helm.Timeout, "Helm: time to wait for any individual Kubernetes operation (like Jobs for hooks). Can be set with $HELM_TIMEOUT.")
	f.StringVar(&cfg.Helm.IgnoreRelease, "helm-ignore-release", cfg.Helm.IgnoreRelease, "Do not treat Helm release in the addon-operator namespace as a part of module releases, save it from auto-deletion at start. Can be set with $HELM_IGNORE_RELEASE.")
	f.Float32Var(&cfg.Helm.MonitorKubeClientQps, "helm-monitor-kube-client-qps", cfg.Helm.MonitorKubeClientQps, "QPS for a rate limiter of a kubernetes client for Helm resources monitor. Can be set with $HELM_MONITOR_KUBE_CLIENT_QPS.")
	f.IntVar(&cfg.Helm.MonitorKubeClientBurst, "helm-monitor-kube-client-burst", cfg.Helm.MonitorKubeClientBurst, "Burst for a rate limiter of a kubernetes client for Helm resources monitor. Can be set with $HELM_MONITOR_KUBE_CLIENT_BURST.")
}

func bindAdmissionFlags(cfg *Config, cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVar(&cfg.Admission.ListenPort, "admission-server-listen-port", cfg.Admission.ListenPort, "Port to use to serve admission webhooks. Can be set with $ADDON_OPERATOR_ADMISSION_SERVER_LISTEN_PORT.")
	f.StringVar(&cfg.Admission.CertsDir, "admission-server-certs-dir", cfg.Admission.CertsDir, "Path to the directory with tls certificates. Can be set with $ADDON_OPERATOR_ADMISSION_SERVER_CERTS_DIR.")
	f.BoolVar(&cfg.Admission.Enabled, "admission-server-enabled", cfg.Admission.Enabled, "Flag to enable admission http server. Can be set with $ADDON_OPERATOR_ADMISSION_SERVER_ENABLED.")
}

func bindKubeFlags(cfg *Config, cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVar(&cfg.Kube.Context, "kube-context", cfg.Kube.Context, "The name of the kubeconfig context to use. Can be set with $KUBE_CONTEXT.")
	f.StringVar(&cfg.Kube.Config, "kube-config", cfg.Kube.Config, "Path to the kubeconfig file. Can be set with $KUBE_CONFIG.")
	f.StringVar(&cfg.Kube.Server, "kube-server", cfg.Kube.Server, "The address and port of the Kubernetes API server. Can be set with $KUBE_SERVER.")
	f.Float32Var(&cfg.Kube.ClientQPS, "kube-client-qps", cfg.Kube.ClientQPS, "QPS for a rate limiter of a Kubernetes client. Can be set with $KUBE_CLIENT_QPS.")
	f.IntVar(&cfg.Kube.ClientBurst, "kube-client-burst", cfg.Kube.ClientBurst, "Burst for a rate limiter of a Kubernetes client. Can be set with $KUBE_CLIENT_BURST.")
	f.Float32Var(&cfg.ObjectPatcher.KubeClientQPS, "object-patcher-kube-client-qps", cfg.ObjectPatcher.KubeClientQPS, "QPS for a rate limiter of a Kubernetes client for Object patcher. Can be set with $OBJECT_PATCHER_KUBE_CLIENT_QPS.")
	f.IntVar(&cfg.ObjectPatcher.KubeClientBurst, "object-patcher-kube-client-burst", cfg.ObjectPatcher.KubeClientBurst, "Burst for a rate limiter of a Kubernetes client for Object patcher. Can be set with $OBJECT_PATCHER_KUBE_CLIENT_BURST.")
	f.DurationVar(&cfg.ObjectPatcher.KubeClientTimeout, "object-patcher-kube-client-timeout", cfg.ObjectPatcher.KubeClientTimeout, "Timeout for object patcher requests to the Kubernetes API server. Can be set with $OBJECT_PATCHER_KUBE_CLIENT_TIMEOUT.")
}

func bindLogFlags(cfg *Config, cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVar(&cfg.Log.Level, "log-level", cfg.Log.Level, "Logging level: debug, info, error. Can be set with $LOG_LEVEL.")
	f.StringVar(&cfg.Log.Type, "log-type", cfg.Log.Type, "Logging formatter type: json, text or color. Can be set with $LOG_TYPE.")
	f.BoolVar(&cfg.Log.NoTime, "log-no-time", cfg.Log.NoTime, "Disable timestamp logging. Can be set with $LOG_NO_TIME.")
	f.BoolVar(&cfg.Log.ProxyHookJSON, "log-proxy-hook-json", cfg.Log.ProxyHookJSON, "Proxy hook stdout/stderr JSON logging. Can be set with $LOG_PROXY_HOOK_JSON.")
}

func bindDebugFlags(cfg *Config, rootCmd *cobra.Command, cmd *cobra.Command) {
	shapp.DebugUnixSocket = cfg.Debug.UnixSocket

	f := cmd.Flags()
	f.StringVar(&cfg.Debug.UnixSocket, "debug-unix-socket", cfg.Debug.UnixSocket, "A path to a unix socket for a debug endpoint. Can be set with $DEBUG_UNIX_SOCKET.")
	_ = f.MarkHidden("debug-unix-socket")

	f.StringVar(&cfg.Debug.HTTPServerAddr, "debug-http-addr", cfg.Debug.HTTPServerAddr, "HTTP address for a debug endpoint. Can be set with $DEBUG_HTTP_SERVER_ADDR.")
	_ = f.MarkHidden("debug-http-addr")

	f.BoolVar(&cfg.Debug.KeepTmpFiles, "debug-keep-tmp-files", cfg.Debug.KeepTmpFiles, "Set to true to disable cleanup of temporary files. Can be set with $DEBUG_KEEP_TMP_FILES.")
	_ = f.MarkHidden("debug-keep-tmp-files")

	f.BoolVar(&cfg.Debug.KubernetesAPI, "debug-kubernetes-api", cfg.Debug.KubernetesAPI, "Enable client-go debug messages. Can be set with $DEBUG_KUBERNETES_API.")
	_ = f.MarkHidden("debug-kubernetes-api")

	startCmd := cmd
	debugOptionsCmd := &cobra.Command{
		Use:    "debug-options",
		Short:  "Show help for debug flags of the start command.",
		Hidden: true,
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Fprintf(os.Stdout, "usage: %s start [flags]\n\nDebug flags:\n", rootCmd.Use)
			startCmd.Flags().VisitAll(func(fl *pflag.Flag) {
				if fl.Hidden && strings.HasPrefix(fl.Name, "debug-") {
					fmt.Fprintf(os.Stdout, "  --%s\n        %s\n", fl.Name, fl.Usage)
				}
			})
			os.Exit(0)
		},
	}
	rootCmd.AddCommand(debugOptionsCmd)
}

func init() {
	helmMonitorQPS, _ := strconv.ParseFloat(HelmMonitorKubeClientQpsDefault, 32)
	HelmMonitorKubeClientQps = float32(helmMonitorQPS)
	helmMonitorBurst, _ := strconv.Atoi(HelmMonitorKubeClientBurstDefault)
	HelmMonitorKubeClientBurst = helmMonitorBurst
}
