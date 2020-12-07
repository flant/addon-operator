package module_manager

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	log "github.com/sirupsen/logrus"
)

func CreateEmptyWritableFile(filePath string) error {
	file, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		return nil
	}

	_ = file.Close()
	return nil
}

//  /global/openapi/config-values.yaml
//  /global/openapi/values.yaml
//  /modules/XXXX/openapi/config-values.yaml
//  /modules/XXXX/openapi/values.yaml
func ReadOpenAPISchemas(openApiDir string) (configSchemaBytes, valuesSchemaBytes []byte, err error) {
	log.Debugf("Check openAPI schemas in '%s'", openApiDir)
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
		log.Infof("Read '%s' OpenAPI schema for config values", configPath)
	}

	valuesPath := filepath.Join(openApiDir, "values.yaml")
	if _, err := os.Stat(valuesPath); !os.IsNotExist(err) {
		valuesSchemaBytes, err = ioutil.ReadFile(valuesPath)
		if err != nil {
			return nil, nil, fmt.Errorf("read file '%s': %v", valuesPath, err)
		}
		log.Infof("Read '%s' OpenAPI schema for values", valuesPath)
	}

	return
}
