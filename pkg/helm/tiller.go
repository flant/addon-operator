package helm

import (
	"fmt"

	"github.com/romana/rlog"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/kube"
)

// InitTillerSidecar adds container to addon-operator Deployment with tiller image
// and pass additional arguments: listen and probe-listen
func InitTillerSidecar() error {

	currentDeployment, err := kube.GetDeploymentOfCurrentPod()
	if err != nil {
		return fmt.Errorf("get current deployment to add Tiller sidecar: %v", err)
	}

	containers := currentDeployment.Spec.Template.Spec.Containers

	imageName := ""

	for _, container := range containers {
		if container.Name == app.TillerContainerName {
			rlog.Infof("INIT TILLER: sidecar is already present")
			return nil
		}
		if container.Name == app.ContainerName {
			imageName = container.Image
		}
	}

	if imageName == "" {
		return fmt.Errorf("cannot determine image of container %d in current pod", app.ContainerName)
	}

	// Add container with tiller
	currentDeployment.Spec.Template.Spec.Containers = append(currentDeployment.Spec.Template.Spec.Containers,
		v1.Container{
			Name: app.TillerContainerName,
			Image: imageName,
			ImagePullPolicy: v1.PullIfNotPresent,
			Command: []string{
				"/tiller",
				"-listen",
				fmt.Sprintf("%s:%d", app.TillerListenAddress, app.TillerListenPort),
				"-probe-listen",
				fmt.Sprintf("%s:%d", app.TillerProbeListenAddress, app.TillerProbeListenPort),
			},
			Ports: []v1.ContainerPort{
				{ContainerPort: app.TillerListenPort, Name: "tiller"},
				{ContainerPort: app.TillerProbeListenPort, Name: "http"},
			},
			Env: []v1.EnvVar{
				{Name: "TILLER_NAMESPACE", Value: app.Namespace},
				{Name: "TILLER_HISTORY_MAX", Value: fmt.Sprintf("%d", app.TillerMaxHistory)},
			},
			LivenessProbe: &v1.Probe{
				Handler: v1.Handler{
					HTTPGet: &v1.HTTPGetAction{
						Path: "/liveness",
						Port: intstr.FromInt(int(app.TillerProbeListenPort)),
						Host: app.TillerProbeListenAddress,
					},
				},
				InitialDelaySeconds: 1,
				TimeoutSeconds:      1,
			},
			ReadinessProbe: &v1.Probe{
				Handler: v1.Handler{
					HTTPGet: &v1.HTTPGetAction{
						Path: "/readiness",
						Port: intstr.FromInt(int(app.TillerProbeListenPort)),
						Host: app.TillerProbeListenAddress,
					},
				},
				InitialDelaySeconds: 1,
				TimeoutSeconds:      1,
			},
		},
	)

	err = kube.UpdateDeployment(currentDeployment)
	if err != nil {
		return fmt.Errorf("Update Deployment/%s with tiller sidecar: %v", currentDeployment.Name, err)
	}

	rlog.Infof("Tiller sidecar installed.")

	return nil
}

