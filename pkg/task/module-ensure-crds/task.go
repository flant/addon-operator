package moduleensurecrds

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	crdinstaller "github.com/deckhouse/module-sdk/pkg/crd-installer"
	"go.opentelemetry.io/otel"

	"github.com/flant/addon-operator/pkg/addon-operator/converge"
	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
	"github.com/flant/addon-operator/pkg/task"
	taskqueue "github.com/flant/addon-operator/pkg/task/queue"
	klient "github.com/flant/kube-client/client"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
)

const (
	taskName = "module-ensure-crds"
)

// TaskDependencies defines the interface for accessing necessary components
type TaskDependencies interface {
	GetKubeClient() *klient.Client
	GetModuleManager() *module_manager.ModuleManager
	GetConvergeState() *converge.ConvergeState
	GetCRDExtraLabels() map[string]string
	GetQueueService() *taskqueue.Service
}

// RegisterTaskHandler creates a factory function for ModuleEnsureCRDs tasks
func RegisterTaskHandler(svc TaskDependencies) func(t sh_task.Task, logger *log.Logger) task.Task {
	return func(t sh_task.Task, logger *log.Logger) task.Task {
		return NewTask(
			t,
			svc.GetKubeClient(),
			svc.GetModuleManager(),
			svc.GetConvergeState(),
			svc.GetQueueService(),
			svc.GetCRDExtraLabels(),
			logger.Named("module-ensure-crds"),
		)
	}
}

// Task handles ensuring CRDs for modules
type Task struct {
	shellTask sh_task.Task

	kubeClient     *klient.Client
	moduleManager  *module_manager.ModuleManager
	convergeState  *converge.ConvergeState
	queueService   *taskqueue.Service
	crdExtraLabels map[string]string

	discoveredGVKsLock sync.Mutex
	discoveredGVKs     map[string]struct{} // GVKs from applied modules' CRDs

	logger *log.Logger
}

// NewTask creates a new task handler for ensuring module CRDs
func NewTask(
	shellTask sh_task.Task,
	kubeClient *klient.Client,
	moduleManager *module_manager.ModuleManager,
	convergeState *converge.ConvergeState,
	queueService *taskqueue.Service,
	crdExtraLabels map[string]string,
	logger *log.Logger,
) *Task {
	return &Task{
		shellTask:      shellTask,
		kubeClient:     kubeClient,
		moduleManager:  moduleManager,
		convergeState:  convergeState,
		queueService:   queueService,
		crdExtraLabels: crdExtraLabels,

		discoveredGVKs: make(map[string]struct{}),

		logger: logger,
	}
}

func (s *Task) Handle(ctx context.Context) queue.TaskResult {
	_, span := otel.Tracer(taskName).Start(ctx, "handle")
	defer span.End()

	hm := task.HookMetadataAccessor(s.shellTask)

	res := queue.TaskResult{
		Status: queue.Success,
	}

	baseModule := s.moduleManager.GetModule(hm.ModuleName)

	s.logger.Debug("Module ensureCRDs", slog.String("name", hm.ModuleName))

	if appliedGVKs, err := s.EnsureCRDs(baseModule); err != nil {
		s.moduleManager.UpdateModuleLastErrorAndNotify(baseModule, err)

		s.logger.Error("ModuleEnsureCRDs failed.", log.Err(err))

		s.shellTask.UpdateFailureMessage(err.Error())
		s.shellTask.WithQueuedAt(time.Now())

		res.Status = queue.Fail
	} else {
		s.discoveredGVKsLock.Lock()

		for _, gvk := range appliedGVKs {
			s.discoveredGVKs[gvk] = struct{}{}
		}

		s.discoveredGVKsLock.Unlock()
	}

	if res.Status == queue.Success {
		s.CheckCRDsEnsured(s.shellTask)
	}

	return res
}

func (s *Task) EnsureCRDs(module *modules.BasicModule) ([]string, error) {
	// do not ensure CRDs if there are no files
	if !module.CRDExist() {
		return nil, nil
	}

	cp := crdinstaller.NewCRDsInstaller(s.kubeClient.Dynamic(), module.GetCRDFilesPaths(), crdinstaller.WithExtraLabels(s.crdExtraLabels))
	if cp == nil {
		return nil, nil
	}

	if err := cp.Run(context.TODO()); err != nil {
		return nil, err
	}

	return cp.GetAppliedGVKs(), nil
}

// CheckCRDsEnsured checks if there any other ModuleEnsureCRDs tasks in the queue
// and if there aren't, sets ConvergeState.CRDsEnsured to true and applies global values patch with
// the discovered GVKs
func (s *Task) CheckCRDsEnsured(t sh_task.Task) {
	if !s.convergeState.CRDsEnsured && !s.queueService.ModuleEnsureCRDsTasksInMainQueueAfterId(t.GetId()) {
		log.Debug("CheckCRDsEnsured: set to true")

		s.convergeState.CRDsEnsured = true

		// apply global values patch
		s.discoveredGVKsLock.Lock()
		defer s.discoveredGVKsLock.Unlock()

		if len(s.discoveredGVKs) != 0 {
			gvks := make([]string, 0, len(s.discoveredGVKs))
			for gvk := range s.discoveredGVKs {
				gvks = append(gvks, gvk)
			}

			s.moduleManager.SetGlobalDiscoveryAPIVersions(gvks)
		}
	}
}
