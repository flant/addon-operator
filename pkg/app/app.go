package app

import (
	"net"

	"gopkg.in/alecthomas/kingpin.v2"
)

var AppName = "addon-operator"
var AppDescription = ""

var Version = "dev"

var ListenAddress, _ = net.ResolveTCPAddr("tcp", "0.0.0.0:9115")

// SetupGlobalSettings init global flags with default values
func SetupGlobalSettings(kpApp *kingpin.Application) {
	kpApp.Flag("listen-address", "Address and port to use for HTTP serving.").
		Envar("ADDON_OPERATOR_LISTEN_ADDRESS").
		Default(ListenAddress.String()).
		TCPVar(&ListenAddress)
}
