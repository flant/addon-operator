package fs

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/deckhouse/deckhouse/pkg/log"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
	"github.com/flant/addon-operator/pkg/utils"
)

type FileSystemLoader struct {
	dirs []string

	logger *log.Logger
}

func NewFileSystemLoader(moduleDirs string, logger *log.Logger) *FileSystemLoader {
	return &FileSystemLoader{
		dirs:   utils.SplitToPaths(moduleDirs),
		logger: logger,
	}
}

func (fl *FileSystemLoader) getBasicModule(definition moduleDefinition, commonStaticValues utils.Values) (*modules.BasicModule, error) {
	err := validateModuleName(definition.Name)
	if err != nil {
		return nil, err
	}

	valuesModuleName := utils.ModuleNameToValuesKey(definition.Name)
	initialValues := utils.Values{valuesModuleName: map[string]interface{}{}}
	// build initial values
	// 1. from common static values
	if commonStaticValues.HasKey(valuesModuleName) {
		initialValues = utils.MergeValues(initialValues, commonStaticValues)
	}

	// 2. from module static values
	moduleStaticValues, err := utils.LoadValuesFileFromDir(definition.Path)
	if err != nil {
		return nil, err
	}

	if moduleStaticValues != nil {
		initialValues = utils.MergeValues(initialValues, moduleStaticValues)
	}

	// 3. from openapi defaults
	cb, vb, err := fl.readOpenAPIFiles(filepath.Join(definition.Path, "openapi"))
	if err != nil {
		return nil, err
	}

	moduleValues, ok := initialValues[valuesModuleName].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("expect map[string]interface{} in module values")
	}

	m, err := modules.NewBasicModule(definition.Name, definition.Path, definition.Order, moduleValues, cb, vb, fl.logger.Named("basic-module"))
	if err != nil {
		return nil, fmt.Errorf("new basic module: %w", err)
	}

	return m, nil
}

// reads single directory and returns BasicModule
func (fl *FileSystemLoader) LoadModule(_, modulePath string) (*modules.BasicModule, error) {
	// the module's parent directory
	var modulesDir string
	if strings.HasSuffix(modulePath, "/") {
		modulesDir = filepath.Dir(strings.TrimRight(modulePath, "/"))
	} else {
		modulesDir = filepath.Dir(modulePath)
	}

	commonStaticValues, err := utils.LoadValuesFileFromDir(modulesDir)
	if err != nil {
		return nil, err
	}

	_, err = readDir(modulePath)
	if err != nil {
		return nil, err
	}

	modDef, err := moduleFromDirName(filepath.Base(modulePath), modulePath)
	if err != nil {
		return nil, err
	}

	bm, err := fl.getBasicModule(modDef, commonStaticValues)
	if err != nil {
		return nil, err
	}

	return bm, nil
}

func (fl *FileSystemLoader) LoadModules() ([]*modules.BasicModule, error) {
	result := make([]*modules.BasicModule, 0)

	for _, dir := range fl.dirs {
		commonStaticValues, err := utils.LoadValuesFileFromDir(dir)
		if err != nil {
			return nil, err
		}

		modDefs, err := fl.findModulesInDir(dir)
		if err != nil {
			return nil, err
		}

		for _, module := range modDefs {
			bm, err := fl.getBasicModule(module, commonStaticValues)
			if err != nil {
				return nil, err
			}
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

// checks if dir exists and returns entries
func readDir(modulesDir string) ([]os.DirEntry, error) {
	dirEntries, err := os.ReadDir(modulesDir)
	if err != nil && os.IsNotExist(err) {
		return nil, fmt.Errorf("path '%s' does not exist", modulesDir)
	}
	if err != nil {
		return nil, fmt.Errorf("listing modules directory '%s': %s", modulesDir, err)
	}

	return dirEntries, nil
}

func (fl *FileSystemLoader) findModulesInDir(modulesDir string) ([]moduleDefinition, error) {
	dirEntries, err := readDir(modulesDir)
	if err != nil {
		return nil, err
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

	if name != utils.ValuesFileName {
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
	return utils.ReadOpenAPIFiles(openApiDir)
}
