package fs

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
	"github.com/flant/addon-operator/pkg/values/validation"

	log "github.com/sirupsen/logrus"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/utils"
)

const (
	PathsSeparator = ":"
	ValuesFileName = "values.yaml"
)

type FileSystemLoader struct {
	dirs []string

	valuesValidator *validation.ValuesValidator
}

func NewFileSystemLoader(moduleDirs string, vv *validation.ValuesValidator) *FileSystemLoader {
	return &FileSystemLoader{
		dirs:            splitToPaths(moduleDirs),
		valuesValidator: vv,
	}
}

func (fl *FileSystemLoader) LoadModules() ([]*modules.BasicModule, error) {
	result := make([]*modules.BasicModule, 0)

	for _, dir := range fl.dirs {
		commonStaticValues, err := loadValuesFileFromDir(dir)
		if err != nil {
			return nil, err
		}

		modDefs, err := fl.findModulesInDir(dir)
		if err != nil {
			return nil, err
		}

		for _, module := range modDefs {
			err = validateModuleName(module.Name)
			if err != nil {
				return nil, err
			}

			valuesModuleName := utils.ModuleNameToValuesKey(module.Name)
			moduleEnabled := false
			initialValues := utils.Values{valuesModuleName: map[string]interface{}{}}
			// build initial values
			// 1. from common static values
			if commonStaticValues.HasKey(valuesModuleName) {
				initialValues = utils.MergeValues(initialValues, commonStaticValues)
			}
			if commonStaticValues.HasKey(valuesModuleName + "Enabled") {
				moduleEnabled = true
			}

			// 2. from module static values
			moduleStaticValues, err := loadValuesFileFromDir(module.Path)
			if err != nil {
				return nil, err
			}

			if moduleStaticValues != nil {
				initialValues = utils.MergeValues(initialValues, moduleStaticValues)
				if moduleStaticValues.HasKey(valuesModuleName + "Enabled") {
					moduleEnabled = true
				}
			}

			// 3. from openapi defaults
			//TODO(yalosev): think about openapi
			cb, vb, err := fl.readOpenAPIFiles(filepath.Join(module.Path, "openapi"))
			if err != nil {
				return nil, err
			}

			if cb != nil && vb != nil {
				err = fl.valuesValidator.SchemaStorage.AddModuleValuesSchemas(valuesModuleName, cb, vb)
				if err != nil {
					return nil, err
				}

				//s := fl.valuesValidator.SchemaStorage.ModuleValuesSchema(valuesModuleName, validation.ModuleSchema)
				//if s != nil {
				//	validation.ApplyDefaults(initialValues, s)
				//}
				//
				//if moduleEnabled {
				//	// we don't need to validate values for disabled modules
				//	err = fl.valuesValidator.ValidateModuleValues(valuesModuleName, initialValues)
				//	if err != nil {
				//		return nil, fmt.Errorf("validation failed: %w", err)
				//	}
				//}
			}

			//
			moduleValues, ok := initialValues[valuesModuleName].(map[string]interface{})
			if !ok {
				// TODO: think about
				fmt.Println(valuesModuleName, moduleValues)
				panic("Here")
			}

			vs := modules.NewValuesStorage(moduleValues, fl.valuesValidator)
			bm := modules.NewBasicModule(module.Name, module.Path, module.Order, moduleEnabled, vs, fl.valuesValidator)

			result = append(result, bm)
		}
	}

	return result, nil
}

type moduleDefinition struct {
	Name  string
	Path  string
	Order uint32
}

//func (fl *FileSystemLoader) searchModules(modulesDirs string) (*moduleset.ModulesSet, error) {
//	paths := splitToPaths(modulesDirs)
//	mset := new(moduleset.ModulesSet)
//	for _, path := range paths {
//		modulesInDir, err := fl.findModulesInDir(path)
//		if err != nil {
//			return nil, err
//		}
//
//		// Add only "new" modules. Modules from first directories are in top priority as commands in $PATH.
//		for _, module := range modulesInDir {
//			if !mset.Has(module.Name) {
//				mset.Add(module)
//			}
//		}
//	}
//
//	return mset, nil
//}

func splitToPaths(dir string) []string {
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

func (fl *FileSystemLoader) findModulesInDir(modulesDir string) ([]moduleDefinition, error) {
	dirEntries, err := os.ReadDir(modulesDir)
	if err != nil && os.IsNotExist(err) {
		return nil, fmt.Errorf("path '%s' does not exist", modulesDir)
	}
	if err != nil {
		return nil, fmt.Errorf("listing modules directory '%s': %s", modulesDir, err)
	}

	mods := make([]moduleDefinition, 0)
	for _, dirEntry := range dirEntries {
		name, absPath, err := resolveDirEntry(modulesDir, dirEntry)
		if err != nil {
			return nil, err
		}
		// Skip non-directories.
		if name == "" {
			continue
		}

		module, err := moduleFromDirName(name, absPath)
		if err != nil {
			return nil, err
		}

		mods = append(mods, module)
	}

	return mods, nil
}

func resolveDirEntry(dirPath string, entry os.DirEntry) (string, string, error) {
	name := entry.Name()
	absPath := filepath.Join(dirPath, name)

	if entry.IsDir() {
		return name, absPath, nil
	}
	// Check if entry is a symlink to a directory.
	targetPath, err := resolveSymlinkToDir(dirPath, entry)
	if err != nil {
		if e, ok := err.(*fs.PathError); ok {
			if e.Err.Error() == "no such file or directory" {
				log.Warnf("Symlink target %q does not exist. Ignoring module", dirPath)
				return "", "", nil
			}
		}

		return "", "", fmt.Errorf("resolve '%s' as a possible symlink: %v", absPath, err)
	}

	if targetPath != "" {
		return name, targetPath, nil
	}

	if name != ValuesFileName {
		log.Warnf("Ignore '%s' while searching for modules", absPath)
	}
	return "", "", nil
}

func resolveSymlinkToDir(dirPath string, entry os.DirEntry) (string, error) {
	info, err := entry.Info()
	if err != nil {
		return "", err
	}
	targetDirPath, isTargetDir, err := utils.SymlinkInfo(filepath.Join(dirPath, info.Name()), info)
	if err != nil {
		return "", err
	}

	if isTargetDir {
		return targetDirPath, nil
	}

	return "", nil
}

// ValidModuleNameRe defines a valid module name. It may have a number prefix: it is an order of the module.
var ValidModuleNameRe = regexp.MustCompile(`^(([0-9]+)-)?(.+)$`)

const (
	ModuleOrderIdx = 2
	ModuleNameIdx  = 3
)

// moduleFromDirName returns Module instance filled with name, order and its absolute path.
func moduleFromDirName(dirName string, absPath string) (moduleDefinition, error) {
	matchRes := ValidModuleNameRe.FindStringSubmatch(dirName)
	if matchRes == nil {
		return moduleDefinition{}, fmt.Errorf("'%s' is invalid name for module: should match regex '%s'", dirName, ValidModuleNameRe.String())
	}

	return moduleDefinition{
		Name:  matchRes[ModuleNameIdx],
		Path:  absPath,
		Order: parseUintOrDefault(matchRes[ModuleOrderIdx], uint32(app.UnnumberedModuleOrder)),
	}, nil
}

// NewModuleWithNameValidation creates module with the name validation: kebab-case and camelCase should be compatible
func validateModuleName(name string) error {
	// Check if name is consistent for conversions between kebab-case and camelCase.
	valuesKey := utils.ModuleNameToValuesKey(name)
	restoredName := utils.ModuleNameFromValuesKey(valuesKey)

	if name != restoredName {
		return fmt.Errorf("'%s' name should be in kebab-case and be restorable from camelCase: consider renaming to '%s'", name, restoredName)
	}

	return nil
}

func parseUintOrDefault(num string, defaultValue uint32) uint32 {
	val, err := strconv.ParseUint(num, 10, 31)
	if err != nil {
		return defaultValue
	}
	return uint32(val)
}

// finds values.yaml files in the specified directory.
func loadValuesFileFromDir(dir string) (utils.Values, error) {
	valuesFilePath := filepath.Join(dir, ValuesFileName)
	valuesYaml, err := os.ReadFile(valuesFilePath)
	if err != nil && os.IsNotExist(err) && !app.StrictModeEnabled {
		log.Debugf("No static values file '%s': %v", valuesFilePath, err)
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load values file '%s': %s", valuesFilePath, err)
	}

	values, err := utils.NewValuesFromBytes(valuesYaml)
	if err != nil {
		return nil, err
	}

	return values, nil
}

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
func (fl *FileSystemLoader) readOpenAPIFiles(openApiDir string) (configValuesBytes, valuesBytes []byte, err error) {
	if openApiDir == "" {
		return nil, nil, nil
	}
	if _, err := os.Stat(openApiDir); os.IsNotExist(err) {
		return nil, nil, nil
	}

	configPath := filepath.Join(openApiDir, "config-values.yaml")
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		configValuesBytes, err = os.ReadFile(configPath)
		if err != nil {
			return nil, nil, fmt.Errorf("read file '%s': %v", configPath, err)
		}
	}

	valuesPath := filepath.Join(openApiDir, "values.yaml")
	if _, err := os.Stat(valuesPath); !os.IsNotExist(err) {
		valuesBytes, err = os.ReadFile(valuesPath)
		if err != nil {
			return nil, nil, fmt.Errorf("read file '%s': %v", valuesPath, err)
		}
	}

	return
}
