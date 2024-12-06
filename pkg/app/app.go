package app

import (
	"strconv"
	"time"

	"gopkg.in/alecthomas/kingpin.v2"

	shapp "github.com/flant/shell-operator/pkg/app"
)

var (
	AppName        = "addon-operator"
	AppDescription = ""
	Version        = "dev"

	ListenAddress = "0.0.0.0"
	ListenPort    = "9650"

	DefaultPrometheusMetricsPrefix = "addon_operator_"

	Helm3HistoryMax   int32 = 10
	Helm3Timeout            = 5 * time.Minute
	HelmIgnoreRelease       = ""

	HelmMonitorKubeClientQpsDefault   = "5" // DefaultQPS from k8s.io/client-go/rest/config.go
	HelmMonitorKubeClientQps          float32
	HelmMonitorKubeClientBurstDefault = "10" // DefaultBurst from k8s.io/client-go/rest/config.go
	HelmMonitorKubeClientBurst        int

	Namespace     = ""
	ConfigMapName = "addon-operator"

	GlobalHooksDir = "global-hooks"
	ModulesDir     = "modules"

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
	NumberOfParallelQueues   = 10
	ParallelQueuePrefix      = "parallel_queue"
	ParallelQueueNamePattern = ParallelQueuePrefix + "_%d"
)

const (
	DefaultTempDir         = "/tmp/addon-operator"
	DefaultDebugUnixSocket = "/var/run/addon-operator/debug.socket"
)

// DefineStartCommandFlags init global flags with default values
func DefineStartCommandFlags(kpApp *kingpin.Application, cmd *kingpin.CmdClause) {
	cmd.Flag("modules-dir", "paths where to search for module directories").
		Envar("MODULES_DIR").
		Default(ModulesDir).
		StringVar(&ModulesDir)

	// TODO Delete this setting after refactoring module dependencies machinery.
	cmd.Flag("unnumbered-modules-order", "default order for modules without numbered prefix in name").
		Envar("UNNUMBERED_MODULES_ORDER").
		Default(strconv.Itoa(UnnumberedModuleOrder)).
		IntVar(&UnnumberedModuleOrder)

	cmd.Flag("global-hooks-dir", "a path where to search for global hook files (and OpenAPI schemas)").
		Envar("GLOBAL_HOOKS_DIR").
		Default(GlobalHooksDir).
		StringVar(&GlobalHooksDir)

	cmd.Flag("tmp-dir", "a path to store temporary files with data for hooks").
		Envar("ADDON_OPERATOR_TMP_DIR").
		Default(DefaultTempDir).
		StringVar(&shapp.TempDir)

	cmd.Flag("namespace", "Namespace of addon-operator.").
		Envar("ADDON_OPERATOR_NAMESPACE").
		Required().
		StringVar(&Namespace)

	cmd.Flag("prometheus-listen-address", "Address to use to serve metrics to Prometheus.").
		Envar("ADDON_OPERATOR_LISTEN_ADDRESS").
		Default(ListenAddress).
		StringVar(&ListenAddress)
	cmd.Flag("prometheus-listen-port", "Port to use to serve metrics to Prometheus.").
		Envar("ADDON_OPERATOR_LISTEN_PORT").
		Default(ListenPort).
		StringVar(&ListenPort)
	cmd.Flag("prometheus-metrics-prefix", "Prefix for Prometheus metrics.").
		Envar("ADDON_OPERATOR_PROMETHEUS_METRICS_PREFIX").
		Default(DefaultPrometheusMetricsPrefix).
		StringVar(&shapp.PrometheusMetricsPrefix)
	cmd.Flag("helm-history-max", "Helm: limit the maximum number of revisions saved per release. Use 0 for no limit.").
		Envar("HELM_HISTORY_MAX").
		Default(strconv.Itoa(int(Helm3HistoryMax))).
		Int32Var(&Helm3HistoryMax)

	cmd.Flag("helm-timeout", "Helm: time to wait for any individual Kubernetes operation (like Jobs for hooks).").
		Envar("HELM_TIMEOUT").
		Default(Helm3Timeout.String()).
		DurationVar(&Helm3Timeout)

	cmd.Flag("helm-ignore-release", "Do not treat Helm release in the addon-operator namespace as a part of module releases, save it from auto-deletion at start.").
		Envar("HELM_IGNORE_RELEASE").
		StringVar(&HelmIgnoreRelease)

	// Rate limit settings for kube client used by Helm resources monitor.
	cmd.Flag("helm-monitor-kube-client-qps", "QPS for a rate limiter of a kubernetes client for Helm resources monitor. Can be set with $HELM_MONITOR_KUBE_CLIENT_QPS.").
		Envar("HELM_MONITOR_KUBE_CLIENT_QPS").
		Default(HelmMonitorKubeClientQpsDefault).
		Float32Var(&HelmMonitorKubeClientQps)
	cmd.Flag("helm-monitor-kube-client-burst", "Burst for a rate limiter of a kubernetes client for Helm resources monitor. Can be set with $HELM_MONITOR_KUBE_CLIENT_BURST.").
		Envar("HELM_MONITOR_KUBE_CLIENT_BURST").
		Default(HelmMonitorKubeClientBurstDefault).
		IntVar(&HelmMonitorKubeClientBurst)

	cmd.Flag("config-map", "Name of a ConfigMap to store values.").
		Envar("ADDON_OPERATOR_CONFIG_MAP").
		Default(ConfigMapName).
		StringVar(&ConfigMapName)

	cmd.Flag("admission-server-listen-port", "Port to use to serve admission webhooks.").
		Envar("ADDON_OPERATOR_ADMISSION_SERVER_LISTEN_PORT").
		Default(AdmissionServerListenPort).
		StringVar(&AdmissionServerListenPort)

	cmd.Flag("admission-server-certs-dir", "Path to the directory with tls certificates.").
		Envar("ADDON_OPERATOR_ADMISSION_SERVER_CERTS_DIR").
		Default("").
		StringVar(&AdmissionServerCertsDir)

	cmd.Flag("admission-server-enabled", "Flag to enable admission http server.").
		Envar("ADDON_OPERATOR_ADMISSION_SERVER_ENABLED").
		Default("false").
		BoolVar(&AdmissionServerEnabled)

	cmd.Flag("strict-check-values-mode-enabled", "Flag to enable strict-check-values mode.").
		Envar("STRICT_CHECK_VALUES_MODE_ENABLED").
		Default("false").
		BoolVar(&StrictModeEnabled)

	cmd.Flag("applied-module-extenders", "Flag to define which module extenders to apply").
		Envar("ADDON_OPERATOR_APPLIED_MODULE_EXTENDERS").
		Default(AppliedExtenders).
		StringVar(&AppliedExtenders)

	cmd.Flag("crd-extra-labels", "String with CRDs label selectors, like `heritage=addon-operator`").
		Envar("ADDON_OPERATOR_CRD_EXTRA_LABELS").
		Default(ExtraLabels).
		StringVar(&ExtraLabels)

	cmd.Flag("crd-filters", "String of filters for the CRD, separated by commas`").
		Envar("ADDON_OPERATOR_CRD_FILTER_PREFIXES").
		Default(CRDsFilters).
		StringVar(&CRDsFilters)

	shapp.DefineKubeClientFlags(cmd)
	shapp.DefineJqFlags(cmd)
	shapp.DefineLoggingFlags(cmd)

	// TODO: working with global values not need to be in flags definition
	shapp.DebugUnixSocket = DefaultDebugUnixSocket
	shapp.DefineDebugFlags(kpApp, cmd)
}
