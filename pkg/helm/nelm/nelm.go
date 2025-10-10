package nelm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/werf/nelm/pkg/action"
	nelmLog "github.com/werf/nelm/pkg/log"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/cli"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/yaml"

	"github.com/flant/addon-operator/pkg/helm/client"
	"github.com/flant/addon-operator/pkg/helm/helm3lib"
	"github.com/flant/addon-operator/pkg/utils"
)

var _ client.HelmClient = (*NelmClient)(nil)

type CommonOptions struct {
	genericclioptions.ConfigFlags

	HistoryMax  int32
	Timeout     time.Duration
	HelmDriver  string
	KubeContext string
}

type NelmActions interface {
	ReleaseGet(ctx context.Context, name, namespace string, opts action.ReleaseGetOptions) (*action.ReleaseGetResultV1, error)
	ReleaseInstall(ctx context.Context, name, namespace string, opts action.ReleaseInstallOptions) error
	ReleaseUninstall(ctx context.Context, name, namespace string, opts action.ReleaseUninstallOptions) error
	ReleaseList(ctx context.Context, opts action.ReleaseListOptions) (*action.ReleaseListResultV1, error)
	ChartRender(ctx context.Context, opts action.ChartRenderOptions) (*action.ChartRenderResultV1, error)
}

type DefaultNelmActions struct{}

func (d *DefaultNelmActions) ReleaseGet(ctx context.Context, name, namespace string, opts action.ReleaseGetOptions) (*action.ReleaseGetResultV1, error) {
	return action.ReleaseGet(ctx, name, namespace, opts)
}

func (d *DefaultNelmActions) ReleaseInstall(ctx context.Context, name, namespace string, opts action.ReleaseInstallOptions) error {
	return action.ReleaseInstall(ctx, name, namespace, opts)
}

func (d *DefaultNelmActions) ReleaseUninstall(ctx context.Context, name, namespace string, opts action.ReleaseUninstallOptions) error {
	return action.ReleaseUninstall(ctx, name, namespace, opts)
}

func (d *DefaultNelmActions) ReleaseList(ctx context.Context, opts action.ReleaseListOptions) (*action.ReleaseListResultV1, error) {
	return action.ReleaseList(ctx, opts)
}

func (d *DefaultNelmActions) ChartRender(ctx context.Context, opts action.ChartRenderOptions) (*action.ChartRenderResultV1, error) {
	return action.ChartRender(ctx, opts)
}

func NewNelmClient(opts *CommonOptions, logger *log.Logger, labels map[string]string) *NelmClient {
	nelmLog.Default = NewNelmLogger(logger)

	if opts == nil {
		opts = &CommonOptions{}
	}

	// TODO: maybe it did not work because of this???
	// opts = applyCommonOptionsDefaults(opts,
	// 	buildConfigFlagsFromEnv(opts.Namespace, cli.New()))

	opts.ConfigFlags = *buildConfigFlagsFromEnv(opts.Namespace, cli.New())

	if opts.HistoryMax == 0 {
		opts.HistoryMax = 10
	}

	if opts.HelmDriver == "" {
		opts.HelmDriver = os.Getenv("HELM_DRIVER")
	}

	clientLabels := make(map[string]string)
	if labels != nil {
		maps.Copy(clientLabels, labels)
	}

	return &NelmClient{
		logger:  logger.With("operator.component", "nelm"),
		labels:  clientLabels,
		opts:    opts,
		actions: &DefaultNelmActions{},
	}
}

type NelmClient struct {
	logger      *log.Logger
	labels      map[string]string
	annotations map[string]string

	opts         *CommonOptions
	actions      NelmActions
	virtualChart bool
	modulePath   string
}

// GetReleaseLabels returns a specific label value from the release.
func (c *NelmClient) GetReleaseLabels(releaseName, labelName string) (string, error) {
	releaseGetResult, err := c.actions.ReleaseGet(context.TODO(), releaseName, *c.opts.Namespace, action.ReleaseGetOptions{
		KubeContext:          c.opts.KubeContext,
		OutputNoPrint:        true,
		ReleaseStorageDriver: c.opts.HelmDriver,
	})
	if err != nil {
		c.logger.Debug("Failed to get nelm release", log.Err(err), slog.String("release", releaseName))

		var releaseNotFoundErr *action.ReleaseNotFoundError
		if errors.As(err, &releaseNotFoundErr) {
			c.logger.Debug("Release not found when getting labels", slog.String("release", releaseName))
			// Return the original ReleaseNotFoundError so it can be checked upstream
			return "", err
		}

		return "", fmt.Errorf("get nelm release %q: %w", releaseName, err)
	}

	if value, ok := releaseGetResult.Release.StorageLabels[labelName]; ok {
		return value, nil
	}

	return "", helm3lib.ErrLabelIsNotFound
}

func (c *NelmClient) WithLogLabels(logLabels map[string]string) {
	c.logger = utils.EnrichLoggerWithLabels(c.logger, logLabels)
}

func (c *NelmClient) WithExtraLabels(labels map[string]string) {
	if labels != nil {
		if c.labels == nil {
			c.labels = make(map[string]string)
		}
		maps.Copy(c.labels, labels)
	}
}

func (c *NelmClient) WithExtraAnnotations(annotations map[string]string) {
	if annotations != nil {
		if c.annotations == nil {
			c.annotations = make(map[string]string)
		}
		maps.Copy(c.annotations, annotations)
	}
}

func (c *NelmClient) WithVirtualChart(virtual bool) {
	c.virtualChart = virtual
}

func (c *NelmClient) WithModulePath(path string) {
	c.modulePath = path
}

// GetAnnotations returns the annotations for testing purposes
func (c *NelmClient) GetAnnotations() map[string]string {
	return c.annotations
}

func (c *NelmClient) LastReleaseStatus(releaseName string) (string, string, error) {
	releaseGetResult, err := c.actions.ReleaseGet(context.TODO(), releaseName, *c.opts.Namespace, action.ReleaseGetOptions{
		KubeContext:          c.opts.KubeContext,
		OutputNoPrint:        true,
		ReleaseStorageDriver: c.opts.HelmDriver,
	})
	if err != nil {
		var releaseNotFoundErr *action.ReleaseNotFoundError
		if errors.As(err, &releaseNotFoundErr) {
			return "0", "", fmt.Errorf("get nelm release %q: %w", releaseName, err)
		}

		return "", "", fmt.Errorf("get nelm release %q: %w", releaseName, err)
	}

	return strconv.FormatInt(int64(releaseGetResult.Release.Revision), 10), releaseGetResult.Release.Status.String(), nil
}

func (c *NelmClient) UpgradeRelease(releaseName string, chart *chart.Chart, valuesPaths []string, setValues []string, releaseLabels map[string]string, namespace string) error {
	logger := c.logger.With(
		slog.String("release_name", releaseName),
		slog.String("chart", chart.Metadata.Name),
		slog.String("namespace", namespace),
	)

	logger.Info("Running nelm upgrade for release")

	// Add client annotations
	extraAnnotations := make(map[string]string)
	if c.annotations != nil {
		maps.Copy(extraAnnotations, c.annotations)
	}

	// Add no-resource-reconciliation annotation to other resources if it exists in the release
	maintenanceLabel, ok := releaseLabels["maintenance.deckhouse.io/no-resource-reconciliation"]
	if ok && maintenanceLabel == "true" {
		extraAnnotations["maintenance.deckhouse.io/no-resource-reconciliation"] = ""
	}

	// First check if release exists
	_, err := c.actions.ReleaseGet(context.Background(), releaseName, namespace, action.ReleaseGetOptions{
		KubeContext:          c.opts.KubeContext,
		OutputNoPrint:        true,
		ReleaseStorageDriver: c.opts.HelmDriver,
	})
	if err != nil {
		logger.Warn("nelm release get has an error", log.Err(err))

		var releaseNotFoundErr *action.ReleaseNotFoundError
		if errors.As(err, &releaseNotFoundErr) {
			logger.Info("Release not found, will create new one")
		} else {
			return fmt.Errorf("get nelm release %q: %w", releaseName, err)
		}
	}

	// Prepare chart options based on whether this is a virtual chart
	opts := action.ReleaseInstallOptions{
		ExtraLabels:          c.labels,
		ExtraAnnotations:     extraAnnotations,
		KubeContext:          c.opts.KubeContext,
		NoInstallCRDs:        true,
		ReleaseHistoryLimit:  int(c.opts.HistoryMax),
		ReleaseLabels:        releaseLabels,
		ReleaseStorageDriver: c.opts.HelmDriver,
		Timeout:              c.opts.Timeout,
		ValuesFilesPaths:     valuesPaths,
		ValuesSets:           setValues,
		ForceAdoption:        true,
		NoPodLogs:            true,
	}

	if c.virtualChart {
		// For virtual charts, use default chart fields and empty chart path
		// The chart object passed to this method already contains filtered files
		logger.Info("NELM: Using virtual chart mode", 
			slog.String("defaultName", chart.Metadata.Name),
			slog.String("defaultVersion", chart.Metadata.Version),
			slog.String("defaultAPIVersion", chart.Metadata.APIVersion),
			slog.String("modulePath", c.modulePath),
			slog.Bool("usePrebuiltChart", true),
			slog.Int("chartTemplatesCount", len(chart.Templates)),
			slog.Int("chartRawCount", len(chart.Raw)))
		opts.Chart = ""
		opts.DefaultChartAPIVersion = chart.Metadata.APIVersion
		opts.DefaultChartName = chart.Metadata.Name
		opts.DefaultChartVersion = chart.Metadata.Version
		
		// IMPORTANT: For virtual charts, we should NOT use modulePath 
		// because NELM might read unfiltered files from it
		// The chart parameter already contains properly filtered files
		
		// Log all important opts fields for debugging
		logger.Debug("NELM virtual chart options", 
			slog.String("opts.Chart", opts.Chart),
			slog.String("opts.DefaultChartName", opts.DefaultChartName),
			slog.String("opts.DefaultChartVersion", opts.DefaultChartVersion),
			slog.String("opts.DefaultChartAPIVersion", opts.DefaultChartAPIVersion))
	} else {
		// For regular charts, use the module path
		logger.Info("NELM: Using regular chart mode", 
			slog.String("chartPath", c.modulePath))
		opts.Chart = c.modulePath
	}

	if err := c.actions.ReleaseInstall(context.TODO(), releaseName, namespace, opts); err != nil {
		return fmt.Errorf("install nelm release %q: %w", releaseName, err)
	}

	logger.Info("Nelm upgrade successful",
		slog.String("release", releaseName),
		slog.String("chart", chart.Metadata.Name),
		slog.String("namespace", namespace))

	return nil
}

func (c *NelmClient) GetReleaseValues(releaseName string) (utils.Values, error) {
	releaseGetResult, err := c.actions.ReleaseGet(context.TODO(), releaseName, *c.opts.Namespace, action.ReleaseGetOptions{
		KubeContext:          c.opts.KubeContext,
		OutputNoPrint:        true,
		ReleaseStorageDriver: c.opts.HelmDriver,
	})
	if err != nil {
		return nil, fmt.Errorf("get nelm release %q: %w", releaseName, err)
	}
	if releaseGetResult == nil || releaseGetResult.Values == nil {
		return nil, fmt.Errorf("no values found for release %q", releaseName)
	}

	valuesBytes, err := yaml.Marshal(releaseGetResult.Values)
	if err != nil {
		return nil, fmt.Errorf("marshal values for release %q: %w", releaseName, err)
	}

	result := make(utils.Values)
	if err := yaml.Unmarshal(valuesBytes, &result); err != nil {
		return nil, fmt.Errorf("unmarshal values for release %q: %w", releaseName, err)
	}

	return result, nil
}

func (c *NelmClient) GetReleaseChecksum(releaseName string) (string, error) {
	logger := c.logger.With(slog.String("release_name", releaseName))

	releaseGetResult, err := c.actions.ReleaseGet(context.TODO(), releaseName, *c.opts.Namespace, action.ReleaseGetOptions{
		KubeContext:          c.opts.KubeContext,
		OutputNoPrint:        true,
		ReleaseStorageDriver: c.opts.HelmDriver,
	})
	if err != nil {
		return "", fmt.Errorf("get nelm release %q: %w", releaseName, err)
	}

	if releaseGetResult.Release != nil {
		if checksum, ok := releaseGetResult.Release.StorageLabels["moduleChecksum"]; ok {
			logger.Debug("using storage labels")
			return checksum, nil
		}
	}

	if recordedChecksum, hasKey := releaseGetResult.Values["_addonOperatorModuleChecksum"]; hasKey {
		if recordedChecksumStr, ok := recordedChecksum.(string); ok {
			logger.Debug("using values")
			return recordedChecksumStr, nil
		}
	}

	logger.Warn("moduleChecksum label not found in nelm release")

	return "", fmt.Errorf("moduleChecksum label not found in nelm release %q", releaseName)
}

func (c *NelmClient) DeleteRelease(releaseName string) error {
	c.logger.Debug("nelm release: execute nelm uninstall", slog.String("release", releaseName))

	if err := c.actions.ReleaseUninstall(context.TODO(), releaseName, *c.opts.Namespace, action.ReleaseUninstallOptions{
		KubeContext:          c.opts.KubeContext,
		ReleaseHistoryLimit:  int(c.opts.HistoryMax),
		ReleaseStorageDriver: c.opts.HelmDriver,
		Timeout:              c.opts.Timeout,
		NoPodLogs:            true,
	}); err != nil {
		return fmt.Errorf("nelm uninstall release %q: %w", releaseName, err)
	}

	c.logger.Debug("nelm release deleted", slog.String("release", releaseName))

	return nil
}

func (c *NelmClient) IsReleaseExists(releaseName string) (bool, error) {
	revision, _, err := c.LastReleaseStatus(releaseName)
	if err == nil {
		return true, nil
	}
	if revision == "0" {
		return false, nil
	}
	return false, err
}

func (c *NelmClient) ListReleasesNames() ([]string, error) {
	releaseListResult, err := c.actions.ReleaseList(context.TODO(), action.ReleaseListOptions{
		KubeContext:          c.opts.KubeContext,
		OutputNoPrint:        true,
		ReleaseStorageDriver: c.opts.HelmDriver,
	})
	if err != nil {
		return nil, fmt.Errorf("list nelm releases: %w", err)
	}

	releaseNames := make([]string, 0)
	for _, release := range releaseListResult.Releases {
		chartName := "unknown"
		if release.Chart != nil {
			chartName = release.Chart.Name
		}
		if release.Name == "" {
			c.logger.Warn("release name is empty, skipped", slog.String("chart", chartName))
			continue
		}

		releaseNames = append(releaseNames, release.Name)
	}

	sort.Strings(releaseNames)

	return releaseNames, nil
}

func (c *NelmClient) Render(releaseName string, chart *chart.Chart, valuesPaths, setValues []string, releaseLabels map[string]string, namespace string, debug bool) (string, error) {
	c.logger.Debug("Render nelm templates for chart ...",
		slog.String("chart", chart.Metadata.Name),
		slog.String("namespace", namespace))

	// Add client annotations
	extraAnnotations := make(map[string]string)
	if c.annotations != nil {
		maps.Copy(extraAnnotations, c.annotations)
	}

	// Add no-resource-reconciliation annotation to other resources if it exists in the release
	maintenanceLabel, ok := releaseLabels["maintenance.deckhouse.io/no-resource-reconciliation"]
	if ok && maintenanceLabel == "true" {
		extraAnnotations["maintenance.deckhouse.io/no-resource-reconciliation"] = ""
	}

	// Prepare chart render options based on whether this is a virtual chart
	renderOpts := action.ChartRenderOptions{
		OutputFilePath:       "/dev/null", // No output file, we want to return the manifest as a string
		ExtraLabels:          c.labels,
		ExtraAnnotations:     extraAnnotations,
		KubeContext:          c.opts.KubeContext,
		ReleaseName:          releaseName,
		ReleaseNamespace:     namespace,
		ReleaseStorageDriver: c.opts.HelmDriver,
		Remote:               true,
		ValuesFilesPaths:     valuesPaths,
		ValuesSets:           setValues,
		ForceAdoption:        true,
	}

	if c.virtualChart {
		// For virtual charts, use default chart fields and empty chart path
		// The chart object passed to this method already contains filtered files
		c.logger.Info("NELM Render: Using virtual chart mode", 
			slog.String("defaultName", chart.Metadata.Name),
			slog.String("defaultVersion", chart.Metadata.Version),
			slog.String("defaultAPIVersion", chart.Metadata.APIVersion),
			slog.String("modulePath", c.modulePath),
			slog.Bool("usePrebuiltChart", true))
		renderOpts.Chart = ""
		renderOpts.DefaultChartAPIVersion = chart.Metadata.APIVersion
		renderOpts.DefaultChartName = chart.Metadata.Name
		renderOpts.DefaultChartVersion = chart.Metadata.Version
		
		// IMPORTANT: For virtual charts, we should NOT use modulePath 
		// because NELM might read unfiltered files from it
		// The chart parameter already contains properly filtered files
	} else {
		// For regular charts, use the module path
		c.logger.Info("NELM Render: Using regular chart mode", 
			slog.String("chartPath", c.modulePath))
		renderOpts.Chart = c.modulePath
	}

	chartRenderResult, err := c.actions.ChartRender(context.TODO(), renderOpts)
	if err != nil {
		if !debug {
			return "", fmt.Errorf("render nelm chart %q: %w\n\nUse --debug flag to render out invalid YAML", chart.Metadata.Name, err)
		}
		return "", fmt.Errorf("render nelm chart %q: %w", chart.Metadata.Name, err)
	}

	c.logger.Info("Render nelm templates for chart was successful", slog.String("chart", chart.Metadata.Name))

	var result strings.Builder
	for _, resource := range chartRenderResult.Resources {
		b, err := yaml.Marshal(resource)
		if err != nil {
			return "", fmt.Errorf("marshal resource: %w", err)
		}
		if result.Len() > 0 {
			result.WriteString("---\n")
		}
		result.Write(b)
	}

	return result.String(), nil
}

func buildConfigFlagsFromEnv(ns *string, env *cli.EnvSettings) *genericclioptions.ConfigFlags {
	flags := genericclioptions.NewConfigFlags(true)

	flags.Namespace = ns
	flags.Context = &env.KubeContext
	flags.BearerToken = &env.KubeToken
	flags.APIServer = &env.KubeAPIServer
	flags.CAFile = &env.KubeCaFile
	flags.KubeConfig = &env.KubeConfig
	flags.Impersonate = &env.KubeAsUser
	flags.Insecure = &env.KubeInsecureSkipTLSVerify
	flags.TLSServerName = &env.KubeTLSServerName
	flags.ImpersonateGroup = &env.KubeAsGroups
	flags.WrapConfigFn = func(config *rest.Config) *rest.Config {
		config.Burst = env.BurstLimit
		return config
	}

	return flags
}
