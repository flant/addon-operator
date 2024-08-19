package helm

import (
	"sync"

	log "github.com/sirupsen/logrus"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm/client"
	"github.com/flant/addon-operator/pkg/helm/helm3"
	"github.com/flant/addon-operator/pkg/helm/helm3lib"
)

type ClientFactory struct {
	NewClientFn         func(logLabels ...map[string]string) client.HelmClient
	NewClientWithLockFn func(helmOperationLock *sync.Mutex, logLabels ...map[string]string) client.HelmClient
}

func (f *ClientFactory) NewClient(logLabels ...map[string]string) client.HelmClient {
	if f.NewClientFn != nil {
		return f.NewClientFn(logLabels...)
	}
	return nil
}

func (f *ClientFactory) NewClientWithLock(helmOperationLock *sync.Mutex, logLabels ...map[string]string) client.HelmClient {
	if f.NewClientWithLockFn != nil {
		return f.NewClientWithLockFn(helmOperationLock, logLabels...)
	}
	return nil
}

func InitHelmClientFactory() (*ClientFactory, error) {
	helmVersion, err := DetectHelmVersion()
	if err != nil {
		return nil, err
	}

	factory := new(ClientFactory)

	switch helmVersion {
	case Helm3Lib:
		log.Info("Helm3Lib detected. Use builtin Helm.")
		factory.NewClientFn = helm3lib.NewClient
		factory.NewClientWithLockFn = helm3lib.NewClientWithLock
		err = helm3lib.Init(&helm3lib.Options{
			Namespace:  app.Namespace,
			HistoryMax: app.Helm3HistoryMax,
			Timeout:    app.Helm3Timeout,
		})

	case Helm3:
		log.Infof("Helm 3 detected (path is '%s')", helm3.Helm3Path)
		// Use helm3 client.
		factory.NewClientFn = helm3.NewClient
		factory.NewClientWithLockFn = helm3.NewClientWithLock
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
