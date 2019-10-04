package module_manager

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kennygrant/sanitize"
	"github.com/otiai10/copy"
	"github.com/romana/rlog"
	"gopkg.in/yaml.v2"

	"github.com/flant/shell-operator/pkg/executor"
	hook2 "github.com/flant/shell-operator/pkg/hook"
	utils_file "github.com/flant/shell-operator/pkg/utils/file"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/utils"
)

type Module struct {
	Name          string
	DirectoryName string
	Path          string
	// module values from modules/values.yaml file
	CommonStaticConfig *utils.ModuleConfig
	// module values from modules/<module name>/values.yaml
	StaticConfig  *utils.ModuleConfig

	moduleManager *MainModuleManager
}

func NewModule(mm *MainModuleManager) *Module {
	module := &Module{}
	module.moduleManager = mm
	return module
}

func (m *Module) SafeName() string {
	return sanitize.BaseName(m.Name)
}

// Run is a phase of module lifecycle that runs onStartup and beforeHelm hooks, helm upgrade --install command and afterHelm hook.
// It is a handler of task MODULE_RUN
func (m *Module) Run(onStartup bool) error {
	if err := m.cleanup(); err != nil {
		return err
	}

	if onStartup {
		if err := m.runHooksByBinding(OnStartup); err != nil {
			return err
		}
	}

	if err := m.runHooksByBinding(BeforeHelm); err != nil {
		return err
	}

	if err := m.runHelmInstall(); err != nil {
		return err
	}

	if err := m.runHooksByBinding(AfterHelm); err != nil {
		return err
	}

	return nil
}

// Delete removes helm release if it exists and runs afterDeleteHelm hooks.
// It is a handler for MODULE_DELETE task.
func (m *Module) Delete() error {
	// Если есть chart, но нет релиза — warning
	// если нет чарта — молча перейти к хукам
	// если есть и chart и релиз — удалить
	chartExists, _ := m.checkHelmChart()
	if chartExists {
		releaseExists, err := helm.Client.IsReleaseExists(m.generateHelmReleaseName())
		if !releaseExists {
			if err != nil {
				rlog.Warnf("Module delete: Cannot find helm release '%s' for module '%s'. Helm error: %s", m.generateHelmReleaseName(), m.Name, err)
			} else {
				rlog.Warnf("Module delete: Cannot find helm release '%s' for module '%s'.", m.generateHelmReleaseName(), m.Name)
			}
		} else {
			// Chart and release are existed, so run helm delete command
			err := helm.Client.DeleteRelease(m.generateHelmReleaseName())
			if err != nil {
				return err
			}
		}
	}

	return m.runHooksByBinding(AfterDeleteHelm)
}


func (m *Module) cleanup() error {
	chartExists, err := m.checkHelmChart()
	if !chartExists {
		if err != nil {
			rlog.Debugf("MODULE '%s': cleanup is not needed: %s", m.Name, err)
			return nil
		}
	}

	//rlog.Infof("MODULE '%s': cleanup helm revisions...", m.Name)
	if err := helm.Client.DeleteSingleFailedRevision(m.generateHelmReleaseName()); err != nil {
		return err
	}

	if err := helm.Client.DeleteOldFailedRevisions(m.generateHelmReleaseName()); err != nil {
		return err
	}

	return nil
}

func (m *Module) runHelmInstall() error {
	chartExists, err := m.checkHelmChart()
	if !chartExists {
		if err != nil {
			rlog.Debugf("Module '%s': no Chart.yaml, helm is not needed: %s", m.Name, err)
			return nil
		}
	}

	helmReleaseName := m.generateHelmReleaseName()

	// valuesPath, err := values.Dump
	//valuesPath := filepath.Join(m.moduleManager.TempDir, fmt.Sprintf("%s.module-values.yaml", m.SafeName()))
	//err := values.Dump(m.values(), NewDumperToYamlFile(valuesPath))
	//err := m.values().Dump(values.ToYamlFile(valuesPath))

	valuesPath, err := m.prepareValuesYamlFile()
	if err != nil {
		return err
	}

	// Create a temporary chart with empty values.yaml
	runChartPath := filepath.Join(m.moduleManager.TempDir, fmt.Sprintf("%s.chart", m.SafeName()))

	err = os.RemoveAll(runChartPath)
	if err != nil {
		return err
	}
	err = copy.Copy(m.Path, runChartPath)
	if err != nil {
		return err
	}

	// Prepare dummy empty values.yaml for helm not to fail
	err = os.Truncate(filepath.Join(runChartPath, "values.yaml"), 0)
	if err != nil {
		return err
	}

	checksum, err := utils.CalculateChecksumOfPaths(runChartPath, valuesPath)
	if err != nil {
		return err
	}

	doRelease := true

	isReleaseExists, err := helm.Client.IsReleaseExists(helmReleaseName)
	if err != nil {
		return err
	}

	if isReleaseExists {
		_, status, err := helm.Client.LastReleaseStatus(helmReleaseName)
		if err != nil {
			return err
		}

		// Skip helm release for unchanged modules only for non FAILED releases
		if status != "FAILED" {
			releaseValues, err := helm.Client.GetReleaseValues(helmReleaseName)
			if err != nil {
				return err
			}

			if recordedChecksum, hasKey := releaseValues["_addonOperatorModuleChecksum"]; hasKey {
				if recordedChecksumStr, ok := recordedChecksum.(string); ok {
					if recordedChecksumStr == checksum {
						doRelease = false
						rlog.Infof("MODULE_RUN '%s': helm release '%s' checksum '%s' is not changed: skip helm upgrade", m.Name, helmReleaseName, checksum)
					} else {
						rlog.Debugf("MODULE_RUN '%s': helm release '%s' checksum '%s' is changed to '%s': upgrade helm release", m.Name, helmReleaseName, recordedChecksumStr, checksum)
					}
				}
			}
		}
	}

	if doRelease {
		rlog.Debugf("MODULE_RUN '%s': helm release '%s' checksum '%s': installing/upgrading release", m.Name, helmReleaseName, checksum)

		return helm.Client.UpgradeRelease(
			helmReleaseName, runChartPath,
			[]string{valuesPath},
			[]string{fmt.Sprintf("_addonOperatorModuleChecksum=%s", checksum)},
			//helm.Client.TillerNamespace(),
			app.Namespace,
		)
	} else {
		rlog.Debugf("MODULE_RUN '%s': helm release '%s' checksum '%s': release install/upgrade is skipped", m.Name, helmReleaseName, checksum)
	}

	return nil
}


func (m *Module) runHooksByBinding(binding BindingType) error {
	moduleHooksAfterHelm, err := m.moduleManager.GetModuleHooksInOrder(m.Name, binding)
	if err != nil {
		return err
	}

	for _, moduleHookName := range moduleHooksAfterHelm {
		moduleHook, err := m.moduleManager.GetModuleHook(moduleHookName)
		if err != nil {
			return err
		}

		err = moduleHook.run(binding, []BindingContext{
			{
				BindingContext: hook2.BindingContext{
					Binding: ContextBindingType[binding],
				},
			},
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func (m *Module) prepareConfigValuesYamlFile() (string, error) {
	values := m.configValues()

	data := utils.MustDump(utils.DumpValuesYaml(values))
	path := filepath.Join(m.moduleManager.TempDir, fmt.Sprintf("%s.module-config-values.yaml", m.SafeName()))
	err := dumpData(path, data)
	if err != nil {
		return "", err
	}

	rlog.Debugf("Prepared module %s config values:\n%s", m.Name, utils.ValuesToString(values))

	return path, nil
}

func (m *Module) prepareConfigValuesJsonFile() (string, error) {
	values := m.configValues()

	data := utils.MustDump(utils.DumpValuesJson(values))
	path := filepath.Join(m.moduleManager.TempDir, fmt.Sprintf("%s.module-config-values.json", m.SafeName()))
	err := dumpData(path, data)
	if err != nil {
		return "", err
	}

	rlog.Debugf("Prepared module %s config values:\n%s", m.Name, utils.ValuesToString(values))

	return path, nil
}

func (m *Module) prepareValuesYamlFile() (string, error) {
	values := m.values()

	data := utils.MustDump(utils.DumpValuesYaml(values))
	path := filepath.Join(m.moduleManager.TempDir, fmt.Sprintf("%s.module-values.yaml", m.SafeName()))
	err := dumpData(path, data)
	if err != nil {
		return "", err
	}

	rlog.Debugf("Prepared module %s values:\n%s", m.Name, utils.ValuesToString(values))

	return path, nil
}

func (m *Module) prepareValuesJsonFileWith(values utils.Values) (string, error) {
	data := utils.MustDump(utils.DumpValuesJson(values))
	path := filepath.Join(m.moduleManager.TempDir, fmt.Sprintf("%s.module-values.json", m.SafeName()))
	err := dumpData(path, data)
	if err != nil {
		return "", err
	}

	rlog.Debugf("Prepared module %s values:\n%s", m.Name, utils.ValuesToString(values))

	return path, nil
}

func (m *Module) prepareValuesJsonFile() (string, error) {
	return m.prepareValuesJsonFileWith(m.values())
}

func (m *Module) prepareValuesJsonFileForEnabledScript(precedingEnabledModules []string) (string, error) {
	return m.prepareValuesJsonFileWith(m.valuesForEnabledScript(precedingEnabledModules))
}

func (m *Module) checkHelmChart() (bool, error) {
	chartPath := filepath.Join(m.Path, "Chart.yaml")

	if _, err := os.Stat(chartPath); os.IsNotExist(err) {
		return false, fmt.Errorf("path '%s' is not found", chartPath)
	}
	return true, nil
}

// generateHelmReleaseName returns a string that can be used as a helm release name.
//
// TODO Now it returns just a module name. Should it be cleaned from special symbols?
func (m *Module) generateHelmReleaseName() string {
	return m.Name
}

// configValues returns values from ConfigMap: global section and module section
func (m *Module) configValues() utils.Values {
	return utils.MergeValues(
		// global section
		utils.Values{"global": map[string]interface{}{}},
		m.moduleManager.kubeGlobalConfigValues,
		// module section
		utils.Values{utils.ModuleNameToValuesKey(m.Name): map[string]interface{}{}},
		m.moduleManager.kubeModulesConfigValues[m.Name],
	)
}

// constructValues returns effective values for module hook:
//
// global section: static + kube + patches from hooks
//
// module section: static + kube + patches from hooks
func (m *Module) constructValues() utils.Values {
	var err error

	res := utils.MergeValues(
		// global
		utils.Values{"global": map[string]interface{}{}},
		m.moduleManager.globalCommonStaticValues,
		m.moduleManager.kubeGlobalConfigValues,
		// module
		utils.Values{utils.ModuleNameToValuesKey(m.Name): map[string]interface{}{}},
		m.CommonStaticConfig.Values,
		m.StaticConfig.Values,
		m.moduleManager.kubeModulesConfigValues[m.Name],
	)

	for _, patches := range [][]utils.ValuesPatch{
		m.moduleManager.globalDynamicValuesPatches,
		m.moduleManager.modulesDynamicValuesPatches[m.Name],
	} {
		for _, patch := range patches {
			// Invariant: do not store patches that does not apply
			// Give user error for patches early, after patch receive

			res, _, err = utils.ApplyValuesPatch(res, patch)
			if err != nil {
				panic(err)
			}
		}
	}

	return res
}

// valuesForEnabledScript returns merged values for enabled script.
// There is enabledModules key in global section with previously enabled modules.
func (m *Module) valuesForEnabledScript(precedingEnabledModules []string) utils.Values {
	res := m.constructValues()
	res = utils.MergeValues(res, utils.Values{
		"global": map[string]interface{}{
			"enabledModules": precedingEnabledModules,
		},
	})
	return res
}

// values returns merged values for hooks.
// There is enabledModules key in global section with all enabled modules.
func (m *Module) values() utils.Values {
	res := m.constructValues()
	res = utils.MergeValues(res, utils.Values{
		"global": map[string]interface{}{
			"enabledModules": m.moduleManager.enabledModulesInOrder,
		},
	})
	return res
}

func (m *Module) moduleValuesKey() string {
	return utils.ModuleNameToValuesKey(m.Name)
}

func (m *Module) prepareModuleEnabledResultFile() (string, error) {
	path := filepath.Join(m.moduleManager.TempDir, fmt.Sprintf("%s.module-enabled-result", m.Name))
	if err := CreateEmptyWritableFile(path); err != nil {
		return "", err
	}
	return path, nil
}

func (m *Module) readModuleEnabledResult(filePath string) (bool, error) {
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return false, fmt.Errorf("cannot read %s: %s", filePath, err)
	}

	value := strings.TrimSpace(string(data))

	if value == "true" {
		return true, nil
	} else if value == "false" {
		return false, nil
	}

	return false, fmt.Errorf("expected 'true' or 'false', got '%s'", value)
}

func (m *Module) checkIsEnabledByScript(precedingEnabledModules []string) (bool, error) {
	enabledScriptPath := filepath.Join(m.Path, "enabled")

	f, err := os.Stat(enabledScriptPath)
	if os.IsNotExist(err) {
		rlog.Debugf("MODULE '%s':  ENABLED. Enabled script is not exist!", m.Name)
		return true, nil
	} else if err != nil {
		return false, err
	}

	if !utils_file.IsFileExecutable(f) {
		return false, fmt.Errorf("cannot execute non-executable enable script '%s'", enabledScriptPath)
	}

	configValuesPath, err := m.prepareConfigValuesJsonFile()
	if err != nil {
		return false, err
	}

	valuesPath, err := m.prepareValuesJsonFileForEnabledScript(precedingEnabledModules)
	if err != nil {
		return false, err
	}

	enabledResultFilePath, err := m.prepareModuleEnabledResultFile()
	if err != nil {
		return false, err
	}

	rlog.Infof("MODULE '%s': run enabled script '%s'...", m.Name, enabledScriptPath)

	envs := make([]string, 0)
	envs = append(envs, os.Environ()...)
	envs = append(envs, fmt.Sprintf("CONFIG_VALUES_PATH=%s", configValuesPath))
	envs = append(envs, fmt.Sprintf("VALUES_PATH=%s", valuesPath))
	envs = append(envs, fmt.Sprintf("MODULE_ENABLED_RESULT=%s", enabledResultFilePath))

	cmd := executor.MakeCommand("", enabledScriptPath, []string{}, envs)

	if err := executor.Run(cmd); err != nil {
		return false, err
	}

	moduleEnabled, err := m.readModuleEnabledResult(enabledResultFilePath)
	if err != nil {
		return false, fmt.Errorf("bad enabled result in file MODULE_ENABLED_RESULT=\"%s\" from enabled script '%s' for module '%s': %s", enabledResultFilePath, enabledScriptPath, m.Name, err)
	}

	if moduleEnabled {
		rlog.Debugf("Module '%s'  ENABLED with script. Preceding: %s", m.Name, precedingEnabledModules)
		return true, nil
	}

	rlog.Debugf("Module '%s' DISABLED with script. Preceding: %s ", m.Name, precedingEnabledModules)
	return false, nil
}

// initModulesIndex load all available modules from modules directory
// FIXME: Only 000-name modules are loaded, allow non-prefixed modules.
func (mm *MainModuleManager) initModulesIndex() error {
	rlog.Debug("INIT: Search modules ...")

	files, err := ioutil.ReadDir(mm.ModulesDir) // returns a list of modules sorted by filename
	if err != nil {
		return fmt.Errorf("INIT: cannot list modules directory '%s': %s", mm.ModulesDir, err)
	}

	// load global and modules common static values from modules/values.yaml
	if err := mm.loadCommonStaticValues(); err != nil {
		return fmt.Errorf("INIT: load common values: %s", err)
	}

	var validModuleName = regexp.MustCompile(`^[0-9][0-9][0-9]-(.*)$`)

	badModulesDirs := make([]string, 0)

	for _, file := range files {
		if file.IsDir() {
			matchRes := validModuleName.FindStringSubmatch(file.Name())
			if matchRes != nil {
				moduleName := matchRes[1]
				rlog.Infof("INIT: Register module '%s'", moduleName)

				modulePath := filepath.Join(mm.ModulesDir, file.Name())

				module := NewModule(mm)
				module.Name = moduleName
				module.DirectoryName = file.Name()
				module.Path = modulePath

				// load static config from values.yaml
				err := module.loadStaticValues()
				if err != nil {
					return err
				}

				mm.allModulesByName[module.Name] = module
				mm.allModulesNamesInOrder = append(mm.allModulesNamesInOrder, module.Name)
			} else {
				badModulesDirs = append(badModulesDirs, filepath.Join(mm.ModulesDir, file.Name()))
			}
		}
	}

	rlog.Debugf("INIT: initModulesIndex registered modules: %v", mm.allModulesByName)

	if len(badModulesDirs) > 0 {
		return fmt.Errorf("found directories not matched regex '%s': %s", validModuleName, strings.Join(badModulesDirs, ", "))
	}

	return nil
}

// loadStaticValues loads config for module from values.yaml
// Module is enabled if values.yaml is not exists.
func (m *Module) loadStaticValues() (err error) {
	m.CommonStaticConfig, err = utils.NewModuleConfig(m.Name).LoadFromValues(m.moduleManager.commonStaticValues)
	if err != nil {
		return err
	}
	rlog.Debugf("module %s common static values: %s", m.Name, utils.ValuesToString(m.CommonStaticConfig.Values))

	valuesYamlPath := filepath.Join(m.Path, "values.yaml")

	if _, err := os.Stat(valuesYamlPath); os.IsNotExist(err) {
		m.StaticConfig = utils.NewModuleConfig(m.Name)
		rlog.Debugf("module %s is static disabled: no values.yaml exists", m.Name)
		return nil
	}

	data, err := ioutil.ReadFile(valuesYamlPath)
	if err != nil {
		return fmt.Errorf("cannot read '%s': %s", m.Path, err)
	}

	m.StaticConfig, err = utils.NewModuleConfig(m.Name).FromYaml(data)
	if err != nil {
		return err
	}
	rlog.Debugf("module %s static values: %s", m.Name, utils.ValuesToString(m.StaticConfig.Values))
	return nil
}

func (mm *MainModuleManager) loadCommonStaticValues() (error) {
	valuesPath := filepath.Join(mm.ModulesDir, "values.yaml")
	if _, err := os.Stat(valuesPath); os.IsNotExist(err) {
		rlog.Debugf("No common static values file: %s", err)
		return nil
	}

	valuesYaml, err := ioutil.ReadFile(valuesPath)
	if err != nil {
		return fmt.Errorf("common values file '%s': %s", valuesPath, err)
	}

	var res map[interface{}]interface{}

	err = yaml.Unmarshal(valuesYaml, &res)
	if err != nil {
		return fmt.Errorf("unmarshal values from common values file '%s': %s\n%s", valuesPath, err, string(valuesYaml))
	}

	values, err := utils.FormatValues(res)
	if err != nil {
		return err
	}

	mm.commonStaticValues = values

	mm.globalCommonStaticValues = utils.GetGlobalValues(values)

	rlog.Debugf("Initialized global values from common static values:\n%s", utils.ValuesToString(mm.globalCommonStaticValues))

	return nil
}

func dumpData(filePath string, data []byte) error {
	err := ioutil.WriteFile(filePath, data, 0644)
	if err != nil {
		return err
	}
	return nil
}
