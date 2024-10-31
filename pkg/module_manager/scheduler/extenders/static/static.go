package static

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ettle/strcase"
	"gopkg.in/yaml.v3"

	log "github.com/deckhouse/deckhouse/go_lib/log"
	"github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders"
	"github.com/flant/addon-operator/pkg/utils"
)

const (
	Name extenders.ExtenderName = "Static"
)

type Extender struct {
	modulesStatus map[string]bool
}

func NewExtender(staticValuesFilePaths string) (*Extender, error) {
	result := make(map[string]bool)
	dirs := utils.SplitToPaths(staticValuesFilePaths)
	for _, dir := range dirs {
		valuesFile := filepath.Join(dir, "values.yaml")
		fileInfo, err := os.Stat(valuesFile)
		if err != nil {
			log.Errorf("Couldn't stat %s", valuesFile)
			continue
		}

		if fileInfo.IsDir() {
			log.Errorf("File %s is a directory", valuesFile)
			continue
		}

		f, err := os.Open(valuesFile)
		if err != nil {
			if os.IsNotExist(err) {
				log.Debugf("File %s doesn't exist", valuesFile)
				continue
			}
			return nil, err
		}
		defer f.Close()

		m := make(map[string]interface{})

		err = yaml.NewDecoder(f).Decode(&m)
		if err != nil {
			return nil, err
		}

		for k, v := range m {
			if strings.HasSuffix(k, "Enabled") {
				m := strings.TrimSuffix(k, "Enabled")
				keb := strcase.ToKebab(m)
				result[keb] = v.(bool)
			}
		}
	}

	e := &Extender{
		modulesStatus: result,
	}

	return e, nil
}

func (e *Extender) Name() extenders.ExtenderName {
	return Name
}

func (e *Extender) Filter(moduleName string, _ map[string]string) (*bool, error) {
	if val, found := e.modulesStatus[moduleName]; found {
		return &val, nil
	}

	return nil, nil
}

func (e *Extender) IsTerminator() bool {
	return false
}
