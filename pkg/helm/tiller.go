package helm

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"time"

	log "github.com/sirupsen/logrus"
)

const TillerPath = "tiller"
const MaxHelmVersionWaits = 32

// TillerOptions
type TillerOptions struct {
	Namespace          string
	HistoryMax         int
	ListenAddress      string
	ListenPort         int32
	ProbeListenAddress string
	ProbeListenPort    int32
}

// InitTillerProcess starts tiller as a subprocess. If tiller is exited, addon-operator also exits.
func InitTillerProcess(options TillerOptions) error {
	logEntry := log.WithField("operator.component", "tiller")

	env := []string{
		fmt.Sprintf("TILLER_NAMESPACE=%s", options.Namespace),
		fmt.Sprintf("TILLER_HISTORY_MAX=%d", options.HistoryMax),
	}

	args := []string{
		"-listen",
		fmt.Sprintf("%s:%d", options.ListenAddress, options.ListenPort),
		"-probe-listen",
		fmt.Sprintf("%s:%d", options.ProbeListenAddress, options.ProbeListenPort),
	}

	tillerCmd := exec.Command(TillerPath, args...)
	tillerCmd.Env = append(os.Environ(), env...)
	tillerCmd.Dir = "/"

	err := tillerCmd.Start()
	if err != nil {
		return fmt.Errorf("start tiller subprocess: %v", err)
	}

	// Wait for success of "helm version"
	helmCounter := 0
	for {
		cliHelm := &helmClient{}
		stdout, stderr, err := cliHelm.Cmd("version")

		if err != nil {
			// log stdout and stderr as fields. Json formatter will escape multilines.
			logEntry.WithField("stdout", stdout).
				WithField("stderr", stderr).
				Warnf("Unable to get tiller version: %v", err)
			time.Sleep(250 * time.Millisecond)
		} else {
			logEntry.WithField("stdout", stdout).
				WithField("stderr", stderr).
				Debugf("Output of helm version")
			logEntry.Infof("Tiller started and is available")
			break
		}
		helmCounter += 1
		if helmCounter > MaxHelmVersionWaits {
			return fmt.Errorf("wait tiller timeout")
		}
	}

	go func() {
		err = tillerCmd.Wait()
		if err != nil {
			logEntry.Errorf("Tiller process exited, now stop. Wait error: %v", err)
		} else {
			logEntry.Errorf("Tiller process exited, now stop.")
		}
		os.Exit(1)
	}()

	return nil
}

// TillerHealthHandler translates tiller's /liveness response
func TillerHealthHandler(tillerProbeAddress string, tillerProbePort int32) func(writer http.ResponseWriter, request *http.Request) {
	return func(writer http.ResponseWriter, request *http.Request) {
		tillerUrl := fmt.Sprintf("http://%s:%d/liveness", tillerProbeAddress, tillerProbePort)
		res, err := http.Get(tillerUrl)
		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			writer.Write([]byte(fmt.Sprintf("Error request tiller: %v", err)))
			return
		}

		tillerLivenessBody, err := ioutil.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			writer.Write([]byte(fmt.Sprintf("Error reading tiller response: %v", err)))
			return
		}

		writer.WriteHeader(http.StatusOK)
		writer.Write(tillerLivenessBody)
	}
}
