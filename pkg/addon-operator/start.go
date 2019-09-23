package addon_operator

import (
	"os"

	"github.com/romana/rlog"

	shell_operator_app "github.com/flant/shell-operator/pkg/app"

	"github.com/flant/addon-operator/pkg/app"
)

// Start is a start command. It is here to work with import.
func Start() {
	var err error

	err = InitHttpServer(app.ListenAddress, app.ListenPort)
	if err != nil {
		rlog.Errorf("HTTP SERVER start failed: %v", err)
		os.Exit(1)
	}

	rlog.Infof("addon-operator %s, shell-operator %s", app.Version, shell_operator_app.Version)

	err = Init()
	if err != nil {
		rlog.Errorf("INIT failed: %v", err)
		os.Exit(1)
	}

	rlog.Debugf("START: Run")
	Run()
}
