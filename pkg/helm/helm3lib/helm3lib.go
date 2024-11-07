package helm3lib

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/releaseutil"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/rest"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm/client"
	"github.com/flant/addon-operator/pkg/helm/post_renderer"
	"github.com/flant/addon-operator/pkg/utils"
)

var helmPostRenderer *post_renderer.PostRenderer

func initPostRenderer(extraLabels map[string]string) {
	if len(extraLabels) > 0 {
		helmPostRenderer = &post_renderer.PostRenderer{
			ExtraLabels: extraLabels,
		}
	}
}

func Init(opts *Options, logger *log.Logger, extraLabels map[string]string) error {
	hc := &LibClient{
		Logger: logger.With("operator.component", "helm3lib"),
	}
	options = opts
	initPostRenderer(extraLabels)

	return hc.initAndVersion()
}

// ReinitActionConfig reinitializes helm3 action configuration to update its list of capabilities
func ReinitActionConfig(logger *log.Logger) error {
	hc := &LibClient{
		Logger: logger.With("operator.component", "helm3lib"),
	}
	log.Debug("Reinitialize Helm 3 lib action configuration")

	return hc.actionConfigInit()
}

// LibClient use helm3 package as Go library.
type LibClient struct {
	Logger    *log.Logger
	Namespace string
}

type Options struct {
	Namespace  string
	HistoryMax int32
	Timeout    time.Duration
}

var (
	_            client.HelmClient = &LibClient{}
	options      *Options
	actionConfig *action.Configuration
)

func NewClient(logger *log.Logger, logLabels ...map[string]string) client.HelmClient {
	logEntry := logger.With("operator.component", "helm3lib")
	if len(logLabels) > 0 {
		logEntry = utils.EnrichLoggerWithLabels(logEntry, logLabels[0])
	}

	return &LibClient{
		Logger:    logEntry,
		Namespace: options.Namespace,
	}
}

// buildConfigFlagsFromEnv builds a ConfigFlags object from the environment and
// returns it. It uses a persistent config, meaning that underlying clients will
// be cached and reused.
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

func (h *LibClient) actionConfigInit() error {
	ac := new(action.Configuration)

	getter := buildConfigFlagsFromEnv(&options.Namespace, cli.New())

	// If env is empty - default storage backend ('secrets') will be used
	helmDriver := os.Getenv("HELM_DRIVER")
	err := ac.Init(getter, options.Namespace, helmDriver, h.Logger.Debugf)
	if err != nil {
		return fmt.Errorf("init helm action config: %v", err)
	}

	actionConfig = ac

	return nil
}

// initAndVersion runs helm version command.
func (h *LibClient) initAndVersion() error {
	if err := h.actionConfigInit(); err != nil {
		return err
	}

	log.Infof("Helm 3 version: %s", chartutil.DefaultCapabilities.HelmVersion.Version)
	return nil
}

// LastReleaseStatus returns last known revision for release and its status
func (h *LibClient) LastReleaseStatus(releaseName string) (revision string, status string, err error) {
	lastRelease, err := actionConfig.Releases.Last(releaseName)
	if err != nil {
		// in the Last(x) function we have the condition:
		// 	if len(h) == 0 {
		//		return nil, errors.Errorf("no revision for release %q", name)
		//	}
		// that's why we also check string representation
		if err == driver.ErrReleaseNotFound || strings.HasPrefix(err.Error(), "no revision for release") {
			return "0", "", fmt.Errorf("release '%s' not found\n", releaseName)
		}
		return "", "", err
	}

	return strconv.FormatInt(int64(lastRelease.Version), 10), lastRelease.Info.Status.String(), nil
}

func (h *LibClient) UpgradeRelease(releaseName string, chartName string, valuesPaths []string, setValues []string, namespace string) error {
	err := h.upgradeRelease(releaseName, chartName, valuesPaths, setValues, namespace)
	if err != nil {
		// helm validation can fail because FeatureGate was enabled for example
		// handling this case we can reinitialize kubeClient and repeat one more time by backoff
		if err := h.actionConfigInit(); err != nil {
			return err
		}
		return h.upgradeRelease(releaseName, chartName, valuesPaths, setValues, namespace)
	}
	h.Logger.Debugf("helm release %s upgraded", releaseName)
	return nil
}

func (h *LibClient) upgradeRelease(releaseName string, chartName string, valuesPaths []string, setValues []string, namespace string) error {
	upg := action.NewUpgrade(actionConfig)
	if namespace != "" {
		upg.Namespace = namespace
	}
	if helmPostRenderer != nil {
		upg.PostRenderer = helmPostRenderer
	}

	upg.Install = true
	upg.MaxHistory = int(options.HistoryMax)
	upg.Timeout = options.Timeout

	chart, err := loader.Load(chartName)
	if err != nil {
		return err
	}

	var resultValues chartutil.Values

	for _, vp := range valuesPaths {
		values, err := chartutil.ReadValuesFile(vp)
		if err != nil {
			return err
		}

		resultValues = chartutil.CoalesceTables(resultValues, values)
	}

	if len(setValues) > 0 {
		m := make(map[string]interface{})
		for _, sv := range setValues {
			arr := strings.Split(sv, "=")
			if len(arr) == 2 {
				m[arr[0]] = arr[1]
			}
		}
		resultValues = chartutil.CoalesceTables(resultValues, m)
	}

	h.Logger.Infof("Running helm upgrade for release '%s' with chart '%s' in namespace '%s' ...", releaseName, chartName, namespace)
	histClient := action.NewHistory(actionConfig)
	// Max is not working!!! Sort the final of releases by your own
	// histClient.Max = 1
	releases, err := histClient.Run(releaseName)
	if err == driver.ErrReleaseNotFound {
		instClient := action.NewInstall(actionConfig)
		if namespace != "" {
			instClient.Namespace = namespace
		}
		if helmPostRenderer != nil {
			instClient.PostRenderer = helmPostRenderer
		}
		instClient.Timeout = options.Timeout
		instClient.ReleaseName = releaseName
		instClient.UseReleaseName = true

		_, err = instClient.Run(chart, resultValues)
		return err
	}
	h.Logger.Debugf("%d old releases found", len(releases))
	if len(releases) > 0 {
		// https://github.com/fluxcd/helm-controller/issues/149
		// looking through this issue you can find the common error: another operation (install/upgrade/rollback) is in progress
		// and hints to fix it. In the future releases of helm they will handle sudden shutdown
		releaseutil.Reverse(releases, releaseutil.SortByRevision)
		latestRelease := releases[0]
		nsReleaseName := fmt.Sprintf("%s/%s", latestRelease.Namespace, latestRelease.Name)
		h.Logger.Debugf("Latest release '%s': revision: %d has status: %s", nsReleaseName, latestRelease.Version, latestRelease.Info.Status)
		if latestRelease.Info.Status.IsPending() {
			objectName := fmt.Sprintf("%s.%s.v%d", storage.HelmStorageType, latestRelease.Name, latestRelease.Version)
			kubeClient, err := actionConfig.KubernetesClientSet()
			if err != nil {
				return fmt.Errorf("couldn't get kubernetes client set: %w", err)
			}
			// switch between storage types (memory, sql, secrets, configmaps) - with secrets and configmaps we can deal a bit more straightforward than doing a rollback
			switch actionConfig.Releases.Name() {
			case driver.ConfigMapsDriverName:
				h.Logger.Debugf("ConfigMap for helm revision %d of release %s in status %s, driver %s: will be deleted", latestRelease.Version, nsReleaseName, latestRelease.Info.Status, driver.ConfigMapsDriverName)
				err := kubeClient.CoreV1().ConfigMaps(latestRelease.Namespace).Delete(context.TODO(), objectName, metav1.DeleteOptions{})
				if err != nil && !errors.IsNotFound(err) {
					return fmt.Errorf("couldn't delete configmap %s of release %s: %w", objectName, nsReleaseName, err)
				}
				h.Logger.Debugf("ConfigMap %s was deleted", objectName)

			case driver.SecretsDriverName:
				h.Logger.Debugf("Secret for helm revision %d of release %s in status %s, driver %s: will be deleted", latestRelease.Version, nsReleaseName, latestRelease.Info.Status, driver.SecretsDriverName)
				err := kubeClient.CoreV1().Secrets(latestRelease.Namespace).Delete(context.TODO(), objectName, metav1.DeleteOptions{})
				if err != nil && !errors.IsNotFound(err) {
					return fmt.Errorf("couldn't delete secret %s of release %s: %w", objectName, nsReleaseName, err)
				}
				h.Logger.Debugf("Secret %s was deleted", objectName)

			default:
				// memory and sql storages a bit more trickier - doing a rollback is justified
				h.Logger.Debugf("Helm revision %d of release %s in status %s, driver %s: will be rolledback", latestRelease.Version, nsReleaseName, latestRelease.Info.Status, actionConfig.Releases.Name())
				h.rollbackLatestRelease(releases)
			}
		}
	}

	_, err = upg.Run(releaseName, chart, resultValues)
	if err != nil {
		return fmt.Errorf("helm upgrade failed: %s\n", err)
	}
	h.Logger.Infof("Helm upgrade for release '%s' with chart '%s' in namespace '%s' successful", releaseName, chartName, namespace)

	return nil
}

func (h *LibClient) rollbackLatestRelease(releases []*release.Release) {
	latestRelease := releases[0]
	nsReleaseName := fmt.Sprintf("%s/%s", latestRelease.Namespace, latestRelease.Name)

	h.Logger.Infof("Trying to rollback '%s'", nsReleaseName)

	if latestRelease.Version == 1 || options.HistoryMax == 1 || len(releases) == 1 {
		rb := action.NewUninstall(actionConfig)
		rb.KeepHistory = false
		_, err := rb.Run(latestRelease.Name)
		if err != nil {
			h.Logger.Warnf("Failed to uninstall pending release %s: %s", nsReleaseName, err)
			return
		}
	} else {
		previousVersion := latestRelease.Version - 1
		for i := 1; i < len(releases); i++ {
			if !releases[i].Info.Status.IsPending() {
				previousVersion = releases[i].Version
				break
			}
		}
		rb := action.NewRollback(actionConfig)
		rb.Version = previousVersion
		rb.CleanupOnFail = true
		err := rb.Run(latestRelease.Name)
		if err != nil {
			h.Logger.Warnf("Failed to rollback pending release %s: %s", nsReleaseName, err)
			return
		}
	}

	h.Logger.Infof("Rollback '%s' successful", nsReleaseName)
}

func (h *LibClient) GetReleaseValues(releaseName string) (utils.Values, error) {
	gv := action.NewGetValues(actionConfig)
	return gv.Run(releaseName)
}

func (h *LibClient) DeleteRelease(releaseName string) error {
	h.Logger.Debugf("helm release '%s': execute helm uninstall", releaseName)

	un := action.NewUninstall(actionConfig)
	_, err := un.Run(releaseName)
	if err != nil {
		return fmt.Errorf("helm uninstall %s invocation error: %v\n", releaseName, err)
	}

	h.Logger.Debugf("helm release %s deleted", releaseName)
	return nil
}

func (h *LibClient) IsReleaseExists(releaseName string) (bool, error) {
	revision, _, err := h.LastReleaseStatus(releaseName)
	if err == nil {
		return true, nil
	}
	if revision == "0" {
		return false, nil
	}
	return false, err
}

// ListReleasesNames returns list of release names.
func (h *LibClient) ListReleasesNames() ([]string, error) {
	l := action.NewList(actionConfig)
	// list all releases regardless of their state
	l.StateMask = action.ListAll
	list, err := l.Run()
	if err != nil {
		return nil, fmt.Errorf("helm list failed: %s", err)
	}

	releases := make([]string, 0, len(list))
	for _, release := range list {
		// Do not return ignored release or empty string.
		if release.Name == app.HelmIgnoreRelease || release.Name == "" {
			continue
		}

		releases = append(releases, release.Name)
	}

	sort.Strings(releases)
	return releases, nil
}

func (h *LibClient) Render(releaseName, chartName string, valuesPaths, setValues []string, namespace string, debug bool) (string, error) {
	chart, err := loader.Load(chartName)
	if err != nil {
		return "", err
	}

	var resultValues chartutil.Values

	for _, vp := range valuesPaths {
		values, err := chartutil.ReadValuesFile(vp)
		if err != nil {
			return "", err
		}

		resultValues = chartutil.CoalesceTables(resultValues, values)
	}

	if len(setValues) > 0 {
		m := make(map[string]interface{})
		for _, sv := range setValues {
			arr := strings.Split(sv, "=")
			if len(arr) == 2 {
				m[arr[0]] = arr[1]
			}
		}
		resultValues = chartutil.CoalesceTables(resultValues, m)
	}

	h.Logger.Debugf("Render helm templates for chart '%s' in namespace '%s' ...", chartName, namespace)

	inst := newInstAction(namespace, releaseName)

	rs, err := inst.Run(chart, resultValues)
	if err != nil {
		// helm render can fail because the CRD were previously created
		// handling this case we can reinitialize RESTClient and repeat one more time by backoff
		h.actionConfigInit()
		inst = newInstAction(namespace, releaseName)

		rs, err = inst.Run(chart, resultValues)
	}

	if err != nil {
		if !debug {
			return "", fmt.Errorf("%w\n\nUse --debug flag to render out invalid YAML", err)
		}
		if rs == nil {
			return "", err
		}

		rs.Manifest += fmt.Sprintf("\n\n\n%v", err)
	}

	h.Logger.Infof("Render helm templates for chart '%s' was successful", chartName)

	return rs.Manifest, nil
}

func newInstAction(namespace, releaseName string) *action.Install {
	inst := action.NewInstall(actionConfig)
	inst.DryRun = true

	if namespace != "" {
		inst.Namespace = namespace
	}
	if helmPostRenderer != nil {
		inst.PostRenderer = helmPostRenderer
	}
	inst.ReleaseName = releaseName
	inst.UseReleaseName = true
	inst.Replace = true // Skip the name check
	inst.IsUpgrade = true
	inst.DisableOpenAPIValidation = true

	return inst
}
