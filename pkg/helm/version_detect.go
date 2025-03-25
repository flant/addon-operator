package helm

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"

	"github.com/flant/addon-operator/pkg/helm/helm3"
)

const (
	DefaultHelmBinPath          = "helm"
	DefaultHelmPostRendererPath = "./post-renderer"
)

var helm2Envs = []string{
	"ADDON_OPERATOR_TILLER_LISTEN_PORT",
	"ADDON_OPERATOR_TILLER_PROBE_LISTEN_PORT",
	"TILLER_MAX_HISTORY",
	"TILLER_BIN_PATH",
	"HELM2",
}

const helm2DeprecationMsg = "Helm2 support is deprecated in Addon-operator v1.1+, please adopt Helm2 releases and use Helm3."

type ClientType string

const (
	Helm3    = ClientType("Helm3")
	Helm3Lib = ClientType("Helm3Lib")
)

func checkPostRenderer(path string) error {
	_, err := os.Stat(path)
	return err
}

func DetectHelmVersion() (ClientType, error) {
	// Detect Helm2 related environment variables.
	for _, env := range helm2Envs {
		if os.Getenv(env) != "" {
			return "", fmt.Errorf("%s environment is found. %s", env, helm2DeprecationMsg)
		}
	}

	// Override helm binary path if requested.
	helmPath := DefaultHelmBinPath
	userHelmPath := os.Getenv("HELM_BIN_PATH")
	if userHelmPath != "" {
		helmPath = userHelmPath
	}
	helmPostRendererPath := DefaultHelmPostRendererPath
	if os.Getenv("HELM_POST_RENDERER_PATH") != "" {
		helmPostRendererPath = os.Getenv("HELM_POST_RENDERER_PATH")
	}
	helm3.Helm3Path = helmPath

	if os.Getenv("HELM3") == "yes" {
		return Helm3, checkPostRenderer(helmPostRendererPath)
	}

	if os.Getenv("HELM3LIB") == "yes" {
		return Helm3Lib, nil
	}

	// No environments, try to autodetect via execution `helm --help` command.
	// Helm3 has no mentions of "tiller" in help output.
	// Note: we rely on help command because 'helm version' is not precise if helm is compiled without setting a version.
	cmd := exec.Command(helmPath, "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if userHelmPath == "" {
			// Use builtin helm if something wrong with the default path.
			return Helm3Lib, nil
		}
		// Report error if HELM_BIN_PATH is set.
		return "", fmt.Errorf("error running requested Helm binary path '%s': %v", userHelmPath, err)
	}
	tillerRe := regexp.MustCompile(`tiller`)
	tillerLoc := tillerRe.FindIndex(out)
	if tillerLoc != nil {
		return "", fmt.Errorf("'%s' is detected as helm2 binary. %s", helmPath, helm2DeprecationMsg)
	}

	// TODO(future) Add helm4 detection here.
	return Helm3Lib, checkPostRenderer(helmPostRendererPath)
}
