package helm3

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kblabels "k8s.io/apimachinery/pkg/labels"
	k8syaml "sigs.k8s.io/yaml"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm/client"
	"github.com/flant/addon-operator/pkg/utils"
	klient "github.com/flant/kube-client/client"
	"github.com/flant/shell-operator/pkg/executor"
)

var Helm3Path = "helm"

type Helm3Options struct {
	Namespace  string
	HistoryMax int32
	KubeClient *klient.Client
	LogEntry   *log.Entry
	Timeout    time.Duration
}

type Helm3Client struct {
	Namespace  string
	HistoryMax int32
	KubeClient *klient.Client
	LogEntry   *log.Entry
	Timeout    time.Duration
}

var _ client.HelmClient = &Helm3Client{}

func NewClient(options *Helm3Options, logLabels ...map[string]string) (client.HelmClient, error) {
	hc := &Helm3Client{
		LogEntry: log.WithField("operator.component", "helm"),
	}
	err := hc.initAndVersion()
	if err != nil {
		return nil, err
	}

	if options.LogEntry == nil {
		options.LogEntry = log.WithField("operator.component", "helm")
	}

	if len(logLabels) > 0 {
		options.LogEntry = options.LogEntry.WithFields(utils.LabelsToLogFields(logLabels[0]))
	}

	return &Helm3Client{
		Namespace:  options.Namespace,
		HistoryMax: options.HistoryMax,
		KubeClient: options.KubeClient,
		LogEntry:   options.LogEntry,
		Timeout:    options.Timeout,
	}, nil
}

func (h *Helm3Client) WithKubeClient(client *klient.Client) {
	h.KubeClient = client
}

// cmd runs Helm binary with specified arguments.
func (h *Helm3Client) cmd(args ...string) (stdout string, stderr string, err error) {
	cmd := exec.Command(Helm3Path, args...)

	var stdoutBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	err = executor.Run(cmd)
	stdout = strings.TrimSpace(stdoutBuf.String())
	stderr = strings.TrimSpace(stderrBuf.String())

	return
}

// initAndVersion runs helm version command.
func (h *Helm3Client) initAndVersion() error {
	stdout, stderr, err := h.cmd("version", "--short")
	if err != nil {
		return fmt.Errorf("unable to get helm version: %v\n%v %v", err, stdout, stderr)
	}
	stdout = strings.Join([]string{stdout, stderr}, "\n")
	stdout = strings.ReplaceAll(stdout, "\n", " ")
	log.Infof("Helm 3 version: %s", stdout)

	return nil
}

// LastReleaseStatus returns last known revision for release and its status
//
//	Example helm history output:
//	REVISION	UPDATED                 	STATUS    	CHART                 	DESCRIPTION
//	1        Fri Jul 14 18:25:00 2017	SUPERSEDED	symfony-demo-0.1.0    	Install complete
func (h *Helm3Client) LastReleaseStatus(releaseName string) (revision string, status string, err error) {
	stdout, stderr, err := h.cmd("history", releaseName, "--max", "1", "--output", "yaml", "--namespace", h.Namespace)
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

	var historyInfo []map[string]string

	err = k8syaml.Unmarshal([]byte(stdout), &historyInfo)
	if err != nil {
		return "", "", fmt.Errorf("helm history returns invalid json: %v", err)
	}
	if len(historyInfo) == 0 {
		return "", "", fmt.Errorf("helm history is empty: '%s'", stdout)
	}
	status, has := historyInfo[0]["status"]
	if !has {
		return "", "", fmt.Errorf("helm history has no 'status' field: '%s'", stdout)
	}
	revision, has = historyInfo[0]["revision"]
	if !has {
		return "", "", fmt.Errorf("helm history has no 'revision' field: '%s'", stdout)
	}
	return
}

func (h *Helm3Client) UpgradeRelease(releaseName string, chart string, valuesPaths []string, setValues []string, namespace string) error {
	args := make([]string, 0)
	args = append(args, "upgrade")
	// releaseName and chart path are positional arguments, put them first.
	args = append(args, releaseName)
	args = append(args, chart)

	// Flags for upgrade command.
	args = append(args, "--install")

	args = append(args, "--history-max")
	args = append(args, fmt.Sprintf("%d", h.HistoryMax))

	args = append(args, "--timeout")
	args = append(args, h.Timeout.String())

	if namespace != "" {
		args = append(args, "--namespace")
		args = append(args, namespace)
		args = append(args, "--create-namespace")
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
	stdout, stderr, err := h.cmd(args...)
	if err != nil {
		return fmt.Errorf("helm upgrade failed: %s:\n%s %s", err, stdout, stderr)
	}
	h.LogEntry.Infof("Helm upgrade for release '%s' with chart '%s' in namespace '%s' successful:\n%s\n%s", releaseName, chart, namespace, stdout, stderr)

	return nil
}

func (h *Helm3Client) GetReleaseValues(releaseName string) (utils.Values, error) {
	args := make([]string, 0)
	args = append(args, "get")
	args = append(args, "values")
	args = append(args, releaseName)

	args = append(args, "--namespace")
	args = append(args, h.Namespace)

	args = append(args, "--output")
	args = append(args, "yaml")

	stdout, stderr, err := h.cmd(args...)
	if err != nil {
		return nil, fmt.Errorf("cannot get values of helm release %s: %s\n%s %s", releaseName, err, stdout, stderr)
	}

	values, err := utils.NewValuesFromBytes([]byte(stdout))
	if err != nil {
		return nil, fmt.Errorf("cannot get values of helm release %s: %s", releaseName, err)
	}

	return values, nil
}

func (h *Helm3Client) DeleteRelease(releaseName string) (err error) {
	h.LogEntry.Debugf("helm release '%s': execute helm uninstall", releaseName)

	args := make([]string, 0)
	args = append(args, "uninstall")
	args = append(args, releaseName)

	args = append(args, "--namespace")
	args = append(args, h.Namespace)

	stdout, stderr, err := h.cmd(args...)
	if err != nil {
		return fmt.Errorf("helm uninstall %s invocation error: %v\n%v %v", releaseName, err, stdout, stderr)
	}

	return
}

func (h *Helm3Client) IsReleaseExists(releaseName string) (bool, error) {
	revision, _, err := h.LastReleaseStatus(releaseName)
	if err != nil && revision == "0" {
		return false, nil
	} else if err != nil {
		return false, err
	}

	return true, nil
}

// ListReleasesNames returns list of release names.
// Names are extracted from label "name" in Secrets with label "owner"=="helm".
func (h *Helm3Client) ListReleasesNames(labelSelector map[string]string) ([]string, error) {
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
		releaseName, hasKey := secret.Labels["name"]
		if hasKey && releaseName != "" {
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
func (h *Helm3Client) Render(releaseName string, chart string, valuesPaths []string, setValues []string, namespace string, debug bool) (string, error) {
	args := make([]string, 0)
	args = append(args, "template")
	args = append(args, releaseName)
	args = append(args, chart)

	if debug {
		args = append(args, "--debug")
	}

	if namespace != "" {
		args = append(args, "--namespace")
		args = append(args, namespace)
		args = append(args, "--create-namespace")
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
	stdout, stderr, err := h.cmd(args...)
	if err != nil {
		return "", fmt.Errorf("helm upgrade failed: %s:\n%s %s", err, stdout, stderr)
	}
	h.LogEntry.Infof("Render helm templates for chart '%s' was successful", chart)

	return stdout, nil
}
