package module_manager

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/utils"
	log "github.com/sirupsen/logrus"
)

const (
	PathsSeparator = ":"
	ValuesFileName = "values.yaml"
)

func SearchModules(modulesDirs string) (*ModuleSet, error) {
	paths := splitToPaths(modulesDirs)
	modules := new(ModuleSet)
	for _, path := range paths {
		modulesInDir, err := findModulesInDir(path)
		if err != nil {
			return nil, err
		}

		// Add only "new" modules. Modules from first directories are in top priority as commands in $PATH.
		for _, module := range modulesInDir {
			if !modules.Has(module.Name) {
				modules.Add(module)
			}
		}
	}

	return modules, nil
}

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

func findModulesInDir(modulesDir string) ([]*Module, error) {
	dirEntries, err := os.ReadDir(modulesDir)
	if err != nil && os.IsNotExist(err) {
		return nil, fmt.Errorf("path '%s' does not exist", modulesDir)
	}
	if err != nil {
		return nil, fmt.Errorf("listing modules directory '%s': %s", modulesDir, err)
	}

	modules := make([]*Module, 0)
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

		modules = append(modules, module)
	}

	return modules, nil
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
func moduleFromDirName(dirName string, absPath string) (*Module, error) {
	matchRes := ValidModuleNameRe.FindStringSubmatch(dirName)
	if matchRes == nil {
		return nil, fmt.Errorf("'%s' is invalid name for module: should match regex '%s'", dirName, ValidModuleNameRe.String())
	}
	name := matchRes[ModuleNameIdx]

	// Check if name is consistent for conversions between kebab-case and camelCase.
	valuesKey := utils.ModuleNameToValuesKey(name)
	restoredName := utils.ModuleNameFromValuesKey(valuesKey)
	if name != restoredName {
		return nil, fmt.Errorf("'%s' name should be in kebab-case and be restorable from camelCase: consider renaming to '%s'", name, restoredName)
	}

	module := NewModule(matchRes[ModuleNameIdx],
		absPath,
		parseIntOrDefault(matchRes[ModuleOrderIdx], app.UnnumberedModuleOrder),
	)

	return module, nil
}

func parseIntOrDefault(num string, defaultValue int) int {
	val, err := strconv.ParseInt(num, 10, 31)
	if err != nil {
		return defaultValue
	}
	return int(val)
}

// LoadCommonStaticValues finds values.yaml files in all specified directories.
func LoadCommonStaticValues(modulesDirs string) (utils.Values, error) {
	paths := splitToPaths(modulesDirs)

	res := make(map[string]interface{})
	for _, path := range paths {
		values, err := loadValuesFileFromDir(path)
		if err != nil {
			return nil, err
		}

		for k, v := range values {
			if _, ok := res[k]; !ok {
				res[k] = v
			}
		}
	}

	return res, nil
}

func loadValuesFileFromDir(dir string) (utils.Values, error) {
	valuesFilePath := filepath.Join(dir, ValuesFileName)
	valuesYaml, err := os.ReadFile(valuesFilePath)
	if err != nil && os.IsNotExist(err) {
		log.Debugf("No common static values file '%s': %v", valuesFilePath, err)
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load common values file '%s': %s", valuesFilePath, err)
	}

	values, err := utils.NewValuesFromBytes(valuesYaml)
	if err != nil {
		return nil, err
	}

	return values, nil
}
