package main

import (
	"fmt"
	"os"

	"gopkg.in/alecthomas/kingpin.v2"

	shell_operator_app "github.com/flant/shell-operator/pkg/app"
	"github.com/flant/shell-operator/pkg/executor"
	utils_signal "github.com/flant/shell-operator/pkg/utils/signal"

	operator "github.com/flant/addon-operator/pkg/addon-operator"
	"github.com/flant/addon-operator/pkg/app"
)

func main() {
	kpApp := kingpin.New(app.AppName, fmt.Sprintf("%s %s: %s", app.AppName, app.Version, app.AppDescription))

	// global defaults
	app.SetupGlobalSettings(kpApp)
	shell_operator_app.SetupGlobalSettings(kpApp)

	// print version
	kpApp.Command("version", "Show version.").Action(func(c *kingpin.ParseContext) error {
		fmt.Printf("%s %s\n", app.AppName, app.Version)
		return nil
	})

	// start main loop
	kpApp.Command("start", "Start events processing.").
		Default().
		Action(func(c *kingpin.ParseContext) error {
			shell_operator_app.SetupLogging()
			// Be a good parent - clean up after the child processes
			// in case if addon-operator is a PID 1 process.
			go executor.Reap()

			operator.Start()

			// Block action by waiting signals from OS.
			utils_signal.WaitForProcessInterruption()

			return nil
		})

	kingpin.MustParse(kpApp.Parse(os.Args[1:]))

	return
}
