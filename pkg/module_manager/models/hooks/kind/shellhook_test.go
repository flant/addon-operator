package kind_test

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/deckhouse/module-sdk/pkg/utils/ptr"
	"github.com/stretchr/testify/assert"

	"github.com/flant/addon-operator/pkg/module_manager/models/hooks/kind"
	"github.com/flant/shell-operator/pkg/hook"
)

func Test_ShellHook_Global_Config_v0_v1(t *testing.T) {
	t.Parallel()

	type meta struct {
		name    string
		enabled bool
	}

	type fields struct{}

	type args struct {
		data string
	}

	type wants struct {
		err       error
		beforeAll *float64
		afterAll  *float64
	}

	tests := []struct {
		meta   meta
		fields fields
		args   args
		wants  wants
	}{
		{
			meta: meta{
				name:    "load v0 config",
				enabled: true,
			},
			args: args{
				data: `{"onStartup":10, "schedule":[{"crontab":"*/5 * * * * *"}], "beforeAll":22, "afterAll":10}`,
			},
			wants: wants{
				err:       nil,
				beforeAll: ptr.To(22.0),
				afterAll:  ptr.To(10.0),
			},
		},
		{
			meta: meta{
				name:    "load v1 config",
				enabled: true,
			},
			args: args{
				data: `{"configVersion": "v1", "onStartup":10, "kubernetes":[{"kind":"Pod", "watchEvent":["Added"]}], "beforeAll":22, "afterAll":10}`,
			},
			wants: wants{
				err:       nil,
				beforeAll: ptr.To(22.0),
				afterAll:  ptr.To(10.0),
			},
		},
		{
			meta: meta{
				name:    "load v1 yaml config",
				enabled: true,
			},
			args: args{
				data: `
configVersion: v1
onStartup: 10
kubernetes:
- kind: Pod
  watchEvent: ["Added"]
beforeAll: 22
afterAll: 10
`,
			},
			wants: wants{
				err:       nil,
				beforeAll: ptr.To(22.0),
				afterAll:  ptr.To(10.0),
			},
		},
	}

	for _, tt := range tests {
		if !tt.meta.enabled {
			continue
		}

		t.Run(tt.meta.name, func(t *testing.T) {
			t.Parallel()

			filename := fmt.Sprintf("./%d-%s-hook.sh", time.Now().Nanosecond(), tt.meta.name)

			shHook := &kind.ShellHook{
				Hook: hook.Hook{
					Path:   filename,
					Logger: log.NewNop(),
				},
			}

			err := os.WriteFile(filename, []byte(`#!/bin/sh
if [[ "${1:-}" == "--config" ]] ; then
echo '`+strings.ReplaceAll(tt.args.data, "\n", "'\n echo '")+`'
exit 0
fi
`), 0o777)
			defer func() {
				os.Remove(filename)
			}()
			assert.NoError(t, err)

			_, err = shHook.GetConfigForModule("global")
			assert.Equal(t, tt.wants.err, err)
			assert.Equal(t, tt.wants.beforeAll, shHook.GetBeforeAll())
			assert.Equal(t, tt.wants.afterAll, shHook.GetAfterAll())
		})
	}
}

func Test_ShellHook_Embedded_Config_v0_v1(t *testing.T) {
	t.Parallel()

	type meta struct {
		name    string
		enabled bool
	}

	type fields struct{}

	type args struct {
		data string
	}

	type wants struct {
		err             error
		onStartup       *float64
		beforeHelm      *float64
		afterHelm       *float64
		afterDeleteHelm *float64
	}

	tests := []struct {
		meta   meta
		fields fields
		args   args
		wants  wants
	}{
		{
			meta: meta{
				name:    "load v0 config",
				enabled: true,
			},
			args: args{
				data: `{"onStartup":10,
               "schedule":[{"crontab":"*/5 * * * * *"}],
               "beforeHelm": 5,
               "afterHelm": 15,
               "afterDeleteHelm": 25
             }`,
			},
			wants: wants{
				err:             nil,
				onStartup:       ptr.To(10.0),
				beforeHelm:      ptr.To(5.0),
				afterHelm:       ptr.To(15.0),
				afterDeleteHelm: ptr.To(25.0),
			},
			// func() {
			// 	g.Expect(err).ShouldNot(HaveOccurred())
			// 	g.Expect(config.Schedules).To(HaveLen(1))
			// 	g.Expect(config.HasBinding(OnStartup)).To(BeTrue())
			// 	g.Expect(config.OnStartup.Order).To(Equal(10.0))
			// 	g.Expect(config.HasBinding(OnKubernetesEvent)).To(BeFalse())
			// 	// Check module specific bindings
			// 	g.Expect(config.HasBinding(BeforeHelm)).To(BeTrue())
			// 	g.Expect(config.BeforeHelm.Order).To(Equal(5.0))
			// 	g.Expect(config.HasBinding(AfterHelm)).To(BeTrue())
			// 	g.Expect(config.AfterHelm.Order).To(Equal(15.0))
			// 	g.Expect(config.HasBinding(AfterDeleteHelm)).To(BeTrue())
			// 	g.Expect(config.AfterDeleteHelm.Order).To(Equal(25.0))
			// },
		},
		{
			meta: meta{
				name:    "load v0 onStart",
				enabled: true,
			},
			args: args{
				data: `{"onStartup":10}`,
			},
			wants: wants{
				err:       nil,
				onStartup: ptr.To(10.0),
			},
			// func() {
			// 	g.Expect(err).ShouldNot(HaveOccurred())
			// 	g.Expect(config.Bindings()).To(HaveLen(1))
			// },
		},
		{
			meta: meta{
				name:    "load v1 config",
				enabled: true,
			},
			args: args{
				data: `{"configVersion": "v1",
                  "onStartup":10,
                  "kubernetes":[{"kind":"Pod", "watchEvent":["Added"]}],
                 "beforeHelm": 98,
                 "afterHelm": 58,
                 "afterDeleteHelm": 18}`,
			},
			wants: wants{
				err:             nil,
				onStartup:       ptr.To(10.0),
				beforeHelm:      ptr.To(98.0),
				afterHelm:       ptr.To(58.0),
				afterDeleteHelm: ptr.To(18.0),
			},
			// func() {
			// 	g.Expect(err).ShouldNot(HaveOccurred())
			// 	g.Expect(config.HasBinding(OnStartup)).To(BeTrue())
			// 	g.Expect(config.HasBinding(Schedule)).To(BeFalse())
			// 	g.Expect(config.HasBinding(OnKubernetesEvent)).To(BeTrue())
			// 	g.Expect(config.OnKubernetesEvents).To(HaveLen(1))
			// 	// Check module specific bindings
			// 	g.Expect(config.HasBinding(BeforeHelm)).To(BeTrue())
			// 	g.Expect(config.BeforeHelm.Order).To(Equal(98.0))
			// 	g.Expect(config.HasBinding(AfterHelm)).To(BeTrue())
			// 	g.Expect(config.AfterHelm.Order).To(Equal(58.0))
			// 	g.Expect(config.HasBinding(AfterDeleteHelm)).To(BeTrue())
			// 	g.Expect(config.AfterDeleteHelm.Order).To(Equal(18.0))
			// },
		},
		{
			meta: meta{
				name:    "load v1 yaml config",
				enabled: true,
			},
			args: args{
				data: `
configVersion: v1
onStartup: 10
kubernetes:
- kind: Pod
  watchEvent: ["Added"]
beforeHelm: 98
afterHelm: 58
afterDeleteHelm: 18
`,
			},
			wants: wants{
				err:             nil,
				onStartup:       ptr.To(10.0),
				beforeHelm:      ptr.To(98.0),
				afterHelm:       ptr.To(58.0),
				afterDeleteHelm: ptr.To(18.0),
			},
			// func() {
			// 	g.Expect(err).ShouldNot(HaveOccurred())
			// 	g.Expect(config.HasBinding(OnStartup)).To(BeTrue())
			// 	g.Expect(config.HasBinding(Schedule)).To(BeFalse())
			// 	g.Expect(config.HasBinding(OnKubernetesEvent)).To(BeTrue())
			// 	g.Expect(config.OnKubernetesEvents).To(HaveLen(1))
			// 	// Check module specific bindings
			// 	g.Expect(config.HasBinding(BeforeHelm)).To(BeTrue())
			// 	g.Expect(config.BeforeHelm.Order).To(Equal(98.0))
			// 	g.Expect(config.HasBinding(AfterHelm)).To(BeTrue())
			// 	g.Expect(config.AfterHelm.Order).To(Equal(58.0))
			// 	g.Expect(config.HasBinding(AfterDeleteHelm)).To(BeTrue())
			// 	g.Expect(config.AfterDeleteHelm.Order).To(Equal(18.0))
			// },
		},
		{
			meta: meta{
				name:    "load v1 bad config",
				enabled: true,
			},
			args: args{
				data: `{"configVersion": "v1", "onStartuppp":10, "kubernetes":[{"kind":"Pod", "watchEvent":["Added"]}], "beforeHelm":98}`,
			},
			wants: wants{
				err: errors.New("onStartuppp is a forbidden property"),
			},
		},
	}

	for _, tt := range tests {
		if !tt.meta.enabled {
			continue
		}

		t.Run(tt.meta.name, func(t *testing.T) {
			t.Parallel()

			filename := fmt.Sprintf("./%d-%s-hook.sh", time.Now().Nanosecond(), tt.meta.name)

			shHook := &kind.ShellHook{
				Hook: hook.Hook{
					Path:   filename,
					Logger: log.NewNop(),
				},
			}

			err := os.WriteFile(filename, []byte(`#!/bin/sh
if [[ "${1:-}" == "--config" ]] ; then
echo '`+strings.ReplaceAll(tt.args.data, "\n", "'\n echo '")+`'
exit 0
fi
`), 0o777)
			defer func() {
				os.Remove(filename)
			}()
			assert.NoError(t, err)

			_, err = shHook.GetConfigForModule("embedded")
			if tt.wants.err == nil {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tt.wants.err.Error())
				return
			}
			assert.Equal(t, tt.wants.onStartup, shHook.GetOnStartup())
			assert.Equal(t, tt.wants.beforeHelm, shHook.GetBeforeAll())
			assert.Equal(t, tt.wants.afterHelm, shHook.GetAfterAll())
			assert.Equal(t, tt.wants.afterDeleteHelm, shHook.GetAfterDeleteHelm())
		})
	}
}
