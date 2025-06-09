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
	ReleaseUninstall(ctx context.Context, name, namespace string, opts action.ReleaseUninstallOptions) error
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

func (d *DefaultNelmActions) ReleaseUninstall(ctx context.Context, name, namespace string, opts action.ReleaseUninstallOptions) error {
	return action.ReleaseUninstall(ctx, name, namespace, opts)
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

func NewNelmClient(opts *CommonOptions, logger *log.Logger, labels map[string]string) *NelmClient {
	if opts == nil {
		opts = &CommonOptions{}
	}

	opts = applyCommonOptionsDefaults(opts,
		buildConfigFlagsFromEnv(opts.Namespace, cli.New()))

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
	logger *log.Logger
	labels map[string]string

	opts    *CommonOptions
	actions NelmActions
}

// GetReleaseLabels returns a specific label value from the release.
func (c *NelmClient) GetReleaseLabels(releaseName, labelName string) (string, error) {
	result, err := c.actions.ReleaseGet(context.Background(), releaseName, c.opts.Namespace, action.ReleaseGetOptions{
		KubeContext:          c.opts.KubeContext,
		OutputNoPrint:        true,
		ReleaseStorageDriver: c.opts.HelmDriver,
	})
	if err != nil {
		return "", fmt.Errorf("get nelm release %q: %w", releaseName, err)
	}

	if result.Release == nil {
		return "", fmt.Errorf("release %s not found", releaseName)
	}

	// Use Annotations instead of Labels
	if value, ok := result.Release.Annotations[labelName]; ok {
		return value, nil
	}

	return "", fmt.Errorf("label %s not found", labelName)
}

func (c *NelmClient) WithLogLabels(logLabels map[string]string) {
	c.logger = c.logger.With(logLabels)
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
	result, err := c.actions.ReleaseGet(context.Background(), releaseName, c.opts.Namespace, action.ReleaseGetOptions{
		KubeContext:          c.opts.KubeContext,
		OutputNoPrint:        true,
		ReleaseStorageDriver: c.opts.HelmDriver,
	})
	if err != nil {
		return "", "", fmt.Errorf("get release: %w", err)
	}

	if result.Release == nil {
		return "", "", fmt.Errorf("release %s not found", releaseName)
	}

	// Convert helmrelease.Status to string and return revision
	return strconv.FormatInt(int64(result.Release.Revision), 10), string(result.Release.Status), nil
}

func (c *NelmClient) UpgradeRelease(releaseName, chartName string, valuesPaths []string, setValues []string, labels map[string]string, namespace string) error {
	c.logger.Info("Running nelm upgrade for release",
		slog.String("release", releaseName),
		slog.String("chart", chartName),
		slog.String("namespace", namespace),
	)

	// First check if release exists
	_, err := c.actions.ReleaseGet(context.TODO(), releaseName, namespace, action.ReleaseGetOptions{
		KubeContext:          c.opts.KubeContext,
		OutputNoPrint:        true,
		ReleaseStorageDriver: c.opts.HelmDriver,
	})
	if err != nil {
		var releaseNotFoundErr *action.ReleaseNotFoundError
		if errors.As(err, &releaseNotFoundErr) {
			// If release doesn't exist, do install
			return c.actions.ReleaseInstall(context.TODO(), releaseName, namespace, action.ReleaseInstallOptions{
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
			})
		}
		return fmt.Errorf("get nelm release %q: %w", releaseName, err)
	}

	// If release exists, do upgrade
	if err := c.actions.ReleasePlanInstall(context.TODO(), releaseName, namespace, action.ReleasePlanInstallOptions{
		Chart:                chartName,
		ExtraLabels:          labels,
		ExtraAnnotations:     map[string]string{"moduleChecksum": fmt.Sprintf("%v", valuesPaths)},
		KubeContext:          c.opts.KubeContext,
		ReleaseStorageDriver: c.opts.HelmDriver,
		Timeout:              c.opts.Timeout,
		ValuesFilesPaths:     valuesPaths,
		ValuesSets:           setValues,
	}); err != nil {
		return fmt.Errorf("upgrade release: %w", err)
	}

	c.logger.Info("Nelm upgrade successful",
		slog.String("release", releaseName),
		slog.String("chart", chartName),
		slog.String("namespace", namespace))

	return nil
}

func (c *NelmClient) GetReleaseValues(releaseName string) (utils.Values, error) {
	releaseGetResult, err := c.actions.ReleaseGet(context.TODO(), releaseName, c.opts.Namespace, action.ReleaseGetOptions{
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
	return c.GetReleaseLabels(releaseName, "moduleChecksum")
}

func (c *NelmClient) DeleteRelease(releaseName string) error {
	c.logger.Debug("nelm release: execute nelm uninstall", slog.String("release", releaseName))

	if err := c.actions.ReleaseUninstall(context.TODO(), releaseName, c.opts.Namespace, action.ReleaseUninstallOptions{
		KubeContext:          c.opts.KubeContext,
		ReleaseHistoryLimit:  int(c.opts.HistoryMax),
		ReleaseStorageDriver: c.opts.HelmDriver,
		Timeout:              c.opts.Timeout,
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
	if revision == "" {
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

func (c *NelmClient) Render(releaseName, chartName string, _, _ []string, namespace string, debug bool) (string, error) {
	c.logger.Debug("Render nelm templates for chart ...",
		slog.String("chart", chartName),
		slog.String("namespace", namespace))

	chartRenderResult, err := c.actions.ChartRender(context.TODO(), action.ChartRenderOptions{
		ExtraLabels:          c.labels,
		KubeContext:          c.opts.KubeContext,
		OutputFilePath:       "/dev/null",
		OutputNoPrint:        true,
		ReleaseName:          releaseName,
		ReleaseNamespace:     namespace,
		ReleaseStorageDriver: c.opts.HelmDriver,
		Remote:               true,
	})
	if err != nil {
		if !debug {
			return "", fmt.Errorf("render nelm chart %q: %w\n\nUse --debug flag to render out invalid YAML", chartName, err)
		}
		return "", fmt.Errorf("render nelm chart %q: %w", chartName, err)
	}

	c.logger.Info("Render nelm templates for chart was successful", slog.String("chart", chartName))

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

func buildConfigFlagsFromEnv(ns string, env *cli.EnvSettings) *genericclioptions.ConfigFlags {
	flags := genericclioptions.NewConfigFlags(true)

	flags.Namespace = &ns
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

func applyCommonOptionsDefaults(opts *CommonOptions, getter *genericclioptions.ConfigFlags) *CommonOptions {
	if opts == nil {
		opts = &CommonOptions{}
	}

	if getter == nil {
		return opts
	}

	if opts.Timeout == 0 && getter.Timeout != nil {
		duration, _ := time.ParseDuration(*getter.Timeout)
		opts.Timeout = duration
	}
	if opts.KubeContext == "" && getter.Context != nil {
		opts.KubeContext = *getter.Context
	}
	if opts.Namespace == "" && getter.Namespace != nil {
		opts.Namespace = *getter.Namespace
	}
	if opts.BearerToken == nil && getter.BearerToken != nil {
		opts.BearerToken = getter.BearerToken
	}
	if opts.APIServer == nil && getter.APIServer != nil {
		opts.APIServer = getter.APIServer
	}
	if opts.CAFile == nil && getter.CAFile != nil {
		opts.CAFile = getter.CAFile
	}
	if opts.KubeConfig == nil && getter.KubeConfig != nil {
		opts.KubeConfig = getter.KubeConfig
	}
	if opts.Impersonate == nil && getter.Impersonate != nil {
		opts.Impersonate = getter.Impersonate
	}
	if opts.Insecure == nil && getter.Insecure != nil {
		opts.Insecure = getter.Insecure
	}
	if opts.TLSServerName == nil && getter.TLSServerName != nil {
		opts.TLSServerName = getter.TLSServerName
	}
	if opts.ImpersonateGroup == nil && getter.ImpersonateGroup != nil {
		opts.ImpersonateGroup = getter.ImpersonateGroup
	}
	if opts.WrapConfigFn == nil && getter.WrapConfigFn != nil {
		opts.WrapConfigFn = getter.WrapConfigFn
	}
	return opts
}
