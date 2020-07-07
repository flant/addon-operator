// +build !release

package helm

import (
	"github.com/flant/addon-operator/pkg/helm/client"
	"github.com/flant/addon-operator/pkg/utils"
)

type MockHelmClient struct {
	client.HelmClient
	DeleteSingleFailedRevisionExecuted bool
	UpgradeReleaseExecuted             bool
	DeleteReleaseExecuted              bool
	ReleaseNames                       []string
}

func (h *MockHelmClient) DeleteOldFailedRevisions(releaseName string) error {
	return nil
}

func (h *MockHelmClient) ListReleases(_ map[string]string) ([]string, error) {
	if h.ReleaseNames != nil {
		return h.ReleaseNames, nil
	}
	return []string{}, nil
}

func (h *MockHelmClient) ListReleasesNames(_ map[string]string) ([]string, error) {
	if h.ReleaseNames != nil {
		return h.ReleaseNames, nil
	}
	return []string{}, nil
}

func (h *MockHelmClient) CommandEnv() []string {
	return []string{}
}

func (h *MockHelmClient) DeleteSingleFailedRevision(_ string) error {
	h.DeleteSingleFailedRevisionExecuted = true
	return nil
}

func (h *MockHelmClient) LastReleaseStatus(_ string) (string, string, error) {
	return "", "", nil
}

func (h *MockHelmClient) IsReleaseExists(_ string) (bool, error) {
	return true, nil
}

func (h *MockHelmClient) GetReleaseValues(_ string) (utils.Values, error) {
	return make(utils.Values), nil
}

func (h *MockHelmClient) UpgradeRelease(_, _ string, _ []string, _ []string, _ string) error {
	h.UpgradeReleaseExecuted = true
	return nil
}

func (h *MockHelmClient) DeleteRelease(_ string) error {
	h.DeleteReleaseExecuted = true
	return nil
}
