package helm3

import (
	"bytes"
	"fmt"
	"log/slog"
	"os/exec"
	"sort"
	"strings"
	"time"

	k8syaml "sigs.k8s.io/yaml"

	"github.com/deckhouse/deckhouse/pkg/log"

	"github.com/flant/addon-operator/pkg/helm/client"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/shell-operator/pkg/executor"
)

var Helm3Path = "helm"

type Helm3Options struct {
	Namespace         string
	HistoryMax        int32
	Timeout           time.Duration
	HelmIgnoreRelease string
	Logger            *log.Logger
}

var Options *Helm3Options

// Init runs
func Init(options *Helm3Options) error {
	hc := &Helm3Client{
		Logger:            options.Logger.With("operator.component", "helm"),
		HelmIgnoreRelease: options.HelmIgnoreRelease,
	}
	err := hc.initAndVersion()
	if err != nil {
		return err
	}
	Options = options
	return nil
}

type Helm3Client struct {
	Logger            *log.Logger
	Namespace         string
	HelmIgnoreRelease string
}

var _ client.HelmClient = &Helm3Client{}

func NewClient(logger *log.Logger, logLabels ...map[string]string) client.HelmClient {
	logEntry := logger.With("operator.component", "helm")
	if len(logLabels) > 0 {
		logEntry = utils.EnrichLoggerWithLabels(logEntry, logLabels[0])
	}

	return &Helm3Client{
		Logger:            logEntry,
		Namespace:         Options.Namespace,
		HelmIgnoreRelease: Options.HelmIgnoreRelease,
	}
}

// cmd runs Helm binary with specified arguments.
func (h *Helm3Client) cmd(args ...string) (string /*stdout*/, string /*stderr*/, error) {
	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer

	cmd := exec.Command(Helm3Path, args...)
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := executor.Run(cmd)
	stdout := strings.TrimSpace(stdoutBuf.String())
	stderr := strings.TrimSpace(stderrBuf.String())

	return stdout, stderr, err
}

// initAndVersion runs helm version command.
func (h *Helm3Client) initAndVersion() error {
	stdout, stderr, err := h.cmd("version", "--short")
	if err != nil {
		return fmt.Errorf("unable to get helm version: %v\n%v %v", err, stdout, stderr)
	}
	stdout = strings.Join([]string{stdout, stderr}, "\n")
	stdout = strings.ReplaceAll(stdout, "\n", " ")
	log.Info("Helm 3 version", slog.String("version", stdout))

	return nil
}

// LastReleaseStatus returns last known revision for release and its status
//
//	Example helm history output:
//	REVISION	UPDATED                 	STATUS    	CHART                 	DESCRIPTION
//	1        Fri Jul 14 18:25:00 2017	SUPERSEDED	symfony-demo-0.1.0    	Install complete
func (h *Helm3Client) LastReleaseStatus(releaseName string) (string /*revision*/, string /*status*/, error) {
	stdout, stderr, err := h.cmd(
		"history", releaseName,
		"--namespace", h.Namespace,
		"--max", "1",
		"--output", "yaml",
	)
	if err != nil {
		errLine := strings.Split(stderr, "\n")[0]
		if strings.Contains(errLine, "Error:") && strings.Contains(errLine, "not found") {
			// Bad module name or no releases installed
			return "0", "", fmt.Errorf("release '%s' not found\n%s %s", releaseName, stdout, stderr)
		}

		return "", "", fmt.Errorf("cannot get history for release '%s'\n%s %s", releaseName, stdout, stderr)
	}

	historyInfo := make([]map[string]string, 0)
	err = k8syaml.Unmarshal([]byte(stdout), &historyInfo)
	if err != nil {
		return "", "", fmt.Errorf("helm history returns invalid json: %w", err)
	}

	if len(historyInfo) == 0 {
		return "", "", fmt.Errorf("helm history is empty: '%s'", stdout)
	}

	status, has := historyInfo[0]["status"]
	if !has {
		return "", "", fmt.Errorf("helm history has no 'status' field: '%s'", stdout)
	}

	revision, has := historyInfo[0]["revision"]
	if !has {
		return "", "", fmt.Errorf("helm history has no 'revision' field: '%s'", stdout)
	}

	return revision, status, nil
}

func (h *Helm3Client) UpgradeRelease(releaseName string, chart string, valuesPaths []string, setValues []string, namespace string) error {
	args := []string{
		"upgrade", releaseName, chart,
		"--install",
		"--skip-crds",
		"--history-max", fmt.Sprintf("%d", Options.HistoryMax),
		"--timeout", Options.Timeout.String(),
		"--post-renderer", "./post-renderer",
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

	h.Logger.Info("Running helm upgrade for release ...",
		slog.String("release", releaseName),
		slog.String("chart", chart),
		slog.String("namespace", namespace))
	stdout, stderr, err := h.cmd(args...)
	if err != nil {
		return fmt.Errorf("helm upgrade failed: %s:\n%s %s", err, stdout, stderr)
	}
	h.Logger.Info("Helm upgrade for release successful",
		slog.String("release", releaseName),
		slog.String("chart", chart),
		slog.String("namespace", namespace),
		slog.String("stdout", stdout),
		slog.String("stderr", stderr))

	return nil
}

func (h *Helm3Client) GetReleaseValues(releaseName string) (utils.Values, error) {
	args := []string{
		"get", "values", releaseName,
		"--namespace", h.Namespace,
		"--output", "yaml",
	}
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

func (h *Helm3Client) DeleteRelease(releaseName string) error {
	h.Logger.Debug("helm release: execute helm uninstall", slog.String("release", releaseName))

	args := []string{
		"uninstall", releaseName,
		"--namespace", h.Namespace,
	}
	stdout, stderr, err := h.cmd(args...)
	if err != nil {
		return fmt.Errorf("helm uninstall %s invocation error: %w\n%v %v", releaseName, err, stdout, stderr)
	}

	h.Logger.Debug("helm release deleted", slog.String("release", releaseName))

	return nil
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
func (h *Helm3Client) ListReleasesNames() ([]string, error) {
	args := []string{
		"list", "--all",
		"--namespace", h.Namespace,
		"--output", "yaml",
	}

	stdout, stderr, err := h.cmd(args...)
	if err != nil {
		return nil, fmt.Errorf("helm list failed: %v\n%s %s", err, stdout, stderr)
	}

	var list []struct {
		Name string `json:"name"`
	}

	if err := k8syaml.Unmarshal([]byte(stdout), &list); err != nil {
		return nil, fmt.Errorf("helm list returned invalid json: %v", err)
	}

	releases := make([]string, 0, len(list))
	for _, release := range list {
		// Do not return ignored release or empty string.
		if release.Name == h.HelmIgnoreRelease || release.Name == "" {
			continue
		}

		releases = append(releases, release.Name)
	}

	sort.Strings(releases)
	return releases, nil
}

// Render renders helm templates for chart
func (h *Helm3Client) Render(releaseName string, chart string, valuesPaths []string, setValues []string, namespace string, debug bool) (string, error) {
	args := []string{
		"template", releaseName, chart,
		"--post-renderer", "./post-renderer",
	}

	if debug {
		args = append(args, "--debug")
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

	h.Logger.Debug("Render helm templates for chart ...",
		slog.String("chart", chart),
		slog.String("namespace", namespace))
	stdout, stderr, err := h.cmd(args...)
	if err != nil {
		return "", fmt.Errorf("helm upgrade failed: %s:\n%s %s", err, stdout, stderr)
	}
	h.Logger.Info("Render helm templates for chart was successful",
		slog.String("chart", chart))

	return stdout, nil
}
