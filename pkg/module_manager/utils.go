package module_manager

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
)

func CreateEmptyWritableFile(filePath string) error {
	file, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		return nil
	}

	_ = file.Close()
	return nil
}

// ReadOpenAPIFiles reads config-values.yaml and values.yaml from the specified directory.
// Global schemas:
//  /global/openapi/config-values.yaml
//  /global/openapi/values.yaml
// Module schemas:
//  /modules/XXX-module-name/openapi/config-values.yaml
//  /modules/XXX-module-name/openapi/values.yaml
func ReadOpenAPIFiles(openApiDir string) (configSchemaBytes, valuesSchemaBytes []byte, err error) {
	if openApiDir == "" {
		return nil, nil, nil
	}
	if _, err := os.Stat(openApiDir); os.IsNotExist(err) {
		return nil, nil, nil
	}

	configPath := filepath.Join(openApiDir, "config-values.yaml")
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		configSchemaBytes, err = ioutil.ReadFile(configPath)
		if err != nil {
			return nil, nil, fmt.Errorf("read file '%s': %v", configPath, err)
		}
	}

	valuesPath := filepath.Join(openApiDir, "values.yaml")
	if _, err := os.Stat(valuesPath); !os.IsNotExist(err) {
		valuesSchemaBytes, err = ioutil.ReadFile(valuesPath)
		if err != nil {
			return nil, nil, fmt.Errorf("read file '%s': %v", valuesPath, err)
		}
	}

	return
}
