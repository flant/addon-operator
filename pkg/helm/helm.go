package helm

import (
	"maps"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"

	"github.com/flant/addon-operator/pkg/helm/client"
	"github.com/flant/addon-operator/pkg/helm/helm3lib"
)

type ClientFactory struct {
	NewClientFn func(logger *log.Logger, labels map[string]string) client.HelmClient
	ClientType  ClientType
	labels      map[string]string
}

type ClientOption func(client.HelmClient)

func WithExtraLabels(labels map[string]string) ClientOption {
	return func(c client.HelmClient) {
		c.WithExtraLabels(labels)
	}
}

func WithLogLabels(logLabels map[string]string) ClientOption {
	return func(c client.HelmClient) {
		c.WithLogLabels(logLabels)
	}
}

func (f *ClientFactory) NewClient(logger *log.Logger, options ...ClientOption) client.HelmClient {
	if f.NewClientFn != nil {
		labels := maps.Clone(f.labels)
		c := f.NewClientFn(logger, labels)
		for _, option := range options {
			option(c)
		}

		return c
	}
	return nil
}

type Options struct {
	Namespace         string
	HistoryMax        int32
	Timeout           time.Duration
	HelmIgnoreRelease string
	Logger            *log.Logger
}

func InitHelmClientFactory(helmopts *Options, labels map[string]string) (*ClientFactory, error) {
	_, err := DetectHelmVersion()
	if err != nil {
		return nil, err
	}

	factory := new(ClientFactory)
	factory.labels = labels

	log.Info("Helm3Lib detected. Use builtin Helm.")

	factory.ClientType = Helm3Lib
	factory.NewClientFn = helm3lib.NewClient

	err = helm3lib.Init(&helm3lib.Options{
		Namespace:         helmopts.Namespace,
		HistoryMax:        helmopts.HistoryMax,
		Timeout:           helmopts.Timeout,
		HelmIgnoreRelease: helmopts.HelmIgnoreRelease,
	}, helmopts.Logger)
	if err != nil {
		return nil, err
	}

	return factory, nil
}
