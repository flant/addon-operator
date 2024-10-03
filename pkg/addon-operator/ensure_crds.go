package addon_operator

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"reflect"
	"sync"

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

func (op *AddonOperator) EnsureCRDs(module *modules.BasicModule) ([]string, error) {
	// do not ensure CRDs if there are no files
	if !module.CRDExist() {
		return nil, nil
	}

	result := new(multierror.Error)
	cp, err := NewCRDsInstaller(op.KubeClient(), module.GetCRDFilesPaths(), op.CRDExtraLabels)
	if err != nil {
		result = multierror.Append(result, err)
		return nil, result
	}

	if cp == nil {
		return nil, nil
	}
	if runErr := cp.Run(context.TODO()).ErrorOrNil(); runErr != nil {
		return nil, runErr
	}

	return cp.appliedGVKs, nil
}

// CRDsInstaller simultaneously installs CRDs from specified directory
type CRDsInstaller struct {
	k8sClient     dynamic.Interface
	crdFilesPaths []string
	buffer        []byte

	// concurrent tasks to create resource in a k8s cluster
	k8sTasks *multierror.Group

	crdExtraLabels map[string]string

	appliedGVKsLock sync.Mutex
	// list of GVKs, applied to the cluster
	appliedGVKs []string
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
		opErr := cp.updateOrInsertCRD(ctx, crd)
		if opErr == nil {
			var crdGroup, crdKind string
			crdVersions := make([]string, 0)
			if len(crd.Spec.Group) > 0 {
				crdGroup = crd.Spec.Group
			} else {
				return fmt.Errorf("Couldn't find CRD's .group key")
			}

			if len(crd.Spec.Names.Kind) > 0 {
				crdKind = crd.Spec.Names.Kind
			} else {
				return fmt.Errorf("Couldn't find CRD's .spec.names.kind key")
			}

			if len(crd.Spec.Versions) > 0 {
				for _, version := range crd.Spec.Versions {
					crdVersions = append(crdVersions, version.Name)
				}
			} else {
				return fmt.Errorf("Couldn't find CRD's .spec.versions key")
			}
			cp.appliedGVKsLock.Lock()
			for _, crdVersion := range crdVersions {
				cp.appliedGVKs = append(cp.appliedGVKs, fmt.Sprintf("%s/%s/%s", crdGroup, crdVersion, crdKind))
			}
			cp.appliedGVKsLock.Unlock()
		}
		return opErr
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
		appliedGVKs:    make([]string, 0),
	}, nil
}
