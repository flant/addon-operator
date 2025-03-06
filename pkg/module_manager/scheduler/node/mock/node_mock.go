package node_mock

import (
	"slices"
)

type MockModule struct {
	EnabledScriptResult   bool
	EnabledScriptErr      error
	EnabledModules        *[]string
	ListOfRequiredModules []string
	Name                  string
	Path                  string
	Order                 uint32
}

func (m MockModule) GetName() string {
	return m.Name
}

func (m MockModule) GetPath() string {
	return m.Path
}

func (m MockModule) GetOrder() uint32 {
	return m.Order
}

func (m MockModule) RunEnabledScript(_ string, _ []string, _ map[string]string) (bool, error) {
	if m.EnabledScriptErr != nil {
		return false, m.EnabledScriptErr
	}

	depsEnabled := true
	if len(m.ListOfRequiredModules) > 0 && m.EnabledModules != nil {
		for _, requiredModule := range m.ListOfRequiredModules {
			if !slices.Contains(*m.EnabledModules, requiredModule) {
				depsEnabled = false
				break
			}
		}
	}

	if depsEnabled && m.EnabledScriptResult && m.EnabledModules != nil {
		*m.EnabledModules = append(*m.EnabledModules, m.Name)
	}

	return depsEnabled && m.EnabledScriptResult, nil
}
