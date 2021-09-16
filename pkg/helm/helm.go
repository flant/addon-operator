package helm

import (
	"fmt"
	"net/http"

	klient "github.com/flant/kube-client/client"
	log "github.com/sirupsen/logrus"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm/client"
	"github.com/flant/addon-operator/pkg/helm/helm2"
	"github.com/flant/addon-operator/pkg/helm/helm3"
	"github.com/flant/addon-operator/pkg/helm/helm3lib"
)

var NewClient = func(logLabels ...map[string]string) client.HelmClient {
	return nil
}

var HealthzHandler func(writer http.ResponseWriter, request *http.Request)

func Init(client klient.Client) error {
	helmVersion, err := DetectHelmVersion()
	if err != nil {
		return err
	}

	switch helmVersion {
	case "v3lib":
		log.Info("Helm3Lib detected")
		NewClient = helm3lib.NewClient
		err = helm3lib.Init(&helm3lib.Options{
			Namespace:  app.Namespace,
			HistoryMax: app.Helm3HistoryMax,
			Timeout:    app.Helm3Timeout,
			KubeClient: client,
		})
		return err

	case "v3":
		log.Info("Helm 3 detected")
		// Use helm3 client.
		NewClient = helm3.NewClient
		err = helm3.Init(&helm3.Helm3Options{
			Namespace:  app.Namespace,
			HistoryMax: app.Helm3HistoryMax,
			Timeout:    app.Helm3Timeout,
			KubeClient: client,
		})
		return err

	case "v2":
		log.Info("Helm 2 detected, start Tiller")
		// TODO make tiller cancelable
		err = helm2.InitTillerProcess(helm2.TillerOptions{
			Namespace:          app.Namespace,
			HistoryMax:         app.TillerMaxHistory,
			ListenAddress:      app.TillerListenAddress,
			ListenPort:         app.TillerListenPort,
			ProbeListenAddress: app.TillerProbeListenAddress,
			ProbeListenPort:    app.TillerProbeListenPort,
		})
		if err != nil {
			return fmt.Errorf("init tiller: %s", err)
		}

		// Initialize helm2 client
		err = helm2.Init(&helm2.Helm2Options{
			Namespace:  app.Namespace,
			KubeClient: client,
		})
		if err != nil {
			return fmt.Errorf("init helm client: %s", err)
		}
		NewClient = helm2.NewClient
		HealthzHandler = helm2.TillerHealthHandler()
	}

	return nil
}
