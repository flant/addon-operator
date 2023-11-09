package module_manager

import (
	"fmt"
	"os"
	"path/filepath"
)

// readOpenAPIFiles reads config-values.yaml and values.yaml from the specified directory.
// Global schemas:
//
//	/global/openapi/config-values.yaml
//	/global/openapi/values.yaml
//
// Module schemas:
//
//	/modules/XXX-module-name/openapi/config-values.yaml
//	/modules/XXX-module-name/openapi/values.yaml
func readOpenAPIFiles(openApiDir string) (configSchemaBytes, valuesSchemaBytes []byte, err error) {
	if openApiDir == "" {
		return nil, nil, nil
	}
	if _, err := os.Stat(openApiDir); os.IsNotExist(err) {
		return nil, nil, nil
	}

	configPath := filepath.Join(openApiDir, "config-values.yaml")
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		configSchemaBytes, err = os.ReadFile(configPath)
		if err != nil {
			return nil, nil, fmt.Errorf("read file '%s': %v", configPath, err)
		}
	}

	valuesPath := filepath.Join(openApiDir, "values.yaml")
	if _, err := os.Stat(valuesPath); !os.IsNotExist(err) {
		valuesSchemaBytes, err = os.ReadFile(valuesPath)
		if err != nil {
			return nil, nil, fmt.Errorf("read file '%s': %v", valuesPath, err)
		}
	}

	return
}
