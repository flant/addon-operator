package modules

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime/trace"
	"strings"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/gofrs/uuid/v5"
	"github.com/kennygrant/sanitize"

	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/helm/client"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/kube-client/manifest"
	"github.com/flant/shell-operator/pkg/utils/measure"
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

// NewHelmModule build HelmModule from the Module templates and values + global values
func NewHelmModule(bm *BasicModule, namespace string, tmpDir string, deps *HelmModuleDependencies, validator HelmValuesValidator, opts ...ModuleOption) (*HelmModule, error) {
	moduleValues := bm.GetValues(false)

	chartValues := map[string]interface{}{
		"global": bm.dc.GlobalValuesGetter.GetValues(false),
		utils.ModuleNameToValuesKey(bm.GetName()): moduleValues,
	}

	hm := &HelmModule{
		name:             bm.Name,
		defaultNamespace: namespace,
		path:             bm.Path,
		values:           chartValues,
		tmpDir:           tmpDir,
		dependencies:     deps,
		validator:        validator,
	}

	for _, opt := range opts {
		opt.Apply(hm)
	}

	if hm.logger == nil {
		hm.logger = log.NewLogger(log.Options{}).Named("helm-module")
	}

	isHelm, err := hm.isHelmChart()
	if err != nil {
		return nil, fmt.Errorf("create HelmModule failed: %w", err)
	}

	if !isHelm {
		hm.logger.Infof("module %q has neither Chart.yaml nor templates/ dir, is't not a helm chart", bm.Name)
		return nil, nil
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
		return true, nil
	}

	if os.IsNotExist(err) {
		// Chart.yaml does not exist
		// check that templates/ dir exists
		_, err = os.Stat(filepath.Join(hm.path, "templates"))
		if err == nil {
			return true, hm.createChartYaml(chartPath)
		}
		if err != nil && os.IsNotExist(err) {
			// if templates not exists - it's not a helm module
			return false, nil
		}
	}

	return false, err
}

func (hm *HelmModule) createChartYaml(chartPath string) error {
	// we already have versions like 0.1.0 or 0.1.1
	// to keep helm updatable, we have to increment this version
	// new minor version of addon-operator seems reasonable to increase minor version of a helm chart
	data := fmt.Sprintf(`name: %s
version: 0.2.0`, hm.name)

	return os.WriteFile(chartPath, []byte(data), 0o644)
}

// checkHelmValues returns error if there is a wrong patch or values are not satisfied
// a Helm values contract defined by schemas in 'openapi' directory.
func (hm *HelmModule) checkHelmValues() error {
	// TODO: key
	return hm.validator.ValidateModuleHelmValues(utils.ModuleNameToValuesKey(hm.name), hm.values)
}

func (hm *HelmModule) RunHelmInstall(logLabels map[string]string) error {
	metricLabels := map[string]string{
		"module":     hm.name,
		"activation": logLabels["event.type"],
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

	helmClient := hm.dependencies.HelmClientFactory.NewClient(hm.logger.Named("helm-client"), logLabels)

	// Render templates to prevent excess helm runs.
	var renderedManifests string
	func() {
		defer trace.StartRegion(context.Background(), "ModuleRun-HelmPhase-helm-render").End()

		metricLabels := map[string]string{
			"module":     hm.name,
			"activation": logLabels["event.type"],
			"operation":  "template",
		}
		defer measure.Duration(func(d time.Duration) {
			hm.dependencies.MetricsStorage.HistogramObserve("{PREFIX}helm_operation_seconds", d.Seconds(), metricLabels, nil)
		})()

		renderedManifests, err = helmClient.Render(
			helmReleaseName,
			hm.path,
			[]string{valuesPath},
			[]string{},
			hm.defaultNamespace,
			false,
		)
	}()
	if err != nil {
		return err
	}
	checksum := utils.CalculateStringsChecksum(renderedManifests)

	manifests, err := manifest.ListFromYamlDocs(renderedManifests)
	if err != nil {
		return err
	}
	logEntry.Debugf("chart has %d resources", len(manifests))

	// Skip upgrades if nothing is changed
	var runUpgradeRelease bool
	func() {
		defer trace.StartRegion(context.Background(), "ModuleRun-HelmPhase-helm-check-upgrade").End()

		metricLabels := map[string]string{
			"module":     hm.name,
			"activation": logLabels["event.type"],
			"operation":  "check-upgrade",
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

	// Run helm upgrade. Trace and measure its time.
	func() {
		defer trace.StartRegion(context.Background(), "ModuleRun-HelmPhase-helm-upgrade").End()

		metricLabels := map[string]string{
			"module":     hm.name,
			"activation": logLabels["event.type"],
			"operation":  "upgrade",
		}
		defer measure.Duration(func(d time.Duration) {
			hm.dependencies.MetricsStorage.HistogramObserve("{PREFIX}helm_operation_seconds", d.Seconds(), metricLabels, nil)
		})()

		err = helmClient.UpgradeRelease(
			helmReleaseName,
			hm.path,
			[]string{valuesPath},
			[]string{fmt.Sprintf("_addonOperatorModuleChecksum=%s", checksum)},
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
		logEntry.Debugf("helm release '%s' not exists: should run upgrade", releaseName)
		return true, nil
	}

	if err != nil {
		return false, err
	}

	// Run helm upgrade if last release isn't `deployed`
	if strings.ToLower(status) != "deployed" {
		logEntry.Debugf("helm release '%s' has %s status: should run upgrade", releaseName, strings.ToLower(status))
		return true, nil
	}

	// Get values for a non-failed release.
	releaseValues, err := helmClient.GetReleaseValues(releaseName)
	if err != nil {
		logEntry.Debugf("helm release '%s' get values error, no upgrade: %v", releaseName, err)
		return false, err
	}

	// Run helm upgrade if there is no stored checksum
	recordedChecksum, hasKey := releaseValues["_addonOperatorModuleChecksum"]
	if !hasKey {
		logEntry.Debugf("helm release '%s' has no saved checksum of values: should run upgrade", releaseName)
		return true, nil
	}

	// Calculate a checksum of current values and compare to a stored checksum.
	// Run helm upgrade if checksum is changed.
	if recordedChecksumStr, ok := recordedChecksum.(string); ok {
		if recordedChecksumStr != checksum {
			logEntry.Debugf("helm release '%s' checksum '%s' is changed to '%s': should run upgrade", releaseName, recordedChecksumStr, checksum)
			return true, nil
		}
	}

	// Check if there are absent resources
	absent, err := hm.dependencies.HelmResourceManager.GetAbsentResources(manifests, hm.defaultNamespace)
	if err != nil {
		return false, err
	}

	// Run helm upgrade if there are absent resources
	if len(absent) > 0 {
		logEntry.Debugf("helm release '%s' has %d absent resources: should run upgrade", releaseName, len(absent))
		return true, nil
	}

	logEntry.Debugf("helm release '%s' is unchanged: skip release upgrade", releaseName)
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

	log.Debugf("Prepared module %s helm values:\n%s", hm.name, hm.values.DebugString())

	return path, nil
}

func (hm *HelmModule) safeName() string {
	return sanitize.BaseName(hm.name)
}

func (hm *HelmModule) Render(namespace string, debug bool) (string, error) {
	if namespace == "" {
		namespace = "default"
	}
	valuesPath, err := hm.PrepareValuesYamlFile()
	if err != nil {
		return "", err
	}
	defer os.Remove(valuesPath)

	return hm.dependencies.HelmClientFactory.NewClient(hm.logger.Named("helm-client")).Render(hm.name, hm.path, []string{valuesPath}, nil, namespace, debug)
}
