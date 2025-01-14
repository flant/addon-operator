package utils

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/deckhouse/deckhouse/pkg/log"
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
func LoadValuesFileFromDir(dir string, strictModeEnabled bool) (Values, error) {
	valuesFilePath := filepath.Join(dir, ValuesFileName)
	valuesYaml, err := os.ReadFile(valuesFilePath)
	if err != nil && os.IsNotExist(err) && !strictModeEnabled {
		log.Debug("No static values file",
			slog.String("path", valuesFilePath),
			log.Err(err))
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
func ReadOpenAPIFiles(openApiDir string) ([]byte /*configValuesBytes*/, []byte /*valuesBytes*/, error) {
	if openApiDir == "" {
		return nil, nil, nil
	}
	if _, err := os.Stat(openApiDir); os.IsNotExist(err) {
		return nil, nil, nil
	}

	configValuesBytes := make([]byte, 0)
	configPath := filepath.Join(openApiDir, ConfigValuesFileName)
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		configValuesBytes, err = os.ReadFile(configPath)
		if err != nil {
			return nil, nil, fmt.Errorf("read file '%s': %w", configPath, err)
		}
	}

	valuesBytes := make([]byte, 0)
	valuesPath := filepath.Join(openApiDir, ValuesFileName)
	if _, err := os.Stat(valuesPath); !os.IsNotExist(err) {
		valuesBytes, err = os.ReadFile(valuesPath)
		if err != nil {
			return nil, nil, fmt.Errorf("read file '%s': %w", valuesPath, err)
		}
	}

	return configValuesBytes, valuesBytes, nil
}
