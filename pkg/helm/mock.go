//go:build !release
// +build !release

package helm

import (
	"github.com/flant/addon-operator/pkg/helm/client"
	"github.com/flant/addon-operator/pkg/utils"
)

func MockHelm(cl client.HelmClient) *Helm {
	return &Helm{
		newClient: func(_ ...map[string]string) client.HelmClient {
			return cl
		},
	}
}

type MockHelmClient struct {
	client.HelmClient
	UpgradeReleaseExecuted bool
	DeleteReleaseExecuted  bool
	ReleaseNames           []string
}

var _ client.HelmClient = &MockHelmClient{}

func (h *MockHelmClient) ListReleasesNames(_ map[string]string) ([]string, error) {
	if h.ReleaseNames != nil {
		return h.ReleaseNames, nil
	}
	return []string{}, nil
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

func (h *MockHelmClient) Render(_ string, _ string, _ []string, _ []string, _ string) (string, error) {
	return "", nil
}
