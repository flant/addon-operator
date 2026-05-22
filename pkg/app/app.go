package app

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var (
	AppName        = "addon-operator"
	AppDescription = ""
	Version        = "dev"

	// AppStartMessage is the line logged at the start of the operator. The
	// binary entrypoint can override it (e.g. to embed the version).
	// Mirrors what shell-operator exposes as app.AppStartMessage so callers
	// no longer have to write into shell-operator globals.
	AppStartMessage = AppName

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

	// Debug settings (previously from shell-operator globals).
	// DebugUnixSocket lives in debug_flag.go so its default and binding
	// helpers sit next to each other, mirroring shell-operator's layout.
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
//
// The returned function applies any "late-merge" flag overrides — these are
// flags whose semantics require deciding between the env-derived value and an
// explicit CLI value AFTER pflag has parsed the command line (e.g.
// repeatable []string flags such as --dedup-client-namespace, where an empty
// CLI slice must NOT clobber an env-populated default). Callers that drive
// the cobra command lifecycle (rootCmd.Execute) get this for free via the
// PreRunE hook BindFlags installs on cmd; the returned func is exposed for
// tests and library consumers that want explicit control.
//
// Existing callers can safely ignore the return value: BindFlags always
// installs the same logic on cmd.PreRunE so the merge runs automatically
// during cobra-driven execution.
func BindFlags(cfg *Config, rootCmd *cobra.Command, cmd *cobra.Command) func() {
	bindAppFlags(cfg, cmd)
	bindHelmFlags(cfg, cmd)
	bindAdmissionFlags(cfg, cmd)
	bindKubeFlags(cfg, cmd)
	bindLogFlags(cfg, cmd)
	applyDedup := bindDedupClientFlags(cfg, cmd)
	bindDebugFlags(cfg, rootCmd, cmd)

	apply := func() {
		applyDedup()
	}

	// Chain into cmd.PreRunE so cobra-driven execution applies the merge
	// automatically. We preserve any pre-existing PreRunE (none today, but
	// keeps the helper composable).
	//
	// Note: cobra only invokes PreRunE on commands that are Runnable
	// (Run/RunE set). main.go sets startCmd.RunE before BindFlags so this
	// fires for `addon-operator start ...`. Tests that drive Execute()
	// must ensure RunE is set on the bound command (see parseFlags helper).
	prev := cmd.PreRunE
	cmd.PreRunE = func(c *cobra.Command, args []string) error {
		apply()
		if prev != nil {
			return prev(c, args)
		}
		return nil
	}

	return apply
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

// bindDedupClientFlags registers flags for the deduplicated kubeclient cache
// integrated by shell-operator's pkg/kube/dedupclient package.
//
// The two []string fields (Namespaces and WatchGVKs) follow the
// "env-default + CLI replaces" pattern, mirroring shell-operator's own
// bindDedupClientFlags: any explicit CLI invocation fully replaces the
// env-var derived slice; otherwise the env value is kept. The returned
// closure performs that merge after pflag has parsed the command line and
// must therefore run between flag parsing and cfg consumption (BindFlags
// wires it into cmd.PreRunE for the cobra-driven path).
func bindDedupClientFlags(cfg *Config, cmd *cobra.Command) func() {
	f := cmd.Flags()
	f.BoolVar(&cfg.DedupClient.Enabled, "dedup-client-enabled", cfg.DedupClient.Enabled,
		"Enable the deduplicated kubeclient cache (github.com/ldmonster/kubeclient). "+
			"When set, addon-operator builds a controller-runtime compatible client backed "+
			"by a deduplicated store via shell-operator. Can be set with $DEDUP_CLIENT_ENABLED.")
	f.BoolVar(&cfg.DedupClient.SnapshotStore, "dedup-client-snapshot-store", cfg.DedupClient.SnapshotStore,
		"Back per-monitor object snapshots with a process-wide deduplicated store "+
			"(github.com/ldmonster/kubeclient/store) via shell-operator's kube-events-manager. "+
			"Trades a small per-snapshot-read CPU cost for a substantial drop in RSS when many "+
			"monitors observe similar objects. Independent of --dedup-client-enabled. "+
			"Can be set with $DEDUP_CLIENT_SNAPSHOT_STORE.")
	f.BoolVar(&cfg.DedupClient.HelmResourcesCache, "dedup-client-helm-resources-cache", cfg.DedupClient.HelmResourcesCache,
		"Route pkg/helm_resources_manager's resource list-watch through the runtime "+
			"DedupClient cache instead of a dedicated controller-runtime cache. "+
			"Requires --dedup-client-enabled=true. Drops the watch-level "+
			"heritage=addon-operator label filter and the metadata-only informer "+
			"optimisation in exchange for shared, value-deduplicated storage. May "+
			"reduce or increase RSS depending on cluster topology — benchmark before "+
			"flipping. Can be set with $DEDUP_CLIENT_HELM_RESOURCES_CACHE.")
	f.IntVar(&cfg.DedupClient.ReconstructLRUSize, "dedup-client-reconstruct-lru-size",
		cfg.DedupClient.ReconstructLRUSize,
		"Size of the LRU that memoises reconstructed Unstructured objects in the dedup cache. "+
			"Zero disables reconstruction caching. Can be set with $DEDUP_CLIENT_RECONSTRUCT_LRU_SIZE.")
	f.DurationVar(&cfg.DedupClient.GCInterval, "dedup-client-gc-interval",
		cfg.DedupClient.GCInterval,
		"How often the deduplicated store reclaims unused interned values and subtrees. "+
			"Zero leaves the kubeclient default in place. Can be set with $DEDUP_CLIENT_GC_INTERVAL.")

	envNamespaces := cfg.DedupClient.Namespaces
	envGVKs := cfg.DedupClient.WatchGVKs
	var cliNamespaces, cliGVKs []string
	f.StringArrayVar(&cliNamespaces, "dedup-client-namespace", nil,
		"Namespace to restrict the dedup cache to. Repeat the flag to add more, or pass a "+
			"comma-separated list via $DEDUP_CLIENT_NAMESPACES. Empty means all namespaces.")
	f.StringArrayVar(&cliGVKs, "dedup-client-watch-gvk", nil,
		"GroupVersionKind to pre-register with the dedup cache, formatted as "+
			"\"<group>/<version>/<kind>\" (the group is empty for core resources, e.g. \"/v1/Pod\"). "+
			"Repeat the flag to add more, or pass a comma-separated list via $DEDUP_CLIENT_WATCH_GVKS.")

	return func() {
		if len(cliNamespaces) > 0 {
			cfg.DedupClient.Namespaces = cliNamespaces
		} else {
			cfg.DedupClient.Namespaces = envNamespaces
		}
		if len(cliGVKs) > 0 {
			cfg.DedupClient.WatchGVKs = cliGVKs
		} else {
			cfg.DedupClient.WatchGVKs = envGVKs
		}
	}
}

func bindLogFlags(cfg *Config, cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVar(&cfg.Log.Level, "log-level", cfg.Log.Level, "Logging level: debug, info, error. Can be set with $LOG_LEVEL.")
	f.StringVar(&cfg.Log.Type, "log-type", cfg.Log.Type, "Logging formatter type: json, text or color. Can be set with $LOG_TYPE.")
	f.BoolVar(&cfg.Log.NoTime, "log-no-time", cfg.Log.NoTime, "Disable timestamp logging. Can be set with $LOG_NO_TIME.")
	f.BoolVar(&cfg.Log.ProxyHookJSON, "log-proxy-hook-json", cfg.Log.ProxyHookJSON, "Proxy hook stdout/stderr JSON logging. Can be set with $LOG_PROXY_HOOK_JSON.")
}

func bindDebugFlags(cfg *Config, rootCmd *cobra.Command, cmd *cobra.Command) {
	// Sync the package-level DebugUnixSocket global so debug sub-commands
	// (global, module, etc.) that bind their --debug-unix-socket flag to it
	// pick up the env/default-merged value. Mirrors shell-operator's
	// bindDebugFlags, which performs the same call for the same reason.
	ApplyConfig(cfg)

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
