package nelm

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"time"

	"helm.sh/helm/v3/pkg/cli"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/rest"

	"github.com/flant/addon-operator/pkg/helm/client"
	"github.com/flant/addon-operator/pkg/utils"

	"github.com/werf/nelm/pkg/action"
)

// FIXME(nelm): get rid of all globals in Nelm to be able to run them sequentially or in parallel

var (
	_             client.HelmClient = &NelmClient{}
	commonOptions *CommonOptions
)

func Init(opts *CommonOptions, logger *NelmLogger) error {
	getter := buildConfigFlagsFromEnv(&commonOptions.Namespace, cli.New())

	// FIXME(addon-operator): add all other options from getter in the same manner
	if opts.KubeContext == "" && getter.Context != nil {
		opts.KubeContext = *getter.Context
	}

	if opts.HelmDriver == "" {
		opts.HelmDriver = os.Getenv("HELM_DRIVER")
	}

	// FIXME(nelm): this function does A LOT. And with what addon-operator expects from us
	// in regards to logging, all of this is kind of a mess now
	action.SetupLogging(context.TODO(), "info", "", "off")

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
	Namespace  string
	HistoryMax int32
	// FIXME(nelm): add global timeout to Nelm actions
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
		// FIXME(nelm): return a specific error from ReleaseGet if release not found
		if regexp.MustCompile(`.+release ".+" (namespace ".+") not found`).Match([]byte(err.Error())) {
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

	// FIXME(nelm): deploy without installing CRDs
	// FIXME(nelm): allow passing labels to Release.Labels
	if err := action.ReleaseInstall(context.TODO(), releaseName, namespace, action.ReleaseInstallOptions{
		// FIXME(nelm): allow passing remote chart path
		ChartDirPath:         chartName,
		ExtraLabels:          c.labels,
		KubeContext:          commonOptions.KubeContext,
		ReleaseHistoryLimit:  int(commonOptions.HistoryMax),
		ReleaseStorageDriver: commonOptions.HelmDriver,
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

	// FIXME(nelm): expose Values in ReleaseGetResult and uncomment this
	// if recordedChecksum, hasKey := releaseGetResult.Values["_addonOperatorModuleChecksum"]; hasKey {
	// 	if recordedChecksumStr, ok := recordedChecksum.(string); ok {
	// 		return recordedChecksumStr, nil
	// 	}
	// }

	return "", fmt.Errorf("moduleChecksum label not found in nelm release %q", releaseName)
}

func (c *NelmClient) DeleteRelease(releaseName string) error {
	c.logger.Debug(context.TODO(), "nelm release: execute nelm uninstall", slog.String("release", releaseName))

	if err := action.ReleaseUninstall(context.TODO(), releaseName, commonOptions.Namespace, action.ReleaseUninstallOptions{
		KubeContext:          commonOptions.KubeContext,
		ReleaseHistoryLimit:  int(commonOptions.HistoryMax),
		ReleaseStorageDriver: commonOptions.HelmDriver,
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
	// FIXME(nelm): implement ReleaseList as a Nelm action
	panic("not implemented yet")
}

func (c *NelmClient) Render(releaseName, chartName string, valuesPaths, setValues []string, namespace string, debug bool) (string, error) {
	// FIXME(nelm): debug arg is not used now. Nelm doesn't return bad manifests on error, instead it has very verbose trace log level which prints them. Do we really need to dump bad manifests on error?

	c.logger.Debug(context.TODO(), "Render nelm templates for chart ...",
		slog.String("chart", chartName),
		slog.String("namespace", namespace))

	// FIXME(nelm): need to return ChartRenderResultV1, like we did in action.ChartRender
	err := action.ChartRender(context.TODO(), action.ChartRenderOptions{
		// FIXME(nelm): allow passing remote chart path
		ChartDirPath:         chartName,
		ExtraLabels:          c.labels,
		KubeContext:          commonOptions.KubeContext,
		Remote:               true,
		ReleaseName:          releaseName,
		ReleaseNamespace:     commonOptions.Namespace,
		ReleaseStorageDriver: commonOptions.HelmDriver,
		ValuesFilesPaths:     valuesPaths,
		ValuesSets:           setValues,
	})
	if err != nil {
		return "", fmt.Errorf("render nelm chart %q: %w", chartName, err)
	}

	c.logger.Info(context.TODO(), "Render nelm templates for chart was successful", slog.String("chart", chartName))

	// FIXME(nelm): we should return manifests from ChartRenderResultV1
	panic("not implemented yet")
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
