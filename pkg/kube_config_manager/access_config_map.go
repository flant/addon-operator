package kube_config_manager

import (
	"context"
	"github.com/flant/addon-operator/pkg/utils"

	klient "github.com/flant/kube-client/client"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConfigMapGet gets the ConfigMap object from the cluster.
func ConfigMapGet(kubeClient klient.Client, namespace string, name string) (*v1.ConfigMap, error) {
	obj, err := kubeClient.CoreV1().
		ConfigMaps(namespace).
		Get(context.TODO(), name, metav1.GetOptions{})

	if errors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return obj, err
}

// ConfigMapUpdate gets the ConfigMap object from the cluster,
// call transformation callback and save modified object.
func ConfigMapUpdate(kubeClient klient.Client, namespace string, name string, transformFn func(*v1.ConfigMap) error) error {
	var err error

	obj, err := ConfigMapGet(kubeClient, namespace, name)
	if err != nil {
		return nil
	}

	isUpdate := true
	if obj == nil {
		obj = &v1.ConfigMap{}
		obj.Name = name
		isUpdate = false
	}

	if obj.Data == nil {
		obj.Data = make(map[string]string)
	}

	err = transformFn(obj)
	if err != nil {
		return err
	}

	if isUpdate {
		_, err = kubeClient.CoreV1().ConfigMaps(namespace).Update(context.TODO(), obj, metav1.UpdateOptions{})
	} else {
		_, err = kubeClient.CoreV1().ConfigMaps(namespace).Create(context.TODO(), obj, metav1.CreateOptions{})
	}
	return err
}

// ConfigMapMergeValues is a helper to use ConfigMapUpdate to save Values object in the ConfigMap.
func ConfigMapMergeValues(kubeClient klient.Client, namespace string, name string, values utils.Values) error {
	cmData, err := values.AsConfigMapData()
	if err != nil {
		return err
	}
	return ConfigMapUpdate(kubeClient, namespace, name, func(obj *v1.ConfigMap) error {
		for k, v := range cmData {
			obj.Data[k] = v
		}
		return nil
	})
}
