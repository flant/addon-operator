package main

import (
	"flag"
	"os"

	"github.com/romana/rlog"

	shell_operator_app "github.com/flant/shell-operator/pkg/app"
	"github.com/flant/shell-operator/pkg/executor"
	utils_signal "github.com/flant/shell-operator/pkg/utils/signal"

	operator "github.com/flant/addon-operator/pkg/addon-operator"
	"github.com/flant/addon-operator/pkg/app"

)

func main() {
	// Setting flag.Parsed() for glog.
	flag.CommandLine.Parse([]string{})

	// Be a good parent - clean up after the child processes
	// in case if shell-operator is a PID1.
	go executor.Reap()

	operator.InitHttpServer()

	rlog.Infof("addon-operator %s, shell-operator %s", app.Version, shell_operator_app.Version)

	err := operator.Init()
	if err != nil {
		os.Exit(1)
	}

	operator.Run()

	// Block action by waiting signals from OS.
	utils_signal.WaitForProcessInterruption()

	return
}
