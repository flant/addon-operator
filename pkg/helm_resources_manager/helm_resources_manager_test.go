package helm_resources_manager

import (
	"context"
	"fmt"
	"testing"

	"github.com/flant/shell-operator/pkg/unilogger"
	. "github.com/onsi/gomega"
	"go.uber.org/goleak"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakemetadata "k8s.io/client-go/metadata/fake"

	"github.com/flant/kube-client/fake"
	"github.com/flant/kube-client/manifest"
)

// Problem: fake client do not support metadata.name filtering
func Test_GetAbsentResources(t *testing.T) {
	// klog has some leak, but it's not our code
	defer goleak.VerifyNone(t, goleak.IgnoreTopFunction("k8s.io/klog/v2.(*loggingT).flushDaemon"))

	g := NewWithT(t)

	fc := fake.NewFakeCluster("")

	defaultNs := "default"

	// Create resources and store manifests
	// TODO fix fake cluster for preferred resources and multiple apiVersions
	chartResources := []manifest.Manifest{
		createResource(fc, defaultNs, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: backend
`),
		createResource(fc, defaultNs, `
apiVersion: v1
kind: Pod
metadata:
  name: pod-0
  namespace: ns1
`),
		createResource(fc, defaultNs, `
apiVersion: v1
kind: Service
metadata:
  name: backend-srv
`),
		createResource(fc, defaultNs, `
apiVersion: v1
kind: Pod
metadata:
  name: pod-1
  namespace: ns2
`),
	}

	g.Expect(chartResources[0].Namespace(defaultNs)).To(Equal(defaultNs))
	g.Expect(chartResources[1].Namespace(defaultNs)).To(Equal("ns1"))
	g.Expect(chartResources[3].Namespace(defaultNs)).To(Equal("ns2"))

	mgr := NewHelmResourcesManager(unilogger.NewNop())
	mgr.WithKubeClient(fc.Client)

	absent, err := mgr.GetAbsentResources(chartResources, "default")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(absent).To(HaveLen(0), "Should be no absent resources after creation")

	deleteResource(fc, "v1", "Pod", "ns1", "pod-0")
	g.Expect(err).ShouldNot(HaveOccurred())

	deleteResource(fc, "apps/v1", "Deployment", defaultNs, "backend")
	g.Expect(err).ShouldNot(HaveOccurred())

	absent, err = mgr.GetAbsentResources(chartResources, "default")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(absent).To(HaveLen(1), "Absent resources should be detected after deletion")
}

func createResource(fc *fake.Cluster, ns, manifestYaml string) manifest.Manifest {
	manifests, err := manifest.ListFromYamlDocs(manifestYaml)
	if err != nil {
		panic(err)
	}
	if len(manifests) == 0 {
		panic(fmt.Errorf("YAML parsed to zero valid manifests! %s", manifestYaml))
	}

	m := manifests[0]

	fmt.Printf("Create %s\n", m.Id())

	apiRes, err := fc.Client.APIResource(m.ApiVersion(), m.Kind())
	if err != nil {
		panic(err)
	}
	gvr := schema.GroupVersionResource{
		Group:    apiRes.Group,
		Version:  apiRes.Version,
		Resource: apiRes.Name,
	}

	defaultNs := m.Namespace(ns)

	_, err = fc.Client.Metadata().(*fakemetadata.FakeMetadataClient).
		Resource(gvr).
		Namespace(defaultNs).(fakemetadata.MetadataClient).
		CreateFake(
			&metav1.PartialObjectMetadata{
				TypeMeta: metav1.TypeMeta{
					APIVersion: m.ApiVersion(),
					Kind:       m.Kind(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      m.Name(),
					Namespace: defaultNs,
				},
			},
			metav1.CreateOptions{})
	if err != nil {
		panic(err)
	}

	return m
}

func deleteResource(fc *fake.Cluster, apiVersion, kind, ns, name string) {
	fmt.Printf("Delete %s/%s/%s\n", kind, ns, name)

	apiRes, err := fc.Client.APIResource(apiVersion, kind)
	if err != nil {
		panic(err)
	}
	gvr := schema.GroupVersionResource{
		Group:    apiRes.Group,
		Version:  apiRes.Version,
		Resource: apiRes.Name,
	}

	err = fc.Client.Metadata().(*fakemetadata.FakeMetadataClient).
		Resource(gvr).
		Namespace(ns).(fakemetadata.MetadataClient).
		Delete(
			context.TODO(),
			name,
			metav1.DeleteOptions{})
	if err != nil {
		panic(err)
	}
}
