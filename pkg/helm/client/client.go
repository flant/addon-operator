package client

import (
	"errors"

	"github.com/flant/addon-operator/pkg/utils"
)

// ErrLabelIsNotFound is returned when a label is not found in the release
var ErrLabelIsNotFound = errors.New("label is not found")

type HelmClient interface {
	LastReleaseStatus(releaseName string) (string, string, error)
	UpgradeRelease(releaseName string, chart string, valuesPaths []string, setValues []string, labels map[string]string, namespace string) error
	Render(releaseName string, chart string, valuesPaths []string, setValues []string, namespace string, debug bool) (string, error)
	GetReleaseValues(releaseName string) (utils.Values, error)
	GetReleaseChecksum(releaseName string) (string, error)
	GetReleaseLabels(releaseName, labelName string) (string, error)
	DeleteRelease(releaseName string) error
	ListReleasesNames() ([]string, error)
	IsReleaseExists(releaseName string) (bool, error)
	WithLogLabels(map[string]string)
	WithExtraLabels(map[string]string)
}
