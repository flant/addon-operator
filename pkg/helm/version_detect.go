package helm

import (
	"fmt"
	"os"
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
	Helm3Lib = ClientType("Helm3Lib")
	Nelm     = ClientType("Nelm")
)

func DetectHelmVersion() (ClientType, error) {
	// Detect Helm2 related environment variables.
	for _, env := range helm2Envs {
		if os.Getenv(env) != "" {
			return "", fmt.Errorf("%s environment is found. %s", env, helm2DeprecationMsg)
		}
	}

	// Check if Nelm client is explicitly requested
	if os.Getenv("USE_NELM") == "true" {
		return Nelm, nil
	}

	// Use Helm3Lib by default
	return Helm3Lib, nil
}
