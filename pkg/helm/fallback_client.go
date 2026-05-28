package helm

import (
	"errors"
	"log/slog"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/werf/nelm/pkg/action"
	"github.com/werf/nelm/pkg/resource"

	"github.com/flant/addon-operator/pkg"
	"github.com/flant/addon-operator/pkg/helm/client"
	"github.com/flant/addon-operator/pkg/metrics"
	"github.com/flant/addon-operator/pkg/utils"
)

// Fallback operations exposed as metric label values.
const (
	fallbackOperationUpgrade = "upgrade"
	fallbackOperationRender  = "render"
	fallbackOperationDelete  = "delete"

	fallbackErrTypeBuildPlan          = "build_plan"
	fallbackErrTypeResourceDuplicates = "resource_duplicates"
	fallbackErrTypeUnknown            = "unknown"
)

// FallbackClient wraps a primary HelmClient (nelm) and falls back to a secondary
// HelmClient (helm3lib) when the primary returns one of the well-known nelm errors
// that helm3lib is able to recover from: action.ErrBuildPlan and
// resource.ErrResourceDuplicatesFound.
//
// Read-only methods are always served by the primary client, both clients receive
// label/annotation updates so the fallback is ready to take over at any moment.
//
// When MetricStorage is configured the client increments
// metrics.HelmFallbackTotal{module, operation, error_type} on every fallback.
type FallbackClient struct {
	primary       client.HelmClient
	fallback      client.HelmClient
	logger        *log.Logger
	metricStorage MetricStorage
}

var _ client.HelmClient = (*FallbackClient)(nil)

// NewFallbackClient builds a FallbackClient. metricStorage is optional; when nil
// no fallback metric is emitted.
func NewFallbackClient(primary, fallback client.HelmClient, logger *log.Logger, metricStorage MetricStorage) *FallbackClient {
	return &FallbackClient{
		primary:       primary,
		fallback:      fallback,
		logger:        logger.With(pkg.LogKeyOperatorComponent, "helm-fallback"),
		metricStorage: metricStorage,
	}
}

// shouldFallback reports whether err is a nelm error that should trigger
// the helm3lib fallback path.
func shouldFallback(err error) bool {
	if err == nil {
		return false
	}

	return errors.Is(err, action.ErrBuildPlan) || errors.Is(err, resource.ErrResourceDuplicatesFound)
}

// fallbackErrorType returns a low-cardinality label value describing which
// recoverable nelm error triggered the fallback.
func fallbackErrorType(err error) string {
	switch {
	case errors.Is(err, action.ErrBuildPlan):
		return fallbackErrTypeBuildPlan
	case errors.Is(err, resource.ErrResourceDuplicatesFound):
		return fallbackErrTypeResourceDuplicates
	default:
		return fallbackErrTypeUnknown
	}
}

// recordFallback emits the helm_fallback_total counter when metric storage is
// configured. releaseName is used as the module label since within addon-operator
// helm release name and module name are equivalent.
func (c *FallbackClient) recordFallback(operation, releaseName string, err error) {
	if c.metricStorage == nil {
		return
	}

	c.metricStorage.CounterAdd(metrics.HelmFallbackTotal, 1.0, map[string]string{
		pkg.MetricKeyModule:    releaseName,
		pkg.MetricKeyOperation: operation,
		pkg.MetricKeyErrorType: fallbackErrorType(err),
	})
}

func (c *FallbackClient) UpgradeRelease(releaseName, chart string, valuesPaths, setValues []string, releaseLabels map[string]string, namespace string) error {
	err := c.primary.UpgradeRelease(releaseName, chart, valuesPaths, setValues, releaseLabels, namespace)
	if !shouldFallback(err) {
		return err
	}

	c.recordFallback(fallbackOperationUpgrade, releaseName, err)

	c.logger.Warn("nelm UpgradeRelease failed with a recoverable error, falling back to helm",
		slog.String(pkg.LogKeyRelease, releaseName),
		slog.String(pkg.LogKeyNamespace, namespace),
		log.Err(err),
	)

	return c.fallback.UpgradeRelease(releaseName, chart, valuesPaths, setValues, releaseLabels, namespace)
}

func (c *FallbackClient) Render(releaseName, chart string, valuesPaths, setValues []string, releaseLabels map[string]string, namespace string, debug bool) (string, error) {
	rendered, err := c.primary.Render(releaseName, chart, valuesPaths, setValues, releaseLabels, namespace, debug)
	if !shouldFallback(err) {
		return rendered, err
	}

	c.recordFallback(fallbackOperationRender, releaseName, err)

	c.logger.Warn("nelm Render failed with a recoverable error, falling back to helm",
		slog.String(pkg.LogKeyRelease, releaseName),
		slog.String(pkg.LogKeyNamespace, namespace),
		log.Err(err),
	)

	return c.fallback.Render(releaseName, chart, valuesPaths, setValues, releaseLabels, namespace, debug)
}

func (c *FallbackClient) DeleteRelease(releaseName string) error {
	err := c.primary.DeleteRelease(releaseName)
	if !shouldFallback(err) {
		return err
	}

	c.recordFallback(fallbackOperationDelete, releaseName, err)

	c.logger.Warn("nelm DeleteRelease failed with a recoverable error, falling back to helm",
		slog.String(pkg.LogKeyRelease, releaseName),
		log.Err(err),
	)

	return c.fallback.DeleteRelease(releaseName)
}

func (c *FallbackClient) LastReleaseStatus(releaseName string) (string, string, error) {
	return c.primary.LastReleaseStatus(releaseName)
}

func (c *FallbackClient) GetReleaseValues(releaseName string) (utils.Values, error) {
	return c.primary.GetReleaseValues(releaseName)
}

func (c *FallbackClient) GetReleaseChecksum(releaseName string) (string, error) {
	return c.primary.GetReleaseChecksum(releaseName)
}

func (c *FallbackClient) GetReleaseLabels(releaseName, labelName string) (string, error) {
	return c.primary.GetReleaseLabels(releaseName, labelName)
}

func (c *FallbackClient) ListReleasesNames() ([]string, error) {
	return c.primary.ListReleasesNames()
}

func (c *FallbackClient) IsReleaseExists(releaseName string) (bool, error) {
	return c.primary.IsReleaseExists(releaseName)
}

func (c *FallbackClient) WithLogLabels(logLabels map[string]string) {
	c.primary.WithLogLabels(logLabels)
	c.fallback.WithLogLabels(logLabels)
}

func (c *FallbackClient) WithExtraLabels(labels map[string]string) {
	c.primary.WithExtraLabels(labels)
	c.fallback.WithExtraLabels(labels)
}
