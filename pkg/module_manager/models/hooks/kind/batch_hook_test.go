package kind_test

import (
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

func Test_BatchHook_Config(t *testing.T) {
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
				data: `[
				{"configVersion":"v1",
				"onStartup":10,
				"beforeHelm":5,
				"afterHelm":15,
				"afterDeleteHelm":25,
				"metadata":{
				"name":"main",
				"path":"some-path/hooks/"},
				"kubernetes":[
				{"name":"apiservers",
				"apiVersion":"v1",
				"kind":"Pod",
				"namespace":{
				"nameSelector":{
				"matchNames":["kube-system"]}},
				"labelSelector":{
				"matchLabels":{"component":"kube-apiserver"}},
				"keepFullObjectsInMemory":false,
				"jqFilter":".metadata.name"}]}]`,
			},
			wants: wants{
				err:             nil,
				onStartup:       ptr.To(10.0),
				beforeHelm:      ptr.To(5.0),
				afterHelm:       ptr.To(15.0),
				afterDeleteHelm: ptr.To(25.0),
			},
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
	}

	for _, tt := range tests {
		if !tt.meta.enabled {
			continue
		}

		t.Run(tt.meta.name, func(t *testing.T) {
			t.Parallel()

			filename := fmt.Sprintf("./%d-hook.sh", time.Now().Nanosecond())

			bHook := &kind.BatchHook{
				Hook: hook.Hook{
					Path:   filename,
					Logger: log.NewNop(),
				},
			}

			err := os.WriteFile(filename, []byte(`#!/bin/sh
if [[ "${1:-}" == "hook" && "${2:-}" == "config" ]] ; then
echo '`+strings.ReplaceAll(tt.args.data, "\n", "'\n echo '")+`'
exit 0
fi
`), 0o777)
			defer func() {
				// os.Remove(filename)
			}()
			assert.NoError(t, err)

			_, err = bHook.GetConfigForModule("")
			if tt.wants.err == nil {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tt.wants.err.Error())
				return
			}
			assert.Equal(t, tt.wants.onStartup, bHook.GetOnStartup())
			assert.Equal(t, tt.wants.beforeHelm, bHook.GetBeforeAll())
			assert.Equal(t, tt.wants.afterHelm, bHook.GetAfterAll())
			assert.Equal(t, tt.wants.afterDeleteHelm, bHook.GetAfterDeleteHelm())
		})
	}
}
