// a bunch of mocked extenders for tests
package extenders_mock

import "github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders"

type FilterOne struct{}

func (f *FilterOne) Name() extenders.ExtenderName {
	return extenders.ExtenderName("FilterOne")
}

func (f *FilterOne) Filter(_ string, _ map[string]string) (*bool, error) {
	return nil, nil
}

func (f *FilterOne) IsTerminator() bool {
	return false
}

type FilterTwo struct{}

func (f *FilterTwo) Name() extenders.ExtenderName {
	return extenders.ExtenderName("FilterTwo")
}

func (f *FilterTwo) Filter(_ string, _ map[string]string) (*bool, error) {
	return nil, nil
}

func (f *FilterTwo) IsTerminator() bool {
	return false
}

type FilterThree struct{}

func (f *FilterThree) Name() extenders.ExtenderName {
	return extenders.ExtenderName("FilterThree")
}

func (f *FilterThree) Filter(_ string, _ map[string]string) (*bool, error) {
	return nil, nil
}

func (f *FilterThree) IsTerminator() bool {
	return false
}

type FilterFour struct{}

func (f *FilterFour) Name() extenders.ExtenderName {
	return extenders.ExtenderName("FilterOne")
}

func (f *FilterFour) Filter(_ string, _ map[string]string) (*bool, error) {
	return nil, nil
}

func (f *FilterFour) IsTerminator() bool {
	return false
}

type TerminatorOne struct{}

func (f *TerminatorOne) Name() extenders.ExtenderName {
	return extenders.ExtenderName("TerminatorOne")
}

func (f *TerminatorOne) Filter(_ string, _ map[string]string) (*bool, error) {
	return nil, nil
}

func (f *TerminatorOne) IsTerminator() bool {
	return true
}

type TerminatorTwo struct{}

func (f *TerminatorTwo) Name() extenders.ExtenderName {
	return extenders.ExtenderName("TerminatorTwo")
}

func (f *TerminatorTwo) Filter(_ string, _ map[string]string) (*bool, error) {
	return nil, nil
}

func (f *TerminatorTwo) IsTerminator() bool {
	return true
}

type TerminatorThree struct{}

func (f *TerminatorThree) Name() extenders.ExtenderName {
	return extenders.ExtenderName("TerminatorThree")
}

func (f *TerminatorThree) Filter(_ string, _ map[string]string) (*bool, error) {
	return nil, nil
}

func (f *TerminatorThree) IsTerminator() bool {
	return true
}

type TopologicalOne struct{}

func (f *TopologicalOne) Name() extenders.ExtenderName {
	return extenders.ExtenderName("TopologicalOne")
}

func (f *TopologicalOne) Filter(_ string, _ map[string]string) (*bool, error) {
	return nil, nil
}

func (f *TopologicalOne) IsTerminator() bool {
	return true
}

func (f *TopologicalOne) GetTopologicalHints(moduleName string) []string {
	switch moduleName {
	case "echo":
		return []string{"prometheus", "cert-manager"}
	case "prometheus":
		return []string{"prometheus-crd"}
	case "foobar":
		return []string{"foo", "bar"}
	case "operator-trivy":
		return []string{"istio", "admission-policy-engine"}
	}

	return nil
}

type TopologicalTwo struct{}

func (f *TopologicalTwo) Name() extenders.ExtenderName {
	return extenders.ExtenderName("TopologicalTwo")
}

func (f *TopologicalTwo) Filter(_ string, _ map[string]string) (*bool, error) {
	return nil, nil
}

func (f *TopologicalTwo) IsTerminator() bool {
	return true
}

func (f *TopologicalTwo) GetTopologicalHints(moduleName string) []string {
	if moduleName == "my-module" {
		return []string{"unknown-module"}
	}

	return nil
}
