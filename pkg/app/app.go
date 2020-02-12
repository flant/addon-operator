package app

import (
	"strconv"

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
var TillerMaxHistory = 0

var Namespace = ""
var ConfigMapName = "addon-operator"
var ValuesChecksumsAnnotation = "addon-operator/values-checksums"

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

	cmd.Flag("tiller-listen-port", "Listen port for tiller.").
		Envar("ADDON_OPERATOR_TILLER_LISTEN_PORT").
		Default(strconv.Itoa(int(TillerListenPort))).
		Int32Var(&TillerListenPort)
	cmd.Flag("tiller-probe-listen-port", "Listen port for tiller.").
		Envar("ADDON_OPERATOR_TILLER_PROBE_LISTEN_PORT").
		Default(strconv.Itoa(int(TillerProbeListenPort))).
		Int32Var(&TillerProbeListenPort)

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
