package helm

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"testing"

	log "github.com/sirupsen/logrus"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "k8s.io/api/core/v1"
	"k8s.io/api/rbac/v1beta1"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm/client"
	"github.com/flant/addon-operator/pkg/helm/helm2"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/shell-operator/pkg/kube"
)

func TestMain(m *testing.M) {
	app.Namespace = fmt.Sprintf("addon-operator-test-%06d", rand.Int31n(1000000))
	os.Exit(m.Run())
}

func Test_Logging(t *testing.T) {
	log.SetFormatter(&log.JSONFormatter{DisableTimestamp: true})

	log.Info("Start test")

	logEntry1 := log.WithField("test", "helm")
	logEntry1.Infof("asd")

	logEntry2 := log.WithField("test2", "helm2")
	logEntry2.Infof("asd")

	logEntry11 := logEntry1.WithField("subtest", "helmm")
	logEntry11.Infof("helmm info")

	logEntry1.Infof("asd again")

	logEntry11.WithField("test", "helm11").Infof("helmm info")

	fields1 := map[string]string{
		"module":    "mod1",
		"hook":      "hook2",
		"component": "main",
	}
	logEntry1F := logEntry1.WithFields(utils.LabelsToLogFields(fields1))
	logEntry1F.Infof("top record")

	fields2 := map[string]string{
		"module":   "mod2",
		"event.id": "123",
	}

	logEntry2F := logEntry1F.WithFields(utils.LabelsToLogFields(fields2))

	logEntry2F.Infof("nested record")
	logEntry1F.Infof("new top record")
	logEntry2F.Infof("new nested record")

	logEntry2F.WithField("result", "qwe\nfoo\bqwe").Infof("record with multiline field")

}

//nolint:golint,unused
func getTestDirectoryPath(testName string) string {
	_, testFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(testFile), "testdata", testName)
}

//nolint:golint,unused
func shouldDeleteRelease(helm client.HelmClient, releaseName string) (err error) {
	err = helm.DeleteRelease(releaseName)
	if err != nil {
		return fmt.Errorf("Should delete existing release '%s' successfully, got error: %s", releaseName, err)
	}
	isExists, err := helm.IsReleaseExists(releaseName)
	if err != nil {
		return err
	}
	if isExists {
		return fmt.Errorf("Release '%s' should not exist after deletion", releaseName)
	}

	return nil
}

//nolint:golint,unused
func releasesListShouldEqual(helm client.HelmClient, expectedList []string) (err error) {
	releases, err := helm.ListReleasesNames(nil)
	if err != nil {
		return err
	}

	sortedExpectedList := make([]string, len(expectedList))
	copy(sortedExpectedList, expectedList)
	sort.Strings(sortedExpectedList)

	if !reflect.DeepEqual(sortedExpectedList, releases) {
		return fmt.Errorf("Expected %+v releases list, got %+v", expectedList, releases)
	}

	return nil
}

//nolint:golint,unused
func shouldUpgradeRelease(helm client.HelmClient, releaseName string, chart string, valuesPaths []string) (err error) {
	err = helm.UpgradeRelease(releaseName, chart, []string{}, []string{}, app.Namespace)
	if err != nil {
		return fmt.Errorf("Cannot install test release: %s", err)
	}
	isExists, err := helm.IsReleaseExists(releaseName)
	if err != nil {
		return err
	}
	if !isExists {
		return fmt.Errorf("Release '%s' should exist", releaseName)
	}
	return nil
}

func TestHelm(t *testing.T) {
	// Skip because this test needs a Kubernetes cluster and a helm binary.
	t.SkipNow()

	var err error
	var stdout, stderr string
	var isExists bool
	var releases []string

	helm := &helm2.Helm2Client{}

	kubeClient := kube.NewKubernetesClient()

	testNs := &v1.Namespace{}
	testNs.Name = app.Namespace
	_, err = kubeClient.CoreV1().Namespaces().Create(context.TODO(), testNs, v12.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	sa := &v1.ServiceAccount{}
	sa.Name = "tiller"
	_, err = kubeClient.CoreV1().ServiceAccounts(app.Namespace).Create(context.TODO(), sa, v12.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	role := &v1beta1.Role{}
	role.Name = "tiller-role"
	role.Rules = []v1beta1.PolicyRule{
		v1beta1.PolicyRule{
			APIGroups: []string{"*"},
			Resources: []string{"*"},
			Verbs:     []string{"*"},
		},
	}
	_, err = kubeClient.RbacV1beta1().Roles(app.Namespace).Create(context.TODO(), role, v12.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	rb := &v1beta1.RoleBinding{}
	rb.Name = "tiller-binding"
	rb.RoleRef.Kind = "Role"
	rb.RoleRef.Name = "tiller-role"
	rb.RoleRef.APIGroup = "rbac.authorization.k8s.io"
	rb.Subjects = []v1beta1.Subject{
		v1beta1.Subject{Kind: "ServiceAccount", Name: "tiller", Namespace: app.Namespace},
	}
	_, err = kubeClient.RbacV1beta1().RoleBindings(app.Namespace).Create(context.TODO(), rb, v12.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err = helm.Cmd("init", "--upgrade", "--wait", "--service-account", "tiller")
	if err != nil {
		t.Errorf("Cannot init test tiller in '%s' namespace: %s\n%s %s", app.Namespace, err, stdout, stderr)
	}

	releases, err = helm.ListReleasesNames(nil)
	if err != nil {
		t.Error(err)
	}
	if !reflect.DeepEqual([]string{}, releases) {
		t.Errorf("Expected empty releases list, got: %+v", releases)
	}

	_, _, err = helm.LastReleaseStatus("asfd")
	if err == nil {
		t.Error(err)
	}
	isExists, err = helm.IsReleaseExists("asdf")
	if err != nil {
		t.Error(err)
	}
	if isExists {
		t.Errorf("Release '%s' should not exist", "asdf")
	}
	err = helm.DeleteRelease("asdf")
	if err == nil {
		t.Errorf("Should fail when trying to delete unexisting release '%s'", "asdf")
	}

	err = shouldUpgradeRelease(helm, "test-redis", "stable/redis", []string{})
	if err != nil {
		t.Error(err)
	}

	err = shouldUpgradeRelease(helm, "test-local-chart", filepath.Join(getTestDirectoryPath("test_helm"), "chart"), []string{})
	if err != nil {
		t.Error(err)
	}

	err = releasesListShouldEqual(helm, []string{"test-local-chart", "test-redis"})
	if err != nil {
		t.Error(err)
	}

	err = shouldDeleteRelease(helm, "test-redis")
	if err != nil {
		t.Error(err)
	}

	err = releasesListShouldEqual(helm, []string{"test-local-chart"})
	if err != nil {
		t.Error(err)
	}

	err = shouldDeleteRelease(helm, "test-local-chart")
	if err != nil {
		t.Error(err)
	}

	err = releasesListShouldEqual(helm, []string{})
	if err != nil {
		t.Error(err)
	}

	err = helm.UpgradeRelease("hello", "no-such-chart", []string{}, []string{}, app.Namespace)
	if err == nil {
		t.Errorf("Expected helm upgrade to fail, got no error from helm client")
	}
}

func Test_PortsPair(t *testing.T) {
	p1, p2, err := helm2.GetOpenPortsPair("127.0.0.1:12345", "127.0.0.1:54321")

	if err != nil {
		t.Errorf("Expect success ports pair, got error: %v", err)
	}

	l1, err := net.Listen("tcp", p1)
	if err != nil {
		t.Errorf("Should be able to listen on chosen port '%s': %v", p1, err)
	}
	defer l1.Close()

	l2, err := net.Listen("tcp", p2)
	if err != nil {
		t.Errorf("Should be able to listen on chosen port '%s': %v", p2, err)
	}
	defer l2.Close()

	t.Logf("GetOpenPortsPair return: '%s' '%s'\n", p1, p2)
}
