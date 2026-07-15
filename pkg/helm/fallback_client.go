package helm

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

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
// Upgrades of charts that use werf.io/* annotations are never handed to helm3lib: it does
// not implement those semantics, so nelm's error is reported instead of a wrong deploy.
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

// werfAnnotationPrefix marks resources whose deploy semantics only nelm implements:
// ordering (werf.io/weight, werf.io/deploy-dependency-*), tracking
// (werf.io/track-termination-mode) and the like. helm3lib ignores such annotations.
const werfAnnotationPrefix = "werf.io/"

// chartRenderSources are the parts of a chart helm renders manifests from, and therefore the
// only places an annotation can reach the release from. Hooks and image sources are left out
// on purpose: a module's hook is a compiled binary, so reading it would be slow and would
// match werf.io/ inside compiled code, disabling the fallback for a chart that never asked
// for werf semantics.
var chartRenderSources = []string{"Chart.yaml", "values.yaml", "templates", "charts"}

// chartUsesWerfAnnotations reports whether the chart declares a werf.io/* annotation. It
// scans the raw sources rather than rendered manifests: the check runs on the fallback path,
// where nelm has just failed, so it must not depend on another render. A false positive only
// disables the fallback, which is the safe way to be wrong.
func chartUsesWerfAnnotations(chartPath string) (bool, error) {
	if _, err := os.Stat(chartPath); err != nil {
		return false, fmt.Errorf("scan chart for werf annotations: %w", err)
	}

	found := false

	scan := func(path string, entry fs.DirEntry, err error) error {
		switch {
		case os.IsNotExist(err):
			// this part of the chart is optional, nothing to scan
			return fs.SkipDir
		case err != nil:
			return err
		case entry.IsDir():
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		if bytes.Contains(data, []byte(werfAnnotationPrefix)) {
			found = true

			return fs.SkipAll
		}

		return nil
	}

	for _, source := range chartRenderSources {
		if err := filepath.WalkDir(filepath.Join(chartPath, source), scan); err != nil {
			return false, fmt.Errorf("scan chart for werf annotations: %w", err)
		}

		if found {
			return true, nil
		}
	}

	return false, nil
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

	// helm3lib does not implement werf.io/* semantics, so handing such a chart to it would
	// deploy the release in a different order and track it differently. Report nelm's error
	// instead of silently deploying the module wrong.
	usesWerf, scanErr := chartUsesWerfAnnotations(chart)
	if scanErr != nil {
		c.logger.Warn("cannot check chart for werf.io annotations, not falling back to helm",
			slog.String(pkg.LogKeyRelease, releaseName),
			slog.String(pkg.LogKeyChart, chart),
			log.Err(scanErr),
		)

		return err
	}

	if usesWerf {
		c.logger.Warn("chart uses werf.io annotations unsupported by helm, not falling back to helm",
			slog.String(pkg.LogKeyRelease, releaseName),
			slog.String(pkg.LogKeyChart, chart),
			log.Err(err),
		)

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
