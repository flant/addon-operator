package kind

// import (
// 	"testing"

// 	. "github.com/onsi/gomega"
// )

// func Test_GlobalHook_Config_v0_v1(t *testing.T) {
// 	g := NewWithT(t)

// 	var err error
// 	var shHook *GlobalHookConfig

// 	tests := []struct {
// 		name      string
// 		hookName  string
// 		data      string
// 		assertion func()
// 	}{
// 		{
// 			"load v0 config",
// 			"hook_v0",
// 			`{"onStartup":10, "schedule":[{"crontab":"*/5 * * * * *"}], "beforeAll":22, "afterAll":10}`,
// 			func() {
// 				g.Expect(err).ShouldNot(HaveOccurred())
// 				g.Expect(shHook.BeforeAll.Order).To(Equal(22.0))
// 				g.Expect(shHook.AfterAll.Order).To(Equal(10.0))
// 			},
// 		},
// 		{
// 			"load v1 config",
// 			"hook_v1",
// 			`{"configVersion": "v1", "onStartup":10, "kubernetes":[{"kind":"Pod", "watchEvent":["Added"]}], "beforeAll":22, "afterAll":10}`,
// 			func() {
// 				g.Expect(err).ShouldNot(HaveOccurred())
// 				g.Expect(shHook.BeforeAll.Order).To(Equal(22.0))
// 				g.Expect(shHook.AfterAll.Order).To(Equal(10.0))
// 			},
// 		},
// 		{
// 			"load v1 yaml config",
// 			"hook_v1",
// 			`
// configVersion: v1
// onStartup: 10
// kubernetes:
// - kind: Pod
//   watchEvent: ["Added"]
// beforeAll: 22
// afterAll: 10
// `,
// 			func() {
// 				g.Expect(err).ShouldNot(HaveOccurred())
// 				g.Expect(shHook.BeforeAll.Order).To(Equal(22.0))
// 				g.Expect(shHook.AfterAll.Order).To(Equal(10.0))
// 			},
// 		},
// 	}

// 	for _, test := range tests {
// 		t.Run(test.name, func(_ *testing.T) {
// 			shHook = &ShellHook{}
// 			err = shHook.LoadAndValidateShellConfig([]byte(test.data))
// 			test.assertion()
// 		})
// 	}
// }
