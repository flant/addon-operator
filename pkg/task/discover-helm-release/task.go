package discoverhelmrelease

import (
	"context"
	"log/slog"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	"go.opentelemetry.io/otel"

	"github.com/flant/addon-operator/pkg"
	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/task"
	"github.com/flant/addon-operator/pkg/task/helpers"
	"github.com/flant/addon-operator/pkg/utils"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
)

const (
	taskName = "discover-helm-releases"
)

// TaskDependencies provides access to the ModuleManager.
type TaskDependencies interface {
	GetModuleManager() *module_manager.ModuleManager
}

// RegisterTaskHandler creates a handler function that constructs Task instances.
func RegisterTaskHandler(svc TaskDependencies) func(t sh_task.Task, logger *log.Logger) task.Task {
	return func(t sh_task.Task, logger *log.Logger) task.Task {
		return NewTask(t, svc.GetModuleManager(), logger.Named("discover-helm-releases"))
	}
}

// Task handles discovery of Helm releases and creation of purge tasks.
type Task struct {
	shellTask     sh_task.Task
	moduleManager *module_manager.ModuleManager
	logger        *log.Logger
}

// NewTask creates a new Task instance.
func NewTask(shellTask sh_task.Task, moduleManager *module_manager.ModuleManager, logger *log.Logger) *Task {
	return &Task{
		shellTask:     shellTask,
		moduleManager: moduleManager,
		logger:        logger,
	}
}

func (s *Task) Handle(ctx context.Context) queue.TaskResult {
	_, span := otel.Tracer(taskName).Start(ctx, "handle")
	defer span.End()

	var res queue.TaskResult

	s.logger.Debug("Discover Helm releases state")

	state, err := s.moduleManager.RefreshStateFromHelmReleases(s.shellTask.GetLogLabels())
	if err != nil {
		res.Status = queue.Fail

		s.logger.Error("Discover helm releases failed, requeue task to retry after delay.",
			slog.Int("count", s.shellTask.GetFailureCount()+1),
			log.Err(err))

		s.shellTask.UpdateFailureMessage(err.Error())
		s.shellTask.WithQueuedAt(time.Now())

		return res
	}

	res.Status = queue.Success

	tasks := s.CreatePurgeTasks(state.ModulesToPurge, s.shellTask)
	res.AfterTasks = tasks

	s.logTaskAdd("after", res.AfterTasks...)

	return res
}

// CreatePurgeTasks returns ModulePurge tasks for each unknown Helm release.
func (s *Task) CreatePurgeTasks(modulesToPurge []string, t sh_task.Task) []sh_task.Task {
	newTasks := make([]sh_task.Task, 0, len(modulesToPurge))
	queuedAt := time.Now()

	hm := task.HookMetadataAccessor(t)

	// Add ModulePurge tasks to purge unknown helm releases at start.
	for _, moduleName := range modulesToPurge {
		newLogLabels := utils.MergeLabels(t.GetLogLabels())
		newLogLabels[pkg.LogKeyModule] = moduleName
		delete(newLogLabels, pkg.LogKeyTaskID)

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

// logTaskAdd prints info about queued tasks.
func (s *Task) logTaskAdd(action string, tasks ...sh_task.Task) {
	logger := s.logger.With(pkg.LogKeyTaskFlow, "add")
	for _, tsk := range tasks {
		logger.Info(helpers.TaskDescriptionForTaskFlowLog(tsk, action, "", ""))
	}
}
