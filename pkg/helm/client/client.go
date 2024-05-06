package client

import "github.com/flant/addon-operator/pkg/utils"

type HelmClient interface {
	LastReleaseStatus(releaseNamespace string, releaseName string) (string, string, error)
	UpgradeRelease(releaseName string, chart string, valuesPaths []string, setValues []string, namespace string) error
	Render(releaseName string, chart string, valuesPaths []string, setValues []string, namespace string, debug bool) (string, error)
	GetReleaseValues(releaseNamespace string, releaseName string) (utils.Values, error)
	DeleteRelease(releaseNamespace string, releaseName string) error
	ListReleasesNames(releaseNamespace string) ([]string, error)
	IsReleaseExists(releaseNamespace string, releaseName string) (bool, error)
}
