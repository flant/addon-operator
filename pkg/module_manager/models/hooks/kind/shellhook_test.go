package kind_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/flant/addon-operator/pkg/module_manager/models/hooks/kind"
	"github.com/flant/shell-operator/pkg/hook"
	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"
)

func Test_ShellHook_Config_v0_v1(t *testing.T) {
	t.Parallel()

	type meta struct {
		name    string
		enabled bool
	}

	type fields struct {
	}

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

			filename := fmt.Sprintf("./%d-hook.sh", time.Now().Nanosecond())

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
