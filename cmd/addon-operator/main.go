package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	"gopkg.in/alecthomas/kingpin.v2"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"

	addon_operator "github.com/flant/addon-operator/pkg/addon-operator"
	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/kube_config_manager/backend/configmap"
	"github.com/flant/addon-operator/pkg/utils/stdliblogtolog"
	"github.com/flant/kube-client/klogtolog"
	shapp "github.com/flant/shell-operator/pkg/app"
	"github.com/flant/shell-operator/pkg/debug"
	utils_signal "github.com/flant/shell-operator/pkg/utils/signal"
)

const (
	leaseName       = "addon-operator-leader-election"
	leaseDuration   = 35
	renewalDeadline = 30
	retryPeriod     = 10
)

func main() {
	kpApp := kingpin.New(app.AppName, fmt.Sprintf("%s %s: %s", app.AppName, app.Version, app.AppDescription))

	logger := log.NewLogger(log.Options{})
	log.SetDefault(logger)

	// override usage template to reveal additional commands with information about start command
	kpApp.UsageTemplate(shapp.OperatorUsageTemplate(app.AppName))

	kpApp.Action(func(_ *kingpin.ParseContext) error {
		klogtolog.InitAdapter(shapp.DebugKubernetesAPI, logger.Named("klog"))
		stdliblogtolog.InitAdapter(logger)
		return nil
	})

	// print version
	kpApp.Command("version", "Show version.").Action(func(_ *kingpin.ParseContext) error {
		fmt.Printf("%s %s\n", app.AppName, app.Version)
		return nil
	})

	// start main loop
	startCmd := kpApp.Command("start", "Start events processing.").
		Default().
		Action(start(logger))

	app.DefineStartCommandFlags(kpApp, startCmd)

	debug.DefineDebugCommands(kpApp)
	app.DefineDebugCommands(kpApp)

	kingpin.MustParse(kpApp.Parse(os.Args[1:]))
}

func start(logger *log.Logger) func(_ *kingpin.ParseContext) error {
	return func(_ *kingpin.ParseContext) error {
		shapp.AppStartMessage = fmt.Sprintf("%s %s, shell-operator %s", app.AppName, app.Version, shapp.Version)

		ctx := context.Background()

		operator := addon_operator.NewAddonOperator(ctx, addon_operator.WithLogger(logger.Named("addon-operator")))

		operator.StartAPIServer()

		if os.Getenv("ADDON_OPERATOR_HA") == "true" {
			operator.Logger.Info("Addon-operator is starting in HA mode")
			runHAMode(ctx, operator)
			return nil
		}

		err := run(ctx, operator)
		if err != nil {
			operator.Logger.Fatal("run operator", log.Err(err))
		}

		return nil
	}
}

func run(ctx context.Context, operator *addon_operator.AddonOperator) error {
	bk := configmap.New(operator.KubeClient(), operator.DefaultNamespace, app.ConfigMapName, operator.Logger.Named("kube-config-manager"))
	operator.SetupKubeConfigManager(bk)

	if err := operator.Setup(); err != nil {
		operator.Logger.Fatal("setup failed", log.Err(err))
	}

	if err := operator.Start(ctx); err != nil {
		operator.Logger.Fatal("start failed", log.Err(err))
	}

	// Block action by waiting signals from OS.
	utils_signal.WaitForProcessInterruption(func() {
		operator.Stop()
		os.Exit(0)
	})

	return nil
}

func runHAMode(ctx context.Context, operator *addon_operator.AddonOperator) {
	podName := os.Getenv("ADDON_OPERATOR_POD")
	if len(podName) == 0 {
		operator.Logger.Fatal("ADDON_OPERATOR_POD env not set or empty")
	}

	podIP := os.Getenv("ADDON_OPERATOR_LISTEN_ADDRESS")
	if len(podIP) == 0 {
		operator.Logger.Fatal("ADDON_OPERATOR_LISTEN_ADDRESS env not set or empty")
	}

	podNs := os.Getenv("ADDON_OPERATOR_NAMESPACE")
	if len(podNs) == 0 {
		operator.Logger.Fatal("ADDON_OPERATOR_NAMESPACE env not set or empty")
	}

	identity := fmt.Sprintf("%s.%s.%s.pod", podName, strings.ReplaceAll(podIP, ".", "-"), podNs)

	err := operator.WithLeaderElector(&leaderelection.LeaderElectionConfig{
		// Create a leaderElectionConfig for leader election
		Lock: &resourcelock.LeaseLock{
			LeaseMeta: v1.ObjectMeta{
				Name:      leaseName,
				Namespace: podNs,
			},
			Client: operator.KubeClient().CoordinationV1(),
			LockConfig: resourcelock.ResourceLockConfig{
				Identity: identity,
			},
		},
		LeaseDuration: time.Duration(leaseDuration) * time.Second,
		RenewDeadline: time.Duration(renewalDeadline) * time.Second,
		RetryPeriod:   time.Duration(retryPeriod) * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				err := run(ctx, operator)
				if err != nil {
					operator.Logger.Fatal("run on stardet leading", log.Err(err))
				}
			},
			OnStoppedLeading: func() {
				operator.Logger.Info("Restarting because the leadership was handed over")
				operator.Stop()
				os.Exit(0)
			},
		},
		ReleaseOnCancel: true,
	})
	if err != nil {
		operator.Logger.Fatal("with leader election", log.Err(err))
	}

	go func() {
		<-ctx.Done()
		log.Info("Context canceled received")
		if err := syscall.Kill(1, syscall.SIGUSR2); err != nil {
			operator.Logger.Fatal("Couldn't shutdown addon-operator", log.Err(err))
		}
	}()

	operator.LeaderElector.Run(ctx)
}
