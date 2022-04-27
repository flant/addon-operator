package task_test

import (
	"testing"

	"github.com/flant/shell-operator/pkg/hook/types"
	. "github.com/onsi/gomega"

	sh_task "github.com/flant/shell-operator/pkg/task"

	. "github.com/flant/addon-operator/pkg/hook/types"
	. "github.com/flant/addon-operator/pkg/task"
)

func Test_MetadataAccessor(tT *testing.T) {
	g := NewWithT(tT)
	t := sh_task.NewTask(ModuleRun)

	t.WithMetadata(HookMetadata{
		BindingType:      BeforeAll,
		ModuleName:       "module-name",
		EventDescription: "ReloadAllTasks",
		DoModuleStartup:  true,
	})

	hm := HookMetadataAccessor(t)

	g.Expect(hm.ModuleName).Should(Equal("module-name"))
}

func Test_TaskDescription(t *testing.T) {
	tests := []struct {
		name     string
		metadata HookMetadata
		expect   string
	}{
		{
			"global hook",
			HookMetadata{
				BindingType:      BeforeAll,
				ModuleName:       "module-name",
				HookName:         "hook.sh",
				EventDescription: "ReloadAllTasks",
				DoModuleStartup:  true,
			},
			"beforeAll:hook.sh:ReloadAllTasks",
		},
		{
			"module run",
			HookMetadata{
				ModuleName:       "module-name",
				EventDescription: "BootstrapMainQueue",
			},
			"module-name:BootstrapMainQueue",
		},
		{
			"module run with DoModuleStartup",
			HookMetadata{
				ModuleName:       "module-name",
				EventDescription: "GlobalValuesChanged",
				DoModuleStartup:  true,
			},
			"module-name:doStartup:GlobalValuesChanged",
		},
		{
			"module hook",
			HookMetadata{
				BindingType:      types.OnKubernetesEvent,
				ModuleName:       "module",
				HookName:         "module/hook.sh",
				EventDescription: "Kubernetes",
			},
			"kubernetes:module/hook.sh:Kubernetes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(tt.metadata.GetDescription()).To(Equal(tt.expect))
		})
	}
}
