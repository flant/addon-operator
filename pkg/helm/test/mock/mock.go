package mock

import (
	"sync"

	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/helm/client"
	"github.com/flant/addon-operator/pkg/utils"
)

func NewClientFactory(cl client.HelmClient) *helm.ClientFactory {
	return &helm.ClientFactory{
		NewClientFn: func(_ ...map[string]string) client.HelmClient {
			return cl
		},
		NewClientWithLockFn: func(_ *sync.Mutex, _ ...map[string]string) client.HelmClient {
			return cl
		},
	}
}

type Client struct {
	client.HelmClient
	UpgradeReleaseExecuted bool
	DeleteReleaseExecuted  bool
	ReleaseNames           []string
}

var _ client.HelmClient = &Client{}

func (c *Client) ListReleasesNames() ([]string, error) {
	if c.ReleaseNames != nil {
		return c.ReleaseNames, nil
	}
	return []string{}, nil
}

func (c *Client) LastReleaseStatus(_ string) (string, string, error) {
	return "", "", nil
}

func (c *Client) IsReleaseExists(_ string) (bool, error) {
	return true, nil
}

func (c *Client) GetReleaseValues(_ string) (utils.Values, error) {
	return make(utils.Values), nil
}

func (c *Client) UpgradeRelease(_, _ string, _ []string, _ []string, _ string) error {
	c.UpgradeReleaseExecuted = true
	return nil
}

func (c *Client) DeleteRelease(_ string) error {
	c.DeleteReleaseExecuted = true
	return nil
}

func (c *Client) Render(_ string, _ string, _ []string, _ []string, _ string, _ bool) (string, error) {
	return "", nil
}
