package nelm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/gookit/color"
	"helm.sh/helm/v3/pkg/cli"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/yaml"

	"github.com/flant/addon-operator/pkg/helm/client"
	"github.com/flant/addon-operator/pkg/utils"

	"github.com/werf/nelm/pkg/action"
	"github.com/werf/nelm/pkg/log"
)

var (
	_             client.HelmClient = &NelmClient{}
	commonOptions *CommonOptions
)

func Init(opts *CommonOptions, logger *NelmLogger) error {
	log.Default = logger
	color.Disable()

	getter := buildConfigFlagsFromEnv(&commonOptions.Namespace, cli.New())

	// FIXME(addon-operator): add all other options from getter in the same manner
	if opts.KubeContext == "" && getter.Context != nil {
		opts.KubeContext = *getter.Context
	}

	if opts.HelmDriver == "" {
		opts.HelmDriver = os.Getenv("HELM_DRIVER")
	}

	commonOptions = opts

	versionResult, err := action.Version(context.TODO(), action.VersionOptions{
		OutputNoPrint: true,
	})
	if err != nil {
		return fmt.Errorf("get nelm version: %w", err)
	}

	logger.Info(context.TODO(), "Nelm version", slog.String("version", versionResult.FullVersion))

	return nil
}

type CommonOptions struct {
	Namespace   string
	HistoryMax  int32
	Timeout     time.Duration
	HelmDriver  string
	KubeContext string
	// FIXME(addon-operator): add all other options from *genericclioptions.ConfigFlags
}

func NewNelmClient(logger *NelmLogger, labels map[string]string) *NelmClient {
	return &NelmClient{
		logger: logger.With("operator.component", "nelm"),
		labels: labels,
	}
}

type NelmClient struct {
	logger *NelmLogger
	labels map[string]string
}

func (c *NelmClient) WithLogLabels(logLabels map[string]string) {
	c.logger = c.logger.EnrichWithLabels(logLabels)
}

func (c *NelmClient) WithExtraLabels(labels map[string]string) {
	for k, v := range labels {
		c.labels[k] = v
	}
}

func (c *NelmClient) LastReleaseStatus(releaseName string) (string, string, error) {
	releaseGetResult, err := action.ReleaseGet(context.TODO(), releaseName, commonOptions.Namespace, action.ReleaseGetOptions{
		KubeContext:          commonOptions.KubeContext,
		OutputNoPrint:        true,
		ReleaseStorageDriver: commonOptions.HelmDriver,
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

func (c *NelmClient) UpgradeRelease(releaseName string, chartName string, valuesPaths []string, setValues []string, labels map[string]string, namespace string) error {
	c.logger.Info(context.TODO(), "Running nelm install for release",
		slog.String("release", releaseName),
		slog.String("chart", chartName),
		slog.String("namespace", namespace))

	if err := action.ReleaseInstall(context.TODO(), releaseName, namespace, action.ReleaseInstallOptions{
		Chart:                chartName,
		ExtraLabels:          c.labels,
		KubeContext:          commonOptions.KubeContext,
		NoInstallCRDs:        true,
		ReleaseHistoryLimit:  int(commonOptions.HistoryMax),
		ReleaseLabels:        labels,
		ReleaseStorageDriver: commonOptions.HelmDriver,
		Timeout:              commonOptions.Timeout,
		ValuesFilesPaths:     valuesPaths,
		ValuesSets:           setValues,
	}); err != nil {
		return fmt.Errorf("install nelm release %q: %w", releaseName, err)
	}

	c.logger.Info(context.TODO(), "Nelm install successful",
		slog.String("release", releaseName),
		slog.String("chart", chartName),
		slog.String("namespace", namespace))

	return nil
}

func (c *NelmClient) GetReleaseValues(releaseName string) (utils.Values, error) {
	panic("not going to be implemented")
}

func (c *NelmClient) GetReleaseChecksum(releaseName string) (string, error) {
	releaseGetResult, err := action.ReleaseGet(context.TODO(), releaseName, commonOptions.Namespace, action.ReleaseGetOptions{
		KubeContext:          commonOptions.KubeContext,
		OutputNoPrint:        true,
		ReleaseStorageDriver: commonOptions.HelmDriver,
	})
	if err != nil {
		return "", fmt.Errorf("get nelm release %q: %w", releaseName, err)
	}

	if checksum, ok := releaseGetResult.Release.Annotations["moduleChecksum"]; ok {
		return checksum, nil
	}

	if recordedChecksum, hasKey := releaseGetResult.Values["_addonOperatorModuleChecksum"]; hasKey {
		if recordedChecksumStr, ok := recordedChecksum.(string); ok {
			return recordedChecksumStr, nil
		}
	}

	return "", fmt.Errorf("moduleChecksum label not found in nelm release %q", releaseName)
}

func (c *NelmClient) DeleteRelease(releaseName string) error {
	c.logger.Debug(context.TODO(), "nelm release: execute nelm uninstall", slog.String("release", releaseName))

	if err := action.ReleaseUninstall(context.TODO(), releaseName, commonOptions.Namespace, action.ReleaseUninstallOptions{
		KubeContext:          commonOptions.KubeContext,
		ReleaseHistoryLimit:  int(commonOptions.HistoryMax),
		ReleaseStorageDriver: commonOptions.HelmDriver,
		Timeout:              commonOptions.Timeout,
	}); err != nil {
		return fmt.Errorf("nelm uninstall release %q: %w", releaseName, err)
	}

	c.logger.Debug(context.TODO(), "nelm release deleted", slog.String("release", releaseName))

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
	releaseListResult, err := action.ReleaseList(context.TODO(), action.ReleaseListOptions{
		KubeContext:          commonOptions.KubeContext,
		OutputNoPrint:        true,
		ReleaseStorageDriver: commonOptions.HelmDriver,
	})
	if err != nil {
		return nil, fmt.Errorf("list nelm releases: %w", err)
	}

	var releaseNames []string
	for _, release := range releaseListResult.Releases {
		// FIXME(addon-operator): not sure how this can happen, it is impossible to deploy a release with no name with Nelm, and, I believe, with Helm too
		if release.Name == "" {
			continue
		}

		releaseNames = append(releaseNames, release.Name)
	}

	sort.Strings(releaseNames)

	return releaseNames, nil
}

func (c *NelmClient) Render(releaseName, chartName string, valuesPaths, setValues []string, namespace string, debug bool) (string, error) {
	c.logger.Debug(context.TODO(), "Render nelm templates for chart ...",
		slog.String("chart", chartName),
		slog.String("namespace", namespace))

	chartRenderResult, err := action.ChartRender(context.TODO(), action.ChartRenderOptions{
		Chart:                chartName,
		ExtraLabels:          c.labels,
		KubeContext:          commonOptions.KubeContext,
		ReleaseName:          releaseName,
		ReleaseNamespace:     commonOptions.Namespace,
		ReleaseStorageDriver: commonOptions.HelmDriver,
		Remote:               true,
		ValuesFilesPaths:     valuesPaths,
		ValuesSets:           setValues,
	})
	if err != nil {
		return "", fmt.Errorf("render nelm chart %q: %w", chartName, err)
	}

	c.logger.Info(context.TODO(), "Render nelm templates for chart was successful", slog.String("chart", chartName))

	var result string
	for _, resource := range chartRenderResult.Resources {
		b, err := yaml.Marshal(resource)
		if err != nil {
			return "", fmt.Errorf("marshal resource: %w", err)
		}

		if result != "" {
			result += "---\n"
		}

		result += string(b)
	}

	return result, nil
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
