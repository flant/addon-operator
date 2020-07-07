package client

import "github.com/flant/addon-operator/pkg/utils"

type HelmClient interface {
	CommandEnv() []string
	Cmd(args ...string) (string, string, error)
	InitAndVersion() error
	DeleteSingleFailedRevision(releaseName string) error
	DeleteOldFailedRevisions(releaseName string) error
	LastReleaseStatus(releaseName string) (string, string, error)
	UpgradeRelease(releaseName string, chart string, valuesPaths []string, setValues []string, namespace string) error
	Render(releaseName string, chart string, valuesPaths []string, setValues []string, namespace string) (string, error)
	GetReleaseValues(releaseName string) (utils.Values, error)
	DeleteRelease(releaseName string) error
	ListReleases(labelSelector map[string]string) ([]string, error)
	ListReleasesNames(labelSelector map[string]string) ([]string, error)
	IsReleaseExists(releaseName string) (bool, error)
}
