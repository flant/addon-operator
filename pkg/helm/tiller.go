package helm

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/flant/shell-operator/pkg/utils/labels"
)

const TillerPath = "tiller"
const TillerWaitTimeoutSeconds = 2
const TillerWaitRetryDelay = 250 * time.Millisecond
const TillerWaitTimeout = 10 * time.Second // Should not be less than TillerWaitTimeoutSeconds + TillerWaitRetryDelay

// TillerOptions
type TillerOptions struct {
	Namespace          string
	HistoryMax         int32
	ListenAddress      string
	ListenPort         int32
	ProbeListenAddress string
	ProbeListenPort    int32
}

var TillerConnectAddress string
var TillerProbeConnectAddress string

// InitTillerProcess starts tiller as a subprocess. If tiller is exited, addon-operator also exits.
func InitTillerProcess(options TillerOptions) error {
	logLabels := map[string]string{
		"operator.component": "tiller",
	}

	logEntry := log.WithFields(utils.LabelsToLogFields(logLabels))

	listenAddr := fmt.Sprintf("%s:%d", options.ListenAddress, options.ListenPort)
	probeListenAddr := fmt.Sprintf("%s:%d", options.ProbeListenAddress, options.ProbeListenPort)

	err := CheckListenAddresses(listenAddr, probeListenAddr)
	if err != nil {
		newListenAddr, newProbeListenAddr, newErr := GetOpenPortsPair(listenAddr, probeListenAddr)
		if newErr != nil {
			return fmt.Errorf("choose open ports for tiller: %v", err)
		}
		listenAddr = newListenAddr
		probeListenAddr = newProbeListenAddr
	}

	env := []string{
		fmt.Sprintf("TILLER_NAMESPACE=%s", options.Namespace),
		fmt.Sprintf("TILLER_HISTORY_MAX=%d", options.HistoryMax),
	}

	args := []string{
		"-listen",
		listenAddr,
		"-probe-listen",
		probeListenAddr,
	}

	tillerCmd := exec.Command(TillerPath, args...)
	tillerCmd.Env = append(os.Environ(), env...)
	tillerCmd.Dir = "/"

	err = StartAndLogLines(tillerCmd, logLabels)
	if err != nil {
		return fmt.Errorf("start tiller subprocess with -listen=%s -probeListen=%s: %v", listenAddr, probeListenAddr, err)
	}

	// save connect address to use with helm
	TillerConnectAddress = listenAddr
	TillerProbeConnectAddress = probeListenAddr
	logEntry.Infof("Tiller starts with -listen=%s -probeListen=%s", listenAddr, probeListenAddr)

	// Start tiller subprocess monitor to exit main process when tiller is exited.
	go func() {
		err := tillerCmd.Wait()
		TillerExitHandler(err, logEntry)
	}()

	return WaitTillerReady(listenAddr, logEntry)
}

// WaitTillerReady retries helm version command until success.
func WaitTillerReady(addr string, logEntry *log.Entry) error {
	tillerWaitRetries := int(TillerWaitTimeout / ((TillerWaitTimeoutSeconds * time.Second) + TillerWaitRetryDelay))

	retries := 0
	for {
		cliHelm := &helmClient{}
		stdout, stderr, err := cliHelm.Cmd("version", "--tiller-connection-timeout", fmt.Sprintf("%d", TillerWaitTimeoutSeconds))

		if err != nil {
			// log stdout and stderr as fields. Json formatter will escape multilines.
			logEntry.WithField("stdout", stdout).
				WithField("stderr", stderr).
				Warnf("Unable to get tiller version: %v", err)
			time.Sleep(TillerWaitRetryDelay)
		} else {
			logEntry.WithField("stdout", stdout).
				WithField("stderr", stderr).
				Debugf("Output of helm version")
			logEntry.Infof("Tiller started and is available")
			break
		}
		retries += 1
		if retries > tillerWaitRetries {
			return fmt.Errorf("wait tiller timeout: wait more than %s", TillerWaitTimeout.String())
		}
	}

	return nil
}

func TillerExitHandler(err error, logEntry *log.Entry) {
	if err != nil {
		logEntry.Errorf("Tiller process exited, now stop. Wait error: %v", err)
	} else {
		logEntry.Errorf("Tiller process exited, now stop.")
	}
	os.Exit(1)
}

// TillerHealthHandler translates tiller's /liveness response
func TillerHealthHandler() func(writer http.ResponseWriter, request *http.Request) {
	return func(writer http.ResponseWriter, request *http.Request) {
		if TillerProbeConnectAddress == "" {
			writer.WriteHeader(http.StatusInternalServerError)
			_, _ = writer.Write([]byte(fmt.Sprintf("Error request tiller: not started yet.")))
			return
		}

		tillerUrl := fmt.Sprintf("http://%s/liveness", TillerProbeConnectAddress)
		res, err := http.Get(tillerUrl)
		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			_, _ = writer.Write([]byte(fmt.Sprintf("Error request tiller: %v", err)))
			return
		}

		tillerLivenessBody, err := ioutil.ReadAll(res.Body)
		_ = res.Body.Close()
		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			_, _ = writer.Write([]byte(fmt.Sprintf("Error reading tiller response: %v", err)))
			return
		}

		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write(tillerLivenessBody)
	}
}

// StartAndLogLines is a non-blocking version of RunAndLogLines in shell-operator/pkg/executor.
func StartAndLogLines(cmd *exec.Cmd, logLabels map[string]string) error {
	logEntry := log.WithFields(utils.LabelsToLogFields(logLabels))
	logEntry.Debugf("Executing command '%s' in '%s' dir", strings.Join(cmd.Args, " "), cmd.Dir)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	err = cmd.Start()
	if err != nil {
		return err
	}

	// read and log stdout lines
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			logEntry.Info(scanner.Text())
		}
	}()
	// read and log stderr lines
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			logEntry.Info(scanner.Text())
		}
	}()
	return nil
}

// CheckListenAddresses tries to listen on two addresses.
// Returns error if ports are already opened by another process.
func CheckListenAddresses(lAddr1, lAddr2 string) error {
	l1, err := net.Listen("tcp", lAddr1)
	if err != nil {
		return err
	}
	defer l1.Close()

	l2, err := net.Listen("tcp", lAddr2)
	if err != nil {
		return err
	}
	defer l2.Close()

	return nil
}

// GetOpenPortsPair determine two ports ready for listen and randomly selected by OS.
func GetOpenPortsPair(lAddr1, lAddr2 string) (string, string, error) {
	tcpAddr1, err := net.ResolveTCPAddr("tcp", lAddr1)
	if err != nil {
		return "", "", err
	}

	lAddr1Random := fmt.Sprintf("%s:0", tcpAddr1.IP)
	l1, err := net.Listen("tcp", lAddr1Random)
	if err != nil {
		return "", "", err
	}
	defer l1.Close()

	tcpAddr2, err := net.ResolveTCPAddr("tcp", lAddr2)
	if err != nil {
		return "", "", err
	}

	lAddr2Random := fmt.Sprintf("%s:0", tcpAddr2.IP)
	l2, err := net.Listen("tcp", lAddr2Random)
	if err != nil {
		return "", "", err
	}
	defer l2.Close()

	return l1.Addr().String(), l2.Addr().String(), nil
}
