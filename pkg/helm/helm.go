package helm

import (
	"fmt"
	"os"

	log "github.com/sirupsen/logrus"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm/client"
	"github.com/flant/addon-operator/pkg/helm/helm3"
	"github.com/flant/addon-operator/pkg/helm/helm3lib"
	klient "github.com/flant/kube-client/client"
)

type ClientFactory struct {
	kubeClient  *klient.Client
	cache       map[string]client.HelmClient
	NewClientFn func(logLabels ...map[string]string) client.HelmClient
}

func (f *ClientFactory) NewClient(namespace string, logLabels ...map[string]string) client.HelmClient {
	if f.NewClientFn != nil {
		return f.NewClientFn(logLabels...)
	}

	if client, ok := f.cache[namespace]; ok {
		return client
	}

	helmVersion, _ := DetectHelmVersion()

	if namespace == "" {
		namespace = app.Namespace
	}

	var client client.HelmClient
	var err error

	switch helmVersion {
	case Helm3Lib:
		log.Info("Helm3Lib detected. Use builtin Helm.")
		client = helm3lib.NewClient(&helm3lib.Options{
			Namespace:  namespace,
			HistoryMax: app.Helm3HistoryMax,
			Timeout:    app.Helm3Timeout,
			KubeClient: f.kubeClient,
		}, logLabels...)

	case Helm3:
		log.Infof("Helm 3 detected (path is '%s')", helm3.Helm3Path)
		// Use helm3 client.
		client, err = helm3.NewClient(&helm3.Helm3Options{
			Namespace:  namespace,
			HistoryMax: app.Helm3HistoryMax,
			Timeout:    app.Helm3Timeout,
			KubeClient: f.kubeClient,
		}, logLabels...)
		if err != nil {
			//!!! TODO Remove os.Exit and return error
			fmt.Printf("Error while init helm3: %s\n", err)
			os.Exit(1)
		}
	}
	f.cache[namespace] = client
	return client
}

func InitHelmClientFactory(kubeClient *klient.Client) (*ClientFactory, error) {
	_, err := DetectHelmVersion()
	if err != nil {
		return nil, err
	}
	factory := new(ClientFactory)
	factory.kubeClient = kubeClient
	factory.cache = make(map[string]client.HelmClient)

	if err != nil {
		return nil, err
	}
	return factory, nil
}
