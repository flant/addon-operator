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
	"helm.sh/helm/v3/pkg/cli"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/yaml"

	"github.com/flant/addon-operator/pkg/helm/client"
	"github.com/flant/addon-operator/pkg/utils"
	nelmlog "github.com/werf/nelm/pkg/log"
)

var _ client.HelmClient = (*NelmClient)(nil)

type CommonOptions struct {
	genericclioptions.ConfigFlags

	Namespace   string
	HistoryMax  int32
	Timeout     time.Duration
	HelmDriver  string
	KubeContext string
}

type NelmActions interface {
	ReleaseGet(ctx context.Context, name, namespace string, opts action.ReleaseGetOptions) (*action.ReleaseGetResultV1, error)
	ReleaseInstall(ctx context.Context, name, namespace string, opts action.ReleaseInstallOptions) error
	ReleaseUninstall(ctx context.Context, name, namespace string, opts action.LegacyReleaseUninstallOptions) error
	ReleaseList(ctx context.Context, opts action.ReleaseListOptions) (*action.ReleaseListResultV1, error)
	ChartRender(ctx context.Context, opts action.ChartRenderOptions) (*action.ChartRenderResultV1, error)
	ReleasePlanInstall(ctx context.Context, name, namespace string, opts action.ReleasePlanInstallOptions) error
}

type DefaultNelmActions struct{}

func (d *DefaultNelmActions) ReleaseGet(ctx context.Context, name, namespace string, opts action.ReleaseGetOptions) (*action.ReleaseGetResultV1, error) {
	return action.ReleaseGet(ctx, name, namespace, opts)
}

func (d *DefaultNelmActions) ReleaseInstall(ctx context.Context, name, namespace string, opts action.ReleaseInstallOptions) error {
	return action.ReleaseInstall(ctx, name, namespace, opts)
}

func (d *DefaultNelmActions) ReleaseUninstall(ctx context.Context, name, namespace string, opts action.LegacyReleaseUninstallOptions) error {
	return action.LegacyReleaseUninstall(ctx, name, namespace, opts)
}

func (d *DefaultNelmActions) ReleaseList(ctx context.Context, opts action.ReleaseListOptions) (*action.ReleaseListResultV1, error) {
	return action.ReleaseList(ctx, opts)
}

func (d *DefaultNelmActions) ChartRender(ctx context.Context, opts action.ChartRenderOptions) (*action.ChartRenderResultV1, error) {
	return action.ChartRender(ctx, opts)
}

func (d *DefaultNelmActions) ReleasePlanInstall(ctx context.Context, name, namespace string, opts action.ReleasePlanInstallOptions) error {
	return action.ReleasePlanInstall(ctx, name, namespace, opts)
}

// nelmBuildConfigFlagsFromEnv is a local copy of helm3lib.buildConfigFlagsFromEnv
func nelmBuildConfigFlagsFromEnv(ns *string, env *cli.EnvSettings) *genericclioptions.ConfigFlags {
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

func NewNelmClient(opts *CommonOptions, logger *log.Logger, labels map[string]string) *NelmClient {
	nelmlog.Default = NewNelmLogger(logger.Named("global"))

	if opts == nil {
		opts = &CommonOptions{}
	}

	// Set ConfigFlags from local copy
	opts.ConfigFlags = *nelmBuildConfigFlagsFromEnv(&opts.Namespace, cli.New())

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

	// Ensure logger is not nil
	if logger == nil {
		panic("logger is required in NewNelmClient")
	}

	return &NelmClient{
		logger:  logger.Named("local"),
		labels:  clientLabels,
		opts:    opts,
		actions: &DefaultNelmActions{},
	}
}

type NelmClient struct {
	logger *log.Logger
	labels map[string]string

	opts    *CommonOptions
	actions NelmActions
}

// GetReleaseLabels returns a specific label value from the release.
func (c *NelmClient) GetReleaseLabels(releaseName, labelName string) (string, error) {
	logger := c.logger.With(
		slog.String("release_name", releaseName),
		slog.String("label_name", releaseName),
	)

	logger.Info("get release labels")

	result, err := c.actions.ReleaseGet(context.Background(), releaseName, c.opts.Namespace, action.ReleaseGetOptions{
		KubeContext:          c.opts.KubeContext,
		OutputNoPrint:        true,
		ReleaseStorageDriver: c.opts.HelmDriver,
	})
	if err != nil {
		logger.Error("get nelm release", log.Err(err))
		return "", fmt.Errorf("get nelm release %q: %w", releaseName, err)
	}

	if result.Release == nil {
		logger.Error("nelm release is not found", log.Err(err))
		return "", fmt.Errorf("release %s not found", releaseName)
	}

	if value, ok := result.Release.StorageLabels[labelName]; ok {
		return value, nil
	}

	logger.Error("label is not found")

	return "", client.ErrLabelIsNotFound
}

func (c *NelmClient) WithLogLabels(logLabels map[string]string) {
	if logLabels != nil {
		c.logger = c.logger.With(mapToSlogArgs(logLabels)...)
	}
}

func (c *NelmClient) WithExtraLabels(labels map[string]string) {
	if labels != nil {
		if c.labels == nil {
			c.labels = make(map[string]string)
		}
		maps.Copy(c.labels, labels)
	}
}

func (c *NelmClient) LastReleaseStatus(releaseName string) (string, string, error) {
	logger := c.logger.With(
		slog.String("release_name", releaseName),
	)

	logger.Info("get last release status")

	result, err := c.actions.ReleaseGet(context.Background(), releaseName, c.opts.Namespace, action.ReleaseGetOptions{
		KubeContext:          c.opts.KubeContext,
		OutputNoPrint:        true,
		ReleaseStorageDriver: c.opts.HelmDriver,
	})
	if err != nil {
		logger.Error("get nelm release", log.Err(err))
		return "", "", fmt.Errorf("get nelm release: %w", err)
	}

	if result.Release == nil {
		logger.Error("nelm release is not found", log.Err(err))
		return "", "", fmt.Errorf("nelm release %s not found", releaseName)
	}

	// Convert helmrelease.Status to string and return revision
	return strconv.FormatInt(int64(result.Release.Revision), 10), string(result.Release.Status), nil
}

func (c *NelmClient) UpgradeRelease(releaseName, chartName string, valuesPaths []string, setValues []string, labels map[string]string, namespace string) error {
	logger := c.logger.With(
		slog.String("release_name", releaseName),
		slog.String("chart", chartName),
		slog.String("namespace", namespace),
	)

	logger.Info("Running nelm upgrade for release")

	// Prepare annotations with correct moduleChecksum from labels
	extraAnnotations := make(map[string]string)
	if checksum, exists := labels["moduleChecksum"]; exists {
		extraAnnotations["moduleChecksum"] = checksum
		extraAnnotations["meta.helm.sh/release-name"] = releaseName
		extraAnnotations["meta.helm.sh/release-namespace"] = namespace
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
			logger.Warn("nelm release get has not found error", log.Err(err))

			// If release doesn't exist, do install
			installOptions := action.ReleaseInstallOptions{
				Chart:                chartName,
				ExtraLabels:          c.labels,
				KubeContext:          c.opts.KubeContext,
				NoInstallCRDs:        true,
				ReleaseHistoryLimit:  int(c.opts.HistoryMax),
				ReleaseLabels:        labels,
				ReleaseStorageDriver: c.opts.HelmDriver,
				Timeout:              c.opts.Timeout,
				ValuesFilesPaths:     valuesPaths,
				ValuesSets:           setValues,
				ForceAdoption:        true,
			}

			if len(extraAnnotations) > 0 {
				installOptions.ExtraAnnotations = extraAnnotations
			}

			err := c.actions.ReleaseInstall(context.Background(), releaseName, namespace, installOptions)
			if err != nil {
				logger.Error("nelm release install", log.Err(err))
				return fmt.Errorf("nelm release install: %w", err)
			}

			return nil
		}

		logger.Error("get nelm release", log.Err(err))
		return fmt.Errorf("get nelm release %q: %w", releaseName, err)
	}

	// If release exists, do upgrade
	planInstallOptions := action.ReleasePlanInstallOptions{
		Chart:                chartName,
		ExtraLabels:          labels,
		KubeContext:          c.opts.KubeContext,
		ReleaseStorageDriver: c.opts.HelmDriver,
		Timeout:              c.opts.Timeout,
		ValuesFilesPaths:     valuesPaths,
		ValuesSets:           setValues,
		ForceAdoption:        true,
	}

	if len(extraAnnotations) > 0 {
		planInstallOptions.ExtraAnnotations = extraAnnotations
	}

	if err := c.actions.ReleasePlanInstall(context.Background(), releaseName, namespace, planInstallOptions); err != nil {
		logger.Error("upgrade nelm release", log.Err(err))
		return fmt.Errorf("upgrade nelm release: %w", err)
	}

	logger.Info("nelm upgrade successful")

	return nil
}

// Render renders the chart templates with provided values and returns the manifest as a string.
func (c *NelmClient) Render(_, chartName string, valuesPaths, setValues []string, namespace string, debug bool) (string, error) {
	logger := c.logger.With(
		slog.String("chart", chartName),
		slog.String("namespace", namespace),
	)

	logger.Debug("Render nelm templates for chart ...")

	// Prepare ChartRenderOptions with merged values
	opts := action.ChartRenderOptions{
		OutputFilePath:   "/dev/null", // No output file, we want to return the manifest as a string
		Chart:            chartName,
		KubeContext:      c.opts.KubeContext,
		ValuesFilesPaths: valuesPaths,
		ValuesSets:       setValues,
		ForceAdoption:    true,
	}

	render := func() (*action.ChartRenderResultV1, error) {
		return c.actions.ChartRender(context.Background(), opts)
	}

	result, err := render()
	if err != nil {
		// Try one more time (like helm3lib does reinit)
		logger.Warn("First nelm render attempt failed, trying again",
			slog.String("error", err.Error()))
		result, err = render()
	}

	if err != nil {
		if !debug {
			return "", fmt.Errorf("nelm render failed: %w", err)
		}
		if result == nil {
			return "", err
		}
	}

	// Collect YAML from Resources and CRDs fields
	var builder strings.Builder
	if result != nil {
		if len(result.Resources) > 0 {
			for _, res := range result.Resources {
				b, marshalErr := yaml.Marshal(res)
				if marshalErr == nil {
					builder.Write(b)
					builder.WriteString("---\n")
				} else {
					logger.Warn("Failed to marshal resource",
						slog.String("error", marshalErr.Error()))
				}
			}
		}
		if len(result.CRDs) > 0 {
			for _, crd := range result.CRDs {
				b, marshalErr := yaml.Marshal(crd)
				if marshalErr == nil {
					builder.Write(b)
					builder.WriteString("---\n")
				} else {
					logger.Warn("Failed to marshal CRD",
						slog.String("error", marshalErr.Error()))
				}
			}
		}
	}

	manifestStr := builder.String()
	if err != nil && debug {
		manifestStr += fmt.Sprintf("\n\n\n%v", err)
	}
	if manifestStr != "" {
		return manifestStr, nil
	}

	return "", fmt.Errorf("no manifest returned by nelm render")
}

// DeleteRelease deletes the specified release.
func (c *NelmClient) DeleteRelease(releaseName string) error {
	logger := c.logger.With(slog.String("release", releaseName))

	logger.Debug("nelm release: execute nelm uninstall")

	if err := c.actions.ReleaseUninstall(context.Background(), releaseName, c.opts.Namespace, action.LegacyReleaseUninstallOptions{
		KubeContext:          c.opts.KubeContext,
		ReleaseHistoryLimit:  int(c.opts.HistoryMax),
		ReleaseStorageDriver: c.opts.HelmDriver,
		Timeout:              c.opts.Timeout,
	}); err != nil {
		logger.Error("nelm release uninstall", log.Err(err))
		return fmt.Errorf("nelm uninstall release %q: %w", releaseName, err)
	}

	logger.Debug("nelm release deleted")

	return nil
}

// ListReleasesNames returns a sorted list of release names.
func (c *NelmClient) ListReleasesNames() ([]string, error) {
	releaseListResult, err := c.actions.ReleaseList(context.Background(), action.ReleaseListOptions{
		KubeContext:          c.opts.KubeContext,
		OutputNoPrint:        true,
		ReleaseStorageDriver: c.opts.HelmDriver,
	})
	if err != nil {
		return nil, fmt.Errorf("list nelm releases: %w", err)
	}

	releaseNames := make([]string, 0)
	for _, release := range releaseListResult.Releases {
		if release.Name != "" {
			releaseNames = append(releaseNames, release.Name)
		}
	}

	sort.Strings(releaseNames)
	return releaseNames, nil
}

// GetReleaseValues returns the values of the specified release as utils.Values.
func (c *NelmClient) GetReleaseValues(releaseName string) (utils.Values, error) {
	releaseGetResult, err := c.actions.ReleaseGet(context.Background(), releaseName, c.opts.Namespace, action.ReleaseGetOptions{
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

// GetReleaseChecksum returns the checksum label of the release. If not found, fallback to values.
func (c *NelmClient) GetReleaseChecksum(releaseName string) (string, error) {
	checksum, err := c.GetReleaseLabels(releaseName, "moduleChecksum")
	if err == nil && checksum != "" {
		return checksum, nil
	}

	// fallback: try to get from values
	releaseValues, errValues := c.GetReleaseValues(releaseName)
	if errValues != nil {
		return "", fmt.Errorf("get moduleChecksum: label and values not found: %w", err)
	}
	if recordedChecksum, hasKey := releaseValues["_addonOperatorModuleChecksum"]; hasKey {
		if recordedChecksumStr, ok := recordedChecksum.(string); ok {
			return recordedChecksumStr, nil
		}
	}

	return "", fmt.Errorf("moduleChecksum not found in release %s", releaseName)
}

// IsReleaseExists checks if the release exists by trying to get its status.
func (c *NelmClient) IsReleaseExists(releaseName string) (bool, error) {
	revision, _, err := c.LastReleaseStatus(releaseName)
	if err == nil && revision != "" {
		return true, nil
	}

	var releaseNotFoundErr *action.ReleaseNotFoundError
	if errors.As(err, &releaseNotFoundErr) {
		return false, nil
	}

	return false, err
}

// mapToSlogArgs converts a map[string]string to a slice of key-value pairs for slog.With.
func mapToSlogArgs(m map[string]string) []any {
	args := make([]any, 0, len(m)*2)
	for k, v := range m {
		args = append(args, k, v)
	}
	return args
}

// ListReleases retrieves all Helm releases regardless of their state.
func (c *NelmClient) ListReleases() ([]*action.ReleaseListResultRelease, error) {
	l, err := action.ReleaseList(context.TODO(), action.ReleaseListOptions{})
	if err != nil {
		return nil, fmt.Errorf("nelm list failed: %w", err)
	}

	return l.Releases, nil
}
