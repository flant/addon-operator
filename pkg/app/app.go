package app

import (
	"strconv"
	"time"

	sh_app "github.com/flant/shell-operator/pkg/app"

	"gopkg.in/alecthomas/kingpin.v2"
)

var AppName = "addon-operator"
var AppDescription = ""
var Version = "dev"

var DefaultListenAddress = "0.0.0.0"
var DefaultListenPort = "9650"
var DefaultPrometheusMetricsPrefix = "addon_operator_"

var TillerListenAddress = "127.0.0.1"
var TillerListenPort int32 = 44434
var TillerProbeListenAddress = "127.0.0.1"
var TillerProbeListenPort int32 = 44435
var TillerMaxHistory int32 = 0

var Helm3HistoryMax int32 = 10
var Helm3Timeout time.Duration = 5 * time.Minute
var HelmIgnoreRelease = ""
var HelmMonitorKubeClientQpsDefault = "5" // DefaultQPS from k8s.io/client-go/rest/config.go
var HelmMonitorKubeClientQps float32
var HelmMonitorKubeClientBurstDefault = "10" // DefaultBurst from k8s.io/client-go/rest/config.go
var HelmMonitorKubeClientBurst int

var Namespace = ""
var ConfigMapName = "addon-operator"

var GlobalHooksDir = "global-hooks"
var ModulesDir = "modules"
var DefaultTempDir = "/tmp/addon-operator"

var DefaultDebugUnixSocket = "/var/run/addon-operator/debug.socket"

// DefineStartCommandFlags init global flags with default values
func DefineStartCommandFlags(kpApp *kingpin.Application, cmd *kingpin.CmdClause) {
	cmd.Flag("tmp-dir", "a path to store temporary files with data for hooks").
		Envar("ADDON_OPERATOR_TMP_DIR").
		Default(DefaultTempDir).
		StringVar(&sh_app.TempDir)

	cmd.Flag("namespace", "Namespace of addon-operator.").
		Envar("ADDON_OPERATOR_NAMESPACE").
		Required().
		StringVar(&Namespace)

	cmd.Flag("prometheus-listen-address", "Address to use to serve metrics to Prometheus.").
		Envar("ADDON_OPERATOR_LISTEN_ADDRESS").
		Default(DefaultListenAddress).
		StringVar(&sh_app.ListenAddress)
	cmd.Flag("prometheus-listen-port", "Port to use to serve metrics to Prometheus.").
		Envar("ADDON_OPERATOR_LISTEN_PORT").
		Default(DefaultListenPort).
		StringVar(&sh_app.ListenPort)
	cmd.Flag("prometheus-metrics-prefix", "Prefix for Prometheus metrics.").
		Envar("ADDON_OPERATOR_PROMETHEUS_METRICS_PREFIX").
		Default(DefaultPrometheusMetricsPrefix).
		StringVar(&sh_app.PrometheusMetricsPrefix)
	cmd.Flag("hook-metrics-listen-port", "Port to use to serve hooks’ custom metrics to Prometheus. Can be set with $ADDON_OPERATOR_HOOK_METRICS_LISTEN_PORT. Equal to prometheus-listen-port if empty.").
		Envar("ADDON_OPERATOR_HOOK_METRICS_LISTEN_PORT").
		Default("").
		StringVar(&sh_app.HookMetricsListenPort)

	cmd.Flag("tiller-listen-port", "Listen port for tiller.").
		Envar("ADDON_OPERATOR_TILLER_LISTEN_PORT").
		Default(strconv.Itoa(int(TillerListenPort))).
		Int32Var(&TillerListenPort)
	cmd.Flag("tiller-probe-listen-port", "Listen port for tiller.").
		Envar("ADDON_OPERATOR_TILLER_PROBE_LISTEN_PORT").
		Default(strconv.Itoa(int(TillerProbeListenPort))).
		Int32Var(&TillerProbeListenPort)

	cmd.Flag("tiller-max-history", "Tiller: limit the maximum number of revisions saved per release. Use 0 for no limit.").
		Envar("TILLER_MAX_HISTORY").
		Default(strconv.Itoa(int(TillerMaxHistory))).
		Int32Var(&TillerMaxHistory)

	cmd.Flag("helm-history-max", "Helm: limit the maximum number of revisions saved per release. Use 0 for no limit.").
		Envar("HELM_HISTORY_MAX").
		Default(strconv.Itoa(int(Helm3HistoryMax))).
		Int32Var(&Helm3HistoryMax)

	cmd.Flag("helm-timeout", "Helm: time to wait for any individual Kubernetes operation (like Jobs for hooks).").
		Envar("HELM_TIMEOUT").
		Default(Helm3Timeout.String()).
		DurationVar(&Helm3Timeout)

	cmd.Flag("helm-ignore-release", "Helm3+: do not count release as a module release.").
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

	sh_app.DefineKubeClientFlags(cmd)
	sh_app.DefineJqFlags(cmd)
	sh_app.DefineLoggingFlags(cmd)

	sh_app.DebugUnixSocket = DefaultDebugUnixSocket
	sh_app.DefineDebugFlags(kpApp, cmd)
}
