package addon_operator

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"

	klient "github.com/flant/kube-client/client"
	sh_app "github.com/flant/shell-operator/pkg/app"
	. "github.com/flant/shell-operator/pkg/hook/types"
	sh_task "github.com/flant/shell-operator/pkg/task"
	"github.com/flant/shell-operator/pkg/task/queue"

	"github.com/flant/addon-operator/pkg/task"
)

// CreateOnStartupTasks fills a working queue with onStartup hooks.
// TaskRunner should run all hooks and clean the queue.
func Test_Operator_Startup(t *testing.T) {
	// TODO tiller and helm should be mocked
	t.SkipNow()
	g := NewWithT(t)

	kubeClient := klient.NewFake(nil)

	sh_app.DebugUnixSocket = "testdata/debug.socket"
	op := NewAddonOperator()
	op.WithContext(context.Background())
	op.WithKubernetesClient(kubeClient)
	op.WithGlobalHooksDir("testdata/global_hooks")

	var err = op.Init()
	g.Expect(err).ShouldNot(HaveOccurred())

	err = op.InitModuleManager()
	g.Expect(err).ShouldNot(HaveOccurred())

	op.PrepopulateMainQueue(op.TaskQueues)

	var head sh_task.Task
	var hm task.HookMetadata

	head = op.TaskQueues.GetMain().RemoveFirst()
	hm = task.HookMetadataAccessor(head)
	g.Expect(hm.BindingType).To(Equal(OnStartup))

	head = op.TaskQueues.GetMain().RemoveFirst()
	hm = task.HookMetadataAccessor(head)
	g.Expect(hm.BindingType).To(Equal(OnStartup))
}

func Test_Operator_QueueHasPendingModuleRunTask(t *testing.T) {
	g := NewWithT(t)

	tests := []struct {
		name   string
		result bool
		queue  func() *queue.TaskQueue
	}{
		{
			name:   "Normal",
			result: true,
			queue: func() *queue.TaskQueue {
				q := queue.NewTasksQueue()

				Task := &sh_task.BaseTask{Type: task.ModuleRun, Id: "unknown"}
				q.AddLast(Task.WithMetadata(task.HookMetadata{ModuleName: "unknown"}))

				Task = &sh_task.BaseTask{Type: task.ModuleRun, Id: "unknown"}
				q.AddLast(Task.WithMetadata(task.HookMetadata{ModuleName: "unknown"}))

				Task = &sh_task.BaseTask{Type: task.ModuleRun, Id: "test"}
				q.AddLast(Task.WithMetadata(task.HookMetadata{ModuleName: "test"}))
				return q
			}},
		{
			name:   "First task",
			result: false,
			queue: func() *queue.TaskQueue {
				q := queue.NewTasksQueue()

				Task := &sh_task.BaseTask{Type: task.ModuleRun, Id: "test"}
				q.AddLast(Task.WithMetadata(task.HookMetadata{ModuleName: "test"}))

				Task = &sh_task.BaseTask{Type: task.GlobalHookRun, Id: "unknown"}
				q.AddLast(Task.WithMetadata(task.HookMetadata{ModuleName: "unknown"}))

				Task = &sh_task.BaseTask{Type: task.ModuleRun, Id: "unknown"}
				q.AddLast(Task.WithMetadata(task.HookMetadata{ModuleName: "unknown"}))
				return q
			}},
		{
			name:   "No module run",
			result: false,
			queue: func() *queue.TaskQueue {
				q := queue.NewTasksQueue()

				Task := &sh_task.BaseTask{Type: task.ModuleRun, Id: "unknown"}
				q.AddLast(Task.WithMetadata(task.HookMetadata{ModuleName: "unknown"}))

				Task = &sh_task.BaseTask{Type: task.ModuleHookRun, Id: "test"}
				q.AddLast(Task.WithMetadata(task.HookMetadata{ModuleName: "test"}))

				Task = &sh_task.BaseTask{Type: task.ModuleRun, Id: "unknown"}
				q.AddLast(Task.WithMetadata(task.HookMetadata{ModuleName: "unknown"}))
				return q
			}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := QueueHasPendingModuleRunTask(tt.queue(), "test")
			g.Expect(result).To(Equal(tt.result))
		})
	}
}
