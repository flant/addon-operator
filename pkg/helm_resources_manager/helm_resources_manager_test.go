package helm_resources_manager

import (
	"fmt"
	"testing"

	"github.com/flant/shell-operator/pkg/kube/fake"
	"github.com/flant/shell-operator/pkg/utils/manifest"
	. "github.com/onsi/gomega"
)

// Problem: fake client do not support metadata.name filtering
func Test_GetAbsentResources(t *testing.T) {
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

	mgr := NewHelmResourcesManager()
	mgr.WithKubeClient(fc.KubeClient)

	absent, err := mgr.GetAbsentResources(chartResources, "default")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(absent).To(HaveLen(0), "Should be no absent resources after creation")

	fc.DeleteSimpleNamespaced("ns1", "Pod", "pod-0")
	g.Expect(err).ShouldNot(HaveOccurred())
	fc.DeleteSimpleNamespaced(defaultNs, "Deployment", "backend")
	g.Expect(err).ShouldNot(HaveOccurred())

	absent, err = mgr.GetAbsentResources(chartResources, "default")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(absent).To(HaveLen(2), "Absent resources should be detected after deletion")

}

func createResource(fc *fake.FakeCluster, ns, manifestYaml string) manifest.Manifest {
	manifests, err := manifest.GetManifestListFromYamlDocuments(manifestYaml)
	if err != nil {
		panic(err)
	}
	if len(manifests) == 0 {
		panic(fmt.Errorf("YAML parsed to zero valid manifests! %s", manifestYaml))
	}

	m := manifests[0]

	fmt.Printf("Create %s\n", m.Id())

	fc.CreateSimpleNamespaced(m.Namespace(ns), m.Kind(), m.Name())

	return m
}
