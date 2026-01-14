package nelm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/werf/nelm/pkg/action"
	"github.com/werf/nelm/pkg/common"
	"github.com/werf/nelm/pkg/featgate"
	nelmLog "github.com/werf/nelm/pkg/log"
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
	ChartRender(ctx context.Context, opts action.ChartRenderOptions) (*action.ChartRenderResultV2, error)
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

func (d *DefaultNelmActions) ChartRender(ctx context.Context, opts action.ChartRenderOptions) (*action.ChartRenderResultV2, error) {
	return action.ChartRender(ctx, opts)
}

// SafeNelmActions wraps NelmActions and provides panic recovery for all action calls.
type SafeNelmActions struct {
	wrapped NelmActions
	logger  *log.Logger
}

//nolint:nonamedreturns // named returns required for defer/recover to modify return values
func (s *SafeNelmActions) ReleaseGet(ctx context.Context, name, namespace string, opts action.ReleaseGetOptions) (result *action.ReleaseGetResultV1, err error) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("panic in ReleaseGet",
				slog.Any("panic", r),
				slog.String("release", name),
				slog.String("namespace", namespace),
				slog.Int("revision", opts.Revision),
				slog.String("stack", string(debug.Stack())),
			)
			err = fmt.Errorf("panic in ReleaseGet: %v", r)
		}
	}()
	return s.wrapped.ReleaseGet(ctx, name, namespace, opts)
}

func (s *SafeNelmActions) ReleaseInstall(ctx context.Context, name, namespace string, opts action.ReleaseInstallOptions) (err error) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("panic in ReleaseInstall",
				slog.Any("panic", r),
				slog.String("release", name),
				slog.String("namespace", namespace),
				slog.String("chart", opts.Chart),
				slog.String("default_chart_name", opts.DefaultChartName),
				slog.Bool("force_adoption", opts.ForceAdoption),
				slog.Bool("auto_rollback", opts.AutoRollback),
				slog.Int("values_files_count", len(opts.ValuesFiles)),
				slog.Int("extra_labels_count", len(opts.ExtraLabels)),
				slog.String("stack", string(debug.Stack())),
			)
			err = fmt.Errorf("panic in ReleaseInstall: %v", r)
		}
	}()
	return s.wrapped.ReleaseInstall(ctx, name, namespace, opts)
}

func (s *SafeNelmActions) ReleaseUninstall(ctx context.Context, name, namespace string, opts action.ReleaseUninstallOptions) (err error) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("panic in ReleaseUninstall",
				slog.Any("panic", r),
				slog.String("release", name),
				slog.String("namespace", namespace),
				slog.String("delete_propagation", opts.DefaultDeletePropagation),
				slog.Bool("delete_release_namespace", opts.DeleteReleaseNamespace),
				slog.String("stack", string(debug.Stack())),
			)
			err = fmt.Errorf("panic in ReleaseUninstall: %v", r)
		}
	}()
	return s.wrapped.ReleaseUninstall(ctx, name, namespace, opts)
}

//nolint:nonamedreturns // named returns required for defer/recover to modify return values
func (s *SafeNelmActions) ReleaseList(ctx context.Context, opts action.ReleaseListOptions) (result *action.ReleaseListResultV1, err error) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("panic in ReleaseList",
				slog.Any("panic", r),
				slog.String("namespace", opts.ReleaseNamespace),
				slog.String("stack", string(debug.Stack())),
			)
			err = fmt.Errorf("panic in ReleaseList: %v", r)
		}
	}()
	return s.wrapped.ReleaseList(ctx, opts)
}

//nolint:nonamedreturns // named returns required for defer/recover to modify return values
func (s *SafeNelmActions) ChartRender(ctx context.Context, opts action.ChartRenderOptions) (result *action.ChartRenderResultV2, err error) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("panic in ChartRender",
				slog.Any("panic", r),
				slog.String("chart", opts.Chart),
				slog.String("release", opts.ReleaseName),
				slog.String("namespace", opts.ReleaseNamespace),
				slog.Bool("remote", opts.Remote),
				slog.Int("values_files_count", len(opts.ValuesFiles)),
				slog.String("stack", string(debug.Stack())),
			)
			err = fmt.Errorf("panic in ChartRender: %v", r)
		}
	}()
	return s.wrapped.ChartRender(ctx, opts)
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

	featgate.FeatGateCleanNullFields.Enable()

	nelmLogger := logger.With("operator.component", "nelm")

	return &NelmClient{
		logger: nelmLogger,
		labels: clientLabels,
		opts:   opts,
		actions: &SafeNelmActions{
			wrapped: &DefaultNelmActions{},
			logger:  nelmLogger,
		},
	}
}

type NelmClient struct {
	logger      *log.Logger
	labels      map[string]string
	annotations map[string]string

	opts    *CommonOptions
	actions NelmActions
}

// GetReleaseLabels returns a specific label value from the release.
func (c *NelmClient) GetReleaseLabels(releaseName, labelName string) (string, error) {
	releaseGetResult, err := c.actions.ReleaseGet(context.TODO(), releaseName, *c.opts.Namespace, action.ReleaseGetOptions{
		KubeConnectionOptions: common.KubeConnectionOptions{
			KubeContextCurrent: c.opts.KubeContext,
		},
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

// GetAnnotations returns the annotations for testing purposes
func (c *NelmClient) GetAnnotations() map[string]string {
	return c.annotations
}

func (c *NelmClient) LastReleaseStatus(releaseName string) (string, string, error) {
	releaseGetResult, err := c.actions.ReleaseGet(context.TODO(), releaseName, *c.opts.Namespace, action.ReleaseGetOptions{
		KubeConnectionOptions: common.KubeConnectionOptions{
			KubeContextCurrent: c.opts.KubeContext,
		},
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

func (c *NelmClient) UpgradeRelease(releaseName, modulePath string, valuesPaths []string, setValues []string, releaseLabels map[string]string, namespace string) error {
	logger := c.logger.With(
		slog.String("release_name", releaseName),
		slog.String("chart", modulePath),
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
		KubeConnectionOptions: common.KubeConnectionOptions{
			KubeContextCurrent: c.opts.KubeContext,
		},
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

	if err := c.actions.ReleaseInstall(context.TODO(), releaseName, namespace, action.ReleaseInstallOptions{
		KubeConnectionOptions: common.KubeConnectionOptions{
			KubeContextCurrent: c.opts.KubeContext,
		},
		ValuesOptions: common.ValuesOptions{
			ValuesFiles: valuesPaths,
			ValuesSet:   setValues,
		},
		TrackingOptions: common.TrackingOptions{
			NoPodLogs:                    true,
			NoFinalTracking:              true,
			LegacyHelmCompatibleTracking: true,
		},
		Chart:                    modulePath,
		DefaultChartName:         releaseName,
		DefaultChartVersion:      "0.2.0",
		DefaultChartAPIVersion:   "v2",
		DefaultDeletePropagation: "Background",
		ExtraLabels:              c.labels,
		ExtraAnnotations:         extraAnnotations,
		NoInstallStandaloneCRDs:  true,
		ReleaseHistoryLimit:      int(c.opts.HistoryMax),
		ReleaseLabels:            releaseLabels,
		ReleaseStorageDriver:     c.opts.HelmDriver,
		Timeout:                  c.opts.Timeout,
		ForceAdoption:            true,
	}); err != nil {
		return fmt.Errorf("install nelm release %q: %w", releaseName, err)
	}

	logger.Info("Nelm upgrade successful",
		slog.String("release", releaseName),
		slog.String("chart", modulePath),
		slog.String("namespace", namespace))

	return nil
}

func (c *NelmClient) GetReleaseValues(releaseName string) (utils.Values, error) {
	releaseGetResult, err := c.actions.ReleaseGet(context.TODO(), releaseName, *c.opts.Namespace, action.ReleaseGetOptions{
		KubeConnectionOptions: common.KubeConnectionOptions{
			KubeContextCurrent: c.opts.KubeContext,
		},
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
		KubeConnectionOptions: common.KubeConnectionOptions{
			KubeContextCurrent: c.opts.KubeContext,
		},
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
		KubeConnectionOptions: common.KubeConnectionOptions{
			KubeContextCurrent: c.opts.KubeContext,
		},
		TrackingOptions: common.TrackingOptions{
			NoPodLogs:                    true,
			NoFinalTracking:              true,
			LegacyHelmCompatibleTracking: true,
		},
		ReleaseHistoryLimit:      int(c.opts.HistoryMax),
		DefaultDeletePropagation: "Background",
		ReleaseStorageDriver:     c.opts.HelmDriver,
		Timeout:                  c.opts.Timeout,
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
		KubeConnectionOptions: common.KubeConnectionOptions{
			KubeContextCurrent: c.opts.KubeContext,
		},
		OutputNoPrint:        true,
		ReleaseNamespace:     *c.opts.Namespace,
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

func (c *NelmClient) Render(releaseName, modulePath string, valuesPaths, setValues []string, releaseLabels map[string]string, namespace string, debug bool) (string, error) {
	c.logger.Debug("Render nelm templates for chart ...",
		slog.String("chart", modulePath),
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

	chartRenderResult, err := c.actions.ChartRender(context.TODO(), action.ChartRenderOptions{
		KubeConnectionOptions: common.KubeConnectionOptions{
			KubeContextCurrent: c.opts.KubeContext,
		},
		ValuesOptions: common.ValuesOptions{
			ValuesFiles: valuesPaths,
			ValuesSet:   setValues,
		},
		OutputFilePath:         "/dev/null", // No output file, we want to return the manifest as a string
		Chart:                  modulePath,
		DefaultChartName:       releaseName,
		DefaultChartVersion:    "0.2.0",
		DefaultChartAPIVersion: "v2",
		ExtraLabels:            c.labels,
		ExtraAnnotations:       extraAnnotations,
		ReleaseName:            releaseName,
		ReleaseNamespace:       namespace,
		ReleaseStorageDriver:   c.opts.HelmDriver,
		Remote:                 true,
		ForceAdoption:          true,
	})
	if err != nil {
		if !debug {
			return "", fmt.Errorf("render nelm chart %q: %w\n\nUse --debug flag to render out invalid YAML", modulePath, err)
		}
		return "", fmt.Errorf("render nelm chart %q: %w", modulePath, err)
	}

	c.logger.Info("Render nelm templates for chart was successful", slog.String("chart", modulePath))

	var result strings.Builder
	for _, resource := range chartRenderResult.Resources {
		b, err := yaml.Marshal(resource.Unstruct)
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
