package addon_operator

import (
	"os"

	"github.com/romana/rlog"

	"github.com/flant/addon-operator/pkg/app"
	shell_operator_app "github.com/flant/shell-operator/pkg/app"
)

// Start is a start command. It is here to work with import.
func Start() {
	InitHttpServer(app.PrometheusListenAddress, app.PrometheusListenPort)

	rlog.Infof("addon-operator %s, shell-operator %s", app.Version, shell_operator_app.Version)

	err := Init()
	if err != nil {
		os.Exit(1)
	}

	Run()
}
