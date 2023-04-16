package addon_operator

import (
	"context"
	"fmt"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm_resources_manager"
	klient "github.com/flant/kube-client/client"
	sh_app "github.com/flant/shell-operator/pkg/app"
	"github.com/flant/shell-operator/pkg/metric_storage"
	shell_operator "github.com/flant/shell-operator/pkg/shell-operator"
)

// DefaultHelmMonitorKubeClientMetricLabels are labels that indicates go client metrics producer.
// Important! These labels should be consistent with similar labels in ShellOperator!
var DefaultHelmMonitorKubeClientMetricLabels = map[string]string{"component": "helm_monitor"}

// DefaultHelmMonitorKubeClient initializes a Kubernetes client for helm monitor.
func DefaultHelmMonitorKubeClient(metricStorage *metric_storage.MetricStorage, metricLabels map[string]string) klient.Client {
	client := klient.New()
	client.WithContextName(sh_app.KubeContext)
	client.WithConfigPath(sh_app.KubeConfig)
	client.WithRateLimiterSettings(app.HelmMonitorKubeClientQps, app.HelmMonitorKubeClientBurst)
	client.WithMetricStorage(metricStorage)
	client.WithMetricLabels(shell_operator.DefaultIfEmpty(metricLabels, DefaultHelmMonitorKubeClientMetricLabels))
	return client
}

func InitDefaultHelmResourcesManager(ctx context.Context, metricStorage *metric_storage.MetricStorage) (helm_resources_manager.HelmResourcesManager, error) {
	kubeClient := DefaultHelmMonitorKubeClient(metricStorage, DefaultHelmMonitorKubeClientMetricLabels)
	if err := kubeClient.Init(); err != nil {
		return nil, fmt.Errorf("initialize Kubernetes client for Helm resources manager: %s\n", err)
	}
	mgr := helm_resources_manager.NewHelmResourcesManager()
	mgr.WithContext(ctx)
	mgr.WithKubeClient(kubeClient)
	mgr.WithDefaultNamespace(app.Namespace)
	return mgr, nil
}
