package modules

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/gofrs/uuid/v5"
	"github.com/kennygrant/sanitize"
	"github.com/werf/nelm/pkg/action"
	"go.opentelemetry.io/otel"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/storage/driver"

	"github.com/flant/addon-operator/pkg"
	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/helm/client"
	"github.com/flant/addon-operator/pkg/helm/helm3lib"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/kube-client/manifest"
	"github.com/flant/shell-operator/pkg/utils/measure"
)

const (
	LabelMaintenanceNoResourceReconciliation = "maintenance.deckhouse.io/no-resource-reconciliation"
	helmModuleServiceName                    = "helm-module"
)

// HelmModule representation of the module, which has Helm Chart and could be installed with the helm lib
type HelmModule struct {
	// Name of the module
	name string
	// default namespace for module
	defaultNamespace string
	// Path of the module on the fs
	path string

	values utils.Values

	tmpDir string

	dependencies *HelmModuleDependencies
	validator    HelmValuesValidator

	logger *log.Logger

	additionalLabels map[string]string

	// hasChartFile if the module has Chart.yaml
	hasChartFile bool
}

type HelmValuesValidator interface {
	ValidateModuleHelmValues(string, utils.Values) error
}

type HelmResourceManager interface {
	GetAbsentResources(manifests []manifest.Manifest, defaultNamespace string) ([]manifest.Manifest, error)
	StartMonitor(moduleName string, manifests []manifest.Manifest, defaultNamespace string, LastReleaseStatus func(releaseName string) (revision string, status string, err error))
	HasMonitor(moduleName string) bool
}

type MetricsStorage interface {
	HistogramObserve(metric string, value float64, labels map[string]string, buckets []float64)
}

type HelmModuleDependencies struct {
	HelmClientFactory   *helm.ClientFactory
	HelmResourceManager HelmResourceManager
	MetricsStorage      MetricsStorage
	HelmValuesValidator HelmValuesValidator
}

var ErrModuleIsNotHelm = errors.New("module is not a helm")

// NewHelmModule build HelmModule from the Module templates and values + global values
func NewHelmModule(bm *BasicModule, namespace string, tmpDir string, deps *HelmModuleDependencies, validator HelmValuesValidator, opts ...ModuleOption) (*HelmModule, error) {
	moduleValues := bm.GetValues(false)

	chartValues := map[string]interface{}{
		"global": bm.dc.GlobalValuesGetter.GetValues(false),
		utils.ModuleNameToValuesKey(bm.GetName()): moduleValues,
	}

	additionalLabels := make(map[string]string)
	// LabelMaintenanceNoResourceReconciliation is now only stored in Helm release metadata,and in resources annotations
	if bm.GetMaintenanceState() != Managed {
		additionalLabels[LabelMaintenanceNoResourceReconciliation] = ""
	}

	hm := &HelmModule{
		name:             bm.GetName(),
		defaultNamespace: namespace,
		path:             bm.Path,
		values:           chartValues,
		tmpDir:           tmpDir,
		dependencies:     deps,
		validator:        validator,
		additionalLabels: additionalLabels,
	}

	for _, opt := range opts {
		opt.Apply(hm)
	}

	if hm.logger == nil {
		hm.logger = log.NewLogger().Named("helm-module")
	}

	isHelm, err := hm.isHelmChart()
	if err != nil {
		return nil, fmt.Errorf("create HelmModule failed: %w", err)
	}

	if !isHelm {
		hm.logger.Info("module has neither Chart.yaml nor templates/ dir, is't not a helm chart",
			slog.String("name", bm.GetName()))
		return nil, ErrModuleIsNotHelm
	}

	return hm, nil
}

func (hm *HelmModule) WithLogger(logger *log.Logger) {
	hm.logger = logger
}

// isHelmChart check, could it be considered as helm chart or not
func (hm *HelmModule) isHelmChart() (bool, error) {
	chartPath := filepath.Join(hm.path, "Chart.yaml")

	_, err := os.Stat(chartPath)
	if err == nil {
		// Chart.yaml exists, consider this module as helm chart
		hm.hasChartFile = true
		return true, nil
	}

	if os.IsNotExist(err) {
		// Chart.yaml does not exist
		// check that templates/ dir exists
		_, err = os.Stat(filepath.Join(hm.path, "templates"))
		if err == nil {
			return true, nil
		}
		if os.IsNotExist(err) {
			// if templates not exists - it's not a helm module
			return false, nil
		}
	}

	return false, err
}

// loadChart either loads Chart.yaml from os or makes it virtual
func (hm *HelmModule) loadChart() (*chart.Chart, error) {
	// if Chart.yaml exists, load chart from os
	if hm.hasChartFile {
		return loader.Load(hm.path)
	}

	var files []*loader.BufferedFile

	chartYaml := fmt.Sprintf(`
name: %s
version: 0.2.0
apiVersion: v2`, hm.name)

	files = append(files, &loader.BufferedFile{
		Name: "Chart.yaml",
		Data: []byte(chartYaml),
	})

	ignored := []string{
		"crds",
		"docs",
		"hooks",
		"images",
		"lib",
	}

	err := filepath.Walk(hm.path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			if slices.Contains(ignored, info.Name()) {
				return filepath.SkipDir
			}

			return nil
		}

		relPath, err := filepath.Rel(hm.path, path)
		if err != nil {
			return err
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		files = append(files, &loader.BufferedFile{
			Name: relPath,
			Data: data,
		})

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read module files: %w", err)
	}

	loaded, err := loader.LoadFiles(files)
	if err != nil {
		return nil, fmt.Errorf("load chart from files: %w", err)
	}

	return loaded, nil
}

// checkHelmValues returns error if there is a wrong patch or values are not satisfied
// a Helm values contract defined by schemas in 'openapi' directory.
func (hm *HelmModule) checkHelmValues() error {
	// TODO: key
	return hm.validator.ValidateModuleHelmValues(utils.ModuleNameToValuesKey(hm.name), hm.values)
}

var ErrReleaseIsUnmanaged = errors.New("release is unmanaged")

// RunHelmInstall installs or upgrades a Helm release for the module.
// The `state` parameter determines the maintenance state of the release:
// - If `state` is `Unmanaged`, a release label check is triggered, and the Helm upgrade is skipped.
func (hm *HelmModule) RunHelmInstall(ctx context.Context, logLabels map[string]string, state MaintenanceState) error {
	_, span := otel.Tracer(helmModuleServiceName).Start(ctx, "RunHelmInstall")
	defer span.End()

	metricLabels := map[string]string{
		"module":                hm.name,
		pkg.MetricKeyActivation: logLabels[pkg.LogKeyEventType],
	}

	defer measure.Duration(func(d time.Duration) {
		hm.dependencies.MetricsStorage.HistogramObserve("{PREFIX}module_helm_seconds", d.Seconds(), metricLabels, nil)
	})()

	logEntry := utils.EnrichLoggerWithLabels(hm.logger, logLabels)

	err := hm.checkHelmValues()
	if err != nil {
		return fmt.Errorf("check helm values: %v", err)
	}

	// TODO Now it returns just a module name. Should it be cleaned from special symbols?
	helmReleaseName := hm.name

	valuesPath, err := hm.PrepareValuesYamlFile()
	if err != nil {
		return err
	}
	defer os.Remove(valuesPath)

	helmClientOptions := []helm.ClientOption{
		helm.WithExtraLabels(hm.additionalLabels),
		helm.WithLogLabels(logLabels),
	}

	helmClient := hm.dependencies.HelmClientFactory.NewClient(hm.logger.Named("helm-client"), helmClientOptions...)

	if state == Unmanaged {
		isUnmanaged, err := helmClient.GetReleaseLabels(helmReleaseName, LabelMaintenanceNoResourceReconciliation)
		if err != nil && !errors.Is(err, helm3lib.ErrLabelIsNotFound) && !errors.Is(err, driver.ErrReleaseNotFound) {
			// Also check for nelm ReleaseNotFoundError
			var releaseNotFoundErr *action.ReleaseNotFoundError
			if !errors.As(err, &releaseNotFoundErr) {
				logEntry.Warn("get release label failed", log.Err(err), slog.String("release", helmReleaseName))
				return fmt.Errorf("get release label failed: %w", err)
			}
			logEntry.Debug("release not found when checking unmanaged state", slog.String("release", helmReleaseName))
		}

		if isUnmanaged == "true" {
			logEntry.Info("helm release is Unmanaged, skip helm upgrade", slog.String("release", helmReleaseName))

			return ErrReleaseIsUnmanaged
		}
	}

	span.AddEvent("ModuleRun-HelmPhase-helm-render")

	moduleChart, err := hm.loadChart()
	if err != nil {
		return fmt.Errorf("load module chart: %w", err)
	}

	// Prepare release labels
	releaseLabels := map[string]string{
		LabelMaintenanceNoResourceReconciliation: "false",
	}

	if state == Unmanaged {
		releaseLabels[LabelMaintenanceNoResourceReconciliation] = "true"
	}

	// Render templates to prevent excess helm runs.
	var renderedManifests string
	func() {
		metricLabels := map[string]string{
			"module":                hm.name,
			pkg.MetricKeyActivation: logLabels[pkg.LogKeyEventType],
			"operation":             "template",
		}

		defer measure.Duration(func(d time.Duration) {
			hm.dependencies.MetricsStorage.HistogramObserve("{PREFIX}helm_operation_seconds", d.Seconds(), metricLabels, nil)
		})()

		renderedManifests, err = helmClient.Render(
			helmReleaseName,
			moduleChart,
			[]string{valuesPath},
			[]string{},
			releaseLabels,
			hm.defaultNamespace,
			false,
		)
	}()
	if err != nil {
		return err
	}

	checksum := utils.CalculateStringsChecksum(renderedManifests)

	// Add checksum to release labels
	releaseLabels["moduleChecksum"] = checksum

	manifests, err := manifest.ListFromYamlDocs(renderedManifests)
	if err != nil {
		return err
	}

	logEntry.Debug("chart has resources", slog.Int("count", len(manifests)))

	span.AddEvent("ModuleRun-HelmPhase-helm-check-upgrade")
	// Skip upgrades if nothing is changed
	var runUpgradeRelease bool
	func() {
		metricLabels := map[string]string{
			"module":                hm.name,
			pkg.MetricKeyActivation: logLabels[pkg.LogKeyEventType],
			"operation":             "check-upgrade",
		}
		defer measure.Duration(func(d time.Duration) {
			hm.dependencies.MetricsStorage.HistogramObserve("{PREFIX}helm_operation_seconds", d.Seconds(), metricLabels, nil)
		})()

		runUpgradeRelease, err = hm.shouldRunHelmUpgrade(helmClient, helmReleaseName, checksum, manifests, logLabels)
	}()
	if err != nil {
		return err
	}

	if !runUpgradeRelease {
		// Start resources monitor if release is not changed
		if !hm.dependencies.HelmResourceManager.HasMonitor(hm.name) {
			hm.dependencies.HelmResourceManager.StartMonitor(hm.name, manifests, hm.defaultNamespace, helmClient.LastReleaseStatus)
		}

		return nil
	}

	span.AddEvent("ModuleRun-HelmPhase-helm-upgrade")
	// Run helm upgrade. Trace and measure its time.
	func() {
		metricLabels := map[string]string{
			"module":                hm.name,
			pkg.MetricKeyActivation: logLabels[pkg.LogKeyEventType],
			"operation":             "upgrade",
		}

		defer measure.Duration(func(d time.Duration) {
			hm.dependencies.MetricsStorage.HistogramObserve("{PREFIX}helm_operation_seconds", d.Seconds(), metricLabels, nil)
		})()

		err = helmClient.UpgradeRelease(
			helmReleaseName,
			moduleChart,
			[]string{valuesPath},
			[]string{},
			releaseLabels,
			hm.defaultNamespace,
		)
	}()

	if err != nil {
		return err
	}

	// Start monitor resources if release was successful
	hm.dependencies.HelmResourceManager.StartMonitor(hm.name, manifests, hm.defaultNamespace, helmClient.LastReleaseStatus)

	return nil
}

// If all these conditions aren't met, helm upgrade can be skipped.
func (hm *HelmModule) shouldRunHelmUpgrade(helmClient client.HelmClient, releaseName string, checksum string, manifests []manifest.Manifest, logLabels map[string]string) (bool, error) {
	logEntry := utils.EnrichLoggerWithLabels(hm.logger, logLabels)

	revision, status, err := helmClient.LastReleaseStatus(releaseName)

	if revision == "0" {
		logEntry.Debug("helm release not exists: should run upgrade", slog.String("release", releaseName))
		return true, nil
	}

	if err != nil {
		return false, err
	}

	// Run helm upgrade if last release isn't `deployed`
	if strings.ToLower(status) != "deployed" {
		logEntry.Debug("helm release: should run upgrade",
			slog.String("release", releaseName),
			slog.String("status", strings.ToLower(status)))
		return true, nil
	}

	// Get values for a non-failed release.
	recordedChecksum, err := helmClient.GetReleaseChecksum(releaseName)
	if err != nil {
		logEntry.Debug("helm release get values error, no upgrade",
			slog.String("release", releaseName),
			log.Err(err))
		return false, err
	}

	// Calculate a checksum of current values and compare to a stored checksum.
	// Run helm upgrade if checksum is changed.
	if recordedChecksum != checksum {
		logEntry.Debug("helm release checksum is changed: should run upgrade",
			slog.String("release", releaseName),
			slog.String("checksum", recordedChecksum),
			slog.String("newChecksum", checksum))
		return true, nil
	}

	// Check if there are absent resources
	absent, err := hm.dependencies.HelmResourceManager.GetAbsentResources(manifests, hm.defaultNamespace)
	if err != nil {
		return false, err
	}

	// Run helm upgrade if there are absent resources
	if len(absent) > 0 {
		logEntry.Debug("helm release has absent resources: should run upgrade",
			slog.String("release", releaseName),
			slog.Int("count", len(absent)),
		)
		return true, nil
	}

	logEntry.Debug("helm release is unchanged: skip release upgrade",
		slog.String("release", releaseName))
	return false, nil
}

func (hm *HelmModule) PrepareValuesYamlFile() (string, error) {
	data, err := hm.values.YamlBytes()
	if err != nil {
		return "", err
	}

	path := filepath.Join(hm.tmpDir, fmt.Sprintf("%s.module-values.yaml-%s", hm.safeName(), uuid.Must(uuid.NewV4()).String()))
	err = utils.DumpData(path, data)
	if err != nil {
		return "", err
	}

	hm.logger.Debug("Prepared module helm values info",
		slog.String("moduleName", hm.name),
		slog.String("values", hm.values.DebugString()))

	return path, nil
}

func (hm *HelmModule) safeName() string {
	return sanitize.BaseName(hm.name)
}

func (hm *HelmModule) Render(namespace string, debug bool, state MaintenanceState) (string, error) {
	if namespace == "" {
		namespace = "default"
	}

	valuesPath, err := hm.PrepareValuesYamlFile()
	if err != nil {
		return "", err
	}
	defer os.Remove(valuesPath)

	helmClientOptions := []helm.ClientOption{
		helm.WithExtraLabels(hm.additionalLabels),
	}

	releaseLabels := map[string]string{
		LabelMaintenanceNoResourceReconciliation: strconv.FormatBool(state == Unmanaged),
	}

	moduleChart, err := hm.loadChart()
	if err != nil {
		return "", fmt.Errorf("load module chart: %v", err)
	}

	return hm.dependencies.HelmClientFactory.NewClient(hm.logger.Named("helm-client"), helmClientOptions...).
		Render(hm.name, moduleChart, []string{valuesPath}, nil, releaseLabels, namespace, debug)
}
