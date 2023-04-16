package main

import (
	"fmt"
	"math/rand"
	"os"
	"time"

	"gopkg.in/alecthomas/kingpin.v2"

	addon_operator "github.com/flant/addon-operator/pkg/addon-operator"
	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/utils/stdliblogtologrus"
	"github.com/flant/kube-client/klogtologrus"
	sh_app "github.com/flant/shell-operator/pkg/app"
	"github.com/flant/shell-operator/pkg/debug"
	utils_signal "github.com/flant/shell-operator/pkg/utils/signal"
)

func main() {
	kpApp := kingpin.New(app.AppName, fmt.Sprintf("%s %s: %s", app.AppName, app.Version, app.AppDescription))

	// override usage template to reveal additional commands with information about start command
	kpApp.UsageTemplate(sh_app.OperatorUsageTemplate(app.AppName))

	kpApp.Action(func(c *kingpin.ParseContext) error {
		klogtologrus.InitAdapter(sh_app.DebugKubernetesAPI)
		stdliblogtologrus.InitAdapter()
		return nil
	})

	// print version
	kpApp.Command("version", "Show version.").Action(func(c *kingpin.ParseContext) error {
		fmt.Printf("%s %s\n", app.AppName, app.Version)
		return nil
	})

	// start main loop
	startCmd := kpApp.Command("start", "Start events processing.").
		Default().
		Action(func(c *kingpin.ParseContext) error {
			sh_app.AppStartMessage = fmt.Sprintf("%s %s, shell-operator %s", app.AppName, app.Version, sh_app.Version)

			// Init rand generator.
			rand.Seed(time.Now().UnixNano())

			operator := addon_operator.NewAddonOperator()
			err := addon_operator.Bootstrap(operator)
			if err != nil {
				os.Exit(1)
			}
			operator.Start()

			// Block action by waiting signals from OS.
			utils_signal.WaitForProcessInterruption(func() {
				operator.Shutdown()
				os.Exit(1)
			})

			return nil
		})
	app.DefineStartCommandFlags(kpApp, startCmd)

	debug.DefineDebugCommands(kpApp)
	app.DefineDebugCommands(kpApp)

	kingpin.MustParse(kpApp.Parse(os.Args[1:]))
}
