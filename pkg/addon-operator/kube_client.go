package addon_operator

import (
	"github.com/flant/addon-operator/pkg/app"
	sh_app "github.com/flant/shell-operator/pkg/app"
	"github.com/flant/shell-operator/pkg/kube"
)

// Important! These labels should be consistent with similar labels in ShellOperator!
var DefaultHelmMonitorKubeClientMetricLabels = map[string]string{"component": "helm_monitor"}

func (op *AddonOperator) GetHelmMonitorKubeClientMetricLabels() map[string]string {
	if op.HelmMonitorKubeClientMetricLabels == nil {
		return DefaultHelmMonitorKubeClientMetricLabels
	}
	return op.HelmMonitorKubeClientMetricLabels
}

// InitHelmMonitorKubeClient initializes a Kubernetes client for helm monitor.
func (op *AddonOperator) InitHelmMonitorKubeClient() (kube.KubernetesClient, error) {
	client := kube.NewKubernetesClient()
	client.WithContextName(sh_app.KubeContext)
	client.WithConfigPath(sh_app.KubeConfig)
	client.WithRateLimiterSettings(app.HelmMonitorKubeClientQps, app.HelmMonitorKubeClientBurst)
	client.WithMetricStorage(op.MetricStorage)
	client.WithMetricLabels(op.GetHelmMonitorKubeClientMetricLabels())
	if err := client.Init(); err != nil {
		return nil, err
	}
	return client, nil
}
