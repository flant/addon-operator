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
	log "github.com/sirupsen/logrus"
	uuid "gopkg.in/satori/go.uuid.v1"
	"gopkg.in/yaml.v2"

	. "github.com/flant/addon-operator/pkg/hook/types"
	. "github.com/flant/shell-operator/pkg/hook/binding_context"
	. "github.com/flant/shell-operator/pkg/hook/types"

	sh_app "github.com/flant/shell-operator/pkg/app"
	"github.com/flant/shell-operator/pkg/executor"
	utils_file "github.com/flant/shell-operator/pkg/utils/file"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/utils"
)

type Module struct {
	Name          string
	Path          string
	// module values from modules/values.yaml file
	CommonStaticConfig *utils.ModuleConfig
	// module values from modules/<module name>/values.yaml
	StaticConfig  *utils.ModuleConfig

	moduleManager *moduleManager
}

func NewModule(name, path string) *Module {
	return &Module{
		Name: name,
		Path: path,
	}
}

func (m *Module) WithModuleManager(moduleManager *moduleManager) {
	m.moduleManager = moduleManager
}

func (m *Module) SafeName() string {
	return sanitize.BaseName(m.Name)
}

// Run is a phase of module lifecycle that runs onStartup and beforeHelm hooks, helm upgrade --install command and afterHelm hook.
// It is a handler of task MODULE_RUN
func (m *Module) Run(onStartup bool, logLabels map[string]string, afterStartupCb func() error) (bool, error) {
	logLabels = utils.MergeLabels(logLabels, map[string]string{
		"module": m.Name,
		"queue": "main",
	})

	if err := m.cleanup(); err != nil {
		return false, err
	}

	if onStartup {
		if err := m.runHooksByBinding(OnStartup, logLabels); err != nil {
			return false, err
		}

		if err := afterStartupCb(); err != nil {
			return false, err
		}
	}

	if err := m.runHooksByBinding(BeforeHelm, logLabels); err != nil {
		return false, err
	}

	if err := m.runHelmInstall(logLabels); err != nil {
		return false, err
	}

	valuesChanged, err := m.runHooksByBindingAndCheckValues(AfterHelm, logLabels)
	if err != nil {
		return false, err
	}
	// Do not send to mm.moduleValuesChanged, changed values are handled by TaskHandler.
	return valuesChanged, nil
}

// Delete removes helm release if it exists and runs afterDeleteHelm hooks.
// It is a handler for MODULE_DELETE task.
func (m *Module) Delete(logLabels map[string]string) error {
	deleteLogLabels := utils.MergeLabels(logLabels,
		map[string]string{
			"module": m.Name,
			"queue": "main",
		})
	logEntry := log.WithFields(utils.LabelsToLogFields(deleteLogLabels))

	// Если есть chart, но нет релиза — warning
	// если нет чарта — молча перейти к хукам
	// если есть и chart и релиз — удалить
	chartExists, _ := m.checkHelmChart()
	if chartExists {
		releaseExists, err := helm.NewClient(deleteLogLabels).IsReleaseExists(m.generateHelmReleaseName())
		if !releaseExists {
			if err != nil {
				logEntry.Warnf("Cannot find helm release '%s' for module '%s'. Helm error: %s", m.generateHelmReleaseName(), m.Name, err)
			} else {
				logEntry.Warnf("Cannot find helm release '%s' for module '%s'.", m.generateHelmReleaseName(), m.Name)
			}
		} else {
			// Chart and release are existed, so run helm delete command
			err := helm.NewClient(deleteLogLabels).DeleteRelease(m.generateHelmReleaseName())
			if err != nil {
				return err
			}
		}
	}

	return m.runHooksByBinding(AfterDeleteHelm, deleteLogLabels)
}


func (m *Module) cleanup() error {
	chartExists, err := m.checkHelmChart()
	if !chartExists {
		if err != nil {
			log.Debugf("MODULE '%s': cleanup is not needed: %s", m.Name, err)
			return nil
		}
	}

	helmLogLabels := map[string]string{
		"module": m.Name,
	}

	if err := helm.NewClient(helmLogLabels).DeleteSingleFailedRevision(m.generateHelmReleaseName()); err != nil {
		return err
	}

	if err := helm.NewClient(helmLogLabels).DeleteOldFailedRevisions(m.generateHelmReleaseName()); err != nil {
		return err
	}

	return nil
}

func (m *Module) runHelmInstall(logLabels map[string]string) error {
	logEntry := log.WithFields(utils.LabelsToLogFields(logLabels))

	chartExists, err := m.checkHelmChart()
	if !chartExists {
		if err != nil {
			logEntry.Debugf("no Chart.yaml, helm is not needed: %s", err)
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

	// Create a temporary chart with empty values.yaml and unique path
	runChartPath := filepath.Join(m.moduleManager.TempDir, fmt.Sprintf("%s.chart.%s", m.SafeName(), uuid.NewV4().String()))

	err = os.RemoveAll(runChartPath)
	if err != nil {
		return err
	}

	// Remove chart directory after helm run
	defer func(){
		if sh_app.DebugKeepTmpFiles == "yes" {
			return
		}
		err := os.RemoveAll(runChartPath)
		log.WithField("module", m.Name).
			Errorf("Remove chart directory '%s': %s", runChartPath, err)
	}()

	err = copy.Copy(m.Path, runChartPath)
	if err != nil {
		return err
	}

	// Prepare dummy empty values.yaml for helm not to fail
	err = os.Truncate(filepath.Join(runChartPath, "values.yaml"), 0)
	if err != nil {
		return err
	}

	// Old style values checksum calculation
	//checksum, err := utils.CalculateChecksumOfPaths(runChartPath, valuesPath)
	//if err != nil {
	//	return err
	//}

	// New style — render templates
	helmClient := helm.NewClient(logLabels)

	helmTemplateOutput, err := helmClient.Render(runChartPath, []string{valuesPath},
		[]string{},
		app.Namespace)
	if err != nil {
		return err
	}

	checksum := utils.CalculateStringsChecksum(helmTemplateOutput)

	doRelease := true

	isReleaseExists, err := helmClient.IsReleaseExists(helmReleaseName)
	if err != nil {
		return err
	}

	if isReleaseExists {
		_, status, err := helmClient.LastReleaseStatus(helmReleaseName)
		if err != nil {
			return err
		}

		// Skip helm release for unchanged modules only for non FAILED releases
		if status != "FAILED" {
			releaseValues, err := helmClient.GetReleaseValues(helmReleaseName)
			if err != nil {
				return err
			}

			if recordedChecksum, hasKey := releaseValues["_addonOperatorModuleChecksum"]; hasKey {
				if recordedChecksumStr, ok := recordedChecksum.(string); ok {
					if recordedChecksumStr == checksum {
						doRelease = false
						logEntry.Infof("helm release '%s' checksum '%s' is not changed: skip helm upgrade", helmReleaseName, checksum)
					} else {
						logEntry.Debugf("helm release '%s' checksum '%s' is changed to '%s': upgrade helm release", helmReleaseName, recordedChecksumStr, checksum)
					}
				}
			}
		}
	}

	if doRelease {
		logEntry.Debugf("helm release '%s' checksum '%s': installing/upgrading release", helmReleaseName, checksum)

		return helmClient.UpgradeRelease(
			helmReleaseName, runChartPath,
			[]string{valuesPath},
			[]string{fmt.Sprintf("_addonOperatorModuleChecksum=%s", checksum)},
			//helm.Client.TillerNamespace(),
			app.Namespace,
		)
	} else {
		logEntry.Debugf("helm release '%s' checksum '%s': release install/upgrade is skipped", helmReleaseName, checksum)
	}

	return nil
}

// runHooksByBinding gets all hooks for binding, for each hook it creates a BindingContext,
// sets KubernetesSnapshots and runs the hook.
func (m *Module) runHooksByBinding(binding BindingType, logLabels map[string]string) error {
	moduleHooks := m.moduleManager.GetModuleHooksInOrder(m.Name, binding)

	for _, moduleHookName := range moduleHooks {
		moduleHook := m.moduleManager.GetModuleHook(moduleHookName)

		bc := BindingContext{
			Binding: ContextBindingType[binding],
		}
		// Update kubernetes snapshots just before execute a hook
		if binding == BeforeHelm || binding == AfterHelm || binding == AfterDeleteHelm {
			bc.Snapshots = moduleHook.HookController.KubernetesSnapshots()
			bc.Metadata.IncludeAllSnapshots = true
		}
		bc.Metadata.BindingType = binding


		err := moduleHook.Run(binding, []BindingContext{bc}, logLabels)
		if err != nil {
			return err
		}
	}

	return nil
}

// runHooksByBinding gets all hooks for binding, for each hook it creates a BindingContext,
// sets KubernetesSnapshots and runs the hook. If values are changed after hooks execution, return true.
func (m *Module) runHooksByBindingAndCheckValues(binding BindingType, logLabels map[string]string) (bool, error) {
	moduleHooks := m.moduleManager.GetModuleHooksInOrder(m.Name, binding)

	valuesChecksum, err := utils.ValuesChecksum(m.values())
	if err != nil {
		return false, err
	}

	for _, moduleHookName := range moduleHooks {
		moduleHook := m.moduleManager.GetModuleHook(moduleHookName)

		bc := BindingContext{
			Binding: ContextBindingType[binding],
		}
		// Update kubernetes snapshots just before execute a hook
		if binding == BeforeHelm || binding == AfterHelm || binding == AfterDeleteHelm {
			bc.Snapshots = moduleHook.HookController.KubernetesSnapshots()
			bc.Metadata.IncludeAllSnapshots = true
		}
		bc.Metadata.BindingType = binding


		err := moduleHook.Run(binding, []BindingContext{bc}, logLabels)
		if err != nil {
			return false, err
		}
	}

	newValuesChecksum, err := utils.ValuesChecksum(m.values())
	if err != nil {
		return false, err
	}

	if newValuesChecksum != valuesChecksum {
		return true, nil
	}

	return false, nil
}



func (m *Module) prepareConfigValuesYamlFile() (string, error) {
	values := m.configValues()

	data := utils.MustDump(utils.DumpValuesYaml(values))
	path := filepath.Join(m.moduleManager.TempDir, fmt.Sprintf("%s.module-config-values.yaml", m.SafeName()))
	err := dumpData(path, data)
	if err != nil {
		return "", err
	}

	log.Debugf("Prepared module %s config values:\n%s", m.Name, utils.ValuesToString(values))

	return path, nil
}

// CONFIG_VALUES_PATH
func (m *Module) prepareConfigValuesJsonFile() (string, error) {
	values := m.configValues()

	data := utils.MustDump(utils.DumpValuesJson(values))
	path := filepath.Join(m.moduleManager.TempDir, fmt.Sprintf("%s.module-config-values-%s.json", m.SafeName(), uuid.NewV4().String()))
	err := dumpData(path, data)
	if err != nil {
		return "", err
	}

	log.Debugf("Prepared module %s config values:\n%s", m.Name, utils.ValuesToString(values))

	return path, nil
}

// values.yaml for helm
func (m *Module) prepareValuesYamlFile() (string, error) {
	values := m.values()

	data := utils.MustDump(utils.DumpValuesYaml(values))
	path := filepath.Join(m.moduleManager.TempDir, fmt.Sprintf("%s.module-values.yaml-%s", m.SafeName(), uuid.NewV4().String()))
	err := dumpData(path, data)
	if err != nil {
		return "", err
	}

	log.Debugf("Prepared module %s values:\n%s", m.Name, utils.ValuesToString(values))

	return path, nil
}

// VALUES_PATH
func (m *Module) prepareValuesJsonFileWith(values utils.Values) (string, error) {
	data := utils.MustDump(utils.DumpValuesJson(values))
	path := filepath.Join(m.moduleManager.TempDir, fmt.Sprintf("%s.module-values-%s.json", m.SafeName(), uuid.NewV4().String()))
	err := dumpData(path, data)
	if err != nil {
		return "", err
	}

	log.Debugf("Prepared module %s values:\n%s", m.Name, utils.ValuesToString(values))

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

func (m *Module) checkIsEnabledByScript(precedingEnabledModules []string, logLabels map[string]string) (bool, error) {
	logEntry := log.WithFields(utils.LabelsToLogFields(logLabels))
	enabledScriptPath := filepath.Join(m.Path, "enabled")

	f, err := os.Stat(enabledScriptPath)
	if os.IsNotExist(err) {
		logEntry.Debugf("MODULE '%s' is ENABLED. Enabled script is not exist!", m.Name)
		return true, nil
	} else if err != nil {
		logEntry.Errorf("Cannot stat enabled script '%s': %s", enabledScriptPath, err)
		return false, err
	}

	if !utils_file.IsFileExecutable(f) {
		logEntry.Errorf("Found non-executable enabled script '%s'", enabledScriptPath)
		return false, fmt.Errorf("non-executable enable script")
	}

	// ValuesLock.Lock()
	configValuesPath, err := m.prepareConfigValuesJsonFile()
	if err != nil {
		logEntry.Errorf("Prepare CONFIG_VALUES_PATH file for '%s': %s", enabledScriptPath, err)
		return false, err
	}
	defer func(){
		if sh_app.DebugKeepTmpFiles == "yes" {
			return
		}
		err := os.Remove(configValuesPath)
		if err != nil {
			log.WithField("module", m.Name).
				Errorf("Remove tmp file '%s': %s", configValuesPath, err)
		}
	}()

	valuesPath, err := m.prepareValuesJsonFileForEnabledScript(precedingEnabledModules)
	if err != nil {
		logEntry.Errorf("Prepare VALUES_PATH file for '%s': %s", enabledScriptPath, err)
		return false, err
	}
	defer func(){
		if sh_app.DebugKeepTmpFiles == "yes" {
			return
		}
		err := os.Remove(valuesPath)
		if err != nil {
			log.WithField("module", m.Name).
				Errorf("Remove tmp file '%s': %s", configValuesPath, err)
		}
	}()

	enabledResultFilePath, err := m.prepareModuleEnabledResultFile()
	if err != nil {
		logEntry.Errorf("Prepare MODULE_ENABLED_RESULT file for '%s': %s", enabledScriptPath, err)
		return false, err
	}
	defer func(){
		if sh_app.DebugKeepTmpFiles == "yes" {
			return
		}
		err := os.Remove(enabledResultFilePath)
		if err != nil {
			log.WithField("module", m.Name).
				Errorf("Remove tmp file '%s': %s", configValuesPath, err)
		}
	}()

	logEntry.Debugf("Execute enabled script '%s', preceding modules: %v", enabledScriptPath, precedingEnabledModules)

	// ValuesLock.UnLock()

	envs := make([]string, 0)
	envs = append(envs, os.Environ()...)
	envs = append(envs, fmt.Sprintf("CONFIG_VALUES_PATH=%s", configValuesPath))
	envs = append(envs, fmt.Sprintf("VALUES_PATH=%s", valuesPath))
	envs = append(envs, fmt.Sprintf("MODULE_ENABLED_RESULT=%s", enabledResultFilePath))

	cmd := executor.MakeCommand("", enabledScriptPath, []string{}, envs)

	if err := executor.RunAndLogLines(cmd, logLabels); err != nil {
		logEntry.Errorf("Fail to run enabled script '%s': %s", enabledScriptPath, err)
		return false, err
	}

	moduleEnabled, err := m.readModuleEnabledResult(enabledResultFilePath)
	if err != nil {
		logEntry.Errorf("Read enabled result from '%s': %s", enabledScriptPath, err)
		return false, fmt.Errorf("bad enabled result")
	}

	result := "Disabled"
	if moduleEnabled {
		result = "Enabled"
	}
	logEntry.Infof("Enabled script run successful, result: module %q", result)
	return moduleEnabled, nil
}

var ValidModuleNameRe = regexp.MustCompile(`^[0-9][0-9][0-9]-(.*)$`)

func SearchModules(modulesDir string) (modules []*Module, err error) {
	files, err := ioutil.ReadDir(modulesDir) // returns a list of modules sorted by filename
	if err != nil {
		return nil, fmt.Errorf("list modules directory '%s': %s", modulesDir, err)
	}

	badModulesDirs := make([]string, 0)
	modules = make([]*Module, 0)

	for _, file := range files {
		if !file.IsDir() {
			continue
		}
		matchRes := ValidModuleNameRe.FindStringSubmatch(file.Name())
		if matchRes != nil {
			moduleName := matchRes[1]
			modulePath := filepath.Join(modulesDir, file.Name())
			module := NewModule(moduleName, modulePath)
			modules = append(modules, module)
		} else {
			badModulesDirs = append(badModulesDirs, filepath.Join(modulesDir, file.Name()))
		}
	}

	if len(badModulesDirs) > 0 {
		return nil, fmt.Errorf("modules directory contains directories not matched ValidModuleRegex '%s': %s", ValidModuleNameRe, strings.Join(badModulesDirs, ", "))
	}

	return
}

// RegisterModules load all available modules from modules directory
// FIXME: Only 000-name modules are loaded, allow non-prefixed modules.
func (mm *moduleManager) RegisterModules() error {
	log.Debug("Search and register modules")

	modules, err := SearchModules(mm.ModulesDir)
	if err != nil {
		return err
	}
	log.Debugf("Found %d modules", len(modules))

	// load global and modules common static values from modules/values.yaml
	if err := mm.loadCommonStaticValues(); err != nil {
		return fmt.Errorf("load common values for modules: %s", err)
	}

	for _, module := range modules {
		logEntry := log.WithField("module", module.Name)

		module.WithModuleManager(mm)

		// load static config from values.yaml
		err := module.loadStaticValues()
		if err != nil {
			logEntry.Errorf("Load values.yaml: %s", err)
			return fmt.Errorf("bad module values")
		}

		mm.allModulesByName[module.Name] = module
		mm.allModulesNamesInOrder = append(mm.allModulesNamesInOrder, module.Name)

		logEntry.Infof("Module is registered")
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
	log.Debugf("module %s common static values: %s", m.Name, utils.ValuesToString(m.CommonStaticConfig.Values))

	valuesYamlPath := filepath.Join(m.Path, "values.yaml")

	if _, err := os.Stat(valuesYamlPath); os.IsNotExist(err) {
		m.StaticConfig = utils.NewModuleConfig(m.Name)
		log.Debugf("module %s is static disabled: no values.yaml exists", m.Name)
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
	log.Debugf("module %s static values: %s", m.Name, utils.ValuesToString(m.StaticConfig.Values))
	return nil
}

func (mm *moduleManager) loadCommonStaticValues() (error) {
	valuesPath := filepath.Join(mm.ModulesDir, "values.yaml")
	if _, err := os.Stat(valuesPath); os.IsNotExist(err) {
		log.Debugf("No common static values file: %s", err)
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

	log.Debugf("Initialized global values from common static values:\n%s", utils.ValuesToString(mm.globalCommonStaticValues))

	return nil
}

func dumpData(filePath string, data []byte) error {
	err := ioutil.WriteFile(filePath, data, 0644)
	if err != nil {
		return err
	}
	return nil
}
