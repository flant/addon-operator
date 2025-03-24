package addon_operator

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/trace"
	"strings"
	"sync"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/gofrs/uuid/v5"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/leaderelection"

	"github.com/flant/addon-operator/pkg"
	"github.com/flant/addon-operator/pkg/addon-operator/converge"
	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm"
	"github.com/flant/addon-operator/pkg/helm_resources_manager"
	hookTypes "github.com/flant/addon-operator/pkg/hook/types"
	"github.com/flant/addon-operator/pkg/kube_config_manager"
	"github.com/flant/addon-operator/pkg/kube_config_manager/config"
	"github.com/flant/addon-operator/pkg/module_manager"
	gohook "github.com/flant/addon-operator/pkg/module_manager/go_hook"
	"github.com/flant/addon-operator/pkg/module_manager/models/hooks"
	"github.com/flant/addon-operator/pkg/module_manager/models/hooks/kind"
	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
	"github.com/flant/addon-operator/pkg/module_manager/models/modules/events"
	"github.com/flant/addon-operator/pkg/task"
	taskservice "github.com/flant/addon-operator/pkg/task/service"
	"github.com/flant/addon-operator/pkg/utils"
	"github.com/flant/kube-client/client"
	shapp "github.com/flant/shell-operator/pkg/app"
	runtimeConfig "github.com/flant/shell-operator/pkg/config"
	"github.com/flant/shell-operator/pkg/debug"
	bc "github.com/flant/shell-operator/pkg/hook/binding_context"
	"github.com/flant/shell-operator/pkg/hook/controller"
	htypes "github.com/flant/shell-operator/pkg/hook/types"
	"github.com/flant/shell-operator/pkg/metric"
	shell_operator "github.com/flant/shell-operator/pkg/shell-operator"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
	fileUtils "github.com/flant/shell-operator/pkg/utils/file"
	"github.com/flant/shell-operator/pkg/utils/measure"
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
	parallelTaskChannels parallelTaskChannels

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

	MetricStorage metric.Storage

	// LeaderElector represents leaderelection client for HA mode
	LeaderElector *leaderelection.LeaderElector

	// CRDExtraLabels contains labels for processing CRD files
	// like heritage=addon-operator
	CRDExtraLabels map[string]string

	discoveredGVKsLock sync.Mutex
	// discoveredGVKs is a map of GVKs from applied modules' CRDs
	discoveredGVKs map[string]struct{}

	Logger *log.Logger

	l sync.Mutex
	// converge state
	ConvergeState *converge.ConvergeState

	TaskService *taskservice.TaskHandlerService
}

type parallelQueueEvent struct {
	moduleName string
	errMsg     string
	succeeded  bool
}

type parallelTaskChannels struct {
	l        sync.Mutex
	channels map[string]chan parallelQueueEvent
}

func (pq *parallelTaskChannels) Set(id string, c chan parallelQueueEvent) {
	pq.l.Lock()
	pq.channels[id] = c
	pq.l.Unlock()
}

func (pq *parallelTaskChannels) Get(id string) (chan parallelQueueEvent, bool) {
	pq.l.Lock()
	defer pq.l.Unlock()
	c, ok := pq.channels[id]
	return c, ok
}

func (pq *parallelTaskChannels) Delete(id string) {
	pq.l.Lock()
	delete(pq.channels, id)
	pq.l.Unlock()
}

type Option func(operator *AddonOperator)

func WithLogger(logger *log.Logger) Option {
	return func(operator *AddonOperator) {
		operator.Logger = logger
	}
}

func NewAddonOperator(ctx context.Context, opts ...Option) *AddonOperator {
	cctx, cancel := context.WithCancel(ctx)

	ao := &AddonOperator{
		ctx:              cctx,
		cancel:           cancel,
		DefaultNamespace: app.Namespace,
		ConvergeState:    converge.NewConvergeState(),
		parallelTaskChannels: parallelTaskChannels{
			channels: make(map[string]chan parallelQueueEvent),
		},
		discoveredGVKs: make(map[string]struct{}),
	}

	for _, opt := range opts {
		opt(ao)
	}

	if ao.Logger == nil {
		ao.Logger = log.NewLogger(log.Options{}).Named("addon-operator")
	}

	so := shell_operator.NewShellOperator(cctx, shell_operator.WithLogger(ao.Logger.Named("shell-operator")))

	// initialize logging before Assemble
	rc := runtimeConfig.NewConfig(ao.Logger)
	// Init logging subsystem.
	shapp.SetupLogging(rc, ao.Logger)

	// Have to initialize common operator to have all common dependencies below
	err := so.AssembleCommonOperator(app.ListenAddress, app.ListenPort, map[string]string{
		"module":             "",
		"hook":               "",
		pkg.MetricKeyBinding: "",
		"queue":              "",
		"kind":               "",
	})
	if err != nil {
		panic(err)
	}

	registerHookMetrics(so.HookMetricStorage)

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
	return op.ConvergeState.FirstRunPhase == converge.FirstDone
}

func (op *AddonOperator) globalHooksNotExecutedYet() bool {
	return op.ConvergeState.FirstRunPhase == converge.FirstNotStarted || (op.ConvergeState.FirstRunPhase == converge.FirstStarted && op.ConvergeState.Phase == converge.StandBy)
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
	tqs.NewNamedQueue("main", op.TaskHandler)

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
			"hook":            hookName,
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
			})
		tasks = append(tasks, newTask.WithQueuedAt(queuedAt))
	}

	// 'Schedule' global hooks.
	schedHooks := op.ModuleManager.GetGlobalHooksInOrder(htypes.Schedule)
	for _, hookName := range schedHooks {
		hookLogLabels := utils.MergeLabels(logLabels, map[string]string{
			"hook":            hookName,
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
			"hook":            hookName,
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

// CreatePurgeTasks returns ModulePurge tasks for each unknown Helm release.
func (op *AddonOperator) CreatePurgeTasks(modulesToPurge []string, t sh_task.Task) []sh_task.Task {
	newTasks := make([]sh_task.Task, 0, len(modulesToPurge))
	queuedAt := time.Now()

	hm := task.HookMetadataAccessor(t)

	// Add ModulePurge tasks to purge unknown helm releases at start.
	for _, moduleName := range modulesToPurge {
		newLogLabels := utils.MergeLabels(t.GetLogLabels())
		newLogLabels["module"] = moduleName
		delete(newLogLabels, "task.id")

		newTask := sh_task.NewTask(task.ModulePurge).
			WithLogLabels(newLogLabels).
			WithQueueName("main").
			WithMetadata(task.HookMetadata{
				EventDescription: hm.EventDescription,
				ModuleName:       moduleName,
			})
		newTasks = append(newTasks, newTask.WithQueuedAt(queuedAt))
	}

	return newTasks
}

// HandleConvergeModules is a multi-phase task.
func (op *AddonOperator) HandleConvergeModules(t sh_task.Task, logLabels map[string]string) queue.TaskResult {
	defer trace.StartRegion(context.Background(), "ConvergeModules").End()

	var res queue.TaskResult
	logEntry := utils.EnrichLoggerWithLabels(op.Logger, logLabels)

	taskEvent, ok := t.GetProp(converge.ConvergeEventProp).(converge.ConvergeEvent)
	if !ok {
		logEntry.Error("Possible bug! Wrong prop type in ConvergeModules: got another type instead string.",
			slog.String("type", fmt.Sprintf("%T(%#[1]v)", t.GetProp("event"))))
		res.Status = queue.Fail
		return res
	}

	hm := task.HookMetadataAccessor(t)

	var handleErr error

	op.ConvergeState.PhaseLock.Lock()
	defer op.ConvergeState.PhaseLock.Unlock()
	if op.ConvergeState.Phase == converge.StandBy {
		logEntry.Debug("ConvergeModules: start")

		// Deduplicate tasks: remove ConvergeModules tasks right after the current task.
		RemoveAdjacentConvergeModules(op.engine.TaskQueues.GetByName(t.GetQueueName()), t.GetId(), logLabels, op.Logger)
		op.ConvergeState.Phase = converge.RunBeforeAll
	}

	if op.ConvergeState.Phase == converge.RunBeforeAll {
		// Put BeforeAll tasks before current task.
		tasks := op.CreateBeforeAllTasks(t.GetLogLabels(), hm.EventDescription)
		op.ConvergeState.Phase = converge.WaitBeforeAll
		if len(tasks) > 0 {
			res.HeadTasks = tasks
			res.Status = queue.Keep
			op.logTaskAdd(logEntry, "head", res.HeadTasks...)
			return res
		}
	}

	if op.ConvergeState.Phase == converge.WaitBeforeAll {
		logEntry.Info("ConvergeModules: beforeAll hooks done, run modules")
		var state *module_manager.ModulesState

		state, handleErr = op.ModuleManager.RefreshEnabledState(t.GetLogLabels())
		if handleErr == nil {
			// TODO disable hooks before was done in DiscoverModulesStateRefresh. Should we stick to this solution or disable events later during the handling each ModuleDelete task?
			// Disable events for disabled modules.
			for _, moduleName := range state.ModulesToDisable {
				op.ModuleManager.DisableModuleHooks(moduleName)
				// op.DrainModuleQueues(moduleName)
			}
			// Set ModulesToEnable list to properly run onStartup hooks for first converge.
			if !op.IsStartupConvergeDone() {
				state.ModulesToEnable = state.AllEnabledModules
				// send ModuleEvents for each disabled module on first converge to update dsabled modules' states (for the sake of disabled by <extender_name>)
				enabledModules := make(map[string]struct{}, len(state.AllEnabledModules))
				for _, enabledModule := range state.AllEnabledModules {
					enabledModules[enabledModule] = struct{}{}
				}

				logEntry.Debug("ConvergeModules: send module disabled events")
				go func() {
					for _, moduleName := range op.ModuleManager.GetModuleNames() {
						if _, enabled := enabledModules[moduleName]; !enabled {
							op.ModuleManager.SendModuleEvent(events.ModuleEvent{
								ModuleName: moduleName,
								EventType:  events.ModuleDisabled,
							})
						}
					}
				}()
			}
			tasks := op.CreateConvergeModulesTasks(state, t.GetLogLabels(), string(taskEvent))

			op.ConvergeState.Phase = converge.WaitDeleteAndRunModules
			if len(tasks) > 0 {
				res.HeadTasks = tasks
				res.Status = queue.Keep
				op.logTaskAdd(logEntry, "head", res.HeadTasks...)
				return res
			}
		}
	}

	if op.ConvergeState.Phase == converge.WaitDeleteAndRunModules {
		logEntry.Info("ConvergeModules: ModuleRun tasks done, execute AfterAll global hooks")
		// Put AfterAll tasks before current task.
		tasks, handleErr := op.CreateAfterAllTasks(t.GetLogLabels(), hm.EventDescription)
		if handleErr == nil {
			op.ConvergeState.Phase = converge.WaitAfterAll
			if len(tasks) > 0 {
				res.HeadTasks = tasks
				res.Status = queue.Keep
				op.logTaskAdd(logEntry, "head", res.HeadTasks...)
				return res
			}
		}
	}

	// It is the last phase of ConvergeModules task, reset operator's Converge phase.
	if op.ConvergeState.Phase == converge.WaitAfterAll {
		op.ConvergeState.Phase = converge.StandBy
		logEntry.Info("ConvergeModules task done")
		res.Status = queue.Success
		return res
	}

	if handleErr != nil {
		res.Status = queue.Fail
		logEntry.Error("ConvergeModules failed, requeue task to retry after delay.",
			slog.String("phase", string(op.ConvergeState.Phase)),
			slog.Int("count", t.GetFailureCount()+1),
			log.Err(handleErr))
		op.MetricStorage.CounterAdd("{PREFIX}modules_discover_errors_total", 1.0, map[string]string{})
		t.UpdateFailureMessage(handleErr.Error())
		t.WithQueuedAt(time.Now())
		return res
	}

	logEntry.Debug("ConvergeModules success")
	res.Status = queue.Success

	return res
}

// CreateBeforeAllTasks returns tasks to run BeforeAll global hooks.
func (op *AddonOperator) CreateBeforeAllTasks(logLabels map[string]string, eventDescription string) []sh_task.Task {
	tasks := make([]sh_task.Task, 0)
	queuedAt := time.Now()

	// Get 'beforeAll' global hooks.
	beforeAllHooks := op.ModuleManager.GetGlobalHooksInOrder(hookTypes.BeforeAll)

	for _, hookName := range beforeAllHooks {
		hookLogLabels := utils.MergeLabels(logLabels, map[string]string{
			"hook":            hookName,
			"hook.type":       "global",
			"queue":           "main",
			pkg.LogKeyBinding: string(hookTypes.BeforeAll),
		})
		// remove task.id — it is set by NewTask
		delete(hookLogLabels, "task.id")

		// bc := module_manager.BindingContext{BindingContext: hook.BindingContext{Binding: stringmodule_manager.BeforeAll)}}
		// bc.KubernetesSnapshots := ModuleManager.GetGlobalHook(hookName).HookController.KubernetesSnapshots()

		beforeAllBc := bc.BindingContext{
			Binding: string(hookTypes.BeforeAll),
		}
		beforeAllBc.Metadata.BindingType = hookTypes.BeforeAll
		beforeAllBc.Metadata.IncludeAllSnapshots = true

		newTask := sh_task.NewTask(task.GlobalHookRun).
			WithLogLabels(hookLogLabels).
			WithQueueName("main").
			WithMetadata(task.HookMetadata{
				EventDescription:         eventDescription,
				HookName:                 hookName,
				BindingType:              hookTypes.BeforeAll,
				BindingContext:           []bc.BindingContext{beforeAllBc},
				ReloadAllOnValuesChanges: false,
			})
		tasks = append(tasks, newTask.WithQueuedAt(queuedAt))
	}

	return tasks
}

// CreateAfterAllTasks returns tasks to run AfterAll global hooks.
func (op *AddonOperator) CreateAfterAllTasks(logLabels map[string]string, eventDescription string) ([]sh_task.Task, error) {
	tasks := make([]sh_task.Task, 0)
	queuedAt := time.Now()

	// Get 'afterAll' global hooks.
	afterAllHooks := op.ModuleManager.GetGlobalHooksInOrder(hookTypes.AfterAll)

	for i, hookName := range afterAllHooks {
		hookLogLabels := utils.MergeLabels(logLabels, map[string]string{
			"hook":            hookName,
			"hook.type":       "global",
			"queue":           "main",
			pkg.LogKeyBinding: string(hookTypes.AfterAll),
		})
		delete(hookLogLabels, "task.id")

		afterAllBc := bc.BindingContext{
			Binding: string(hookTypes.AfterAll),
		}
		afterAllBc.Metadata.BindingType = hookTypes.AfterAll
		afterAllBc.Metadata.IncludeAllSnapshots = true

		taskMetadata := task.HookMetadata{
			EventDescription: eventDescription,
			HookName:         hookName,
			BindingType:      hookTypes.AfterAll,
			BindingContext:   []bc.BindingContext{afterAllBc},
		}
		if i == len(afterAllHooks)-1 {
			taskMetadata.LastAfterAllHook = true
			globalValues := op.ModuleManager.GetGlobal().GetValues(false)
			taskMetadata.ValuesChecksum = globalValues.Checksum()
		}

		newTask := sh_task.NewTask(task.GlobalHookRun).
			WithLogLabels(hookLogLabels).
			WithQueueName("main").
			WithMetadata(taskMetadata)
		tasks = append(tasks, newTask.WithQueuedAt(queuedAt))
	}

	return tasks, nil
}

// CreateAndStartParallelQueues creates and starts named queues for executing parallel tasks in parallel
func (op *AddonOperator) CreateAndStartParallelQueues() {
	for i := range app.NumberOfParallelQueues {
		queueName := fmt.Sprintf(app.ParallelQueueNamePattern, i)
		if op.IsQueueExists(queueName) {
			log.Warn("Parallel queue already exists", slog.String("queue", queueName))
			continue
		}

		op.startQueue(queueName, op.ParallelTasksHandler)
		log.Debug("Parallel queue started",
			slog.String("queue", queueName))
	}
}

// CreateAndStartQueue creates a named queue and starts it.
// It returns false is queue is already created
func (op *AddonOperator) CreateAndStartQueue(queueName string) {
	op.startQueue(queueName, op.TaskHandler)
}

func (op *AddonOperator) startQueue(queueName string, handler func(ctx context.Context, t sh_task.Task) queue.TaskResult) {
	op.engine.TaskQueues.NewNamedQueue(queueName, handler)
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
					slog.String("hook", hookName))
			}
		}
		for _, hookBinding := range h.GetHookConfig().OnKubernetesEvents {
			if !op.IsQueueExists(hookBinding.Queue) {
				op.CreateAndStartQueue(hookBinding.Queue)

				log.Debug("Queue started for global 'kubernetes' hook",
					slog.String("queue", hookBinding.Queue),
					slog.String("hook", hookName))
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
					slog.String("hook", hook.GetName()))
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
					slog.String("hook", hook.GetName()))
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

func (op *AddonOperator) DrainModuleQueues(modName string) {
	m := op.ModuleManager.GetModule(modName)
	if m == nil {
		log.Warn("Module is absent when we try to drain its queue", slog.String("module", modName))
		return
	}

	scheduleHooks := m.GetHooks(htypes.Schedule)
	for _, hook := range scheduleHooks {
		for _, hookBinding := range hook.GetHookConfig().Schedules {
			DrainNonMainQueue(op.engine.TaskQueues.GetByName(hookBinding.Queue))
		}
	}

	kubeEventsHooks := m.GetHooks(htypes.OnKubernetesEvent)
	for _, hook := range kubeEventsHooks {
		for _, hookBinding := range hook.GetHookConfig().OnKubernetesEvents {
			DrainNonMainQueue(op.engine.TaskQueues.GetByName(hookBinding.Queue))
		}
	}

	// TODO: duplication here?
	// for _, hookName := range op.ModuleManager.GetModuleHookNames(modName) {
	//	h := op.ModuleManager.GetModuleHook(hookName)
	//	for _, hookBinding := range h.Get.Schedules {
	//		DrainNonMainQueue(op.engine.TaskQueues.GetByName(hookBinding.Queue))
	//	}
	//	for _, hookBinding := range h.Config.OnKubernetesEvents {
	//		DrainNonMainQueue(op.engine.TaskQueues.GetByName(hookBinding.Queue))
	//	}
	//}
}

// ParallelTasksHandler handles limited types of tasks in parallel queues.
func (op *AddonOperator) ParallelTasksHandler(_ context.Context, t sh_task.Task) queue.TaskResult {
	taskLogLabels := t.GetLogLabels()
	taskLogEntry := utils.EnrichLoggerWithLabels(op.Logger, taskLogLabels)
	var res queue.TaskResult

	op.logTaskStart(taskLogEntry, t)

	op.UpdateWaitInQueueMetric(t)

	switch t.GetType() {
	case task.ModuleRun:
		res = op.HandleModuleRun(t, taskLogLabels)

	case task.ModuleHookRun:
		res = op.HandleModuleHookRun(t, taskLogLabels)
	}

	op.logTaskEnd(taskLogEntry, t, res)
	hm := task.HookMetadataAccessor(t)
	if hm.ParallelRunMetadata == nil || len(hm.ParallelRunMetadata.ChannelId) == 0 {
		taskLogEntry.Warn("Parallel task had no communication channel set")
	}

	if parallelChannel, ok := op.parallelTaskChannels.Get(hm.ParallelRunMetadata.ChannelId); ok {
		if res.Status == queue.Fail {
			parallelChannel <- parallelQueueEvent{
				moduleName: hm.ModuleName,
				errMsg:     t.GetFailureMessage(),
				succeeded:  false,
			}
		}
		if res.Status == queue.Success && t.GetType() == task.ModuleRun && len(res.AfterTasks) == 0 {
			parallelChannel <- parallelQueueEvent{
				moduleName: hm.ModuleName,
				succeeded:  true,
			}
		}
	}
	return res
}

// TaskHandler handles tasks in queue.
func (op *AddonOperator) TaskHandler(ctx context.Context, t sh_task.Task) queue.TaskResult {
	taskLogLabels := t.GetLogLabels()
	taskLogEntry := utils.EnrichLoggerWithLabels(op.Logger, taskLogLabels)
	var res queue.TaskResult

	op.logTaskStart(taskLogEntry, t)

	op.UpdateWaitInQueueMetric(t)

	switch t.GetType() {
	case task.GlobalHookRun:
		res = op.TaskService.Handle(ctx, t)

	case task.GlobalHookEnableScheduleBindings:
		res = op.TaskService.Handle(ctx, t)

	case task.GlobalHookEnableKubernetesBindings:
		res = op.TaskService.Handle(ctx, t)

	case task.GlobalHookWaitKubernetesSynchronization:
		res = op.TaskService.Handle(ctx, t)

	case task.DiscoverHelmReleases:
		res = op.TaskService.Handle(ctx, t)

	case task.ApplyKubeConfigValues:
		res = op.TaskService.Handle(ctx, t)

	case task.ConvergeModules:
		res = op.HandleConvergeModules(t, taskLogLabels)

	case task.ModuleRun:
		res = op.HandleModuleRun(t, taskLogLabels)

	case task.ParallelModuleRun:
		res = op.HandleParallelModuleRun(t, taskLogLabels)

	case task.ModuleDelete:
		res = op.TaskService.Handle(ctx, t)

	case task.ModuleHookRun:
		res = op.TaskService.Handle(ctx, t)

	case task.ModulePurge:
		res = op.TaskService.Handle(ctx, t)

	case task.ModuleEnsureCRDs:
		res = op.TaskService.Handle(ctx, t)
	}

	if res.Status == queue.Success {
		origAfterHandle := res.AfterHandle
		res.AfterHandle = func() {
			op.CheckConvergeStatus(t)
			if origAfterHandle != nil {
				origAfterHandle()
			}
		}
	}

	op.logTaskEnd(taskLogEntry, t, res)

	return res
}

// UpdateWaitInQueueMetric increases task_wait_in_queue_seconds_total counter for the task type.
// TODO pass queue name from handler, not from task
func (op *AddonOperator) UpdateWaitInQueueMetric(t sh_task.Task) {
	metricLabels := map[string]string{
		"module":             "",
		"hook":               "",
		pkg.MetricKeyBinding: string(t.GetType()),
		"queue":              t.GetQueueName(),
	}

	hm := task.HookMetadataAccessor(t)

	switch t.GetType() {
	case task.GlobalHookRun,
		task.GlobalHookEnableScheduleBindings,
		task.GlobalHookEnableKubernetesBindings,
		task.GlobalHookWaitKubernetesSynchronization:
		metricLabels["hook"] = hm.HookName

	case task.ModuleRun,
		task.ModuleDelete,
		task.ModuleHookRun,
		task.ModulePurge:
		metricLabels["module"] = hm.ModuleName

	case task.ConvergeModules,
		task.DiscoverHelmReleases:
		// no action required
	}

	if t.GetType() == task.GlobalHookRun {
		// set binding name instead of type
		metricLabels[pkg.MetricKeyBinding] = hm.Binding
	}
	if t.GetType() == task.ModuleHookRun {
		// set binding name instead of type
		metricLabels["hook"] = hm.HookName
		metricLabels[pkg.MetricKeyBinding] = hm.Binding
	}

	taskWaitTime := time.Since(t.GetQueuedAt()).Seconds()
	op.MetricStorage.CounterAdd("{PREFIX}task_wait_in_queue_seconds_total", taskWaitTime, metricLabels)
}

// HandleModuleEnsureCRDs ensure CRDs for module.
func (op *AddonOperator) HandleModuleEnsureCRDs(t sh_task.Task, labels map[string]string) queue.TaskResult {
	defer trace.StartRegion(context.Background(), "ModuleEnsureCRDs").End()

	hm := task.HookMetadataAccessor(t)
	res := queue.TaskResult{
		Status: queue.Success,
	}
	baseModule := op.ModuleManager.GetModule(hm.ModuleName)
	logEntry := utils.EnrichLoggerWithLabels(op.Logger, labels)
	logEntry.Debug("Module ensureCRDs", slog.String("name", hm.ModuleName))

	if appliedGVKs, err := op.EnsureCRDs(baseModule); err != nil {
		op.ModuleManager.UpdateModuleLastErrorAndNotify(baseModule, err)
		logEntry.Error("ModuleEnsureCRDs failed.", log.Err(err))
		t.UpdateFailureMessage(err.Error())
		t.WithQueuedAt(time.Now())
		res.Status = queue.Fail
	} else {
		op.discoveredGVKsLock.Lock()
		for _, gvk := range appliedGVKs {
			op.discoveredGVKs[gvk] = struct{}{}
		}
		op.discoveredGVKsLock.Unlock()
	}

	return res
}

// HandleParallelModuleRun runs multiple ModuleRun tasks in parallel and aggregates their results.
// It creates tasks for each module in separate parallel queues, then monitors their completion
// through a communication channel.
func (op *AddonOperator) HandleParallelModuleRun(t sh_task.Task, labels map[string]string) queue.TaskResult {
	defer trace.StartRegion(context.Background(), "ParallelModuleRun").End()

	var res queue.TaskResult
	logEntry := utils.EnrichLoggerWithLabels(op.Logger, labels)
	hm := task.HookMetadataAccessor(t)

	if hm.ParallelRunMetadata == nil {
		logEntry.Error("Possible bug! Couldn't get task ParallelRunMetadata for a parallel task.",
			slog.String("description", hm.EventDescription))
		res.Status = queue.Fail
		return res
	}

	i := 0
	parallelChannel := make(chan parallelQueueEvent)
	op.parallelTaskChannels.Set(t.GetId(), parallelChannel)
	logEntry.Debug("ParallelModuleRun available parallel event channels",
		slog.String("channels", fmt.Sprintf("%v", op.parallelTaskChannels.channels)))
	for moduleName, moduleMetadata := range hm.ParallelRunMetadata.GetModulesMetadata() {
		queueName := fmt.Sprintf(app.ParallelQueueNamePattern, i%(app.NumberOfParallelQueues-1))
		newLogLabels := utils.MergeLabels(labels)
		newLogLabels["module"] = moduleName
		delete(newLogLabels, "task.id")
		newTask := sh_task.NewTask(task.ModuleRun).
			WithLogLabels(newLogLabels).
			WithQueueName(queueName).
			WithMetadata(task.HookMetadata{
				EventDescription: hm.EventDescription,
				ModuleName:       moduleName,
				DoModuleStartup:  moduleMetadata.DoModuleStartup,
				IsReloadAll:      hm.IsReloadAll,
				ParallelRunMetadata: &task.ParallelRunMetadata{
					ChannelId: t.GetId(),
				},
			})
		op.engine.TaskQueues.GetByName(queueName).AddLast(newTask)
		i++
	}

	// map to hold modules' errors
	tasksErrors := make(map[string]string)
L:
	for {
		select {
		case parallelEvent := <-parallelChannel:
			logEntry.Debug("ParallelModuleRun event received",
				slog.String("event", fmt.Sprintf("%v", parallelEvent)))
			if len(parallelEvent.errMsg) != 0 {
				if tasksErrors[parallelEvent.moduleName] != parallelEvent.errMsg {
					tasksErrors[parallelEvent.moduleName] = parallelEvent.errMsg
					t.UpdateFailureMessage(formatErrorSummary(tasksErrors))
				}
				t.IncrementFailureCount()
				continue L
			}
			if parallelEvent.succeeded {
				hm.ParallelRunMetadata.DeleteModuleMetadata(parallelEvent.moduleName)
				// all ModuleRun tasks were executed successfully
				if len(hm.ParallelRunMetadata.GetModulesMetadata()) == 0 {
					break L
				}
				delete(tasksErrors, parallelEvent.moduleName)
				t.UpdateFailureMessage(formatErrorSummary(tasksErrors))
				newMeta := task.HookMetadata{
					EventDescription:    hm.EventDescription,
					ModuleName:          fmt.Sprintf("Parallel run for %s", strings.Join(hm.ParallelRunMetadata.ListModules(), ", ")),
					IsReloadAll:         hm.IsReloadAll,
					ParallelRunMetadata: hm.ParallelRunMetadata,
				}
				t.UpdateMetadata(newMeta)
			}

		case <-hm.ParallelRunMetadata.Context.Done():
			logEntry.Debug("ParallelModuleRun context canceled")
			// remove channel from map so that ModuleRun handlers couldn't send into it
			op.parallelTaskChannels.Delete(t.GetId())
			t := time.NewTimer(time.Second * 3)
			for {
				select {
				// wait for several seconds if any ModuleRun task wants to send an event
				case <-t.C:
					logEntry.Debug("ParallelModuleRun task aborted")
					res.Status = queue.Success
					return res

				// drain channel to unblock handlers if any
				case <-parallelChannel:
				}
			}
		}
	}
	op.parallelTaskChannels.Delete(t.GetId())
	res.Status = queue.Success

	return res
}

// fomartErrorSumamry constructs a string of errors from the map
func formatErrorSummary(errors map[string]string) string {
	errSummary := "\n\tErrors:\n"
	for moduleName, moduleErr := range errors {
		errSummary += fmt.Sprintf("\t- %s: %s", moduleName, moduleErr)
	}

	return errSummary
}

// HandleModuleRun starts a module by executing module hooks and installing a Helm chart.
//
// Execution sequence:
// - Run onStartup hooks.
// - Queue kubernetes hooks as Synchronization tasks.
// - Wait until Synchronization tasks are done to fill all snapshots.
// - Enable kubernetes events.
// - Enable schedule events.
// - Run beforeHelm hooks
// - Run Helm to install or upgrade a module's chart.
// - Run afterHelm hooks.
//
// ModuleRun is restarted if hook or chart is failed.
// After first HandleModuleRun success, no onStartup and kubernetes.Synchronization tasks will run.
func (op *AddonOperator) HandleModuleRun(t sh_task.Task, labels map[string]string) (res queue.TaskResult) { //nolint: nonamedreturns
	defer trace.StartRegion(context.Background(), "ModuleRun").End()

	logEntry := utils.EnrichLoggerWithLabels(op.Logger, labels)
	hm := task.HookMetadataAccessor(t)
	baseModule := op.ModuleManager.GetModule(hm.ModuleName)

	// Break error loop when module becomes disabled.
	if !op.ModuleManager.IsModuleEnabled(baseModule.GetName()) {
		res.Status = queue.Success
		return res
	}

	metricLabels := map[string]string{
		"module":     hm.ModuleName,
		"activation": labels["event.type"],
	}

	defer measure.Duration(func(d time.Duration) {
		op.engine.MetricStorage.HistogramObserve("{PREFIX}module_run_seconds", d.Seconds(), metricLabels, nil)
	})()

	var moduleRunErr error
	valuesChanged := false

	defer func(res *queue.TaskResult, valuesChanged *bool) {
		op.ModuleManager.UpdateModuleLastErrorAndNotify(baseModule, moduleRunErr)
		if moduleRunErr != nil {
			res.Status = queue.Fail
			logEntry.Error("ModuleRun failed. Requeue task to retry after delay.",
				slog.String("phase", string(baseModule.GetPhase())),
				slog.Int("count", t.GetFailureCount()+1),
				log.Err(moduleRunErr))
			op.engine.MetricStorage.CounterAdd("{PREFIX}module_run_errors_total", 1.0, map[string]string{"module": hm.ModuleName})
			t.UpdateFailureMessage(moduleRunErr.Error())
			t.WithQueuedAt(time.Now())
		}

		if res.Status != queue.Success {
			return
		}

		if *valuesChanged {
			logEntry.Info("ModuleRun success, values changed, restart module")
			// One of afterHelm hooks changes values, run ModuleRun again: copy task, but disable startup hooks.
			hm.DoModuleStartup = false
			hm.EventDescription = "AfterHelm-Hooks-Change-Values"
			newLabels := utils.MergeLabels(t.GetLogLabels())
			delete(newLabels, "task.id")
			newTask := sh_task.NewTask(task.ModuleRun).
				WithLogLabels(newLabels).
				WithQueueName(t.GetQueueName()).
				WithMetadata(hm)

			res.AfterTasks = []sh_task.Task{newTask.WithQueuedAt(time.Now())}
			op.logTaskAdd(logEntry, "after", res.AfterTasks...)
		} else {
			logEntry.Info("ModuleRun success, module is ready")
		}
	}(&res, &valuesChanged)

	// First module run on operator startup or when module is enabled.
	if baseModule.GetPhase() == modules.Startup {
		// Register module hooks on every enable.
		moduleRunErr = op.ModuleManager.RegisterModuleHooks(baseModule, labels)
		if moduleRunErr == nil {
			if hm.DoModuleStartup {
				logEntry.Debug("ModuleRun phase",
					slog.String("phase", string(baseModule.GetPhase())))

				treg := trace.StartRegion(context.Background(), "ModuleRun-OnStartup")

				// Start queues for module hooks.
				op.CreateAndStartQueuesForModuleHooks(baseModule.GetName())

				// Run onStartup hooks.
				moduleRunErr = op.ModuleManager.RunModuleHooks(baseModule, htypes.OnStartup, t.GetLogLabels())
				if moduleRunErr == nil {
					op.ModuleManager.SetModulePhaseAndNotify(baseModule, modules.OnStartupDone)
				}
				treg.End()
			} else {
				op.ModuleManager.SetModulePhaseAndNotify(baseModule, modules.OnStartupDone)
			}

			res.Status = queue.Repeat

			return res
		}
	}

	if baseModule.GetPhase() == modules.OnStartupDone {
		logEntry.Debug("ModuleRun phase", slog.String("phase", string(baseModule.GetPhase())))
		if baseModule.HasKubernetesHooks() {
			op.ModuleManager.SetModulePhaseAndNotify(baseModule, modules.QueueSynchronizationTasks)
		} else {
			// Skip Synchronization process if there are no kubernetes hooks.
			op.ModuleManager.SetModulePhaseAndNotify(baseModule, modules.EnableScheduleBindings)
		}

		res.Status = queue.Repeat

		return res
	}

	// Note: All hooks should be queued to fill snapshots before proceed to beforeHelm hooks.
	if baseModule.GetPhase() == modules.QueueSynchronizationTasks {
		logEntry.Debug("ModuleRun phase", slog.String("phase", string(baseModule.GetPhase())))

		// ModuleHookRun.Synchronization tasks for bindings with the "main" queue.
		mainSyncTasks := make([]sh_task.Task, 0)
		// ModuleHookRun.Synchronization tasks to add in parallel queues.
		parallelSyncTasks := make([]sh_task.Task, 0)
		// Wait for these ModuleHookRun.Synchronization tasks from parallel queues.
		parallelSyncTasksToWait := make([]sh_task.Task, 0)

		// Start monitors for each kubernetes binding in each module hook.
		err := op.ModuleManager.HandleModuleEnableKubernetesBindings(hm.ModuleName, func(hook *hooks.ModuleHook, info controller.BindingExecutionInfo) {
			queueName := info.QueueName
			if queueName == "main" && strings.HasPrefix(t.GetQueueName(), app.ParallelQueuePrefix) {
				// override main queue with parallel queue
				queueName = t.GetQueueName()
			}
			taskLogLabels := utils.MergeLabels(t.GetLogLabels(), map[string]string{
				"binding":   string(htypes.OnKubernetesEvent) + "Synchronization",
				"module":    hm.ModuleName,
				"hook":      hook.GetName(),
				"hook.type": "module",
				"queue":     queueName,
			})
			if len(info.BindingContext) > 0 {
				taskLogLabels["binding.name"] = info.BindingContext[0].Binding
			}
			delete(taskLogLabels, "task.id")

			kubernetesBindingID := uuid.Must(uuid.NewV4()).String()
			parallelRunMetadata := &task.ParallelRunMetadata{}
			if hm.ParallelRunMetadata != nil && len(hm.ParallelRunMetadata.ChannelId) != 0 {
				parallelRunMetadata.ChannelId = hm.ParallelRunMetadata.ChannelId
			}
			taskMeta := task.HookMetadata{
				EventDescription:         hm.EventDescription,
				ModuleName:               hm.ModuleName,
				HookName:                 hook.GetName(),
				BindingType:              htypes.OnKubernetesEvent,
				BindingContext:           info.BindingContext,
				AllowFailure:             info.AllowFailure,
				KubernetesBindingId:      kubernetesBindingID,
				WaitForSynchronization:   info.KubernetesBinding.WaitForSynchronization,
				MonitorIDs:               []string{info.KubernetesBinding.Monitor.Metadata.MonitorId},
				ExecuteOnSynchronization: info.KubernetesBinding.ExecuteHookOnSynchronization,
				ParallelRunMetadata:      parallelRunMetadata,
			}
			newTask := sh_task.NewTask(task.ModuleHookRun).
				WithLogLabels(taskLogLabels).
				WithQueueName(queueName).
				WithMetadata(taskMeta)
			newTask.WithQueuedAt(time.Now())

			if queueName == t.GetQueueName() {
				// Ignore "waitForSynchronization: false" for hooks in the main queue.
				// There is no way to not wait for these hooks.
				mainSyncTasks = append(mainSyncTasks, newTask)
			} else {
				// Do not wait for parallel hooks on "waitForSynchronization: false".
				if info.KubernetesBinding.WaitForSynchronization {
					parallelSyncTasksToWait = append(parallelSyncTasksToWait, newTask)
				} else {
					parallelSyncTasks = append(parallelSyncTasks, newTask)
				}
			}
		})
		if err != nil {
			// Fail to enable bindings: cannot start Kubernetes monitors.
			moduleRunErr = err
		} else {
			// Queue parallel tasks that should be waited.
			for _, tsk := range parallelSyncTasksToWait {
				q := op.engine.TaskQueues.GetByName(tsk.GetQueueName())
				if q == nil {
					logEntry.Error("queue is not found while EnableKubernetesBindings task",
						slog.String("queue", tsk.GetQueueName()))
				} else {
					thm := task.HookMetadataAccessor(tsk)
					q.AddLast(tsk)
					baseModule.Synchronization().QueuedForBinding(thm)
				}
			}
			op.logTaskAdd(logEntry, "append", parallelSyncTasksToWait...)

			// Queue regular parallel tasks.
			for _, tsk := range parallelSyncTasks {
				q := op.engine.TaskQueues.GetByName(tsk.GetQueueName())
				if q == nil {
					logEntry.Error("queue is not found while EnableKubernetesBindings task",
						slog.String("queue", tsk.GetQueueName()))
				} else {
					q.AddLast(tsk)
				}
			}
			op.logTaskAdd(logEntry, "append", parallelSyncTasks...)

			if len(parallelSyncTasksToWait) == 0 {
				// Skip waiting tasks in parallel queues, proceed to schedule bindings.

				op.ModuleManager.SetModulePhaseAndNotify(baseModule, modules.EnableScheduleBindings)
			} else {
				// There are tasks to wait.

				op.ModuleManager.SetModulePhaseAndNotify(baseModule, modules.WaitForSynchronization)
				logEntry.With("module.state", "wait-for-synchronization").
					Debug("ModuleRun wait for Synchronization")
			}

			// Put Synchronization tasks for kubernetes hooks before ModuleRun task.
			if len(mainSyncTasks) > 0 {
				res.HeadTasks = mainSyncTasks
				res.Status = queue.Keep
				op.logTaskAdd(logEntry, "head", mainSyncTasks...)
				return res
			}
		}

		res.Status = queue.Repeat

		return res
	}

	// Repeat ModuleRun if there are running Synchronization tasks to wait.
	if baseModule.GetPhase() == modules.WaitForSynchronization {
		if baseModule.Synchronization().IsCompleted() {
			// Proceed with the next phase.
			op.ModuleManager.SetModulePhaseAndNotify(baseModule, modules.EnableScheduleBindings)
			logEntry.Info("Synchronization done for module hooks")
		} else {
			// Debug messages every fifth second: print Synchronization state.
			if time.Now().UnixNano()%5000000000 == 0 {
				logEntry.Debug("ModuleRun wait Synchronization state",
					slog.Bool("moduleStartup", hm.DoModuleStartup),
					slog.Bool("syncNeeded", baseModule.SynchronizationNeeded()),
					slog.Bool("syncQueued", baseModule.Synchronization().HasQueued()),
					slog.Bool("syncDone", baseModule.Synchronization().IsCompleted()))
				baseModule.Synchronization().DebugDumpState(logEntry)
			}
			logEntry.Debug("Synchronization not completed, keep ModuleRun task in repeat mode")
			t.WithQueuedAt(time.Now())
		}

		res.Status = queue.Repeat

		return res
	}

	// Enable schedule events once at module start.
	if baseModule.GetPhase() == modules.EnableScheduleBindings {
		logEntry.Debug("ModuleRun phase", slog.String("phase", string(baseModule.GetPhase())))

		op.ModuleManager.EnableModuleScheduleBindings(hm.ModuleName)
		op.ModuleManager.SetModulePhaseAndNotify(baseModule, modules.CanRunHelm)

		res.Status = queue.Repeat

		return res
	}

	// Module start is done, module is ready to run hooks and helm chart.
	if baseModule.GetPhase() == modules.CanRunHelm {
		logEntry.Debug("ModuleRun phase", slog.String("phase", string(baseModule.GetPhase())))
		// run beforeHelm, helm, afterHelm
		valuesChanged, moduleRunErr = op.ModuleManager.RunModule(baseModule.Name, t.GetLogLabels())
	}

	res.Status = queue.Success

	return res
}

func (op *AddonOperator) HandleModuleHookRun(t sh_task.Task, labels map[string]string) queue.TaskResult {
	defer trace.StartRegion(context.Background(), "ModuleHookRun").End()

	var res queue.TaskResult
	logEntry := utils.EnrichLoggerWithLabels(op.Logger, labels)
	hm := task.HookMetadataAccessor(t)
	baseModule := op.ModuleManager.GetModule(hm.ModuleName)
	// TODO: check if module exists
	taskHook := baseModule.GetHookByName(hm.HookName)

	// Prevent hook running in parallel queue if module is disabled in "main" queue.
	if !op.ModuleManager.IsModuleEnabled(baseModule.GetName()) {
		res.Status = queue.Success
		return res
	}

	err := taskHook.RateLimitWait(context.Background())
	if err != nil {
		// This could happen when the Context is
		// canceled, or the expected wait time exceeds the Context's Deadline.
		// The best we can do without proper context usage is to repeat the task.
		res.Status = queue.Repeat
		return res
	}

	metricLabels := map[string]string{
		"module":     hm.ModuleName,
		"hook":       hm.HookName,
		"binding":    string(hm.BindingType),
		"queue":      t.GetQueueName(),
		"activation": labels["event.type"],
	}

	defer measure.Duration(func(d time.Duration) {
		op.engine.MetricStorage.HistogramObserve("{PREFIX}module_hook_run_seconds", d.Seconds(), metricLabels, nil)
	})()

	shouldRunHook := true

	isSynchronization := hm.IsSynchronization()
	if isSynchronization {
		// Synchronization is not a part of v0 contract, skip hook execution.
		if taskHook.GetHookConfig().Version == "v0" {
			shouldRunHook = false
			res.Status = queue.Success
		}
		// Check for "executeOnSynchronization: false".
		if !hm.ExecuteOnSynchronization {
			shouldRunHook = false
			res.Status = queue.Success
		}
	}

	// Combine tasks in the queue and compact binding contexts for v1 hooks.
	if shouldRunHook && taskHook.GetHookConfig().Version == "v1" {
		combineResult := op.engine.CombineBindingContextForHook(op.engine.TaskQueues.GetByName(t.GetQueueName()), t, func(tsk sh_task.Task) bool {
			thm := task.HookMetadataAccessor(tsk)
			// Stop combine process when Synchronization tasks have different
			// values in WaitForSynchronization or ExecuteOnSynchronization fields.
			if hm.KubernetesBindingId != "" && thm.KubernetesBindingId != "" {
				if hm.WaitForSynchronization != thm.WaitForSynchronization {
					return true
				}
				if hm.ExecuteOnSynchronization != thm.ExecuteOnSynchronization {
					return true
				}
			}
			// Task 'tsk' will be combined, so remove it from the SynchronizationState.
			if thm.IsSynchronization() {
				logEntry.Debug("Synchronization task is combined, mark it as Done",
					slog.String("name", thm.HookName),
					slog.String("binding", thm.Binding),
					slog.String("id", thm.KubernetesBindingId))
				baseModule.Synchronization().DoneForBinding(thm.KubernetesBindingId)
			}
			return false // do not stop combine process on this task
		})

		if combineResult != nil {
			hm.BindingContext = combineResult.BindingContexts
			// Extra monitor IDs can be returned if several Synchronization binding contexts are combined.
			if len(combineResult.MonitorIDs) > 0 {
				hm.MonitorIDs = append(hm.MonitorIDs, combineResult.MonitorIDs...)
			}
			logEntry.Debug("Got monitorIDs", slog.Any("monitorIDs", hm.MonitorIDs))
			t.UpdateMetadata(hm)
		}
	}

	if shouldRunHook {
		// Module hook can recreate helm objects, so pause resources monitor.
		// Parallel hooks can interfere, so pause-resume only for hooks in the main queue.
		// FIXME pause-resume for parallel hooks
		if t.GetQueueName() == "main" {
			op.HelmResourcesManager.PauseMonitor(hm.ModuleName)
			defer op.HelmResourcesManager.ResumeMonitor(hm.ModuleName)
		}

		errors := 0.0
		success := 0.0
		allowed := 0.0

		beforeChecksum, afterChecksum, err := op.ModuleManager.RunModuleHook(hm.ModuleName, hm.HookName, hm.BindingType, hm.BindingContext, t.GetLogLabels())
		if err != nil {
			if hm.AllowFailure {
				allowed = 1.0
				logEntry.Info("Module hook failed, but allowed to fail.", log.Err(err))
				res.Status = queue.Success
				op.ModuleManager.UpdateModuleHookStatusAndNotify(baseModule, hm.HookName, nil)
			} else {
				errors = 1.0
				logEntry.Error("Module hook failed, requeue task to retry after delay.",
					slog.Int("count", t.GetFailureCount()+1),
					log.Err(err))
				t.UpdateFailureMessage(err.Error())
				t.WithQueuedAt(time.Now())
				res.Status = queue.Fail
				op.ModuleManager.UpdateModuleHookStatusAndNotify(baseModule, hm.HookName, err)
			}
		} else {
			success = 1.0
			logEntry.Debug("Module hook success", slog.String("name", hm.HookName))
			res.Status = queue.Success
			op.ModuleManager.UpdateModuleHookStatusAndNotify(baseModule, hm.HookName, nil)

			// Handle module values change.
			reloadModule := false
			eventDescription := ""
			switch hm.BindingType {
			case htypes.Schedule:
				if beforeChecksum != afterChecksum {
					logEntry.Info("Module hook changed values, will restart ModuleRun.")
					reloadModule = true
					eventDescription = "Schedule-Change-ModuleValues"
				}
			case htypes.OnKubernetesEvent:
				// Do not reload module on changes during Synchronization.
				if beforeChecksum != afterChecksum {
					if hm.IsSynchronization() {
						logEntry.Info("Module hook changed values, but restart ModuleRun is ignored for the Synchronization task.")
					} else {
						logEntry.Info("Module hook changed values, will restart ModuleRun.")
						reloadModule = true
						eventDescription = "Kubernetes-Change-ModuleValues"
					}
				}
			}
			if reloadModule {
				// relabel
				logLabels := t.GetLogLabels()
				// Save event source info to add it as props to the task and use in logger later.
				triggeredBy := []slog.Attr{
					slog.String("event.triggered-by.hook", logLabels["hook"]),
					slog.String("event.triggered-by.binding", logLabels["binding"]),
					slog.String("event.triggered-by.binding.name", logLabels["binding.name"]),
					slog.String("event.triggered-by.watchEvent", logLabels["watchEvent"]),
				}
				delete(logLabels, "hook")
				delete(logLabels, "hook.type")
				delete(logLabels, "binding")
				delete(logLabels, "binding.name")
				delete(logLabels, "watchEvent")

				// Do not add ModuleRun task if it is already queued.
				hasTask := QueueHasPendingModuleRunTask(op.engine.TaskQueues.GetMain(), hm.ModuleName)
				if !hasTask {
					newTask := sh_task.NewTask(task.ModuleRun).
						WithLogLabels(logLabels).
						WithQueueName("main").
						WithMetadata(task.HookMetadata{
							EventDescription: eventDescription,
							ModuleName:       hm.ModuleName,
						})
					newTask.SetProp("triggered-by", triggeredBy)

					op.engine.TaskQueues.GetMain().AddLast(newTask.WithQueuedAt(time.Now()))
					op.logTaskAdd(logEntry, "module values are changed, append", newTask)
				} else {
					logEntry.With(pkg.LogKeyTaskFlow, "noop").Info("module values are changed, ModuleRun task already queued")
				}
			}
		}

		op.engine.MetricStorage.CounterAdd("{PREFIX}module_hook_allowed_errors_total", allowed, metricLabels)
		op.engine.MetricStorage.CounterAdd("{PREFIX}module_hook_errors_total", errors, metricLabels)
		op.engine.MetricStorage.CounterAdd("{PREFIX}module_hook_success_total", success, metricLabels)
	}

	if isSynchronization && res.Status == queue.Success {
		baseModule.Synchronization().DoneForBinding(hm.KubernetesBindingId)
		// Unlock Kubernetes events for all monitors when Synchronization task is done.
		logEntry.Debug("Synchronization done, unlock Kubernetes events")
		for _, monitorID := range hm.MonitorIDs {
			taskHook.GetHookController().UnlockKubernetesEventsFor(monitorID)
		}
	}

	return res
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

// CreateConvergeModulesTasks creates tasks for module lifecycle management based on the current state.
// It generates:
// - ModuleEnsureCRDs tasks for modules that need CRD installation
// - ModuleDelete tasks for modules that need to be disabled
// - ModuleRun tasks for individual modules that need to be enabled or rerun
// - ParallelModuleRun tasks for groups of modules that can be processed in parallel
func (op *AddonOperator) CreateConvergeModulesTasks(state *module_manager.ModulesState, logLabels map[string]string, eventDescription string) []sh_task.Task {
	modulesTasks := make([]sh_task.Task, 0, len(state.ModulesToDisable)+len(state.AllEnabledModules))
	resultingTasks := make([]sh_task.Task, 0, len(state.ModulesToDisable)+len(state.AllEnabledModules))
	queuedAt := time.Now()

	// Add ModuleDelete tasks to delete helm releases of disabled modules.
	log.Debug("The following modules are going to be disabled",
		slog.Any("modules", state.ModulesToDisable))
	for _, moduleName := range state.ModulesToDisable {
		ev := events.ModuleEvent{
			ModuleName: moduleName,
			EventType:  events.ModuleDisabled,
		}
		op.ModuleManager.SendModuleEvent(ev)
		newLogLabels := utils.MergeLabels(logLabels)
		newLogLabels["module"] = moduleName
		delete(newLogLabels, "task.id")

		newTask := sh_task.NewTask(task.ModuleDelete).
			WithLogLabels(newLogLabels).
			WithQueueName("main").
			WithMetadata(task.HookMetadata{
				EventDescription: eventDescription,
				ModuleName:       moduleName,
			})
		modulesTasks = append(modulesTasks, newTask.WithQueuedAt(queuedAt))
	}

	// Add ModuleRun tasks to install or reload enabled modules.
	newlyEnabled := utils.ListToMapStringStruct(state.ModulesToEnable)
	log.Debug("The following modules are going to be enabled/rerun",
		slog.String("modules", fmt.Sprintf("%v", state.AllEnabledModulesByOrder)))

	for _, modules := range state.AllEnabledModulesByOrder {
		newLogLabels := utils.MergeLabels(logLabels)
		delete(newLogLabels, "task.id")
		switch {
		// create parallel moduleRun task
		case len(modules) > 1:
			parallelRunMetadata := task.ParallelRunMetadata{}
			newLogLabels["modules"] = strings.Join(modules, ",")
			for _, moduleName := range modules {
				ev := events.ModuleEvent{
					ModuleName: moduleName,
					EventType:  events.ModuleEnabled,
				}
				op.ModuleManager.SendModuleEvent(ev)
				doModuleStartup := false
				if _, has := newlyEnabled[moduleName]; has {
					// add EnsureCRDs task if module is about to be enabled
					if op.ModuleManager.ModuleHasCRDs(moduleName) {
						resultingTasks = append(resultingTasks, sh_task.NewTask(task.ModuleEnsureCRDs).
							WithLogLabels(newLogLabels).
							WithQueueName("main").
							WithMetadata(task.HookMetadata{
								EventDescription: "EnsureCRDs",
								ModuleName:       moduleName,
								IsReloadAll:      true,
							}).WithQueuedAt(queuedAt))
					}
					doModuleStartup = true
				}
				parallelRunMetadata.SetModuleMetadata(moduleName, task.ParallelRunModuleMetadata{
					DoModuleStartup: doModuleStartup,
				})
			}
			parallelRunMetadata.Context, parallelRunMetadata.CancelF = context.WithCancel(context.Background())
			newTask := sh_task.NewTask(task.ParallelModuleRun).
				WithLogLabels(newLogLabels).
				WithQueueName("main").
				WithMetadata(task.HookMetadata{
					EventDescription:    eventDescription,
					ModuleName:          fmt.Sprintf("Parallel run for %s", strings.Join(modules, ", ")),
					IsReloadAll:         true,
					ParallelRunMetadata: &parallelRunMetadata,
				})
			modulesTasks = append(modulesTasks, newTask.WithQueuedAt(queuedAt))

		// otherwise, create an original moduleRun task
		case len(modules) == 1:
			ev := events.ModuleEvent{
				ModuleName: modules[0],
				EventType:  events.ModuleEnabled,
			}
			op.ModuleManager.SendModuleEvent(ev)
			newLogLabels["module"] = modules[0]
			doModuleStartup := false
			if _, has := newlyEnabled[modules[0]]; has {
				// add EnsureCRDs task if module is about to be enabled
				if op.ModuleManager.ModuleHasCRDs(modules[0]) {
					resultingTasks = append(resultingTasks, sh_task.NewTask(task.ModuleEnsureCRDs).
						WithLogLabels(newLogLabels).
						WithQueueName("main").
						WithMetadata(task.HookMetadata{
							EventDescription: "EnsureCRDs",
							ModuleName:       modules[0],
							IsReloadAll:      true,
						}).WithQueuedAt(queuedAt))
				}
				doModuleStartup = true
			}
			newTask := sh_task.NewTask(task.ModuleRun).
				WithLogLabels(newLogLabels).
				WithQueueName("main").
				WithMetadata(task.HookMetadata{
					EventDescription: eventDescription,
					ModuleName:       modules[0],
					DoModuleStartup:  doModuleStartup,
					IsReloadAll:      true,
				})
			modulesTasks = append(modulesTasks, newTask.WithQueuedAt(queuedAt))

		default:
			log.Error("Invalid ModulesState",
				slog.String("state", fmt.Sprintf("%v", state)))
		}
	}
	// as resultingTasks contains new ensureCRDsTasks we invalidate
	// ConvregeState.CRDsEnsured if there are new ensureCRDsTasks to execute
	if op.ConvergeState.CRDsEnsured && len(resultingTasks) > 0 {
		log.Debug("CheckCRDsEnsured: set to false")
		op.ConvergeState.CRDsEnsured = false
	}

	// append modulesTasks to resultingTasks
	resultingTasks = append(resultingTasks, modulesTasks...)

	return resultingTasks
}

// CheckCRDsEnsured checks if there any other ModuleEnsureCRDs tasks in the queue
// and if there aren't, sets ConvergeState.CRDsEnsured to true and applies global values patch with
// the discovered GVKs
func (op *AddonOperator) CheckCRDsEnsured(t sh_task.Task) {
	if !op.ConvergeState.CRDsEnsured && !ModuleEnsureCRDsTasksInQueueAfterId(op.engine.TaskQueues.GetMain(), t.GetId()) {
		log.Debug("CheckCRDsEnsured: set to true")
		op.ConvergeState.CRDsEnsured = true
		// apply global values patch
		op.discoveredGVKsLock.Lock()
		defer op.discoveredGVKsLock.Unlock()
		if len(op.discoveredGVKs) != 0 {
			gvks := make([]string, 0, len(op.discoveredGVKs))
			for gvk := range op.discoveredGVKs {
				gvks = append(gvks, gvk)
			}
			op.ModuleManager.SetGlobalDiscoveryAPIVersions(gvks)
		}
	}
}

// CheckConvergeStatus monitors the convergence process and updates metrics.
// It detects when convergence starts and completes, tracks timing metrics,
// and keeps track of the first convergence for operator readiness.
func (op *AddonOperator) CheckConvergeStatus(t sh_task.Task) {
	// Check all queues that might contain convergence tasks
	convergeTasks := 0
	for _, q := range op.getConvergeQueues() {
		convergeTasks += ConvergeTasksInQueue(q)
	}

	op.l.Lock()
	defer op.l.Unlock()

	// Track convergence state changes
	op.handleConvergeStateChanges(convergeTasks, t)

	// Track the first convergence operation for readiness
	op.UpdateFirstConvergeStatus(convergeTasks)

	// Log progress information when appropriate
	op.logConvergeProgress(convergeTasks, t)
}

// handleConvergeStateChanges updates the convergence state tracking and metrics
// based on whether convergence is starting or completing
func (op *AddonOperator) handleConvergeStateChanges(convergeTasks int, t sh_task.Task) {
	// Detect convergence start
	if convergeTasks > 0 && op.ConvergeState.StartedAt == 0 {
		// Convergence just started - record the start time and activation source
		op.ConvergeState.StartedAt = time.Now().UnixNano()
		op.ConvergeState.Activation = t.GetLogLabels()[pkg.LogKeyEventType]
	}

	// Detect convergence completion
	if convergeTasks == 0 && op.ConvergeState.StartedAt != 0 {
		// Convergence just completed - update metrics and reset tracking
		convergeSeconds := time.Duration(time.Now().UnixNano() - op.ConvergeState.StartedAt).Seconds()
		op.recordConvergenceMetrics(convergeSeconds)

		// Reset state for next convergence cycle
		op.ConvergeState.StartedAt = 0
		op.ConvergeState.Activation = ""
	}
}

// recordConvergenceMetrics adds metrics about convergence duration and count
func (op *AddonOperator) recordConvergenceMetrics(durationSeconds float64) {
	metricLabels := map[string]string{pkg.MetricKeyActivation: op.ConvergeState.Activation}

	// Record the time taken for convergence
	op.MetricStorage.CounterAdd(
		"{PREFIX}convergence_seconds",
		durationSeconds,
		metricLabels,
	)

	// Increment the total convergence operations counter
	op.MetricStorage.CounterAdd(
		"{PREFIX}convergence_total",
		1.0,
		metricLabels,
	)
}

// logConvergeProgress logs information about ongoing module convergence
func (op *AddonOperator) logConvergeProgress(convergeTasks int, t sh_task.Task) {
	// Only log progress for module tasks when convergence is in progress
	isModuleTask := t.GetType() == task.ModuleRun || t.GetType() == task.ModuleDelete

	if convergeTasks > 0 && isModuleTask {
		// Count remaining module tasks and log progress
		moduleTasks := 0
		for _, q := range op.getConvergeQueues() {
			moduleTasks += ConvergeModulesInQueue(q)
		}

		if moduleTasks > 0 {
			log.Info("Converge modules in progress", slog.Int("count", moduleTasks))
		}
	}
}

// UpdateFirstConvergeStatus tracks the progress of the first convergence operation
// and logs when it completes.
func (op *AddonOperator) UpdateFirstConvergeStatus(convergeTasks int) {
	// Early return if first run is already completed
	if op.ConvergeState.FirstRunPhase == converge.FirstDone {
		return
	}

	switch op.ConvergeState.FirstRunPhase {
	case converge.FirstNotStarted:
		// Mark as started when convergence tasks are detected
		if convergeTasks > 0 {
			op.ConvergeState.SetFirstRunPhase(converge.FirstStarted)
		}
	case converge.FirstStarted:
		// Mark as done when all convergence tasks are completed
		if convergeTasks == 0 {
			log.Info("First converge is finished. Operator is ready now.")
			op.ConvergeState.SetFirstRunPhase(converge.FirstDone)
		}
	}
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

// logTaskStart prints info about task at start. Also prints event source info from task props.
func (op *AddonOperator) logTaskStart(logEntry *log.Logger, tsk sh_task.Task) {
	// Prevent excess messages for highly frequent tasks.
	if tsk.GetType() == task.GlobalHookWaitKubernetesSynchronization {
		return
	}

	if tsk.GetType() == task.ModuleRun {
		hm := task.HookMetadataAccessor(tsk)
		baseModule := op.ModuleManager.GetModule(hm.ModuleName)

		if baseModule.GetPhase() == modules.WaitForSynchronization {
			return
		}
	}

	logger := logEntry.
		With(pkg.LogKeyTaskFlow, "start")
	logger = utils.EnrichLoggerWithLabels(logger, tsk.GetLogLabels())

	if triggeredBy, ok := tsk.GetProp("triggered-by").([]slog.Attr); ok {
		for _, attr := range triggeredBy {
			logger = logger.With(attr)
		}
	}

	logger.Info(taskDescriptionForTaskFlowLog(tsk, "start", op.taskPhase(tsk), ""))
}

// logTaskEnd prints info about task at the end. Info level used only for the ConvergeModules task.
func (op *AddonOperator) logTaskEnd(logEntry *log.Logger, tsk sh_task.Task, result queue.TaskResult) {
	logger := logEntry.
		With(pkg.LogKeyTaskFlow, "end")
	logger = utils.EnrichLoggerWithLabels(logger, tsk.GetLogLabels())

	level := log.LevelDebug
	if tsk.GetType() == task.ConvergeModules {
		level = log.LevelInfo
	}

	logger.Log(context.TODO(), level.Level(), taskDescriptionForTaskFlowLog(tsk, "end", op.taskPhase(tsk), string(result.Status)))
}

func (op *AddonOperator) taskPhase(tsk sh_task.Task) string {
	switch tsk.GetType() {
	case task.ConvergeModules:
		return string(op.ConvergeState.Phase)
	case task.ModuleRun:
		hm := task.HookMetadataAccessor(tsk)
		mod := op.ModuleManager.GetModule(hm.ModuleName)
		return string(mod.GetPhase())
	}
	return ""
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
