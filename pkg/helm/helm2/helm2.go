package helm2

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"

	klient "github.com/flant/kube-client/client"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kblabels "k8s.io/apimachinery/pkg/labels"

	"github.com/flant/addon-operator/pkg/helm/client"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/shell-operator/pkg/executor"
)

var Helm2Path = "helm"

type Helm2Options struct {
	Namespace  string
	KubeClient klient.Client
}

var HelmOptions *Helm2Options

func Init(options *Helm2Options) error {
	hc := &Helm2Client{
		LogEntry: log.WithField("operator.component", "helm"),
	}
	err := hc.InitAndVersion()
	if err != nil {
		return err
	}
	HelmOptions = options
	return nil
}

type Helm2Client struct {
	KubeClient klient.Client
	LogEntry   *log.Entry
	Namespace  string
}

var _ client.HelmClient = &Helm2Client{}

func NewClient(logLabels ...map[string]string) client.HelmClient {
	logEntry := log.WithField("operator.component", "helm")
	if len(logLabels) > 0 {
		logEntry = logEntry.WithFields(utils.LabelsToLogFields(logLabels[0]))
	}

	return &Helm2Client{
		LogEntry:   logEntry,
		KubeClient: HelmOptions.KubeClient,
		Namespace:  HelmOptions.Namespace,
	}
}

func (h *Helm2Client) WithKubeClient(client klient.Client) {
	h.KubeClient = client
}

func (h *Helm2Client) CommandEnv() []string {
	res := make([]string, 0)
	res = append(res, fmt.Sprintf("TILLER_NAMESPACE=%s", h.Namespace))
	if TillerConnectAddress != "" {
		res = append(res, fmt.Sprintf("HELM_HOST=%s", TillerConnectAddress))
	}
	return res
}

// Cmd starts Helm with specified arguments.
// Sets the TILLER_NAMESPACE environment variable before starting, because Addon-operator works with its own Tiller.
func (h *Helm2Client) Cmd(args ...string) (stdout string, stderr string, err error) {
	cmd := exec.Command(Helm2Path, args...)
	cmd.Env = append(os.Environ(), h.CommandEnv()...)

	var stdoutBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	err = executor.Run(cmd)
	stdout = strings.TrimSpace(stdoutBuf.String())
	stderr = strings.TrimSpace(stderrBuf.String())

	return
}

// InitAndVersion runs helm init and helm version commands
func (h *Helm2Client) InitAndVersion() error {
	stdout, stderr, err := h.Cmd("init", "--client-only")
	if err != nil {
		return fmt.Errorf("helm init: %v\n%v %v", err, stdout, stderr)
	}

	stdout, stderr, err = h.Cmd("version", "--short", "--tiller-connection-timeout", fmt.Sprintf("%d", TillerWaitTimeoutSeconds))
	if err != nil {
		return fmt.Errorf("unable to get helm or tiller version: %v\n%v %v", err, stdout, stderr)
	}
	stdout = strings.Join([]string{stdout, stderr}, "\n")
	stdout = strings.ReplaceAll(stdout, "\n", " ")
	log.Infof("Helm successfully initialized. Version: %s", stdout)

	return nil
}

func (h *Helm2Client) DeleteSingleFailedRevision(releaseName string) (err error) {
	revision, status, err := h.LastReleaseStatus(releaseName)
	if err != nil {
		if revision == "0" {
			// Revision 0 is not an error. Just skips deletion.
			log.Debugf("helm release '%s': Release not found, no cleanup required.", releaseName)
			return nil
		}
		h.LogEntry.Errorf("helm release '%s': got error from LastReleaseStatus: %s", releaseName, err)
		return err
	}

	if revision == "1" && status == "FAILED" {
		// Deletes and purges!
		err = h.DeleteRelease(releaseName)
		if err != nil {
			h.LogEntry.Errorf("helm release '%s': cleanup of failed revision got error: %v", releaseName, err)
			return err
		}
		h.LogEntry.Infof("helm release '%s': cleanup of failed revision succeeded", releaseName)
	} else {
		// No interest of revisions older than 1.
		h.LogEntry.Debugf("helm release '%s': has revision '%s' with status %s", releaseName, revision, status)
	}

	return
}

func (h *Helm2Client) DeleteOldFailedRevisions(releaseName string) error {
	cmNames, err := h.ListReleases(map[string]string{"STATUS": "FAILED", "NAME": releaseName})
	if err != nil {
		return err
	}

	h.LogEntry.Debugf("helm release '%s': found ConfigMaps: %v", releaseName, cmNames)

	var releaseCmNamePattern = regexp.MustCompile(`^(.*).v([0-9]+)$`)

	revisions := make([]int, 0)
	for _, cmName := range cmNames {
		matchRes := releaseCmNamePattern.FindStringSubmatch(cmName)
		if matchRes != nil {
			revision, err := strconv.Atoi(matchRes[2])
			if err != nil {
				continue
			}
			revisions = append(revisions, revision)
		}
	}
	sort.Ints(revisions)

	// Do not removes last FAILED revision.
	if len(revisions) > 0 {
		revisions = revisions[:len(revisions)-1]
	}

	for _, revision := range revisions {
		cmName := fmt.Sprintf("%s.v%d", releaseName, revision)
		h.LogEntry.Infof("helm release '%s': delete old FAILED revision cm/%s", releaseName, cmName)

		err := h.KubeClient.CoreV1().
			ConfigMaps(h.Namespace).
			Delete(context.TODO(), cmName, metav1.DeleteOptions{})

		if err != nil {
			return err
		}
	}

	return nil
}

// TODO get this info from cm
// Get last known revision and status
// helm history output:
// REVISION	UPDATED                 	STATUS    	CHART                 	DESCRIPTION
// 1        Fri Jul 14 18:25:00 2017	SUPERSEDED	symfony-demo-0.1.0    	Install complete
func (h *Helm2Client) LastReleaseStatus(releaseName string) (revision string, status string, err error) {
	stdout, stderr, err := h.Cmd("history", releaseName, "--max", "1")

	if err != nil {
		errLine := strings.Split(stderr, "\n")[0]
		if strings.Contains(errLine, "Error:") && strings.Contains(errLine, "not found") {
			// Bad module name or no releases installed
			err = fmt.Errorf("release '%s' not found\n%v %v", releaseName, stdout, stderr)
			revision = "0"
			return
		}

		err = fmt.Errorf("cannot get history for release '%s'\n%v %v", releaseName, stdout, stderr)
		return
	}

	historyLines := strings.Split(stdout, "\n")
	lastLine := historyLines[len(historyLines)-1]
	fields := strings.SplitN(lastLine, "\t", 5) //regexp.MustCompile("\\t").Split(lastLine, 5)
	revision = strings.TrimSpace(fields[0])
	status = strings.TrimSpace(fields[2])
	return
}

func (h *Helm2Client) UpgradeRelease(releaseName string, chart string, valuesPaths []string, setValues []string, namespace string) error {
	args := make([]string, 0)
	args = append(args, "upgrade")
	args = append(args, "--install")
	args = append(args, releaseName)
	args = append(args, chart)

	if namespace != "" {
		args = append(args, "--namespace")
		args = append(args, namespace)
	}

	for _, valuesPath := range valuesPaths {
		args = append(args, "--values")
		args = append(args, valuesPath)
	}

	for _, setValue := range setValues {
		args = append(args, "--set")
		args = append(args, setValue)
	}

	h.LogEntry.Infof("Running helm upgrade for release '%s' with chart '%s' in namespace '%s' ...", releaseName, chart, namespace)
	stdout, stderr, err := h.Cmd(args...)
	if err != nil {
		return fmt.Errorf("helm upgrade failed: %s:\n%s %s", err, stdout, stderr)
	}
	h.LogEntry.Infof("Helm upgrade for release '%s' with chart '%s' in namespace '%s' successful:\n%s\n%s", releaseName, chart, namespace, stdout, stderr)

	return nil
}

func (h *Helm2Client) GetReleaseValues(releaseName string) (utils.Values, error) {
	stdout, stderr, err := h.Cmd("get", "values", releaseName)
	if err != nil {
		return nil, fmt.Errorf("cannot get values of helm release %s: %s\n%s %s", releaseName, err, stdout, stderr)
	}

	values, err := utils.NewValuesFromBytes([]byte(stdout))
	if err != nil {
		return nil, fmt.Errorf("cannot get values of helm release %s: %s", releaseName, err)
	}

	return values, nil
}

func (h *Helm2Client) DeleteRelease(releaseName string) (err error) {
	h.LogEntry.Debugf("helm release '%s': execute helm delete --purge", releaseName)

	stdout, stderr, err := h.Cmd("delete", "--purge", releaseName)
	if err != nil {
		return fmt.Errorf("helm delete --purge %s invocation error: %v\n%v %v", releaseName, err, stdout, stderr)
	}

	return
}

func (h *Helm2Client) IsReleaseExists(releaseName string) (bool, error) {
	revision, _, err := h.LastReleaseStatus(releaseName)
	if err != nil && revision == "0" {
		return false, nil
	} else if err != nil {
		return false, err
	}

	return true, nil
}

// Returns all known releases as strings — "<release_name>.v<release_number>"
// Helm looks for ConfigMaps by label 'OWNER=TILLER' and gets release info from the 'release' key.
// https://github.com/kubernetes/helm/blob/8981575082ea6fc2a670f81fb6ca5b560c4f36a7/pkg/storage/driver/cfgmaps.go#L88
func (h *Helm2Client) ListReleases(labelSelector map[string]string) ([]string, error) {
	labelsSet := make(kblabels.Set)
	for k, v := range labelSelector {
		labelsSet[k] = v
	}
	labelsSet["OWNER"] = "TILLER"

	cmList, err := h.KubeClient.CoreV1().
		ConfigMaps(h.Namespace).
		List(context.TODO(), metav1.ListOptions{LabelSelector: labelsSet.AsSelector().String()})
	if err != nil {
		h.LogEntry.Debugf("helm: list of releases ConfigMaps failed: %s", err)
		return nil, err
	}

	releases := make([]string, 0)
	for _, cm := range cmList.Items {
		if _, has_key := cm.Data["release"]; has_key {
			releases = append(releases, cm.Name)
		}
	}

	sort.Strings(releases)

	return releases, nil
}

// ListReleasesNames returns list of release names without suffixes ".v<release_number>"
func (h *Helm2Client) ListReleasesNames(labelSelector map[string]string) ([]string, error) {
	// Get all release names
	releases, err := h.ListReleases(labelSelector)
	if err != nil {
		return []string{}, err
	}

	var releaseNameRegex = regexp.MustCompile(`^(.*).v[0-9]+$`)

	// Drop .v* suffix and make array of unique names
	uniqNamesMap := map[string]bool{}
	for _, release := range releases {
		matchRes := releaseNameRegex.FindStringSubmatch(release)
		if matchRes != nil {
			releaseName := matchRes[1]
			uniqNamesMap[releaseName] = true
		}
	}

	uniqNames := make([]string, 0)
	for name := range uniqNamesMap {
		uniqNames = append(uniqNames, name)
	}

	return uniqNames, nil
}

// ListReleasesNames returns list of release names without suffixes ".v<release_number>"
func (h *Helm2Client) Render(releaseName string, chart string, valuesPaths []string, setValues []string, namespace string) (string, error) {
	args := make([]string, 0)
	args = append(args, "template")
	args = append(args, chart)

	// Release name
	if releaseName != "" {
		args = append(args, "--name")
		args = append(args, releaseName)
	}

	if namespace != "" {
		args = append(args, "--namespace")
		args = append(args, namespace)
	}

	for _, valuesPath := range valuesPaths {
		args = append(args, "--values")
		args = append(args, valuesPath)
	}

	for _, setValue := range setValues {
		args = append(args, "--set")
		args = append(args, setValue)
	}

	h.LogEntry.Debugf("Render helm templates for chart '%s' in namespace '%s' ...", chart, namespace)
	stdout, stderr, err := h.Cmd(args...)
	if err != nil {
		return "", fmt.Errorf("helm upgrade failed: %s:\n%s %s", err, stdout, stderr)
	}
	h.LogEntry.Infof("Render helm templates for chart '%s' was successful", chart)

	return stdout, nil
}
