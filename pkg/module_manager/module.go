package module_manager

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime/trace"
	"strings"
	"time"

	"github.com/flant/kube-client/manifest"
	"github.com/kennygrant/sanitize"
	log "github.com/sirupsen/logrus"
	uuid "gopkg.in/satori/go.uuid.v1"

	. "github.com/flant/addon-operator/pkg/hook/types"
	. "github.com/flant/shell-operator/pkg/hook/binding_context"
	. "github.com/flant/shell-operator/pkg/hook/types"

	sh_app "github.com/flant/shell-operator/pkg/app"
	"github.com/flant/shell-operator/pkg/executor"
	"github.com/flant/shell-operator/pkg/metric_storage"
	utils_file "github.com/flant/shell-operator/pkg/utils/file"
	"github.com/flant/shell-operator/pkg/utils/measure"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/helm/client"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/addon-operator/pkg/values/validation"
)

type Module struct {
	Name string
	Path string
	// module values from modules/values.yaml file
	CommonStaticConfig *utils.ModuleConfig
	// module values from modules/<module name>/values.yaml
	StaticConfig *utils.ModuleConfig

	State *ModuleState

	moduleManager *moduleManager
	metricStorage *metric_storage.MetricStorage
	helm          *helm.ClientFactory
}

func NewModule(name, path string) *Module {
	return &Module{
		Name:  name,
		Path:  path,
		State: NewModuleState(),
	}
}

func (m *Module) WithModuleManager(moduleManager *moduleManager) {
	m.moduleManager = moduleManager
}

func (m *Module) WithMetricStorage(mstor *metric_storage.MetricStorage) {
	m.metricStorage = mstor
}

func (m *Module) WithHelm(helm *helm.ClientFactory) {
	m.helm = helm
}

func (m *Module) SafeName() string {
	return sanitize.BaseName(m.Name)
}

// HasKubernetesHooks is true if module has at least one kubernetes hook.
func (m *Module) HasKubernetesHooks() bool {
	hooks := m.moduleManager.GetModuleHooksInOrder(m.Name, OnKubernetesEvent)
	return len(hooks) > 0
}

// SynchronizationNeeded is true if module has at least one kubernetes hook
// with executeHookOnSynchronization.
func (m *Module) SynchronizationNeeded() bool {
	for _, hookName := range m.moduleManager.GetModuleHookNames(m.Name) {
		modHook := m.moduleManager.GetModuleHook(hookName)
		if modHook.SynchronizationNeeded() {
			return true
		}
	}
	return false
}

// RunOnStartup is a phase of module lifecycle that runs onStartup hooks.
// It is a handler of task MODULE_RUN
func (m *Module) RunOnStartup(logLabels map[string]string) error {
	logLabels = utils.MergeLabels(logLabels, map[string]string{
		"module": m.Name,
		"queue":  "main",
	})

	m.State.Enabled = true

	// Hooks can delete release resources, so stop resources monitor before run hooks.
	// m.moduleManager.HelmResourcesManager.PauseMonitor(m.Name)

	if err := m.runHooksByBinding(OnStartup, logLabels); err != nil {
		return err
	}

	return nil
}

// Run is a phase of module lifecycle that runs onStartup and beforeHelm hooks, helm upgrade --install command and afterHelm hook.
// It is a handler of task MODULE_RUN
func (m *Module) Run(logLabels map[string]string) (bool, error) {
	defer trace.StartRegion(context.Background(), "ModuleRun-HelmPhase").End()

	logLabels = utils.MergeLabels(logLabels, map[string]string{
		"module": m.Name,
		"queue":  "main",
	})

	// Hooks can delete release resources, so pause resources monitor before run hooks.
	m.moduleManager.HelmResourcesManager.PauseMonitor(m.Name)
	defer m.moduleManager.HelmResourcesManager.ResumeMonitor(m.Name)

	var err error

	treg := trace.StartRegion(context.Background(), "ModuleRun-HelmPhase-beforeHelm")
	err = m.runHooksByBinding(BeforeHelm, logLabels)
	treg.End()
	if err != nil {
		return false, err
	}

	treg = trace.StartRegion(context.Background(), "ModuleRun-HelmPhase-helm")
	err = m.runHelmInstall(logLabels)
	treg.End()
	if err != nil {
		return false, err
	}

	treg = trace.StartRegion(context.Background(), "ModuleRun-HelmPhase-afterHelm")
	valuesChanged, err := m.runHooksByBindingAndCheckValues(AfterHelm, logLabels)
	treg.End()
	if err != nil {
		return false, err
	}
	// Do not send to mm.moduleValuesChanged, changed values are handled by TaskHandler.
	return valuesChanged, nil
}

// Delete removes helm release if it exists and runs afterDeleteHelm hooks.
// It is a handler for MODULE_DELETE task.
func (m *Module) Delete(logLabels map[string]string) error {
	defer trace.StartRegion(context.Background(), "ModuleDelete-HelmPhase").End()

	deleteLogLabels := utils.MergeLabels(logLabels,
		map[string]string{
			"module": m.Name,
			"queue":  "main",
		})
	logEntry := log.WithFields(utils.LabelsToLogFields(deleteLogLabels))

	// Stop resources monitor before deleting release
	m.moduleManager.HelmResourcesManager.StopMonitor(m.Name)

	// Module has chart, but there is no release -> log a warning.
	// Module has chart and release -> execute helm delete.
	chartExists, _ := m.checkHelmChart()
	if chartExists {
		releaseExists, err := m.helm.NewClient(deleteLogLabels).IsReleaseExists(m.generateHelmReleaseName())
		if !releaseExists {
			if err != nil {
				logEntry.Warnf("Cannot find helm release '%s' for module '%s'. Helm error: %s", m.generateHelmReleaseName(), m.Name, err)
			} else {
				logEntry.Warnf("Cannot find helm release '%s' for module '%s'.", m.generateHelmReleaseName(), m.Name)
			}
		} else {
			// Chart and release are existed, so run helm delete command
			err := m.helm.NewClient(deleteLogLabels).DeleteRelease(m.generateHelmReleaseName())
			if err != nil {
				return err
			}
		}
	}

	err := m.runHooksByBinding(AfterDeleteHelm, deleteLogLabels)
	if err != nil {
		return err
	}

	// Cleanup state.
	m.State = NewModuleState()
	return nil
}

func (m *Module) runHelmInstall(logLabels map[string]string) error {
	metricLabels := map[string]string{
		"module":     m.Name,
		"activation": logLabels["event.type"],
	}
	defer measure.Duration(func(d time.Duration) {
		m.metricStorage.HistogramObserve("{PREFIX}module_helm_seconds", d.Seconds(), metricLabels, nil)
	})()

	logEntry := log.WithFields(utils.LabelsToLogFields(logLabels))

	chartExists, err := m.checkHelmChart()
	if !chartExists {
		if err != nil {
			logEntry.Debugf("no Chart.yaml, helm is not needed: %s", err)
			return nil
		}
	}

	err = m.checkHelmValues()
	if err != nil {
		return fmt.Errorf("check helm values: %v", err)
	}

	helmReleaseName := m.generateHelmReleaseName()

	valuesPath, err := m.PrepareValuesYamlFile()
	if err != nil {
		return err
	}
	defer os.Remove(valuesPath)

	helmClient := m.helm.NewClient(logLabels)

	// Render templates to prevent excess helm runs.
	var renderedManifests string
	func() {
		defer trace.StartRegion(context.Background(), "ModuleRun-HelmPhase-helm-render").End()

		metricLabels := map[string]string{
			"module":     m.Name,
			"activation": logLabels["event.type"],
			"operation":  "template",
		}
		defer measure.Duration(func(d time.Duration) {
			m.metricStorage.HistogramObserve("{PREFIX}helm_operation_seconds", d.Seconds(), metricLabels, nil)
		})()

		renderedManifests, err = helmClient.Render(
			helmReleaseName,
			m.Path,
			[]string{valuesPath},
			[]string{},
			app.Namespace)
	}()
	if err != nil {
		return err
	}
	checksum := utils.CalculateStringsChecksum(renderedManifests)

	manifests, err := manifest.ListFromYamlDocs(renderedManifests)
	if err != nil {
		return err
	}
	logEntry.Debugf("chart has %d resources", len(manifests))

	// Skip upgrades if nothing is changes
	var runUpgradeRelease bool
	func() {
		defer trace.StartRegion(context.Background(), "ModuleRun-HelmPhase-helm-check-upgrade").End()

		metricLabels := map[string]string{
			"module":     m.Name,
			"activation": logLabels["event.type"],
			"operation":  "check-upgrade",
		}
		defer measure.Duration(func(d time.Duration) {
			m.metricStorage.HistogramObserve("{PREFIX}helm_operation_seconds", d.Seconds(), metricLabels, nil)
		})()

		runUpgradeRelease, err = m.ShouldRunHelmUpgrade(helmClient, helmReleaseName, checksum, manifests, logLabels)
	}()
	if err != nil {
		return err
	}

	if !runUpgradeRelease {
		// Start resources monitor if release is not changed
		if !m.moduleManager.HelmResourcesManager.HasMonitor(m.Name) {
			m.moduleManager.HelmResourcesManager.StartMonitor(m.Name, manifests, app.Namespace)
		}
		return nil
	}

	// Run helm upgrade. Trace and measure its time.
	func() {
		defer trace.StartRegion(context.Background(), "ModuleRun-HelmPhase-helm-upgrade").End()

		metricLabels := map[string]string{
			"module":     m.Name,
			"activation": logLabels["event.type"],
			"operation":  "upgrade",
		}
		defer measure.Duration(func(d time.Duration) {
			m.metricStorage.HistogramObserve("{PREFIX}helm_operation_seconds", d.Seconds(), metricLabels, nil)
		})()

		err = helmClient.UpgradeRelease(
			helmReleaseName,
			m.Path,
			[]string{valuesPath},
			[]string{fmt.Sprintf("_addonOperatorModuleChecksum=%s", checksum)},
			app.Namespace,
		)
	}()

	if err != nil {
		return err
	}

	// Start monitor resources if release was successful
	m.moduleManager.HelmResourcesManager.StartMonitor(m.Name, manifests, app.Namespace)

	return nil
}

// ShouldRunHelmUpgrade tells if there is a case to run `helm upgrade`:
//   - Helm chart in not installed yet.
//   - Last release has FAILED status.
//   - Checksum in release values not equals to checksum argument.
//   - Some resources installed previously are missing.
//
// If all these conditions aren't met, helm upgrade can be skipped.
func (m *Module) ShouldRunHelmUpgrade(helmClient client.HelmClient, releaseName string, checksum string, manifests []manifest.Manifest, logLabels map[string]string) (bool, error) {
	logEntry := log.WithFields(utils.LabelsToLogFields(logLabels))

	revision, status, err := helmClient.LastReleaseStatus(releaseName)

	if revision == "0" {
		logEntry.Debugf("helm release '%s' not exists: should run upgrade", releaseName)
		return true, nil
	}

	if err != nil {
		return false, err
	}

	// Run helm upgrade if last release is failed
	if strings.ToLower(status) == "failed" {
		logEntry.Debugf("helm release '%s' has FAILED status: should run upgrade", releaseName)
		return true, nil
	}

	// Get values for a non-failed release.
	releaseValues, err := helmClient.GetReleaseValues(releaseName)
	if err != nil {
		logEntry.Debugf("helm release '%s' get values error, no upgrade: %v", releaseName, err)
		return false, err
	}

	// Run helm upgrade if there is no stored checksum
	recordedChecksum, hasKey := releaseValues["_addonOperatorModuleChecksum"]
	if !hasKey {
		logEntry.Debugf("helm release '%s' has no saved checksum of values: should run upgrade", releaseName)
		return true, nil
	}

	// Calculate a checksum of current values and compare to a stored checksum.
	// Run helm upgrade if checksum is changed.
	if recordedChecksumStr, ok := recordedChecksum.(string); ok {
		if recordedChecksumStr != checksum {
			logEntry.Debugf("helm release '%s' checksum '%s' is changed to '%s': should run upgrade", releaseName, recordedChecksumStr, checksum)
			return true, nil
		}
	}

	// Check if there are absent resources
	absent, err := m.moduleManager.HelmResourcesManager.GetAbsentResources(manifests, app.Namespace)
	if err != nil {
		return false, err
	}

	// Run helm upgrade if there are absent resources
	if len(absent) > 0 {
		logEntry.Debugf("helm release '%s' has %d absent resources: should run upgrade", releaseName, len(absent))
		return true, nil
	}

	logEntry.Debugf("helm release '%s' is unchanged: skip release upgrade", releaseName)
	return false, nil
}

// runHooksByBinding gets all hooks for binding, for each hook it creates a BindingContext,
// sets KubernetesSnapshots and runs the hook.
func (m *Module) runHooksByBinding(binding BindingType, logLabels map[string]string) error {
	var err error
	moduleHooks := m.moduleManager.GetModuleHooksInOrder(m.Name, binding)

	for _, moduleHookName := range moduleHooks {
		moduleHook := m.moduleManager.GetModuleHook(moduleHookName)

		err = moduleHook.RateLimitWait(context.Background())
		if err != nil {
			// This could happen when the Context is
			// canceled, or the expected wait time exceeds the Context's Deadline.
			// The best we can do without proper context usage is to repeat the task.
			return err
		}

		bc := BindingContext{
			Binding: string(binding),
		}
		// Update kubernetes snapshots just before execute a hook
		if binding == BeforeHelm || binding == AfterHelm || binding == AfterDeleteHelm {
			bc.Snapshots = moduleHook.HookController.KubernetesSnapshots()
			bc.Metadata.IncludeAllSnapshots = true
		}
		bc.Metadata.BindingType = binding

		metricLabels := map[string]string{
			"module":     m.Name,
			"hook":       moduleHook.Name,
			"binding":    string(binding),
			"queue":      "main", // AfterHelm,BeforeHelm hooks always handle in main queue
			"activation": logLabels["event.type"],
		}

		func() {
			defer measure.Duration(func(d time.Duration) {
				m.metricStorage.HistogramObserve("{PREFIX}module_hook_run_seconds", d.Seconds(), metricLabels, nil)
			})()
			err = moduleHook.Run(binding, []BindingContext{bc}, logLabels, metricLabels)
		}()
		if err != nil {
			return err
		}
	}

	return nil
}

// runHooksByBinding gets all hooks for binding, for each hook it creates a BindingContext,
// sets KubernetesSnapshots and runs the hook. If values are changed after hooks execution, return true.
func (m *Module) runHooksByBindingAndCheckValues(binding BindingType, logLabels map[string]string) (bool, error) {
	var err error
	moduleHooks := m.moduleManager.GetModuleHooksInOrder(m.Name, binding)

	values, err := m.Values()
	if err != nil {
		return false, err
	}
	valuesChecksum, err := values.Checksum()
	if err != nil {
		return false, err
	}

	for _, moduleHookName := range moduleHooks {
		moduleHook := m.moduleManager.GetModuleHook(moduleHookName)

		err = moduleHook.RateLimitWait(context.Background())
		if err != nil {
			// This could happen when the Context is
			// canceled, or the expected wait time exceeds the Context's Deadline.
			// The best we can do without proper context usage is to repeat the task.
			return false, err
		}

		bc := BindingContext{
			Binding: string(binding),
		}
		// Update kubernetes snapshots just before execute a hook
		if binding == BeforeHelm || binding == AfterHelm || binding == AfterDeleteHelm {
			bc.Snapshots = moduleHook.HookController.KubernetesSnapshots()
			bc.Metadata.IncludeAllSnapshots = true
		}
		bc.Metadata.BindingType = binding

		metricLabels := map[string]string{
			"module":     m.Name,
			"hook":       moduleHook.Name,
			"binding":    string(binding),
			"queue":      "main", // AfterHelm,BeforeHelm hooks always handle in main queue
			"activation": logLabels["event.type"],
		}

		func() {
			defer measure.Duration(func(d time.Duration) {
				m.metricStorage.HistogramObserve("{PREFIX}module_hook_run_seconds", d.Seconds(), metricLabels, nil)
			})()
			err = moduleHook.Run(binding, []BindingContext{bc}, logLabels, metricLabels)
		}()
		if err != nil {
			return false, err
		}
	}

	newValues, err := m.Values()
	if err != nil {
		return false, err
	}
	newValuesChecksum, err := newValues.Checksum()
	if err != nil {
		return false, err
	}

	if newValuesChecksum != valuesChecksum {
		return true, nil
	}

	return false, nil
}

// CONFIG_VALUES_PATH
func (m *Module) prepareConfigValuesJsonFile() (string, error) {
	data, err := m.ConfigValues().JsonBytes()
	if err != nil {
		return "", err
	}

	path := filepath.Join(m.moduleManager.TempDir, fmt.Sprintf("%s.module-config-values-%s.json", m.SafeName(), uuid.NewV4().String()))
	err = dumpData(path, data)
	if err != nil {
		return "", err
	}

	log.Debugf("Prepared module %s hook config values:\n%s", m.Name, m.ConfigValues().DebugString())

	return path, nil
}

// values.yaml for helm
func (m *Module) PrepareValuesYamlFile() (string, error) {
	values, err := m.Values()
	if err != nil {
		return "", err
	}

	data, err := values.YamlBytes()
	if err != nil {
		return "", err
	}

	path := filepath.Join(m.moduleManager.TempDir, fmt.Sprintf("%s.module-values.yaml-%s", m.SafeName(), uuid.NewV4().String()))
	err = dumpData(path, data)
	if err != nil {
		return "", err
	}

	log.Debugf("Prepared module %s helm values:\n%s", m.Name, values.DebugString())

	return path, nil
}

// VALUES_PATH
func (m *Module) prepareValuesJsonFileWith(values utils.Values) (string, error) {
	data, err := values.JsonBytes()
	if err != nil {
		return "", err
	}

	path := filepath.Join(m.moduleManager.TempDir, fmt.Sprintf("%s.module-values-%s.json", m.SafeName(), uuid.NewV4().String()))
	err = dumpData(path, data)
	if err != nil {
		return "", err
	}

	log.Debugf("Prepared module %s hook values:\n%s", m.Name, values.DebugString())

	return path, nil
}

func (m *Module) prepareValuesJsonFile() (string, error) {
	values, err := m.Values()
	if err != nil {
		return "", err
	}
	return m.prepareValuesJsonFileWith(values)
}

func (m *Module) prepareValuesJsonFileForEnabledScript(precedingEnabledModules []string) (string, error) {
	values, err := m.ValuesForEnabledScript(precedingEnabledModules)
	if err != nil {
		return "", err
	}
	return m.prepareValuesJsonFileWith(values)
}

// TODO run when module is registered and save bool value in Module’s field.
func (m *Module) checkHelmChart() (bool, error) {
	chartPath := filepath.Join(m.Path, "Chart.yaml")

	if _, err := os.Stat(chartPath); os.IsNotExist(err) {
		return false, fmt.Errorf("path '%s' is not found", chartPath)
	}
	return true, nil
}

// checkHelmValues returns error if there is a wrong patch or values are not satisfied
// a Helm values contract defined by schemas in 'openapi' directory.
func (m *Module) checkHelmValues() error {
	values, err := m.Values()
	if err != nil {
		return err
	}
	return m.moduleManager.ValuesValidator.ValidateModuleHelmValues(m.ValuesKey(), values)
}

// generateHelmReleaseName returns a string that can be used as a helm release name.
//
// TODO Now it returns just a module name. Should it be cleaned from special symbols?
func (m *Module) generateHelmReleaseName() string {
	return m.Name
}

// ConfigValues returns raw values from ConfigMap:
// - global section
// - module section
func (m *Module) ConfigValues() utils.Values {
	return MergeLayers(
		// Global values from ConfigMap with defaults from schema.
		m.moduleManager.GlobalConfigValues(),
		// Init module section.
		utils.Values{m.ValuesKey(): map[string]interface{}{}},
		// Merge overrides from ConfigMap.
		m.moduleManager.ModuleConfigValues(m.Name),
	)
}

// StaticAndConfigValues returns global common static values defined in
// various values.yaml files and in a ConfigMap and module values
// defined in various values.yaml files and in a ConfigMap.
func (m *Module) StaticAndConfigValues() utils.Values {
	return MergeLayers(
		// Global values from values.yaml and ConfigMap with defaults from schema.
		m.moduleManager.GlobalStaticAndConfigValues(),
		// Init module section.
		utils.Values{m.ValuesKey(): map[string]interface{}{}},
		// Merge static values from various values.yaml files.
		m.CommonStaticConfig.Values,
		m.StaticConfig.Values,
		// Apply config values defaults before ConfigMap overrides.
		&ApplyDefaultsForModule{
			m.ValuesKey(),
			validation.ConfigValuesSchema,
			m.moduleManager.ValuesValidator,
		},
		// Merge overrides from ConfigMap.
		m.moduleManager.ModuleConfigValues(m.Name),
	)
}

// StaticAndNewValues returns global values defined in
// various values.yaml files and in a ConfigMap and module values
// defined in various values.yaml files merged with newValues.
func (m *Module) StaticAndNewValues(newValues utils.Values) utils.Values {
	return MergeLayers(
		// Global values from values.yaml and ConfigMap with defaults from schema.
		m.moduleManager.GlobalStaticAndConfigValues(),
		// Init module section.
		utils.Values{m.ValuesKey(): map[string]interface{}{}},
		// Merge static values from various values.yaml files.
		m.CommonStaticConfig.Values,
		m.StaticConfig.Values,
		// Apply config values defaults before overrides.
		&ApplyDefaultsForModule{
			m.ValuesKey(),
			validation.ConfigValuesSchema,
			m.moduleManager.ValuesValidator,
		},
		// Merge overrides from newValues.
		newValues,
	)
}

// Values returns effective values for module hook or helm chart:
//
// global section: static + config + defaults + patches from hooks
//
// module section: static + config + defaults + patches from hooks
func (m *Module) Values() (utils.Values, error) {
	var err error

	globalValues, err := m.moduleManager.GlobalValues()
	if err != nil {
		return nil, fmt.Errorf("construct module values: %s", err)
	}

	// Apply global and module values defaults before applying patches.
	res := MergeLayers(
		// Global values with patches and defaults.
		globalValues,
		// Init module section.
		utils.Values{m.ValuesKey(): map[string]interface{}{}},
		// Merge static values from various values.yaml files.
		m.CommonStaticConfig.Values,
		m.StaticConfig.Values,
		// Apply config values defaults before ConfigMap overrides.
		&ApplyDefaultsForModule{
			m.ValuesKey(),
			validation.ConfigValuesSchema,
			m.moduleManager.ValuesValidator,
		},
		// Merge overrides from ConfigMap.
		m.moduleManager.ModuleConfigValues(m.Name),
		// Apply dynamic values defaults before patches.
		&ApplyDefaultsForModule{
			m.ValuesKey(),
			validation.ValuesSchema,
			m.moduleManager.ValuesValidator,
		},
	)

	// Do not store patches that does not apply, give user error
	// for patches early, after patch receive.
	res, err = m.moduleManager.ApplyModuleDynamicValuesPatches(m.Name, res)
	if err != nil {
		return nil, fmt.Errorf("construct module values: apply module patch error: %s", err)
	}

	// Add enabledModules array.
	res = MergeLayers(
		res,
		utils.Values{"global": map[string]interface{}{
			"enabledModules": m.moduleManager.enabledModules,
		}},
	)

	return res, nil
}

// ValuesForEnabledScript returns effective values for enabled script.
// There is enabledModules key in global section with previously enabled modules.
func (m *Module) ValuesForEnabledScript(precedingEnabledModules []string) (utils.Values, error) {
	res, err := m.Values()
	if err != nil {
		return nil, err
	}
	res = MergeLayers(
		res,
		utils.Values{
			"global": map[string]interface{}{
				"enabledModules": precedingEnabledModules,
			}},
	)
	return res, nil
}

// TODO Transform to a field.
func (m *Module) ValuesKey() string {
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
	data, err := os.ReadFile(filePath)
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

func (m *Module) runEnabledScript(precedingEnabledModules []string, logLabels map[string]string) (bool, error) {
	// Copy labels and set 'module' label.
	logLabels = utils.MergeLabels(logLabels)
	logLabels["module"] = m.Name

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

	configValuesPath, err := m.prepareConfigValuesJsonFile()
	if err != nil {
		logEntry.Errorf("Prepare CONFIG_VALUES_PATH file for '%s': %s", enabledScriptPath, err)
		return false, err
	}
	defer func() {
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
	defer func() {
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
	defer func() {
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

	envs := make([]string, 0)
	envs = append(envs, os.Environ()...)
	envs = append(envs, fmt.Sprintf("CONFIG_VALUES_PATH=%s", configValuesPath))
	envs = append(envs, fmt.Sprintf("VALUES_PATH=%s", valuesPath))
	envs = append(envs, fmt.Sprintf("MODULE_ENABLED_RESULT=%s", enabledResultFilePath))

	cmd := executor.MakeCommand("", enabledScriptPath, []string{}, envs)

	usage, err := executor.RunAndLogLines(cmd, logLabels)
	if usage != nil {
		// usage metrics
		metricLabels := map[string]string{
			"module":     m.Name,
			"hook":       "enabled",
			"binding":    "enabled",
			"queue":      logLabels["queue"],
			"activation": logLabels["event.type"],
		}
		m.moduleManager.metricStorage.HistogramObserve("{PREFIX}module_hook_run_sys_cpu_seconds", usage.Sys.Seconds(), metricLabels, nil)
		m.moduleManager.metricStorage.HistogramObserve("{PREFIX}module_hook_run_user_cpu_seconds", usage.User.Seconds(), metricLabels, nil)
		m.moduleManager.metricStorage.GaugeSet("{PREFIX}module_hook_run_max_rss_bytes", float64(usage.MaxRss)*1024, metricLabels)
	}
	if err != nil {
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
	logEntry.Infof("Enabled script run successful, result '%v', module '%s'", moduleEnabled, result)
	return moduleEnabled, nil
}

var ValidModuleNameRe = regexp.MustCompile(`^[0-9][0-9][0-9]-(.*)$`)

func SearchModules(modulesDir string) (modules []*Module, err error) {
	if modulesDir == "" {
		log.Warnf("Modules directory path is empty! No modules to load.")
		return nil, nil
	}

	files, err := os.ReadDir(modulesDir) // returns a list of modules sorted by filename
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
		module.WithMetricStorage(mm.metricStorage)
		module.WithHelm(mm.helm)

		// load static config from values.yaml
		err := module.loadStaticValues()
		if err != nil {
			logEntry.Errorf("Load values.yaml: %s", err)
			return fmt.Errorf("bad module values")
		}

		mm.allModulesByName[module.Name] = module
		mm.allModulesNamesInOrder = append(mm.allModulesNamesInOrder, module.Name)

		// Load validation schemas
		openAPIPath := filepath.Join(module.Path, "openapi")
		configBytes, valuesBytes, err := ReadOpenAPIFiles(openAPIPath)
		if err != nil {
			return fmt.Errorf("module '%s' read openAPI schemas: %v", module.Name, err)
		}

		err = mm.ValuesValidator.SchemaStorage.AddModuleValuesSchemas(
			module.ValuesKey(),
			configBytes,
			valuesBytes,
		)
		if err != nil {
			return fmt.Errorf("add module '%s' schemas: %v", module.Name, err)
		}

		logEntry.Infof("Module from '%s'. %s", module.Path, mm.ValuesValidator.SchemaStorage.ModuleSchemasDescription(module.ValuesKey()))
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
	log.Debugf("module %s common static values: %s", m.Name, m.CommonStaticConfig.Values.DebugString())

	valuesYamlPath := filepath.Join(m.Path, "values.yaml")

	if _, err := os.Stat(valuesYamlPath); os.IsNotExist(err) {
		m.StaticConfig = utils.NewModuleConfig(m.Name)
		log.Debugf("module %s is static disabled: no values.yaml exists", m.Name)
		return nil
	}

	data, err := os.ReadFile(valuesYamlPath)
	if err != nil {
		return fmt.Errorf("cannot read '%s': %s", m.Path, err)
	}

	m.StaticConfig, err = utils.NewModuleConfig(m.Name).FromYaml(data)
	if err != nil {
		return err
	}
	log.Debugf("module %s static values: %s", m.Name, m.StaticConfig.Values.DebugString())
	return nil
}

func (mm *moduleManager) loadCommonStaticValues() error {
	valuesPath := filepath.Join(mm.ModulesDir, "values.yaml")
	if _, err := os.Stat(valuesPath); os.IsNotExist(err) {
		log.Debugf("No common static values file: %s", err)
		return nil
	}

	valuesYaml, err := os.ReadFile(valuesPath)
	if err != nil {
		return fmt.Errorf("load common values file '%s': %s", valuesPath, err)
	}

	values, err := utils.NewValuesFromBytes(valuesYaml)
	if err != nil {
		return err
	}

	mm.commonStaticValues = values

	log.Debugf("Successfully load common static values:\n%s", mm.commonStaticValues.DebugString())

	return nil
}

func dumpData(filePath string, data []byte) error {
	err := os.WriteFile(filePath, data, 0644)
	if err != nil {
		return err
	}
	return nil
}
