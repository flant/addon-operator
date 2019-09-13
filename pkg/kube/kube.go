package kube

import (
	"os"

	"github.com/romana/rlog"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	client "github.com/flant/shell-operator/pkg/kube"
)

const (
	DefaultNamespace = "addon-operator"
)

var (
	Kubernetes             kubernetes.Interface
	AddonOperatorNamespace string
	AddonOperatorPod       string
)

// InitKube initialize a Kubernetes client config.
// This method calls shell-operator kube client method that can work
// in-cluster, out-of-cluster or in-cluster with out-of-cluster kube config
func InitKube() error {
	err := client.Init(client.InitOptions{})
	if err != nil {
		return err
	}

	Kubernetes = client.Kubernetes

	AddonOperatorNamespace = client.DefaultNamespace
	if AddonOperatorNamespace == "" {
		AddonOperatorNamespace = os.Getenv("ADDON_OPERATOR_NAMESPACE")
	}
	if AddonOperatorNamespace == "" {
		AddonOperatorNamespace = DefaultNamespace
	}
	rlog.Infof("KUBE-INIT: Addon-operator namespace: %s", AddonOperatorNamespace)
	return nil
}

func GetCurrentPod() (pod *v1.Pod, err error) {
	podName := AddonOperatorPod
	if podName == "" {
		podName = os.Getenv("ADDON_OPERATOR_POD")
	}
	if podName == "" {
		podName, err = os.Hostname()
		if err != nil {
			return nil, err
		}
	}
	pod, err = client.Kubernetes.CoreV1().Pods(AddonOperatorNamespace).Get(podName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return pod, nil
}

func GetCurrentPodSpec() (podSpec v1.PodSpec, err error) {
	pod, err := GetCurrentPod()
	if err != nil {
		return
	}
	return pod.Spec, nil
}
