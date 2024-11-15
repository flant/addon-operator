package helm

import (
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"

	"github.com/flant/addon-operator/pkg/helm/client"
	"github.com/flant/addon-operator/pkg/helm/helm3"
	"github.com/flant/addon-operator/pkg/helm/helm3lib"
)

type ClientFactory struct {
	NewClientFn func(logger *log.Logger, logLabels ...map[string]string) client.HelmClient
	ClientType  ClientType
}

func (f *ClientFactory) NewClient(logger *log.Logger, logLabels ...map[string]string) client.HelmClient {
	if f.NewClientFn != nil {
		return f.NewClientFn(logger, logLabels...)
	}
	return nil
}

type Options struct {
	Namespace  string
	HistoryMax int32
	Timeout    time.Duration
	Logger     *log.Logger
}

func InitHelmClientFactory(helmopts *Options, extraLabels map[string]string) (*ClientFactory, error) {
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
			Namespace:  helmopts.Namespace,
			HistoryMax: helmopts.HistoryMax,
			Timeout:    helmopts.Timeout,
		}, helmopts.Logger, extraLabels)

	case Helm3:
		log.Infof("Helm 3 detected (path is '%s')", helm3.Helm3Path)
		// Use helm3 client.
		factory.ClientType = Helm3
		factory.NewClientFn = helm3.NewClient
		err = helm3.Init(&helm3.Helm3Options{
			Namespace:  helmopts.Namespace,
			HistoryMax: helmopts.HistoryMax,
			Timeout:    helmopts.Timeout,
			Logger:     helmopts.Logger,
		})
	}

	if err != nil {
		return nil, err
	}
	return factory, nil
}
