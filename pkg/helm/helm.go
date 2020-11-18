package helm

import (
	"fmt"
	"net/http"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm/client"
	"github.com/flant/addon-operator/pkg/helm/helm2"
	"github.com/flant/addon-operator/pkg/helm/helm3"
	"github.com/flant/shell-operator/pkg/kube"
)

var NewClient = func(logLabels ...map[string]string) client.HelmClient {
	return nil
}

var HealthzHandler func(writer http.ResponseWriter, request *http.Request)

func Init(client kube.KubernetesClient) error {
	helmVersion, err := DetectHelmVersion()
	if err != nil {
		return err
	}

	if helmVersion == "v3" {
		// Use helm3 client.
		NewClient = helm3.NewClient
		err = helm3.Init(&helm3.Helm3Options{
			Namespace:  app.Namespace,
			HistoryMax: app.Helm3HistoryMax,
			Timeout:    app.Helm3Timeout,
			KubeClient: client,
		})
		return err
	}

	if helmVersion == "v2" {
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
