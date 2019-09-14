package addon_operator

import (
	"flag"
	"os"

	"github.com/romana/rlog"

	shell_operator_app "github.com/flant/shell-operator/pkg/app"
	"github.com/flant/shell-operator/pkg/executor"
	utils_signal "github.com/flant/shell-operator/pkg/utils/signal"

	"github.com/flant/addon-operator/pkg/app"
)

// Start is a start command. It is here to work with import.
func Start() {
	// Setting flag.Parsed() for glog.
	_ = flag.CommandLine.Parse([]string{})

	// Be a good parent - clean up after the child processes
	// in case if addon-operator is a PID 1 process.
	go executor.Reap()

	InitHttpServer(app.PrometheusListenAddress, app.PrometheusListenPort)

	rlog.Infof("addon-operator %s, shell-operator %s", app.Version, shell_operator_app.Version)

	err := Init()
	if err != nil {
		os.Exit(1)
	}

	Run()

	// Block action by waiting signals from OS.
	utils_signal.WaitForProcessInterruption()
}
