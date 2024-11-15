package addon_operator

import (
	"context"
	"fmt"

	"github.com/deckhouse/deckhouse/pkg/log"
	metricstorage "github.com/flant/shell-operator/pkg/metric-storage"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm_resources_manager"
	klient "github.com/flant/kube-client/client"
	sh_app "github.com/flant/shell-operator/pkg/app"
	utils "github.com/flant/shell-operator/pkg/utils/labels"
)

// DefaultHelmMonitorKubeClientMetricLabels are labels that indicates go client metrics producer.
// Important! These labels should be consistent with similar labels in ShellOperator!
var DefaultHelmMonitorKubeClientMetricLabels = map[string]string{"component": "helm_monitor"}

// defaultHelmMonitorKubeClient initializes a Kubernetes client for helm monitor.
func defaultHelmMonitorKubeClient(metricStorage *metricstorage.MetricStorage, metricLabels map[string]string, logger *log.Logger) *klient.Client {
	client := klient.New(klient.WithLogger(logger))
	client.WithContextName(sh_app.KubeContext)
	client.WithConfigPath(sh_app.KubeConfig)
	client.WithRateLimiterSettings(app.HelmMonitorKubeClientQps, app.HelmMonitorKubeClientBurst)
	client.WithMetricStorage(metricStorage)
	client.WithMetricLabels(utils.DefaultIfEmpty(metricLabels, DefaultHelmMonitorKubeClientMetricLabels))
	return client
}

func InitDefaultHelmResourcesManager(ctx context.Context, namespace string, metricStorage *metricstorage.MetricStorage, logger *log.Logger) (helm_resources_manager.HelmResourcesManager, error) {
	kubeClient := defaultHelmMonitorKubeClient(metricStorage, DefaultHelmMonitorKubeClientMetricLabels, logger.Named("helm-monitor-kube-client"))
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
