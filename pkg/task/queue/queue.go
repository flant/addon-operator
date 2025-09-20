package queue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/deckhouse/deckhouse/pkg/log"

	"github.com/flant/addon-operator/pkg/addon-operator/converge"
	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/task"
	shell_operator "github.com/flant/shell-operator/pkg/shell-operator"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
)

type ServiceConfig struct {
	Engine *shell_operator.ShellOperator
	Handle func(ctx context.Context, t sh_task.Task) sh_task.TaskResult
}

type Service struct {
	engine *shell_operator.ShellOperator
	ctx    context.Context

	Handle func(ctx context.Context, t sh_task.Task) sh_task.TaskResult

	logger *log.Logger
}

func NewService(ctx context.Context, cfg *ServiceConfig, logger *log.Logger) *Service {
	return &Service{
		ctx:    ctx,
		engine: cfg.Engine,
		Handle: cfg.Handle,
		logger: logger,
	}
}

// CreateAndStartQueue creates a named queue with default handler and starts it.
// It returns false is queue is already created
func (s *Service) CreateAndStartQueue(queueName string, callback Callback) {
	s.startQueue(queueName, s.Handle, callback)
}

func (s *Service) startQueue(queueName string, handler func(ctx context.Context, t sh_task.Task) sh_task.TaskResult, callback Callback) {
	s.engine.TaskQueues.NewNamedQueue(queueName, handler,
		queue.WithCompactionCallback(callback),
		queue.WithCompactableTypes(MergeTasks...),
		queue.WithLogger(s.logger.With("operator.component", "queue", "queue", queueName)),
	)
	s.engine.TaskQueues.GetByName(queueName).Start(s.ctx)
}

// IsQueueExists returns true is queue is already created
func (s *Service) IsQueueExists(queueName string) bool {
	return s.engine.TaskQueues.GetByName(queueName) != nil
}

var ErrQueueNotFound = errors.New("queue is not found")

func (s *Service) AddLastTaskToMain(t sh_task.Task) error {
	q := s.engine.TaskQueues.GetMain()
	if q == nil {
		return ErrQueueNotFound
	}

	q.AddLast(t)

	return nil
}

func (s *Service) GetQueueLength(queueName string) int {
	q := s.engine.TaskQueues.GetByName(queueName)
	if q == nil {
		return 0
	}

	return q.Length()
}

func (s *Service) AddLastTaskToQueue(queueName string, t sh_task.Task) error {
	q := s.engine.TaskQueues.GetByName(queueName)
	if q == nil {
		return ErrQueueNotFound
	}

	q.AddLast(t)

	return nil
}

func (s *Service) GetNumberOfConvergeTasks() int {
	convergeTasks := 0
	for _, q := range s.getConvergeQueues() {
		convergeTasks += convergeTasksInQueue(q)
	}

	return convergeTasks
}

// getConvergeQueues returns list of all queues where modules converge tasks may be running
func (s *Service) getConvergeQueues() []sh_task.TaskQueue {
	convergeQueues := make([]sh_task.TaskQueue, 0, app.NumberOfParallelQueues+1)
	for i := 0; i < app.NumberOfParallelQueues; i++ {
		convergeQueues = append(convergeQueues, s.engine.TaskQueues.GetByName(fmt.Sprintf(app.ParallelQueueNamePattern, i)))
	}

	convergeQueues = append(convergeQueues, s.engine.TaskQueues.GetMain())

	return convergeQueues
}

func convergeTasksInQueue(q sh_task.TaskQueue) int {
	if q == nil {
		return 0
	}

	convergeTasks := 0
	q.Iterate(func(t sh_task.Task) {
		if converge.IsConvergeTask(t) || converge.IsFirstConvergeTask(t) {
			convergeTasks++
		}
	})

	return convergeTasks
}

func (s *Service) DrainNonMainQueue(queueName string) {
	q := s.engine.TaskQueues.GetByName(queueName)
	if q == nil || q.GetName() == "main" {
		return
	}

	// Remove all tasks.
	q.Filter(func(_ sh_task.Task) bool {
		return false
	})
}

func (s *Service) CombineBindingContextForHook(queueName string, t sh_task.Task, stopCombineFn func(tsk sh_task.Task) bool) *shell_operator.CombineResult {
	q := s.engine.TaskQueues.GetByName(queueName)

	return s.engine.CombineBindingContextForHook(q, t, stopCombineFn)
}

// RemoveAdjacentConvergeModules removes ConvergeModules tasks right
// after the task with the specified ID.
func (s *Service) RemoveAdjacentConvergeModules(queueName string, afterId string) {
	q := s.engine.TaskQueues.GetByName(queueName)
	if q == nil {
		return
	}

	IDFound := false
	stop := false

	q.Filter(func(t sh_task.Task) bool {
		if stop {
			return true
		}

		if !IDFound {
			if t.GetId() == afterId {
				IDFound = true
			}

			return true
		}

		// Remove ConvergeModules after current.
		if t.GetType() == task.ConvergeModules {
			hm := task.HookMetadataAccessor(t)

			s.logger.Debug("Drained adjacent ConvergeModules task",
				slog.String("type", string(t.GetType())),
				slog.String("description", hm.EventDescription))

			return false
		}

		stop = true

		return true
	})
}

// queueHasPendingModuleRunTask returns true if queue has pending tasks
// with the type "ModuleRun" related to the module "moduleName".
func (s *Service) MainQueueHasPendingModuleRunTask(moduleName string) bool {
	q := s.engine.TaskQueues.GetMain()
	if q == nil {
		return false
	}

	modules := modulesWithPendingModuleRun(q)
	_, has := modules[moduleName]

	return has
}

// modulesWithPendingModuleRun returns names of all modules in pending
// ModuleRun tasks. First task in queue considered not pending and is ignored.
func modulesWithPendingModuleRun(q sh_task.TaskQueue) map[string]struct{} {
	if q == nil {
		return nil
	}

	modules := make(map[string]struct{})

	skipFirstTask := true

	q.Iterate(func(t sh_task.Task) {
		// Skip the first task in the queue as it can be executed already, i.e. "not pending".
		if skipFirstTask {
			skipFirstTask = false
			return
		}

		switch t.GetType() {
		case task.ModuleRun:
			hm := task.HookMetadataAccessor(t)
			modules[hm.ModuleName] = struct{}{}

		case task.ParallelModuleRun:
			hm := task.HookMetadataAccessor(t)
			for _, moduleName := range hm.ParallelRunMetadata.ListModules() {
				modules[moduleName] = struct{}{}
			}
		}
	})

	return modules
}

func (s *Service) ModuleEnsureCRDsTasksInMainQueueAfterId(afterId string) bool {
	q := s.engine.TaskQueues.GetMain()
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
