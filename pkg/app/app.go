package app

import (
	"strconv"

	"gopkg.in/alecthomas/kingpin.v2"
)

var AppName = "addon-operator"
var AppDescription = ""

var Version = "dev"

var Namespace = ""

var ListenAddress = "0.0.0.0"
var ListenPort = "9650"
var PrometheusMetricsPrefix = "addon_operator_"

var TillerListenAddress = "127.0.0.1"
var TillerListenPort int32 = 44434
var TillerProbeListenAddress = "127.0.0.1"
var TillerProbeListenPort int32 = 44435
var TillerMaxHistory = 0

var WerfTillerNamespace = ""
var WerfArgs = ""

var ConfigMapName = "addon-operator"
var ValuesChecksumsAnnotation = "addon-operator/values-checksums"
var TasksQueueDumpFilePath = "/tmp/addon-operator-tasks-queue"

var GlobalHooksDir = "global-hooks"
var ModulesDir = "modules"
var TmpDir = "/tmp/addon-operator"

// SetupGlobalSettings init global flags with default values
func SetupGlobalSettings(kpApp *kingpin.Application) {
	kpApp.Flag("namespace", "Namespace of addon-operator.").
		Envar("ADDON_OPERATOR_NAMESPACE").
		Required().
		StringVar(&Namespace)

	kpApp.Flag("prometheus-listen-address", "Address to use to serve metrics to Prometheus.").
		Envar("ADDON_OPERATOR_LISTEN_ADDRESS").
		Default(ListenAddress).
		StringVar(&ListenAddress)
	kpApp.Flag("prometheus-listen-port", "Port to use to serve metrics to Prometheus.").
		Envar("ADDON_OPERATOR_LISTEN_PORT").
		Default(ListenPort).
		StringVar(&ListenPort)
	kpApp.Flag("prometheus-metrics-prefix", "Prefix for Prometheus metrics.").
		Envar("ADDON_OPERATOR_PROMETHEUS_METRICS_PREFIX").
		Default(PrometheusMetricsPrefix).
		StringVar(&PrometheusMetricsPrefix)

	kpApp.Flag("tiller-listen-port", "Listen port for tiller.").
		Envar("ADDON_OPERATOR_TILLER_LISTEN_PORT").
		Default(strconv.Itoa(int(TillerListenPort))).
		Int32Var(&TillerListenPort)
	kpApp.Flag("tiller-probe-listen-port", "Listen port for tiller.").
		Envar("ADDON_OPERATOR_TILLER_PROBE_LISTEN_PORT").
		Default(strconv.Itoa(int(TillerProbeListenPort))).
		Int32Var(&TillerProbeListenPort)

	kpApp.Flag("config-map", "Name of a ConfigMap to store values.").
		Envar("ADDON_OPERATOR_CONFIG_MAP").
		Default(ConfigMapName).
		StringVar(&ConfigMapName)

}
