package addon_operator

import (
	"github.com/flant/addon-operator/pkg/task"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"
)

// QueueHasPendingModuleRunTask returns true if queue has pending tasks
// with the type "ModuleRun" related to the module "moduleName".
func QueueHasPendingModuleRunTask(q *queue.TaskQueue, moduleName string) bool {
	if q == nil {
		return false
	}
	modules := ModulesWithPendingModuleRun(q)
	_, has := modules[moduleName]
	return has
}

// ModulesWithPendingModuleRun returns names of all modules in pending
// ModuleRun tasks. First task in queue considered not pending and is ignored.
func ModulesWithPendingModuleRun(q *queue.TaskQueue) map[string]struct{} {
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

		if t.GetType() == task.ModuleRun {
			hm := task.HookMetadataAccessor(t)
			modules[hm.ModuleName] = struct{}{}
		}
	})

	return modules
}

func ConvergeTasksInQueue(q *queue.TaskQueue) int {
	if q == nil {
		return 0
	}

	convergeTasks := 0
	q.Iterate(func(t sh_task.Task) {
		if IsConvergeTask(t) || IsFirstConvergeTask(t) {
			convergeTasks++
		}
	})

	return convergeTasks
}

// RemoveCurrentConvergeTasks detects if converge tasks present in the main
// queue after task which ID equals to 'afterID'. These tasks are drained
// and the method returns true.
func RemoveCurrentConvergeTasks(q *queue.TaskQueue, afterId string) bool {
	if q == nil {
		return false
	}

	IDFound := false
	convergeDrained := false
	stop := false
	q.Filter(func(t sh_task.Task) bool {
		if stop {
			return true
		}
		// Keep tasks until specified task.
		if !IDFound {
			// Also keep specified task.
			if t.GetId() == afterId {
				IDFound = true
			}
			return true
		}

		// Return false to remove converge task right after the specified task.
		if IsConvergeTask(t) {
			convergeDrained = true
			// Stop draining when ConvergeModules task is found.
			if t.GetType() == task.ConvergeModules {
				stop = true
			}
			return false
		}
		// Stop filtering when there is non-converge task after specified task.
		stop = true
		return true
	})

	return convergeDrained
}

// RemoveAdjacentConvergeModules removes ConvergeModules tasks right
// after the task with the specified ID.
func RemoveAdjacentConvergeModules(q *queue.TaskQueue, afterId string) {
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
			return false
		}

		stop = true
		return true
	})
}

func DrainNonMainQueue(q *queue.TaskQueue) {
	if q == nil || q.Name == "main" {
		return
	}

	// Remove all tasks.
	q.Filter(func(_ sh_task.Task) bool {
		return false
	})
}
