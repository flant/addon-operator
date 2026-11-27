package kind

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	gohook "github.com/flant/addon-operator/pkg/module_manager/go_hook"
	. "github.com/flant/shell-operator/pkg/hook/binding_context"
)

func Test_GoHookConfig_KeepManagedFields(t *testing.T) {
	g := NewWithT(t)

	filter := func(*unstructured.Unstructured) (gohook.FilterResult, error) { return nil, nil }

	cfg, err := newHookConfigFromGoConfig(&gohook.HookConfig{
		Kubernetes: []gohook.KubernetesConfig{
			{Name: "default", ApiVersion: "v1", Kind: "ConfigMap", FilterFunc: filter},
			{Name: "kept", ApiVersion: "v1", Kind: "ConfigMap", FilterFunc: filter, KeepManagedFields: true},
		},
	})
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(cfg.OnKubernetesEvents).To(HaveLen(2))
	g.Expect(cfg.OnKubernetesEvents[0].Monitor.KeepManagedFields).To(BeFalse(), "must be false by default")
	g.Expect(cfg.OnKubernetesEvents[1].Monitor.KeepManagedFields).To(BeTrue())
}

func Test_Config_GoHook(t *testing.T) {
	g := NewWithT(t)

	gh := NewGoHook(&gohook.HookConfig{
		OnAfterAll: &gohook.OrderedConfig{Order: 5},
	}, func(_ context.Context, input *gohook.HookInput) error {
		input.Values.Set("test", "test")
		input.MetricsCollector.Set("test", 1.0, nil)

		return nil
	})

	bc := make([]BindingContext, 0)

	res, err := gh.Execute(context.Background(), "", bc, "", nil, nil, nil)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(res.Patches).ShouldNot(BeEmpty())
	g.Expect(res.Metrics).ShouldNot(BeEmpty())
}
