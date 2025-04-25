package nelm

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	"helm.sh/helm/v3/pkg/cli"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/rest"

	"github.com/flant/addon-operator/pkg/helm/client"
	"github.com/flant/addon-operator/pkg/utils"

	"github.com/werf/nelm/pkg/action"
)

var (
	_             client.HelmClient = &NelmClient{}
	commonOptions *CommonOptions
)

func Init(opts *CommonOptions, logger *log.Logger) error {
	getter := buildConfigFlagsFromEnv(&commonOptions.Namespace, cli.New())

	// XXX: add all other options from getter in the same manner
	if opts.KubeContext == "" && getter.Context != nil {
		opts.KubeContext = *getter.Context
	}

	if opts.HelmDriver == "" {
		opts.HelmDriver = os.Getenv("HELM_DRIVER")
	}

	// FIXME(ilya-lesikov): this function does A LOT. And with what addon-operator expects from us
	// in regards to logging, all of this is kind of a mess now
	opts.context = action.SetupLogging(context.TODO(), "info", "", "off")

	commonOptions = opts

	versionResult, err := action.Version(commonOptions.context, action.VersionOptions{
		OutputNoPrint: true,
	})
	if err != nil {
		return fmt.Errorf("get nelm version: %w", err)
	}

	logger.Info("Nelm version", slog.String("version", versionResult.FullVersion))

	return nil
}

type CommonOptions struct {
	Namespace  string
	HistoryMax int32
	// XXX: no global timeout in Nelm
	Timeout     time.Duration
	HelmDriver  string
	KubeContext string
	// XXX: add all other options from *genericclioptions.ConfigFlags

	// FIXME(ilya-lesikov): we need a special context with logboek attached to it to be passed to
	// Nelm actions to actually log anything
	context context.Context
}

func NewNelmClient(logger *log.Logger, labels map[string]string) *NelmClient {
	logEntry := logger.With("operator.component", "nelm")

	return &NelmClient{
		logger: logEntry,
		labels: labels,
	}
}

type NelmClient struct {
	// FIXME(ilya-lesikov): not gonna be used in Nelm atm
	logger *log.Logger
	labels map[string]string
}

func (c *NelmClient) WithLogLabels(logLabels map[string]string) {
	c.logger = utils.EnrichLoggerWithLabels(c.logger, logLabels)
}

func (c *NelmClient) WithExtraLabels(labels map[string]string) {
	for k, v := range labels {
		c.labels[k] = v
	}
}

func (c *NelmClient) LastReleaseStatus(releaseName string) (string, string, error) {
	releaseGetResult, err := action.ReleaseGet(commonOptions.context, releaseName, commonOptions.Namespace, action.ReleaseGetOptions{
		KubeContext:          commonOptions.KubeContext,
		OutputNoPrint:        true,
		ReleaseStorageDriver: commonOptions.HelmDriver,
	})
	if err != nil {
		// FIXME(ilya-lesikov): return a specific error from ReleaseGet if release not found
		if regexp.MustCompile(`.+release ".+" (namespace ".+") not found`).Match([]byte(err.Error())) {
			return "0", "", fmt.Errorf("get nelm release %q: %w", releaseName, err)
		}

		return "", "", fmt.Errorf("get nelm release %q: %w", releaseName, err)
	}

	return strconv.FormatInt(int64(releaseGetResult.Release.Revision), 10), releaseGetResult.Release.Status.String(), nil
}

func (c *NelmClient) UpgradeRelease(releaseName string, chartName string, valuesPaths []string, setValues []string, labels map[string]string, namespace string) error {
	c.logger.Info("Running nelm install for release",
		slog.String("release", releaseName),
		slog.String("chart", chartName),
		slog.String("namespace", namespace))

	// XXX: for some reason upgrade was called twice previously?
	// XXX: why we always deployed with upg.SkipCRDs = true?
	// XXX: Nelm does not care about pending status and will upgrade pending release just fine. Does it mean we are going to be fine without this: https://github.com/flant/addon-operator/blob/b7c97010128922e066fed35e2e53e91834a250de/pkg/helm/helm3lib/helm3lib.go#L293-L300
	// FIXME(ilya-lesikov): allow passing labels to Release.Labels
	if err := action.ReleaseInstall(commonOptions.context, releaseName, namespace, action.ReleaseInstallOptions{
		// XXX: Nelm doesn't allow anything besides chart dir
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

	c.logger.Info("Nelm install successful",
		slog.String("release", releaseName),
		slog.String("chart", chartName),
		slog.String("namespace", namespace))

	return nil
}

func (c *NelmClient) GetReleaseValues(releaseName string) (utils.Values, error) {
	panic("not going to be implemented")
}

func (c *NelmClient) GetReleaseChecksum(releaseName string) (string, error) {
	releaseGetResult, err := action.ReleaseGet(commonOptions.context, releaseName, commonOptions.Namespace, action.ReleaseGetOptions{
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

	// XXX: here was a fallback to get "_addonOperatorModuleChecksum" value from last release values
	// instead. In Nelm via action.ReleaseGet we don't expose values of last release, I don't think
	// they are relevant when we already expose manifests. Do we really need this?

	return "", fmt.Errorf("moduleChecksum label not found in nelm release %q", releaseName)
}

func (c *NelmClient) DeleteRelease(releaseName string) error {
	c.logger.Debug("nelm release: execute nelm uninstall", slog.String("release", releaseName))

	if err := action.ReleaseUninstall(commonOptions.context, releaseName, commonOptions.Namespace, action.ReleaseUninstallOptions{
		KubeContext:          commonOptions.KubeContext,
		ReleaseHistoryLimit:  int(commonOptions.HistoryMax),
		ReleaseStorageDriver: commonOptions.HelmDriver,
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
	// FIXME(ilya-lesikov): not implemented in Nelm actions yet
	panic("not implemented yet")
}

func (c *NelmClient) Render(releaseName, chartName string, valuesPaths, setValues []string, namespace string, debug bool) (string, error) {
	// XXX: debug arg is not used now. Nelm doesn't return bad manifests on error, instead it has very verbose trace log level which prints them. Do we really need to dump bad manifests on error?

	c.logger.Debug("Render nelm templates for chart ...",
		slog.String("chart", chartName),
		slog.String("namespace", namespace))

	// FIXME(ilya-lesikov): need to return ChartRenderResultV1, like we did in action.ChartRender
	err := action.ChartRender(commonOptions.context, action.ChartRenderOptions{
		// XXX: only local chart dirs are supported
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

	c.logger.Info("Render nelm templates for chart was successful", slog.String("chart", chartName))

	// FIXME(ilya-lesikov): we should return manifests from ChartRenderResultV1
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
