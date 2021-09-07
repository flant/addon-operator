package helm3lib

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/storage/driver"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kblabels "k8s.io/apimachinery/pkg/labels"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm/client"
	"github.com/flant/addon-operator/pkg/utils"

	klient "github.com/flant/kube-client/client"
)

// Init runs
func Init(opts *Options) error {
	hc := &LibClient{
		LogEntry: log.WithField("operator.component", "helm3lib"),
	}
	options = opts
	err := hc.initAndVersion()
	if err != nil {
		return err
	}
	return nil
}

// Library use client
type LibClient struct {
	KubeClient klient.Client
	LogEntry   *log.Entry
	Namespace  string
}

type Options struct {
	Namespace  string
	HistoryMax int32
	Timeout    time.Duration
	KubeClient klient.Client
}

var _ client.HelmClient = &LibClient{}
var options *Options
var actionConfig *action.Configuration

func NewClient(logLabels ...map[string]string) client.HelmClient {
	logEntry := log.WithField("operator.component", "helm3lib")
	if len(logLabels) > 0 {
		logEntry = logEntry.WithFields(utils.LabelsToLogFields(logLabels[0]))
	}

	return &LibClient{
		LogEntry:   logEntry,
		KubeClient: options.KubeClient,
		Namespace:  options.Namespace,
	}
}

func (h *LibClient) CommandEnv() []string {
	res := make([]string, 0)
	return res
}

func (h *LibClient) WithKubeClient(client klient.Client) {
	h.KubeClient = client
}

// initAndVersion runs helm version command.
func (h *LibClient) initAndVersion() error {
	ac := new(action.Configuration)

	env := cli.New()

	err := ac.Init(env.RESTClientGetter(), options.Namespace, "secrets", h.LogEntry.Debugf)
	if err != nil {
		return err
	}

	actionConfig = ac

	log.Infof("Helm 3 version: %s", chartutil.DefaultCapabilities.HelmVersion.Version)

	return nil
}

func (h *LibClient) DeleteSingleFailedRevision(releaseName string) error {
	// No need to delete single failed revision anymore
	// https://github.com/helm/helm/issues/8037#issuecomment-622217632
	return nil
}

func (h *LibClient) DeleteOldFailedRevisions(releaseName string) error {
	// No need to delete single failed revision anymore
	// https://github.com/helm/helm/issues/8037#issuecomment-622217632
	return nil
}

// LastReleaseStatus returns last known revision for release and its status
func (h *LibClient) LastReleaseStatus(releaseName string) (revision string, status string, err error) {
	release, err := actionConfig.Releases.Last(releaseName)
	if err != nil {
		if err == driver.ErrReleaseNotFound || strings.HasPrefix(err.Error(), "no revision for release") {
			return "0", "", fmt.Errorf("release '%s' not found\n", releaseName)
		}
		return "", "", err
	}

	return strconv.FormatInt(int64(release.Version), 10), release.Info.Status.String(), nil
}

func (h *LibClient) UpgradeRelease(releaseName string, chartName string, valuesPaths []string, setValues []string, namespace string) error {
	fmt.Println("NEW UPGRADE")

	upg := action.NewUpgrade(actionConfig)
	if namespace != "" {
		upg.Namespace = namespace
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

	h.LogEntry.Infof("Running helm upgrade for release '%s' with chart '%s' in namespace '%s' ...", releaseName, chartName, namespace)
	histClient := action.NewHistory(actionConfig)
	histClient.Max = 1
	fmt.Println("BEFORE UPGRADE")
	if _, err := histClient.Run(releaseName); err == driver.ErrReleaseNotFound {
		fmt.Println("RELEAST NOT FOUND")
		instClient := action.NewInstall(actionConfig)
		if namespace != "" {
			instClient.Namespace = namespace
		}
		instClient.Timeout = options.Timeout
		instClient.ReleaseName = releaseName
		instClient.UseReleaseName = true

		_, err = instClient.Run(chart, resultValues)

		return err
	}

	_, err = upg.Run(releaseName, chart, resultValues)
	if err != nil {
		return fmt.Errorf("helm upgrade failed: %s\n", err)
	}
	h.LogEntry.Infof("Helm upgrade for release '%s' with chart '%s' in namespace '%s' successful", releaseName, chartName, namespace)

	return nil
}

func (h *LibClient) GetReleaseValues(releaseName string) (utils.Values, error) {
	gv := action.NewGetValues(actionConfig)
	return gv.Run(releaseName)
}

func (h *LibClient) DeleteRelease(releaseName string) error {
	h.LogEntry.Debugf("helm release '%s': execute helm uninstall", releaseName)

	un := action.NewUninstall(actionConfig)
	_, err := un.Run(releaseName)
	if err != nil {
		return fmt.Errorf("helm uninstall %s invocation error: %v\n", releaseName, err)
	}

	return nil
}

func (h *LibClient) IsReleaseExists(releaseName string) (bool, error) {
	revision, _, err := h.LastReleaseStatus(releaseName)
	if err != nil && revision == "0" {
		return false, nil
	} else if err != nil {
		return false, err
	}

	return true, nil
}

// ListReleases returns all known releases as strings — "<release_name>.v<release_number>"
// It is required only for helm2.
func (h *LibClient) ListReleases(labelSelector map[string]string) (releases []string, err error) {
	return
}

// ListReleasesNames returns list of release names.
// Names are extracted from label "name" in Secrets with label "owner"=="helm".
func (h *LibClient) ListReleasesNames(labelSelector map[string]string) ([]string, error) {
	labelsSet := make(kblabels.Set)
	for k, v := range labelSelector {
		labelsSet[k] = v
	}
	labelsSet["owner"] = "helm"

	list, err := h.KubeClient.CoreV1().
		Secrets(h.Namespace).
		List(context.TODO(), metav1.ListOptions{LabelSelector: labelsSet.AsSelector().String()})
	if err != nil {
		h.LogEntry.Debugf("helm: list of releases ConfigMaps failed: %s", err)
		return nil, err
	}

	uniqNamesMap := make(map[string]struct{})
	for _, secret := range list.Items {
		releaseName, has_key := secret.Labels["name"]
		if has_key && releaseName != "" {
			uniqNamesMap[releaseName] = struct{}{}
		}
	}

	// Do not return ignored release.
	delete(uniqNamesMap, app.HelmIgnoreRelease)

	uniqNames := make([]string, 0)
	for name := range uniqNamesMap {
		uniqNames = append(uniqNames, name)
	}

	sort.Strings(uniqNames)
	return uniqNames, nil
}

// Render renders helm templates for chart
func (h *LibClient) Render(releaseName string, chartName string, valuesPaths []string, setValues []string, namespace string) (string, error) {
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

	h.LogEntry.Debugf("Render helm templates for chart '%s' in namespace '%s' ...", chartName, namespace)

	inst := action.NewInstall(actionConfig)
	inst.DryRun = true

	if namespace != "" {
		inst.Namespace = namespace
	}
	inst.ReleaseName = releaseName
	inst.UseReleaseName = true
	inst.Replace = true // Skip the name check

	rs, err := inst.Run(chart, resultValues)
	if err != nil {
		return "", err
	}

	h.LogEntry.Infof("Render helm templates for chart '%s' was successful", chartName)

	return rs.Manifest, nil
}
