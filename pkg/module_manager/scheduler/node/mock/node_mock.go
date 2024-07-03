package node_mock

type MockModule struct {
	EnabledScriptResult bool
	EnabledScriptErr    error
	Name                string
	Path                string
	Order               uint32
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
	return m.EnabledScriptResult, nil
}
