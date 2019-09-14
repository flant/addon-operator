package app

import (
	"strconv"

	"gopkg.in/alecthomas/kingpin.v2"
)

var AppName = "addon-operator"
var AppDescription = ""

var Version = "dev"

var Namespace = ""
var PodName = ""
var ContainerName = "addon-operator"

var PrometheusListenAddress = "0.0.0.0"
var PrometheusListenPort = "9115"
var PrometheusMetricsPrefix = "addon_operator_"

var TillerContainerName = "tiller"
var TillerListenAddress = "127.0.0.1"
var TillerListenPort int32 = 44134
var TillerProbeListenAddress = "127.0.0.1"
var TillerProbeListenPort int32 = 44135
var TillerMaxHistory = 0

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
	kpApp.Flag("pod-name", "Pod name to init additional container with tiller.").
		Envar("ADDON_OPERATOR_POD").
		Required().
		StringVar(&PodName)

	kpApp.Flag("prometheus-listen-address", "Address to use to serve metrics to Prometheus.").
		Envar("ADDON_OPERATOR_PROMETHEUS_LISTEN_ADDRESS").
		Default(PrometheusListenAddress).
		StringVar(&PrometheusListenAddress)
	kpApp.Flag("prometheus-listen-port", "Port to use to serve metrics to Prometheus.").
		Envar("ADDON_OPERATOR_PROMETHEUS_LISTEN_PORT").
		Default(PrometheusListenPort).
		StringVar(&PrometheusListenPort)
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

	kpApp.Flag("container-name", "Name of a container with addon-operator to get image name.").
		Envar("ADDON_OPERATOR_CONTAINER_NAME").
		Default(ContainerName).
		StringVar(&ContainerName)

	kpApp.Flag("values-checksums-annotation", "Annotation where checksums are saved.").
		Envar("ADDON_OPERATOR_VALUES_CHECKSUMS_ANNOTATION").
		Default(ValuesChecksumsAnnotation).
		StringVar(&ValuesChecksumsAnnotation)

}
