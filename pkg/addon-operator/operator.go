package addon_operator

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	metricsstorage "github.com/deckhouse/deckhouse/pkg/metrics-storage"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/leaderelection"

	"github.com/flant/addon-operator/pkg"
	"github.com/flant/addon-operator/pkg/addon-operator/converge"
	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/helm_resources_manager"
	"github.com/flant/addon-operator/pkg/kube_config_manager"
	"github.com/flant/addon-operator/pkg/kube_config_manager/config"
	"github.com/flant/addon-operator/pkg/metrics"
	"github.com/flant/addon-operator/pkg/module_manager"
	gohook "github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/pkg/module_manager/models/hooks/kind"
	"github.com/flant/addon-operator/pkg/module_manager/models/modules/events"
	"github.com/flant/addon-operator/pkg/task"
	paralleltask "github.com/flant/addon-operator/pkg/task/parallel"
	queueutils "github.com/flant/addon-operator/pkg/task/queue"
	taskservice "github.com/flant/addon-operator/pkg/task/service"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/kube-client/client"
	shapp "github.com/flant/shell-operator/pkg/app"
	runtimeConfig "github.com/flant/shell-operator/pkg/config"
	"github.com/flant/shell-operator/pkg/debug"
	bc "github.com/flant/shell-operator/pkg/hook/binding_context"
	htypes "github.com/flant/shell-operator/pkg/hook/types"
	shmetrics "github.com/flant/shell-operator/pkg/metrics"
	shell_operator "github.com/flant/shell-operator/pkg/shell-operator"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
	fileUtils "github.com/flant/shell-operator/pkg/utils/file"
)

const (
	LabelHeritage string = "heritage"
)

// AddonOperator extends ShellOperator with modules and global hooks
// and with a value storage.
type AddonOperator struct {
	engine *shell_operator.ShellOperator
	ctx    context.Context
	cancel context.CancelFunc

	DefaultNamespace string

	runtimeConfig *runtimeConfig.Config

	// a map of channels to communicate with parallel queues and its lock
	parallelTaskChannels *paralleltask.TaskChannels

	DebugServer *debug.Server

	// KubeConfigManager monitors changes in ConfigMap.
	KubeConfigManager *kube_config_manager.KubeConfigManager

	// ModuleManager is the module manager object, which monitors configuration
	// and variable changes.
	ModuleManager *module_manager.ModuleManager

	Helm *helm.ClientFactory

	// HelmResourcesManager monitors absent resources created for modules.
	HelmResourcesManager helm_resources_manager.HelmResourcesManager

	// Initial KubeConfig to bypass initial loading from the ConfigMap.
	InitialKubeConfig *config.KubeConfig

	// AdmissionServer handles validation and mutation admission webhooks
	AdmissionServer *AdmissionServer

	MetricStorage     metricsstorage.Storage
	HookMetricStorage metricsstorage.Storage

	// LeaderElector represents leaderelection client for HA mode
	LeaderElector *leaderelection.LeaderElector

	// CRDExtraLabels contains labels for processing CRD files
	// like heritage=addon-operator
	CRDExtraLabels map[string]string

	Logger *log.Logger

	// converge state
	ConvergeState *converge.ConvergeState

	TaskService *taskservice.TaskHandlerService
}

type Option func(operator *AddonOperator)

func WithLogger(logger *log.Logger) Option {
	return func(operator *AddonOperator) {
		operator.Logger = logger
	}
}

func WithMetricStorage(storage metricsstorage.Storage) Option {
	return func(operator *AddonOperator) {
		operator.MetricStorage = storage
	}
}

func WithHookMetricStorage(storage metricsstorage.Storage) Option {
	return func(operator *AddonOperator) {
		operator.HookMetricStorage = storage
	}
}

func NewAddonOperator(ctx context.Context, opts ...Option) *AddonOperator {
	cctx, cancel := context.WithCancel(ctx)

	ao := &AddonOperator{
		ctx:                  cctx,
		cancel:               cancel,
		DefaultNamespace:     app.Namespace,
		ConvergeState:        converge.NewConvergeState(),
		parallelTaskChannels: paralleltask.NewTaskChannels(),
	}

	for _, opt := range opts {
		opt(ao)
	}

	if ao.Logger == nil {
		ao.Logger = log.NewLogger().Named("addon-operator")
	}

	// Use provided metric storage or create default
	if ao.MetricStorage == nil {
		ao.MetricStorage = metricsstorage.NewMetricStorage(
			metricsstorage.WithPrefix(shapp.PrometheusMetricsPrefix),
			metricsstorage.WithLogger(ao.Logger.Named("metric-storage")),
		)
	}

	// Use provided hook metric storage or create default
	if ao.HookMetricStorage == nil {
		ao.HookMetricStorage = metricsstorage.NewMetricStorage(
			metricsstorage.WithPrefix(shapp.PrometheusMetricsPrefix),
			metricsstorage.WithNewRegistry(),
			metricsstorage.WithLogger(ao.Logger.Named("hook-metric-storage")),
		)
	}

	so := shell_operator.NewShellOperator(
		cctx,
		shell_operator.WithLogger(ao.Logger.Named("shell-operator")),
		shell_operator.WithMetricStorage(ao.MetricStorage),
		shell_operator.WithHookMetricStorage(ao.HookMetricStorage),
	)

	// initialize logging before Assemble
	rc := runtimeConfig.NewConfig(ao.Logger)
	// Init logging subsystem.
	shapp.SetupLogging(rc, ao.Logger)

	shmetrics.RegisterOperatorMetrics(so.MetricStorage, []string{
		"module",
		pkg.MetricKeyHook,
		pkg.MetricKeyBinding,
		"queue",
		"kind",
	})

	// Have to initialize common operator to have all common dependencies below
	err := so.AssembleCommonOperator(app.ListenAddress, app.ListenPort)
	if err != nil {
		panic(err)
	}

	metrics.RegisterHookMetrics(so.HookMetricStorage)

	labelSelector, err := metav1.ParseToLabelSelector(app.ExtraLabels)
	if err != nil {
		panic(err)
	}
	crdExtraLabels := labelSelector.MatchLabels

	// use `heritage=addon-operator` by default if not set
	if _, ok := crdExtraLabels[LabelHeritage]; !ok {
		crdExtraLabels[LabelHeritage] = "addon-operator"
	}

	ao.engine = so
	ao.runtimeConfig = rc
	ao.MetricStorage = so.MetricStorage
	ao.CRDExtraLabels = crdExtraLabels

	ao.AdmissionServer = NewAdmissionServer(app.AdmissionServerListenPort, app.AdmissionServerCertsDir)

	return ao
}

func (op *AddonOperator) WithLeaderElector(config *leaderelection.LeaderElectionConfig) error {
	var err error
	op.LeaderElector, err = leaderelection.NewLeaderElector(*config)
	if err != nil {
		return err
	}

	return nil
}

// Setup initializes the AddonOperator with required components and configurations.
// It performs the following steps:
// 1. Initializes the Helm client factory with specified options like namespace, history max, timeout, and logger.
// 2. Sets up the Helm resources manager using a separate client-go instance.
// 3. Validates the existence of the global hooks directory.
// 4. Ensures the temporary directory exists.
// 5. Verifies that KubeConfigManager is set before proceeding.
// 6. Finally sets up the module manager with the appropriate directories.
//
// Returns an error if any initialization step fails.
func (op *AddonOperator) Setup() error {
	// Helm client factory.
	helmClient, err := helm.InitHelmClientFactory(&helm.Options{
		Namespace:  op.DefaultNamespace,
		HistoryMax: app.Helm3HistoryMax,
		Timeout:    app.Helm3Timeout,
		Logger:     op.Logger.Named("helm"),
	}, op.CRDExtraLabels)
	if err != nil {
		return fmt.Errorf("initialize Helm: %s", err)
	}

	// Helm resources monitor.
	// It uses a separate client-go instance. (Metrics are registered when 'main' client is initialized).
	helmResourcesManager, err := InitDefaultHelmResourcesManager(op.ctx, op.DefaultNamespace, op.MetricStorage, op.Logger)
	if err != nil {
		return fmt.Errorf("initialize Helm resources manager: %s", err)
	}

	op.Helm = helmClient
	op.HelmResourcesManager = helmResourcesManager

	globalHooksDir, err := fileUtils.RequireExistingDirectory(app.GlobalHooksDir)
	if err != nil {
		return fmt.Errorf("global hooks directory: %s", err)
	}
	log.Info("global hooks directory",
		slog.String("dir", globalHooksDir))

	tempDir, err := ensureTempDirectory(shapp.TempDir)
	if err != nil {
		return fmt.Errorf("temp directory: %s", err)
	}

	if op.KubeConfigManager == nil {
		return fmt.Errorf("KubeConfigManager must be set before Setup")
	}

	op.SetupModuleManager(app.ModulesDir, globalHooksDir, tempDir)

	return nil
}

func ensureTempDirectory(inDir string) (string, error) {
	// No path to temporary dir, use default temporary dir.
	if inDir == "" {
		tmpPath := app.AppName + "-*"
		dir, err := os.MkdirTemp("", tmpPath)
		if err != nil {
			return "", fmt.Errorf("create tmp dir in '%s': %s", tmpPath, err)
		}
		return dir, nil
	}

	// Get absolute path for temporary directory and create if needed.
	dir, err := filepath.Abs(inDir)
	if err != nil {
		return "", fmt.Errorf("get absolute path: %v", err)
	}

	if exists := fileUtils.DirExists(dir); !exists {
		err := os.Mkdir(dir, os.FileMode(0o777))
		if err != nil {
			return "", fmt.Errorf("create tmp dir '%s': %s", dir, err)
		}
	}
	return dir, nil
}

// Start runs all managers, event and queue handlers.
// TODO: implement context in various dependencies (ModuleManager, KubeConfigManaer, etc)
func (op *AddonOperator) Start(ctx context.Context) error {
	if err := op.bootstrap(); err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}

	log.Info("Start first converge for modules")
	// Loading the onStartup hooks into the queue and running all modules.
	// Turning tracking changes on only after startup ends.

	// Bootstrap main queue with tasks to run Startup process.
	op.BootstrapMainQueue(op.engine.TaskQueues)
	// Start main task queue handler
	op.engine.TaskQueues.StartMain(ctx)
	// Precreate queues for parallel tasks
	op.CreateAndStartParallelQueues()

	// Global hooks are registered, initialize their queues.
	op.CreateAndStartQueuesForGlobalHooks()

	// ManagerEventsHandler handle events for kubernetes and schedule bindings.
	// Start it before start all informers to catch all kubernetes events (#42)
	op.engine.ManagerEventsHandler.Start()

	// Enable events from schedule manager.
	op.engine.ScheduleManager.Start()

	op.KubeConfigManager.Start()
	op.ModuleManager.Start()
	op.StartModuleManagerEventHandler()

	return nil
}

func (op *AddonOperator) Stop() {
	op.KubeConfigManager.Stop()
	op.engine.Shutdown()

	if op.cancel != nil {
		op.cancel()
	}
}

func (op *AddonOperator) StartAPIServer() {
	// start http server with metrics
	op.engine.APIServer.Start(op.ctx)
	op.registerReadyzRoute()
}

// KubeClient returns default common kubernetes client initialized by shell-operator
func (op *AddonOperator) KubeClient() *client.Client {
	return op.engine.KubeClient
}

func (op *AddonOperator) IsStartupConvergeDone() bool {
	return op.ConvergeState.GetFirstRunPhase() == converge.FirstDone
}

func (op *AddonOperator) globalHooksNotExecutedYet() bool {
	return op.ConvergeState.GetFirstRunPhase() == converge.FirstNotStarted ||
		(op.ConvergeState.GetFirstRunPhase() == converge.FirstStarted && op.ConvergeState.GetPhase() == converge.StandBy)
}

// InitModuleManager initialize KubeConfigManager and ModuleManager,
// reads values from ConfigMap for the first time and sets handlers
// for kubernetes and schedule events.
func (op *AddonOperator) InitModuleManager() error {
	var err error

	// Initializing ConfigMap storage for values
	err = op.KubeConfigManager.Init()
	if err != nil {
		return fmt.Errorf("init kube config manager: %s", err)
	}

	err = op.ModuleManager.Init(op.Logger.Named("module-manager"))
	if err != nil {
		return fmt.Errorf("init module manager: %s", err)
	}

	// Load existing config values from KubeConfigManager.
	// Also, it is possible to override initial KubeConfig to give global hooks a chance
	// to handle the KubeConfigManager content later.
	if op.InitialKubeConfig == nil {
		op.KubeConfigManager.SafeReadConfig(func(config *config.KubeConfig) {
			err = op.ModuleManager.ApplyNewKubeConfigValues(config, true)
		})
		if err != nil {
			return fmt.Errorf("init module manager: load initial config for KubeConfigManager: %s", err)
		}
	} else {
		err = op.ModuleManager.ApplyNewKubeConfigValues(op.InitialKubeConfig, true)
		if err != nil {
			return fmt.Errorf("init module manager: load overridden initial config: %s", err)
		}
	}

	// Initialize 'valid kube config' flag.
	op.ModuleManager.SetKubeConfigValid(true)

	// ManagerEventsHandlers created, register handlers to create tasks from events.
	op.RegisterManagerEventsHandlers()

	return nil
}

type typedHook interface {
	GetKind() kind.HookKind
	GetGoHookInputSettings() *gohook.HookConfigSettings
}

// allowHandleScheduleEvent returns false if the Schedule event can be ignored.
// TODO: this method is compatible opnly with Go hooks, probably we want shell hooks to support this logic also
func (op *AddonOperator) allowHandleScheduleEvent(hook typedHook) bool {
	// Always allow if first converge is done.
	if op.IsStartupConvergeDone() {
		return true
	}

	// Allow when first converge is still in progress,
	// but hook explicitly enable schedules after OnStartup.
	return shouldEnableSchedulesOnStartup(hook)
}

// ShouldEnableSchedulesOnStartup returns true for Go hooks if EnableSchedulesOnStartup is set.
// This flag for schedule hooks that start after onStartup hooks.
func shouldEnableSchedulesOnStartup(hk typedHook) bool {
	if hk.GetKind() != kind.HookKindGo {
		return false
	}

	settings := hk.GetGoHookInputSettings()
	if settings == nil {
		return false
	}

	if settings.EnableSchedulesOnStartup {
		return true
	}

	return false
}

// BootstrapMainQueue adds tasks to initiate Startup sequence:
//
// - Run onStartup hooks.
// - Enable global schedule bindings.
// - Enable kubernetes bindings: run Synchronization tasks.
// - Purge unknown Helm releases.
// - Start reload all modules.
func (op *AddonOperator) BootstrapMainQueue(tqs *queue.TaskQueueSet) {
	logLabels := map[string]string{
		// TODO: what is the purpose of this label?
		// we set it only here and read in lot of places
		// maybe we should remove it?
		pkg.LogKeyEventType: "OperatorStartup",
	}
	// create onStartup for global hooks
	logEntry := utils.EnrichLoggerWithLabels(op.Logger, logLabels)

	// Prepopulate main queue with 'onStartup' and 'enable kubernetes bindings' tasks for
	// global hooks and add a task to discover modules state.
	tqs.WithMainName("main")
	tqs.NewNamedQueue("main", op.TaskService.Handle,
		queue.WithCompactionCallback(queueutils.CompactionCallback(op.ModuleManager, op.Logger)),
		queue.WithCompactableTypes(queueutils.MergeTasks...),
		queue.WithLogger(op.Logger.With("operator.component", "mainQueue")),
	)

	tasks := op.CreateBootstrapTasks(logLabels)
	op.logTaskAdd(logEntry, "append", tasks...)
	for _, tsk := range tasks {
		op.engine.TaskQueues.GetMain().AddLast(tsk)
	}

	go func() {
		<-op.ConvergeState.FirstRunDoneC
		// Add "DiscoverHelmReleases" task to detect unknown releases and purge them.
		// this task will run only after the first converge, to keep all modules
		discoverLabels := utils.MergeLabels(logLabels, map[string]string{
			"queue":           "main",
			pkg.LogKeyBinding: string(task.DiscoverHelmReleases),
		})
		discoverTask := sh_task.NewTask(task.DiscoverHelmReleases).
			WithLogLabels(discoverLabels).
			WithQueueName("main").
			WithMetadata(task.HookMetadata{
				EventDescription: "Operator-PostConvergeCleanup",
			})
		op.engine.TaskQueues.GetMain().AddLast(discoverTask.WithQueuedAt(time.Now()))
		op.ModuleManager.SendModuleEvent(events.ModuleEvent{EventType: events.FirstConvergeDone})
	}()
}

func (op *AddonOperator) CreateBootstrapTasks(logLabels map[string]string) []sh_task.Task {
	const eventDescription = "Operator-Startup"
	tasks := make([]sh_task.Task, 0)
	queuedAt := time.Now()

	// 'OnStartup' global hooks.
	onStartupHooks := op.ModuleManager.GetGlobalHooksInOrder(htypes.OnStartup)
	for _, hookName := range onStartupHooks {
		hookLogLabels := utils.MergeLabels(logLabels, map[string]string{
			pkg.LogKeyHook:    hookName,
			"hook.type":       "global",
			"queue":           "main",
			pkg.LogKeyBinding: string(htypes.OnStartup),
		})

		onStartupBindingContext := bc.BindingContext{Binding: string(htypes.OnStartup)}
		onStartupBindingContext.Metadata.BindingType = htypes.OnStartup

		newTask := sh_task.NewTask(task.GlobalHookRun).
			WithLogLabels(hookLogLabels).
			WithQueueName("main").
			WithMetadata(task.HookMetadata{
				EventDescription:         eventDescription,
				HookName:                 hookName,
				BindingType:              htypes.OnStartup,
				BindingContext:           []bc.BindingContext{onStartupBindingContext},
				ReloadAllOnValuesChanges: false,
			}).
			WithCompactionID(hookName)
		tasks = append(tasks, newTask.WithQueuedAt(queuedAt))
	}

	// 'Schedule' global hooks.
	schedHooks := op.ModuleManager.GetGlobalHooksInOrder(htypes.Schedule)
	for _, hookName := range schedHooks {
		hookLogLabels := utils.MergeLabels(logLabels, map[string]string{
			pkg.LogKeyHook:    hookName,
			"hook.type":       "global",
			"queue":           "main",
			pkg.LogKeyBinding: string(task.GlobalHookEnableScheduleBindings),
		})

		newTask := sh_task.NewTask(task.GlobalHookEnableScheduleBindings).
			WithLogLabels(hookLogLabels).
			WithQueueName("main").
			WithMetadata(task.HookMetadata{
				EventDescription: eventDescription,
				HookName:         hookName,
			})
		tasks = append(tasks, newTask.WithQueuedAt(queuedAt))
	}

	// Tasks to enable kubernetes events for all global hooks with kubernetes bindings.
	kubeHooks := op.ModuleManager.GetGlobalHooksInOrder(htypes.OnKubernetesEvent)
	for _, hookName := range kubeHooks {
		hookLogLabels := utils.MergeLabels(logLabels, map[string]string{
			pkg.LogKeyHook:    hookName,
			"hook.type":       "global",
			"queue":           "main",
			pkg.LogKeyBinding: string(task.GlobalHookEnableKubernetesBindings),
		})

		newTask := sh_task.NewTask(task.GlobalHookEnableKubernetesBindings).
			WithLogLabels(hookLogLabels).
			WithQueueName("main").
			WithMetadata(task.HookMetadata{
				EventDescription: eventDescription,
				HookName:         hookName,
			})
		tasks = append(tasks, newTask.WithQueuedAt(queuedAt))
	}

	// Task to wait for kubernetes.Synchronization.
	waitLogLabels := utils.MergeLabels(logLabels, map[string]string{
		"queue":           "main",
		pkg.LogKeyBinding: string(task.GlobalHookWaitKubernetesSynchronization),
	})
	waitTask := sh_task.NewTask(task.GlobalHookWaitKubernetesSynchronization).
		WithLogLabels(waitLogLabels).
		WithQueueName("main").
		WithMetadata(task.HookMetadata{
			EventDescription: eventDescription,
		})
	tasks = append(tasks, waitTask.WithQueuedAt(queuedAt))

	// Add "ConvergeModules" task to run modules converge sequence for the first time.
	convergeLabels := utils.MergeLabels(logLabels, map[string]string{
		"queue":           "main",
		pkg.LogKeyBinding: string(task.ConvergeModules),
	})
	convergeTask := converge.NewConvergeModulesTask(eventDescription, converge.OperatorStartup, convergeLabels)
	tasks = append(tasks, convergeTask.WithQueuedAt(queuedAt))

	return tasks
}

// CreateAndStartParallelQueues creates and starts named queues for executing parallel tasks in parallel
func (op *AddonOperator) CreateAndStartParallelQueues() {
	for i := range app.NumberOfParallelQueues {
		queueName := fmt.Sprintf(app.ParallelQueueNamePattern, i)
		if op.IsQueueExists(queueName) {
			log.Warn("Parallel queue already exists", slog.String("queue", queueName))
			continue
		}

		op.startQueue(queueName, op.TaskService.ParallelHandle)
		log.Debug("Parallel queue started",
			slog.String("queue", queueName))
	}
}

// CreateAndStartQueue creates a named queue and starts it.
// It returns false is queue is already created
func (op *AddonOperator) CreateAndStartQueue(queueName string) {
	op.startQueue(queueName, op.TaskService.Handle)
}

func (op *AddonOperator) startQueue(queueName string, handler func(ctx context.Context, t sh_task.Task) queue.TaskResult) {
	op.engine.TaskQueues.NewNamedQueue(queueName, handler,
		queue.WithCompactionCallback(queueutils.CompactionCallback(op.ModuleManager, op.Logger)),
		queue.WithCompactableTypes(queueutils.MergeTasks...),
		queue.WithLogger(op.Logger.With("operator.component", "queue", "queue", queueName)),
	)
	op.engine.TaskQueues.GetByName(queueName).Start(op.ctx)
}

// IsQueueExists returns true is queue is already created
func (op *AddonOperator) IsQueueExists(queueName string) bool {
	return op.engine.TaskQueues.GetByName(queueName) != nil
}

// CreateAndStartQueuesForGlobalHooks creates queues for all registered global hooks.
// It is safe to run this method multiple times, as it checks
// for existing queues.
func (op *AddonOperator) CreateAndStartQueuesForGlobalHooks() {
	for _, hookName := range op.ModuleManager.GetGlobalHooksNames() {
		h := op.ModuleManager.GetGlobalHook(hookName)
		for _, hookBinding := range h.GetHookConfig().Schedules {
			if !op.IsQueueExists(hookBinding.Queue) {
				op.CreateAndStartQueue(hookBinding.Queue)

				log.Debug("Queue started for global 'schedule' hook",
					slog.String("queue", hookBinding.Queue),
					slog.String(pkg.LogKeyHook, hookName))
			}
		}
		for _, hookBinding := range h.GetHookConfig().OnKubernetesEvents {
			if !op.IsQueueExists(hookBinding.Queue) {
				op.CreateAndStartQueue(hookBinding.Queue)

				log.Debug("Queue started for global 'kubernetes' hook",
					slog.String("queue", hookBinding.Queue),
					slog.String(pkg.LogKeyHook, hookName))
			}
		}
	}
}

// CreateAndStartQueuesForModuleHooks creates queues for registered module hooks.
// It is safe to run this method multiple times, as it checks
// for existing queues.
func (op *AddonOperator) CreateAndStartQueuesForModuleHooks(moduleName string) {
	m := op.ModuleManager.GetModule(moduleName)
	if m == nil {
		return
	}

	scheduleHooks := m.GetHooks(htypes.Schedule)
	for _, hook := range scheduleHooks {
		for _, hookBinding := range hook.GetHookConfig().Schedules {
			if !op.IsQueueExists(hookBinding.Queue) {
				op.CreateAndStartQueue(hookBinding.Queue)

				log.Debug("Queue started for module 'schedule'",
					slog.String("queue", hookBinding.Queue),
					slog.String(pkg.LogKeyHook, hook.GetName()))
			}
		}
	}

	kubeEventsHooks := m.GetHooks(htypes.OnKubernetesEvent)
	for _, hook := range kubeEventsHooks {
		for _, hookBinding := range hook.GetHookConfig().OnKubernetesEvents {
			if !op.IsQueueExists(hookBinding.Queue) {
				op.CreateAndStartQueue(hookBinding.Queue)

				log.Debug("Queue started for module 'kubernetes'",
					slog.String("queue", hookBinding.Queue),
					slog.String(pkg.LogKeyHook, hook.GetName()))
			}
		}
	}

	// TODO: probably we have some duplications here

	// for _, hookName := range op.ModuleManager.GetModuleHookNames(moduleName) {
	//	h := op.ModuleManager.GetModuleHook(hookName)
	//	for _, hookBinding := range h.Config.Schedules {
	//		if op.CreateAndStartQueue(hookBinding.Queue) {
	//			log.Debugf("Queue '%s' started for module 'schedule' hook %s", hookBinding.Queue, hookName)
	//		}
	//	}
	//	for _, hookBinding := range h.Config.OnKubernetesEvents {
	//		if op.CreateAndStartQueue(hookBinding.Queue) {
	//			log.Debugf("Queue '%s' started for module 'kubernetes' hook %s", hookBinding.Queue, hookName)
	//		}
	//	}
	//}
}

func (op *AddonOperator) CreateReloadModulesTasks(moduleNames []string, logLabels map[string]string, eventDescription string) []sh_task.Task {
	newTasks := make([]sh_task.Task, 0, len(moduleNames))
	queuedAt := time.Now()

	queuedModuleNames := ModulesWithPendingModuleRun(op.engine.TaskQueues.GetMain())

	// Add ModuleRun tasks to reload only specific modules.
	for _, moduleName := range moduleNames {
		// No task is required if ModuleRun is already in queue.
		if _, has := queuedModuleNames[moduleName]; has {
			continue
		}

		newLogLabels := utils.MergeLabels(logLabels)
		newLogLabels["module"] = moduleName
		delete(newLogLabels, "task.id")

		newTask := sh_task.NewTask(task.ModuleRun).
			WithLogLabels(newLogLabels).
			WithQueueName("main").
			WithMetadata(task.HookMetadata{
				EventDescription: eventDescription,
				ModuleName:       moduleName,
			})
		newTasks = append(newTasks, newTask.WithQueuedAt(queuedAt))
	}
	return newTasks
}

// taskDescriptionForTaskFlowLog generates a human-readable description of a task
// for logging purposes.
func taskDescriptionForTaskFlowLog(tsk sh_task.Task, action string, phase string, status string) string {
	hm := task.HookMetadataAccessor(tsk)
	taskType := string(tsk.GetType())

	// Start building the description
	description := formatTaskAction(taskType, action, status)

	// Format task-specific details based on task type
	details := formatTaskDetails(tsk, hm, phase)

	// Add trigger information if available
	triggerInfo := ""
	if hm.EventDescription != "" {
		triggerInfo = fmt.Sprintf(", trigger is %s", hm.EventDescription)
	}

	return description + details + triggerInfo
}

// formatTaskAction creates the first part of the task description
func formatTaskAction(taskType, action, status string) string {
	switch action {
	case "start":
		return fmt.Sprintf("%s task", taskType)
	case "end":
		return fmt.Sprintf("%s task done, result is '%s'", taskType, status)
	default:
		return fmt.Sprintf("%s task %s", action, taskType)
	}
}

// formatTaskDetails creates the task-specific part of the description
func formatTaskDetails(tsk sh_task.Task, hm task.HookMetadata, phase string) string {
	switch tsk.GetType() {
	case task.GlobalHookRun, task.ModuleHookRun:
		return formatHookTaskDetails(hm)

	case task.ConvergeModules:
		return formatConvergeTaskDetails(tsk, phase)

	case task.ModuleRun:
		details := fmt.Sprintf(" for module '%s', phase '%s'", hm.ModuleName, phase)
		if hm.DoModuleStartup {
			details += " with doModuleStartup"
		}
		return details

	case task.ParallelModuleRun:
		return fmt.Sprintf(" for modules '%s'", hm.ModuleName)

	case task.ModulePurge, task.ModuleDelete, task.ModuleEnsureCRDs:
		return fmt.Sprintf(" for module '%s'", hm.ModuleName)

	case task.GlobalHookEnableKubernetesBindings,
		task.GlobalHookWaitKubernetesSynchronization,
		task.GlobalHookEnableScheduleBindings:
		return " for the hook"

	case task.DiscoverHelmReleases:
		return ""

	default:
		return ""
	}
}

// formatHookTaskDetails formats details specific to hook tasks
func formatHookTaskDetails(hm task.HookMetadata) string {
	// Handle case with no binding contexts
	if len(hm.BindingContext) == 0 {
		return " for no binding"
	}

	var sb strings.Builder
	sb.WriteString(" for ")

	// Get primary binding context
	bc := hm.BindingContext[0]

	// Check if this is a synchronization task
	if bc.IsSynchronization() {
		sb.WriteString("Synchronization of ")
	}

	// Format binding information based on type and group
	bindingType := bc.Metadata.BindingType
	bindingGroup := bc.Metadata.Group

	if bindingGroup == "" {
		// No group specified, format based on binding type
		if bindingType == htypes.OnKubernetesEvent || bindingType == htypes.Schedule {
			fmt.Fprintf(&sb, "'%s/%s'", bindingType, bc.Binding)
		} else {
			sb.WriteString(string(bindingType))
		}
	} else {
		// Use group information
		fmt.Fprintf(&sb, "'%s' group", bindingGroup)
	}

	// Add information about additional bindings
	if len(hm.BindingContext) > 1 {
		fmt.Fprintf(&sb, " and %d more bindings", len(hm.BindingContext)-1)
	} else {
		sb.WriteString(" binding")
	}

	return sb.String()
}

// formatConvergeTaskDetails formats details specific to converge tasks
func formatConvergeTaskDetails(tsk sh_task.Task, phase string) string {
	if taskEvent, ok := tsk.GetProp(converge.ConvergeEventProp).(converge.ConvergeEvent); ok {
		return fmt.Sprintf(" for %s in phase '%s'", string(taskEvent), phase)
	}
	return ""
}

// logTaskAdd prints info about queued tasks.
func (op *AddonOperator) logTaskAdd(logEntry *log.Logger, action string, tasks ...sh_task.Task) {
	logger := logEntry.With(pkg.LogKeyTaskFlow, "add")
	for _, tsk := range tasks {
		logger.Info(taskDescriptionForTaskFlowLog(tsk, action, "", ""))
	}
}

// getConvergeQueues returns list of all queues where modules converge tasks may be running
func (op *AddonOperator) getConvergeQueues() []*queue.TaskQueue {
	convergeQueues := make([]*queue.TaskQueue, 0, app.NumberOfParallelQueues+1)
	for i := 0; i < app.NumberOfParallelQueues; i++ {
		convergeQueues = append(convergeQueues, op.engine.TaskQueues.GetByName(fmt.Sprintf(app.ParallelQueueNamePattern, i)))
	}
	convergeQueues = append(convergeQueues, op.engine.TaskQueues.GetMain())
	return convergeQueues
}
