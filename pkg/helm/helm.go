package helm

import (
	log "github.com/sirupsen/logrus"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm/client"
	"github.com/flant/addon-operator/pkg/helm/helm3"
	"github.com/flant/addon-operator/pkg/helm/helm3lib"
)

type ClientFactory struct {
	NewClientFn func(logLabels ...map[string]string) client.HelmClient
	ClientType  ClientType
}

func (f *ClientFactory) NewClient(logLabels ...map[string]string) client.HelmClient {
	if f.NewClientFn != nil {
		return f.NewClientFn(logLabels...)
	}
	return nil
}

func InitHelmClientFactory(extraLabels map[string]string) (*ClientFactory, error) {
	helmVersion, err := DetectHelmVersion()
	if err != nil {
		return nil, err
	}

	factory := new(ClientFactory)

	switch helmVersion {
	case Helm3Lib:
		log.Info("Helm3Lib detected. Use builtin Helm.")
		factory.ClientType = Helm3Lib
		factory.NewClientFn = helm3lib.NewClient
		err = helm3lib.Init(&helm3lib.Options{
			Namespace:  app.Namespace,
			HistoryMax: app.Helm3HistoryMax,
			Timeout:    app.Helm3Timeout,
		}, extraLabels)

	case Helm3:
		log.Infof("Helm 3 detected (path is '%s')", helm3.Helm3Path)
		// Use helm3 client.
		factory.ClientType = Helm3
		factory.NewClientFn = helm3.NewClient
		err = helm3.Init(&helm3.Helm3Options{
			Namespace:  app.Namespace,
			HistoryMax: app.Helm3HistoryMax,
			Timeout:    app.Helm3Timeout,
		})
	}

	if err != nil {
		return nil, err
	}
	return factory, nil
}
