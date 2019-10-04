package module_manager

import "github.com/flant/addon-operator/pkg/kube_config_manager"

type ModuleManagerMockFns struct {
	Init func() error
	Run func()
	DiscoverModulesState func() (*ModulesState, error)
	GetModule func(name string) (*Module, error)
	GetModuleNamesInOrder func() []string
	GetGlobalHook func(name string) (*GlobalHook, error)
	GetModuleHook func(name string) (*ModuleHook, error)
	GetGlobalHooksInOrder func(bindingType BindingType) []string
	GetModuleHooksInOrder func(moduleName string, bindingType BindingType) ([]string, error)
	DeleteModule func(moduleName string) error
	RunModule func(moduleName string, onStartup bool) error
	RunGlobalHook func(hookName string, binding BindingType, bindingContext []BindingContext) error
	RunModuleHook func(hookName string, binding BindingType, bindingContext []BindingContext) error
	Retry func()
	WithDirectories func(modulesDir string, globalHooksDir string, tempDir string) ModuleManager
	WithKubeConfigManager func(kubeConfigManager kube_config_manager.KubeConfigManager) ModuleManager
	RegisterModuleHooks func(*Module) error
}


func NewModuleManagerMock(fns ModuleManagerMockFns) ModuleManager {
	return &ModuleManagerMock{
		Fns: fns,
	}
}

type ModuleManagerMock struct {
	Fns ModuleManagerMockFns
}

func (m *ModuleManagerMock) Init() error {
	if m.Fns.Init != nil {
		return m.Fns.Init()
	}
	panic("implement me")
}

func (m *ModuleManagerMock) Run() {
	if m.Fns.Run != nil {
		m.Fns.Run()
	}
	panic("implement me")
}

func (m *ModuleManagerMock) DiscoverModulesState() (*ModulesState, error) {
	if m.Fns.DiscoverModulesState != nil {
		return m.Fns.DiscoverModulesState()
	}
	panic("implement me")
}

func (m *ModuleManagerMock) GetModule(name string) (*Module, error) {
	if m.Fns.GetModule != nil {
		return m.Fns.GetModule(name)
	}
	panic("implement me")
}

func (m *ModuleManagerMock) GetModuleNamesInOrder() []string {
	if m.Fns.GetModuleNamesInOrder != nil {
		return m.Fns.GetModuleNamesInOrder()
	}
	panic("implement me")
}

func (m *ModuleManagerMock) GetGlobalHook(name string) (*GlobalHook, error) {
	if m.Fns.GetGlobalHook != nil {
		return m.Fns.GetGlobalHook(name)
	}
	panic("implement me")
}

func (m *ModuleManagerMock) GetModuleHook(name string) (*ModuleHook, error) {
	if m.Fns.GetModuleHook != nil {
		return m.Fns.GetModuleHook(name)
	}
	panic("implement me")
}

func (m *ModuleManagerMock) GetGlobalHooksInOrder(bindingType BindingType) []string {
	if m.Fns.GetGlobalHooksInOrder != nil {
		return m.Fns.GetGlobalHooksInOrder(bindingType)
	}
	panic("implement me")
}

func (m *ModuleManagerMock) GetModuleHooksInOrder(moduleName string, bindingType BindingType) ([]string, error) {
	if m.Fns.GetModuleHooksInOrder != nil {
		return m.Fns.GetModuleHooksInOrder(moduleName, bindingType)
	}
	panic("implement me")
}

func (m *ModuleManagerMock) DeleteModule(moduleName string) error {
	if m.Fns.DeleteModule != nil {
		return m.Fns.DeleteModule(moduleName)
	}
	panic("implement me")
}

func (m *ModuleManagerMock) RunModule(moduleName string, onStartup bool) error {
	if m.Fns.RunModule != nil {
		return m.Fns.RunModule(moduleName, onStartup)
	}
	panic("implement me")
}

func (m *ModuleManagerMock) RunGlobalHook(hookName string, binding BindingType, bindingContext []BindingContext) error {
	if m.Fns.RunGlobalHook != nil {
		return m.Fns.RunGlobalHook(hookName, binding, bindingContext)
	}
	panic("implement me")
}

func (m *ModuleManagerMock) RunModuleHook(hookName string, binding BindingType, bindingContext []BindingContext) error {
	if m.Fns.RunModuleHook != nil {
		return m.Fns.RunModuleHook(hookName, binding, bindingContext)
	}
	panic("implement me")
}

func (m *ModuleManagerMock) Retry() {
	if m.Fns.Retry != nil {
		m.Fns.Retry()
	}
	panic("implement me")
}

func (m *ModuleManagerMock) WithDirectories(modulesDir string, globalHooksDir string, tempDir string) ModuleManager {
	if m.Fns.WithDirectories != nil {
		return m.Fns.WithDirectories(modulesDir, globalHooksDir, tempDir)
	}
	panic("implement me")
}

func (m *ModuleManagerMock) WithKubeConfigManager(kubeConfigManager kube_config_manager.KubeConfigManager) ModuleManager {
	if m.Fns.WithKubeConfigManager != nil {
		return m.Fns.WithKubeConfigManager(kubeConfigManager)
	}
	panic("implement me")
}

func (m *ModuleManagerMock) RegisterModuleHooks(module *Module) error {
	if m.Fns.RegisterModuleHooks != nil {
		return m.Fns.RegisterModuleHooks(module)
	}
	panic("implement me")
}
