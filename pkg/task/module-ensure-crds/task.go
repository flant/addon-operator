package moduleensurecrds

import (
	"context"
	"log/slog"
	"runtime/trace"
	"sync"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	crdinstaller "github.com/deckhouse/module-sdk/pkg/crd-installer"

	"github.com/flant/addon-operator/pkg/addon-operator/converge"
	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/module_manager/models/modules"
	"github.com/flant/addon-operator/pkg/task"
	shell_operator "github.com/flant/shell-operator/pkg/shell-operator"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
)

type TaskConfig interface {
	GetEngine() *shell_operator.ShellOperator
	GetModuleManager() *module_manager.ModuleManager
	GetConvergeState() *converge.ConvergeState
	GetCRDExtraLabels() map[string]string
}

func RegisterTaskHandler(svc TaskConfig) func(t sh_task.Task, logger *log.Logger) task.Task {
	return func(t sh_task.Task, logger *log.Logger) task.Task {
		cfg := &taskConfig{
			ShellTask: t,

			Engine:         svc.GetEngine(),
			ModuleManager:  svc.GetModuleManager(),
			ConvergeState:  svc.GetConvergeState(),
			CRDExtraLabels: svc.GetCRDExtraLabels(),
		}

		return newModuleEnsureCRDs(cfg, logger.Named("module-ensure-crds"))
	}
}

type taskConfig struct {
	ShellTask sh_task.Task

	Engine         *shell_operator.ShellOperator
	ModuleManager  *module_manager.ModuleManager
	ConvergeState  *converge.ConvergeState
	CRDExtraLabels map[string]string
}

type Task struct {
	shellTask sh_task.Task

	engine        *shell_operator.ShellOperator
	moduleManager *module_manager.ModuleManager

	discoveredGVKsLock sync.Mutex
	// discoveredGVKs is a map of GVKs from applied modules' CRDs
	discoveredGVKs map[string]struct{}

	convergeState  *converge.ConvergeState
	crdExtraLabels map[string]string

	logger *log.Logger
}

// newModuleEnsureCRDs creates a new task handler service
func newModuleEnsureCRDs(cfg *taskConfig, logger *log.Logger) *Task {
	service := &Task{
		shellTask: cfg.ShellTask,

		engine:         cfg.Engine,
		moduleManager:  cfg.ModuleManager,
		convergeState:  cfg.ConvergeState,
		crdExtraLabels: cfg.CRDExtraLabels,

		discoveredGVKs: make(map[string]struct{}),

		logger: logger,
	}

	return service
}

func (s *Task) Handle(ctx context.Context) queue.TaskResult {
	defer trace.StartRegion(ctx, "ModuleEnsureCRDs").End()

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

	cp := crdinstaller.NewCRDsInstaller(s.engine.KubeClient.Dynamic(), module.GetCRDFilesPaths(), crdinstaller.WithExtraLabels(s.crdExtraLabels))
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
	if !s.convergeState.CRDsEnsured && !moduleEnsureCRDsTasksInQueueAfterId(s.engine.TaskQueues.GetMain(), t.GetId()) {
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

func moduleEnsureCRDsTasksInQueueAfterId(q *queue.TaskQueue, afterId string) bool {
	if q == nil {
		return false
	}
	IDFound := false
	taskFound := false
	stop := false
	q.Filter(func(t sh_task.Task) bool {
		if stop {
			return true
		}
		if !IDFound {
			if t.GetId() == afterId {
				IDFound = true
			}
		} else {
			// task found
			if t.GetType() == task.ModuleEnsureCRDs {
				taskFound = true
				stop = true
			}
		}
		// continue searching
		return true
	})

	return taskFound
}
