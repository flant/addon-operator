package kind

// import (
// 	"testing"

// 	. "github.com/onsi/gomega"

// 	. "github.com/flant/addon-operator/pkg/hook/types"
// 	. "github.com/flant/shell-operator/pkg/hook/types"
// )

// func Test_ModuleHook_Batch_Config_v0_v1(t *testing.T) {
// 	var g *WithT
// 	var err error
// 	var config *ModuleHookConfig

// 	tests := []struct {
// 		name      string
// 		hookName  string
// 		config    *sdkhook.HookConfig
// 		assertion func()
// 	}{
// 		{
// 			"load v0 module config",
// 			"hook_v0",
// 			// `{"onStartup":10,
// 			//    "schedule":[{"crontab":"*/5 * * * * *"}],
// 			//    "beforeHelm": 5,
// 			//    "afterHelm": 15,
// 			//    "afterDeleteHelm": 25
// 			//  }`,
// 			&sdkhook.HookConfig{
// 				OnStartup: ptr.To(uint(10)),
// 				Schedule: []sdkhook.ScheduleConfig{
// 					{Crontab: "*/5 * * * * *"},
// 				},
// 				OnBeforeHelm:      ptr.To(uint(5)),
// 				OnAfterHelm:       ptr.To(uint(15)),
// 				OnAfterDeleteHelm: ptr.To(uint(25)),
// 			},
// 			func() {
// 				g.Expect(err).ShouldNot(HaveOccurred())
// 				g.Expect(config.Schedules).To(HaveLen(1))
// 				g.Expect(config.HasBinding(OnStartup)).To(BeTrue())
// 				g.Expect(config.OnStartup.Order).To(Equal(10.0))
// 				g.Expect(config.HasBinding(OnKubernetesEvent)).To(BeFalse())
// 				// Check module specific bindings
// 				g.Expect(config.HasBinding(BeforeHelm)).To(BeTrue())
// 				g.Expect(config.BeforeHelm.Order).To(Equal(5.0))
// 				g.Expect(config.HasBinding(AfterHelm)).To(BeTrue())
// 				g.Expect(config.AfterHelm.Order).To(Equal(15.0))
// 				g.Expect(config.HasBinding(AfterDeleteHelm)).To(BeTrue())
// 				g.Expect(config.AfterDeleteHelm.Order).To(Equal(25.0))
// 			},
// 		},
// 		{
// 			"load v0 onStart",
// 			"hook_v0",
// 			// `{"onStartup":10}`,
// 			&sdkhook.HookConfig{
// 				OnStartup: ptr.To(uint(10)),
// 			},
// 			func() {
// 				g.Expect(err).ShouldNot(HaveOccurred())
// 				g.Expect(config.Bindings()).To(HaveLen(1))
// 			},
// 		},
// 		{
// 			"load v1 module config",
// 			"hook_v1",
// 			// `{"configVersion": "v1",
// 			//       "onStartup":10,
// 			//       "kubernetes":[{"kind":"Pod", "watchEvent":["Added"]}],
// 			//      "beforeHelm": 98,
// 			//      "afterHelm": 58,
// 			//      "afterDeleteHelm": 18}`,
// 			&sdkhook.HookConfig{
// 				ConfigVersion: "v1",
// 				OnStartup:     ptr.To(uint(10)),
// 				Kubernetes: []sdkhook.KubernetesConfig{
// 					{Kind: "Pod"},
// 				},
// 				OnBeforeHelm:      ptr.To(uint(98)),
// 				OnAfterHelm:       ptr.To(uint(58)),
// 				OnAfterDeleteHelm: ptr.To(uint(18)),
// 			},
// 			func() {
// 				g.Expect(err).ShouldNot(HaveOccurred())
// 				g.Expect(config.HasBinding(OnStartup)).To(BeTrue())
// 				g.Expect(config.HasBinding(Schedule)).To(BeFalse())
// 				g.Expect(config.HasBinding(OnKubernetesEvent)).To(BeTrue())
// 				g.Expect(config.OnKubernetesEvents).To(HaveLen(1))
// 				// Check module specific bindings
// 				g.Expect(config.HasBinding(BeforeHelm)).To(BeTrue())
// 				g.Expect(config.BeforeHelm.Order).To(Equal(98.0))
// 				g.Expect(config.HasBinding(AfterHelm)).To(BeTrue())
// 				g.Expect(config.AfterHelm.Order).To(Equal(58.0))
// 				g.Expect(config.HasBinding(AfterDeleteHelm)).To(BeTrue())
// 				g.Expect(config.AfterDeleteHelm.Order).To(Equal(18.0))
// 			},
// 		},
// 		{
// 			"load v1 module yaml config",
// 			"hook_v1",
// 			//	`
// 			//
// 			// configVersion: v1
// 			// onStartup: 10
// 			// kubernetes:
// 			//   - kind: Pod
// 			//     watchEvent: ["Added"]
// 			//
// 			// beforeHelm: 98
// 			// afterHelm: 58
// 			// afterDeleteHelm: 18
// 			// `,
// 			&sdkhook.HookConfig{
// 				ConfigVersion: "v1",
// 				OnStartup:     ptr.To(uint(10)),
// 				Kubernetes: []sdkhook.KubernetesConfig{
// 					{Kind: "Pod"},
// 				},
// 				OnBeforeHelm:      ptr.To(uint(98)),
// 				OnAfterHelm:       ptr.To(uint(58)),
// 				OnAfterDeleteHelm: ptr.To(uint(18)),
// 			},
// 			func() {
// 				g.Expect(err).ShouldNot(HaveOccurred())
// 				g.Expect(config.HasBinding(OnStartup)).To(BeTrue())
// 				g.Expect(config.HasBinding(Schedule)).To(BeFalse())
// 				g.Expect(config.HasBinding(OnKubernetesEvent)).To(BeTrue())
// 				g.Expect(config.OnKubernetesEvents).To(HaveLen(1))
// 				// Check module specific bindings
// 				g.Expect(config.HasBinding(BeforeHelm)).To(BeTrue())
// 				g.Expect(config.BeforeHelm.Order).To(Equal(98.0))
// 				g.Expect(config.HasBinding(AfterHelm)).To(BeTrue())
// 				g.Expect(config.AfterHelm.Order).To(Equal(58.0))
// 				g.Expect(config.HasBinding(AfterDeleteHelm)).To(BeTrue())
// 				g.Expect(config.AfterDeleteHelm.Order).To(Equal(18.0))
// 			},
// 		},
// 	}

// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			g = NewWithT(t)
// 			config = &ModuleHookConfig{}
// 			err = config.LoadAndValidateBatchConfig(tt.config)
// 			tt.assertion()
// 		})
// 	}
// }
