package service

import (
	"log/slog"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"

	"github.com/flant/addon-operator/pkg"
	"github.com/flant/addon-operator/pkg/addon-operator/converge"
	"github.com/flant/addon-operator/pkg/task"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
)

// CheckConvergeStatus monitors the convergence process and updates metrics.
// It detects when convergence starts and completes, tracks timing metrics,
// and keeps track of the first convergence for operator readiness.
func (s *TaskHandlerService) CheckConvergeStatus(t sh_task.Task) {
	// Check all queues that might contain convergence tasks
	convergeTasks := s.queueService.GetNumberOfConvergeTasks()

	s.convergeMu.Lock()
	defer s.convergeMu.Unlock()

	// Track convergence state changes
	s.handleConvergeStateChanges(convergeTasks, t)

	// Track the first convergence operation for readiness
	s.UpdateFirstConvergeStatus(convergeTasks)

	// Log progress information when appropriate
	s.logConvergeProgress(convergeTasks, t)
}

// handleConvergeStateChanges updates the convergence state tracking and metrics
// based on whether convergence is starting or completing
func (s *TaskHandlerService) handleConvergeStateChanges(convergeTasks int, t sh_task.Task) {
	// Detect convergence start
	if convergeTasks > 0 && s.convergeState.StartedAt == 0 {
		// Convergence just started - record the start time and activation source
		s.convergeState.StartedAt = time.Now().UnixNano()
		s.convergeState.Activation = t.GetLogLabels()[pkg.LogKeyEventType]
	}

	// Detect convergence completion
	if convergeTasks == 0 && s.convergeState.StartedAt != 0 {
		// Convergence just completed - update metrics and reset tracking
		convergeSeconds := time.Duration(time.Now().UnixNano() - s.convergeState.StartedAt).Seconds()
		s.recordConvergenceMetrics(convergeSeconds)

		// Reset state for next convergence cycle
		s.convergeState.StartedAt = 0
		s.convergeState.Activation = ""
	}
}

// recordConvergenceMetrics adds metrics about convergence duration and count
func (s *TaskHandlerService) recordConvergenceMetrics(durationSeconds float64) {
	metricLabels := map[string]string{pkg.MetricKeyActivation: s.convergeState.Activation}

	// Record the time taken for convergence
	s.metricStorage.CounterAdd(
		"{PREFIX}convergence_seconds",
		durationSeconds,
		metricLabels,
	)

	// Increment the total convergence operations counter
	s.metricStorage.CounterAdd(
		"{PREFIX}convergence_total",
		1.0,
		metricLabels,
	)
}

// logConvergeProgress logs information about ongoing module convergence
func (s *TaskHandlerService) logConvergeProgress(convergeTasks int, t sh_task.Task) {
	// Only log progress for module tasks when convergence is in progress
	isModuleTask := t.GetType() == task.ModuleRun || t.GetType() == task.ModuleDelete

	if convergeTasks > 0 && isModuleTask {
		// Count remaining module tasks and log progress
		moduleTasks := s.queueService.GetNumberOfConvergeTasks()

		if moduleTasks > 0 {
			log.Info("Converge modules in progress", slog.Int("count", moduleTasks))
		}
	}
}

// UpdateFirstConvergeStatus tracks the progress of the first convergence operation
// and logs when it completes.
func (s *TaskHandlerService) UpdateFirstConvergeStatus(convergeTasks int) {
	// Early return if first run is already completed
	if s.convergeState.GetFirstRunPhase() == converge.FirstDone {
		return
	}

	switch s.convergeState.GetFirstRunPhase() {
	case converge.FirstNotStarted:
		// Mark as started when convergence tasks are detected
		if convergeTasks > 0 {
			s.convergeState.SetFirstRunPhase(converge.FirstStarted)
		}
	case converge.FirstStarted:
		// Mark as done when all convergence tasks are completed
		if convergeTasks == 0 {
			s.logger.Info("first converge is finished. Operator is ready now.")
			s.convergeState.SetFirstRunPhase(converge.FirstDone)
		}
	}
}

func ConvergeTasksInQueue(q *queue.TaskQueue) int {
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

func ConvergeModulesInQueue(q *queue.TaskQueue) int {
	if q == nil {
		return 0
	}

	tasks := 0
	q.Iterate(func(t sh_task.Task) {
		taskType := t.GetType()
		if converge.IsConvergeTask(t) && (taskType == task.ModuleRun || taskType == task.ModuleDelete) {
			tasks++
		}
	})

	return tasks
}
