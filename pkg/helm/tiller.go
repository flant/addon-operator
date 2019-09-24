package helm

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/romana/rlog"
)

const TillerPath = "tiller"

// TillerOptions
type TillerOptions struct {
	Namespace string
	HistoryMax int
	ListenAddress string
	ListenPort int32
	ProbeListenAddress string
	ProbeListenPort int32
}

// InitTillerProcess starts tiller as a subprocess. If tiller is exited, addon-operator also exits.
func InitTillerProcess(options TillerOptions) error {
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
		rlog.Errorf("Tiller process not started: %v", err)
		return err
	}

	// Wait for success of "helm version"
	for {
		cliHelm := &CliHelm{}
		stdout, stderr, err := cliHelm.Cmd("version")
		rlog.Debugf("helm version: %s %s", stdout, stderr)
		if err != nil {
			rlog.Errorf("unable to get helm version: %v\n%v %v", err, stdout, stderr)
			time.Sleep(100*time.Millisecond)
		} else {
			rlog.Infof("tiller started and is available")
			break
		}
	}

	go func() {
		err = tillerCmd.Wait()
		if err != nil {
			rlog.Errorf("Tiller process exited, now stop. (%v)", err)
		} else {
			rlog.Errorf("Tiller process exited, now stop.")
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

		body, err := ioutil.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			writer.Write([]byte(fmt.Sprintf("Error reading tiller response: %v", err)))
			return
		}

		// TODO translate response of GET /liveness from tiller probe port
		writer.WriteHeader(http.StatusOK)
		writer.Write(body)
	}
}
