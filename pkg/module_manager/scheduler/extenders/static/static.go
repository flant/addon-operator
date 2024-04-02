package static

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/ettle/strcase"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"

	"github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders"
	"github.com/flant/addon-operator/pkg/module_manager/scheduler/node"
	"github.com/flant/addon-operator/pkg/utils"
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

func (e Extender) Dump() map[string]bool {
	return e.modulesStatus
}

func (e Extender) Name() extenders.ExtenderName {
	return extenders.StaticExtender
}

func (e Extender) IsShutter() bool {
	return false
}

func (e Extender) Filter(module node.ModuleInterface) (*bool, error) {
	if val, found := e.modulesStatus[module.GetName()]; found {
		return &val, nil
	}

	return nil, nil
}

func (e *Extender) IsNotifier() bool {
	return false
}

func (e *Extender) SetNotifyChannel(_ context.Context, _ chan extenders.ExtenderEvent) {
}

func (e Extender) Order() {
}

func (e *Extender) Reset() {
}
