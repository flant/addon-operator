package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	log "github.com/flant/shell-operator/pkg/unilogger"

	"github.com/flant/addon-operator/pkg/app"
)

const (
	PathsSeparator       = ":"
	ValuesFileName       = "values.yaml"
	ConfigValuesFileName = "config-values.yaml"
)

// SplitToPaths split concatenated dirs to the array
func SplitToPaths(dir string) []string {
	res := make([]string, 0)
	paths := strings.Split(dir, PathsSeparator)
	for _, path := range paths {
		if path == "" {
			continue
		}
		res = append(res, path)
	}
	return res
}

// LoadValuesFileFromDir finds and parses values.yaml files in the specified directory.
func LoadValuesFileFromDir(dir string) (Values, error) {
	valuesFilePath := filepath.Join(dir, ValuesFileName)
	valuesYaml, err := os.ReadFile(valuesFilePath)
	if err != nil && os.IsNotExist(err) && !app.StrictModeEnabled {
		log.Debugf("No static values file '%s': %v", valuesFilePath, err)
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load values file '%s': %s", valuesFilePath, err)
	}

	values, err := NewValuesFromBytes(valuesYaml)
	if err != nil {
		return nil, err
	}

	return values, nil
}

// ReadOpenAPIFiles reads config-values.yaml and values.yaml from the specified directory.
// Global schemas:
//
//	/global/openapi/config-values.yaml
//	/global/openapi/values.yaml
//
// Module schemas:
//
//	/modules/XXX-module-name/openapi/config-values.yaml
//	/modules/XXX-module-name/openapi/values.yaml
func ReadOpenAPIFiles(openApiDir string) (configValuesBytes, valuesBytes []byte, err error) {
	if openApiDir == "" {
		return nil, nil, nil
	}
	if _, err := os.Stat(openApiDir); os.IsNotExist(err) {
		return nil, nil, nil
	}

	configPath := filepath.Join(openApiDir, ConfigValuesFileName)
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		configValuesBytes, err = os.ReadFile(configPath)
		if err != nil {
			return nil, nil, fmt.Errorf("read file '%s': %v", configPath, err)
		}
	}

	valuesPath := filepath.Join(openApiDir, ValuesFileName)
	if _, err := os.Stat(valuesPath); !os.IsNotExist(err) {
		valuesBytes, err = os.ReadFile(valuesPath)
		if err != nil {
			return nil, nil, fmt.Errorf("read file '%s': %v", valuesPath, err)
		}
	}

	return
}
