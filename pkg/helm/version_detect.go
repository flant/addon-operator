package helm

import (
	"os"
	"os/exec"
	"regexp"

	"github.com/flant/addon-operator/pkg/helm/helm2"
	"github.com/flant/addon-operator/pkg/helm/helm3"
)

const DefaultHelmBinPath = "helm"

func DetectHelmVersion() (string, error) {
	// Setup default universal helm path
	helmPath := os.Getenv("HELM_BIN_PATH")
	if helmPath == "" {
		helmPath = DefaultHelmBinPath
	}
	helm2.Helm2Path = helmPath
	helm3.Helm3Path = helmPath

	tillerPath := os.Getenv("TILLER_BIN_PATH")
	if tillerPath != "" {
		helm2.TillerPath = tillerPath
	}

	if os.Getenv("HELM3") == "yes" {
		return "v3", nil
	}

	if os.Getenv("HELM2") == "yes" {
		return "v2", nil
	}

	if os.Getenv("HELM3LIB") == "yes" {
		return "v3lib", nil
	}

	// No environments, try to autodetect via execution `helm --help` command.
	// Helm3 has no mentions of "tiller" in help output.
	cmd := exec.Command(helmPath, "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		// no helm3 binary found, rollback to library
		return "v3lib", nil
	}
	tillerRe := regexp.MustCompile(`tiller`)
	tillerLoc := tillerRe.FindIndex(out)
	if tillerLoc != nil {
		return "v2", nil
	}

	// TODO helm4 detection?
	return "v3", nil
}
