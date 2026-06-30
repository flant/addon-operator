package helm

import (
	"fmt"
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

// MetricStorage is the minimal counter-emitting surface FallbackClient needs.
// It is intentionally small so the helm package does not depend on the full
// metrics-storage interface.
type MetricStorage interface {
	CounterAdd(metric string, value float64, labels map[string]string)
}

type Options struct {
	Namespace         string
	HistoryMax        int32
	Timeout           time.Duration
	HelmIgnoreRelease string
	Logger            *log.Logger
	// MetricStorage is optional. When set, the fallback client emits a counter
	// every time it falls back from nelm to helm3lib.
	MetricStorage MetricStorage
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
		// Initialize helm3lib alongside nelm so it can be used as a fallback
		// when nelm returns action.ErrBuildPlan or
		// resource.ErrResourceDuplicatesFound.
		if err := helm3lib.Init(&helm3lib.Options{
			Namespace:         helmopts.Namespace,
			HistoryMax:        helmopts.HistoryMax,
			Timeout:           helmopts.Timeout,
			HelmIgnoreRelease: helmopts.HelmIgnoreRelease,
		}, helmopts.Logger); err != nil {
			return nil, fmt.Errorf("init helm3lib for nelm fallback: %w", err)
		}

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

			primary := nelm.NewNelmClient(opts, logger.Named("nelm"), labels)
			fallback := helm3lib.NewClient(logger.Named("helm3lib-fallback"), maps.Clone(labels))

			return NewFallbackClient(primary, fallback, logger, helmopts.MetricStorage)
		}
	}

	return factory, nil
}
