package addon_operator

import (
	"context"
	"fmt"

	"github.com/deckhouse/deckhouse/pkg/log"
	metricsstorage "github.com/deckhouse/deckhouse/pkg/metrics-storage"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm_resources_manager"
	klient "github.com/flant/kube-client/client"
	shapp "github.com/flant/shell-operator/pkg/app"
	"github.com/flant/shell-operator/pkg/metric"
)

// DefaultHelmMonitorKubeClientMetricLabels are labels that indicates go client metrics producer.
// Important! These labels should be consistent with similar labels in ShellOperator!
var DefaultHelmMonitorKubeClientMetricLabels = map[string]string{"component": "helm_monitor"}

// defaultKubeClient initializes a Kubernetes client
func defaultKubeClient(metricStorage metricsstorage.Storage, metricLabels map[string]string, logger *log.Logger) *klient.Client {
	client := klient.New(klient.WithLogger(logger))
	client.WithContextName(shapp.KubeContext)
	client.WithConfigPath(shapp.KubeConfig)
	client.WithMetricStorage(metric.NewMetricsAdapter(metricStorage, logger.Named("kube-client-metrics-adapter")))
	client.WithMetricLabels(metricLabels)
	return client
}

func InitDefaultHelmResourcesManager(ctx context.Context, namespace string, metricStorage metricsstorage.Storage, logger *log.Logger) (helm_resources_manager.HelmResourcesManager, error) {
	kubeClient := defaultKubeClient(metricStorage, DefaultHelmMonitorKubeClientMetricLabels, logger.Named("helm-monitor-kube-client"))
	kubeClient.WithRateLimiterSettings(app.HelmMonitorKubeClientQps, app.HelmMonitorKubeClientBurst)
	if err := kubeClient.Init(); err != nil {
		return nil, fmt.Errorf("initialize Kubernetes client for Helm resources manager: %s\n", err)
	}
	mgr, err := helm_resources_manager.NewHelmResourcesManager(ctx, kubeClient, logger.Named("helm-resource-manager"))
	if err != nil {
		return nil, fmt.Errorf("initialize Helm resources manager: %s\n", err)
	}
	mgr.WithDefaultNamespace(namespace)
	return mgr, nil
}
