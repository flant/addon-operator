package module_manager

import (
	"testing"

	. "github.com/onsi/gomega"

	. "github.com/flant/addon-operator/pkg/hook/types"
	. "github.com/flant/shell-operator/pkg/hook/types"
)

func Test_GlobalHook_WithConfig(t *testing.T) {
	g := NewWithT(t)

	var gh *GlobalHook
	var err error

	tests := []struct {
		name     string
		config   string
		assertFn func()
	}{
		{
			"asd",
			`configVersion: v1
onStartup: 10
kubernetes:
- name: pods
  kind: Pod
schedule:
- name: planned
  crontab: '* * * * *'
afterAll: 22
beforeAll: 23
`,
			func() {
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(gh).ShouldNot(BeNil())
				config := gh.Config
				g.Expect(gh.Order(OnStartup)).To(Equal(10.0))
				g.Expect(gh.Order(BeforeAll)).To(Equal(23.0))
				g.Expect(gh.Order(AfterAll)).To(Equal(22.0))
				g.Expect(config.OnKubernetesEvents).To(HaveLen(1))
				g.Expect(config.Schedules).To(HaveLen(1))

				g.Expect(gh.GetConfigDescription()).To(ContainSubstring("beforeAll:23, afterAll:22, OnStartup:10"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gh = NewGlobalHook("test", "/global-hooks/test.sh")
			err = gh.WithConfig([]byte(tt.config))
			tt.assertFn()
		})
	}
}
