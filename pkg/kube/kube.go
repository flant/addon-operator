package kube

import (
	"fmt"
	"os"

	"github.com/romana/rlog"
	"k8s.io/client-go/kubernetes"

	client "github.com/flant/shell-operator/pkg/kube"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	//appsv1beta1 "k8s.io/client-go/kubernetes/typed/apps/v1beta1"
	//corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	//rbacv1alpha1 "k8s.io/client-go/kubernetes/typed/rbac/v1alpha1"
	//rbacv1beta1 "k8s.io/client-go/kubernetes/typed/rbac/v1beta1"
)

const (
	DefaultNamespace      = "antiopa"
	AntiopaDeploymentName = "antiopa"
	AntiopaContainerName  = "antiopa"
	AntiopaConfigMap      = "antiopa"
)

var (
	//KubernetesClient           Client
	Kubernetes                 kubernetes.Interface
	KubernetesAntiopaNamespace string
)
//
//type Client interface {
//	CoreV1() corev1.CoreV1Interface
//	AppsV1beta1() appsv1beta1.AppsV1beta1Interface
//	RbacV1alpha1() rbacv1alpha1.RbacV1alpha1Interface
//	RbacV1beta1() rbacv1beta1.RbacV1beta1Interface
//}

//func IsRunningOutOfKubeCluster() bool {
//	_, err := os.Stat(KubeTokenFilePath)
//	return os.IsNotExist(err)
//}

// InitKube - инициализация kubernetes клиента
// Можно подключить изнутри, а можно на основе .kube директории
func InitKube() {
	err := client.Init(client.InitOptions{})
	if err != nil {
		os.Exit(1)
	}

	Kubernetes = client.Kubernetes

	//rlog.Info("KUBE Init Kubernetes client")
	//
	//var err error
	//var config *rest.Config
	//
	//if IsRunningOutOfKubeCluster() {
	//	rlog.Info("KUBE-INIT Connecting to kubernetes out-of-cluster")
	//
	//	var kubeconfig string
	//	if kubeconfig = os.Getenv("KUBECONFIG"); kubeconfig == "" {
	//		kubeconfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	//	}
	//	rlog.Infof("KUBE-INIT Using kube config at %s", kubeconfig)
	//
	//	// use the current context in kubeconfig
	//	config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	//	if err != nil {
	//		rlog.Errorf("KUBE-INIT Kubernetes out-of-cluster configuration problem: %s", err)
	//		os.Exit(1)
	//	}
	//} else {
	//	rlog.Info("KUBE-INIT Connecting to kubernetes in-cluster")
	//
	//	config, err = rest.InClusterConfig()
	//	if err != nil {
	//		rlog.Errorf("KUBE-INIT Kubernetes in-cluster configuration problem: %s", err)
	//		os.Exit(1)
	//	}
	//}


	//if _, err := os.Stat(KubeNamespaceFilePath); !os.IsNotExist(err) {
	//	res, err := ioutil.ReadFile(KubeNamespaceFilePath)
	//	if err != nil {
	//		rlog.Errorf("KUBE-INIT Cannot read namespace from %s: %s", KubeNamespaceFilePath, err)
	//		os.Exit(1)
	//	}
	//
	//	KubernetesAntiopaNamespace = string(res)
	//}
	KubernetesAntiopaNamespace = client.DefaultNamespace
	if KubernetesAntiopaNamespace == "" {
		KubernetesAntiopaNamespace = os.Getenv("ANTIOPA_NAMESPACE")
	}
	if KubernetesAntiopaNamespace == "" {
		KubernetesAntiopaNamespace = DefaultNamespace
	}
	rlog.Infof("KUBE-INIT Antiopa namespace: %s", KubernetesAntiopaNamespace)

	//clientset, err := kubernetes.NewForConfig(config)
	//if err != nil {
	//	rlog.Errorf("KUBE-INIT Kubernetes connection problem: %s", err)
	//	os.Exit(1)
	//}
	//Kubernetes = clientset
	//KubernetesClient = clientset

	//rlog.Info("KUBE-INIT Successfully connected to kubernetes")
}

func KubeGetDeploymentImageName() string {
	res, err := Kubernetes.AppsV1beta1().Deployments(KubernetesAntiopaNamespace).Get(AntiopaDeploymentName, metav1.GetOptions{})

	if err != nil {
		rlog.Errorf("KUBE Cannot get antiopa deployment! %v", err)
		return ""
	}

	containersSpecs := res.Spec.Template.Spec.Containers

	for _, spec := range containersSpecs {
		if spec.Name == AntiopaContainerName {
			return spec.Image
		}
	}

	return ""
}



func GetConfigMap() (*v1.ConfigMap, error) {
	configMap, err := Kubernetes.CoreV1().ConfigMaps(KubernetesAntiopaNamespace).Get(AntiopaConfigMap, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("Cannot get ConfigMap %s from namespace %s: %s", AntiopaConfigMap, KubernetesAntiopaNamespace, err)
	}

	return configMap, nil
}
