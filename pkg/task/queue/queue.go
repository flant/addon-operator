package queue

import (
	"context"
	"log/slog"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/flant/addon-operator/pkg/task"
	shell_operator "github.com/flant/shell-operator/pkg/shell-operator"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
)

type ServiceConfig struct {
	Engine *shell_operator.ShellOperator
	Handle func(ctx context.Context, t sh_task.Task) queue.TaskResult
}

type Service struct {
	engine *shell_operator.ShellOperator
	ctx    context.Context

	Handle func(ctx context.Context, t sh_task.Task) queue.TaskResult

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
func (s *Service) CreateAndStartQueue(queueName string) {
	s.startQueue(queueName, s.Handle)
}

func (s *Service) startQueue(queueName string, handler func(ctx context.Context, t sh_task.Task) queue.TaskResult) {
	s.engine.TaskQueues.NewNamedQueue(queueName, handler)
	s.engine.TaskQueues.GetByName(queueName).Start(s.ctx)
}

// IsQueueExists returns true is queue is already created
func (s *Service) IsQueueExists(queueName string) bool {
	return s.engine.TaskQueues.GetByName(queueName) != nil
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
