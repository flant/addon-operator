package helm

import (
	"maps"
	"os"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"

	"github.com/flant/addon-operator/pkg/helm/client"
	"github.com/flant/addon-operator/pkg/helm/helm3lib"
	"github.com/flant/addon-operator/pkg/helm/nelm"
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

func WithExtraAnnotations(annotations map[string]string) ClientOption {
	return func(c client.HelmClient) {
		c.WithExtraAnnotations(annotations)
	}
}

func (f *ClientFactory) NewClient(logger *log.Logger, options ...ClientOption) client.HelmClient {
	if f.NewClientFn != nil {
		labels := maps.Clone(f.labels)
		c := f.NewClientFn(logger, labels)
		
		// Add werf.io/skip-logs annotation when using nelm
		if f.ClientType == Nelm {
			c.WithExtraAnnotations(map[string]string{
				"werf.io/skip-logs": "true",
			})
		}
		
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
	clientType, err := DetectHelmVersion()
	if err != nil {
		return nil, err
	}

	factory := new(ClientFactory)
	factory.labels = labels
	factory.ClientType = clientType

	switch clientType {
	case Helm3Lib:
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
	case Nelm:
		factory.NewClientFn = func(logger *log.Logger, labels map[string]string) client.HelmClient {
			opts := &nelm.CommonOptions{
				HistoryMax:  helmopts.HistoryMax,
				Timeout:     helmopts.Timeout,
				HelmDriver:  os.Getenv("HELM_DRIVER"),
				KubeContext: os.Getenv("KUBE_CONTEXT"),
			}
			if helmopts.Namespace != "" {
				opts.Namespace = &helmopts.Namespace
			}
			return nelm.NewNelmClient(opts, logger.Named("nelm"), labels)
		}
	}

	return factory, nil
}
