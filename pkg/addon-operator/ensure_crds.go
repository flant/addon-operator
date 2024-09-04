package addon_operator

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"reflect"

	"github.com/hashicorp/go-multierror"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimachineryv1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	apimachineryYaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/retry"

	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
	"github.com/flant/addon-operator/sdk"
	"github.com/flant/kube-client/client"
)

var crdGVR = schema.GroupVersionResource{
	Group:    "apiextensions.k8s.io",
	Version:  "v1",
	Resource: "customresourcedefinitions",
}

func (op *AddonOperator) EnsureCRDs(module *modules.BasicModule) error {
	// do not ensure CRDs if there are no files
	if !module.CRDExist() {
		return nil
	}

	result := new(multierror.Error)
	cp, err := NewCRDsInstaller(op.KubeClient(), module.GetCRDFilesPaths(), op.CrdExtraLabels)
	if err != nil {
		result = multierror.Append(result, err)
		return result
	}

	if cp == nil {
		return nil
	}

	return cp.Run(context.TODO()).ErrorOrNil()
}

// CRDsInstaller simultaneously installs CRDs from specified directory
type CRDsInstaller struct {
	k8sClient     dynamic.Interface
	crdFilesPaths []string
	buffer        []byte

	// concurrent tasks to create resource in a k8s cluster
	k8sTasks *multierror.Group

	crdExtraLabels map[string]string
}

func (cp *CRDsInstaller) Run(ctx context.Context) *multierror.Error {
	result := new(multierror.Error)

	for _, crdFilePath := range cp.crdFilesPaths {
		err := cp.processCRD(ctx, crdFilePath)
		if err != nil {
			err = fmt.Errorf("error occurred during processing %q file: %w", crdFilePath, err)
			result = multierror.Append(result, err)
			continue
		}
	}

	errs := cp.k8sTasks.Wait()
	if errs.ErrorOrNil() != nil {
		result = multierror.Append(result, errs.Errors...)
	}

	return result
}

func (cp *CRDsInstaller) processCRD(ctx context.Context, crdFilePath string) error {
	crdFileReader, err := os.Open(crdFilePath)
	if err != nil {
		return err
	}
	defer crdFileReader.Close()

	crdReader := apimachineryYaml.NewDocumentDecoder(crdFileReader)

	for {
		n, err := crdReader.Read(cp.buffer)
		if err != nil {
			if err == io.EOF {
				break
			}

			return err
		}

		data := cp.buffer[:n]
		if len(data) == 0 {
			// some empty yaml document, or empty string before separator
			continue
		}
		rd := bytes.NewReader(data)
		err = cp.putCRDToCluster(ctx, rd, n)
		if err != nil {
			return err
		}
	}

	return nil
}

func (cp *CRDsInstaller) putCRDToCluster(ctx context.Context, crdReader io.Reader, bufferSize int) error {
	var crd *v1.CustomResourceDefinition

	err := apimachineryYaml.NewYAMLOrJSONDecoder(crdReader, bufferSize).Decode(&crd)
	if err != nil {
		return err
	}

	// it could be a comment or some other peace of yaml file, skip it
	if crd == nil {
		return nil
	}

	if crd.APIVersion != v1.SchemeGroupVersion.String() && crd.Kind != "CustomResourceDefinition" {
		return fmt.Errorf("invalid CRD document apiversion/kind: '%s/%s'", crd.APIVersion, crd.Kind)
	}

	if len(crd.ObjectMeta.Labels) == 0 {
		crd.ObjectMeta.Labels = make(map[string]string, 1)
	}
	for crdExtraLabel := range cp.crdExtraLabels {
		crd.ObjectMeta.Labels[crdExtraLabel] = cp.crdExtraLabels[crdExtraLabel]
	}

	cp.k8sTasks.Go(func() error {
		return cp.updateOrInsertCRD(ctx, crd)
	})

	return nil
}

func (cp *CRDsInstaller) updateOrInsertCRD(ctx context.Context, crd *v1.CustomResourceDefinition) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		existCRD, err := cp.getCRDFromCluster(ctx, crd.GetName())
		if err != nil {
			if apierrors.IsNotFound(err) {
				ucrd, err := sdk.ToUnstructured(crd)
				if err != nil {
					return err
				}

				_, err = cp.k8sClient.Resource(crdGVR).Create(ctx, ucrd, apimachineryv1.CreateOptions{})
				return err
			}

			return err
		}

		if existCRD.Spec.Conversion != nil {
			crd.Spec.Conversion = existCRD.Spec.Conversion
		}

		if existCRD.GetObjectMeta().GetLabels()[LabelHeritage] == cp.crdExtraLabels[LabelHeritage] &&
			reflect.DeepEqual(existCRD.Spec, crd.Spec) {
			return nil
		}

		existCRD.Spec = crd.Spec
		if len(existCRD.ObjectMeta.Labels) == 0 {
			existCRD.ObjectMeta.Labels = make(map[string]string, 1)
		}
		existCRD.ObjectMeta.Labels[LabelHeritage] = cp.crdExtraLabels[LabelHeritage]

		ucrd, err := sdk.ToUnstructured(existCRD)
		if err != nil {
			return err
		}

		_, err = cp.k8sClient.Resource(crdGVR).Update(ctx, ucrd, apimachineryv1.UpdateOptions{})
		return err
	})
}

func (cp *CRDsInstaller) getCRDFromCluster(ctx context.Context, crdName string) (*v1.CustomResourceDefinition, error) {
	crd := &v1.CustomResourceDefinition{}

	o, err := cp.k8sClient.Resource(crdGVR).Get(ctx, crdName, apimachineryv1.GetOptions{})
	if err != nil {
		return nil, err
	}

	err = sdk.FromUnstructured(o, &crd)
	if err != nil {
		return nil, err
	}

	return crd, nil
}

// NewCRDsInstaller creates new installer for CRDs
func NewCRDsInstaller(client *client.Client, crdFilesPaths []string, crdExtraLabels map[string]string) (*CRDsInstaller, error) {
	return &CRDsInstaller{
		k8sClient:     client.Dynamic(),
		crdFilesPaths: crdFilesPaths,
		// 1Mb - maximum size of kubernetes object
		// if we take less, we have to handle io.ErrShortBuffer error and increase the buffer
		// take more does not make any sense due to kubernetes limitations
		buffer:         make([]byte, 1*1024*1024),
		k8sTasks:       &multierror.Group{},
		crdExtraLabels: crdExtraLabels,
	}, nil
}
