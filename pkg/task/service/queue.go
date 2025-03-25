package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/deckhouse/deckhouse/pkg/log"
	shell_operator "github.com/flant/shell-operator/pkg/shell-operator"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
)

type QueueServiceConfig struct {
	Engine                   *shell_operator.ShellOperator
	NumberOfParallelQueues   int
	ParallelQueueNamePattern string
	Handle                   func(ctx context.Context, t sh_task.Task) queue.TaskResult
	ParallelHandle           func(ctx context.Context, t sh_task.Task) queue.TaskResult
}

type QueueService struct {
	engine *shell_operator.ShellOperator
	ctx    context.Context

	// 1 by default
	numberOfParallelQueues int
	// must contain one %d format specifier
	parallelQueueNamePattern string

	Handle         func(ctx context.Context, t sh_task.Task) queue.TaskResult
	ParallelHandle func(ctx context.Context, t sh_task.Task) queue.TaskResult

	logger *log.Logger
}

func NewQueueService(cfg *QueueServiceConfig) *QueueService {
	return &QueueService{
		numberOfParallelQueues:   cfg.NumberOfParallelQueues,
		parallelQueueNamePattern: cfg.ParallelQueueNamePattern,
		Handle:                   cfg.Handle,
		ParallelHandle:           cfg.ParallelHandle,
		logger:                   cfg.Logger,
	}
}

// CreateAndStartParallelQueues creates and starts named queues for executing tasks in parallel
func (s *QueueService) CreateAndStartParallelQueues() {
	for i := range s.numberOfParallelQueues {
		queueName := fmt.Sprintf(s.parallelQueueNamePattern, i)

		if s.IsQueueExists(queueName) {
			s.logger.Warn("Parallel queue already exists", slog.String("queue", queueName))
			continue
		}

		s.startQueue(queueName, s.ParallelHandle)
		s.logger.Debug("Parallel queue started",
			slog.String("queue", queueName))
	}
}

// CreateAndStartQueue creates a named queue with default handler and starts it.
// It returns false is queue is already created
func (s *QueueService) CreateAndStartQueue(queueName string) {
	s.startQueue(queueName, s.Handle)
}

func (s *QueueService) startQueue(queueName string, handler func(ctx context.Context, t sh_task.Task) queue.TaskResult) {
	s.engine.TaskQueues.NewNamedQueue(queueName, handler)
	s.engine.TaskQueues.GetByName(queueName).Start(s.ctx)
}

// IsQueueExists returns true is queue is already created
func (s *QueueService) IsQueueExists(queueName string) bool {
	return s.engine.TaskQueues.GetByName(queueName) != nil
}
