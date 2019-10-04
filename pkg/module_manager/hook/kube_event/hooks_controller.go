package kube_event

import (
	"fmt"

	"github.com/flant/shell-operator/pkg/hook"
	"github.com/flant/shell-operator/pkg/hook/kube_event"
	"github.com/flant/shell-operator/pkg/kube_events_manager"

	"github.com/flant/addon-operator/pkg/module_manager"
	"github.com/flant/addon-operator/pkg/task"
)

type KubernetesHooksController interface {
	WithModuleManager(moduleManager module_manager.ModuleManager)
	WithKubeEventsManager(kube_events_manager.KubeEventsManager)
	EnableGlobalHooks() error
	EnableModuleHooks(moduleName string) error
	DisableModuleHooks(moduleName string) error
	HandleEvent(kubeEvent kube_events_manager.KubeEvent) ([]task.Task, error)
}

type kubernetesHooksController struct {
	// storages for registered module hooks and global hooks
	GlobalHooks    map[string]*kube_event.KubeEventHook
	ModuleHooks    map[string]*kube_event.KubeEventHook
	EnabledModules []string

	// dependencies
	moduleManager  module_manager.ModuleManager
	kubeEventsManager kube_events_manager.KubeEventsManager
}

// kubernetesHooksController should implement KubernetesHooksController
var _ KubernetesHooksController = &kubernetesHooksController{}

// NewKubernetesHooksController returns new instance of kubernetesHooksController
func NewKubernetesHooksController() *kubernetesHooksController {
	obj := &kubernetesHooksController{}
	obj.GlobalHooks = make(map[string]*kube_event.KubeEventHook)
	obj.ModuleHooks = make(map[string]*kube_event.KubeEventHook)
	obj.EnabledModules = make([]string, 0)
	return obj
}

func (c *kubernetesHooksController) WithModuleManager(mm module_manager.ModuleManager) {
	c.moduleManager = mm
}

func (c *kubernetesHooksController) WithKubeEventsManager(kem kube_events_manager.KubeEventsManager) {
	c.kubeEventsManager = kem
}

// EnableGlobalHooks starts kube events informers for all global hooks
func (c *kubernetesHooksController) EnableGlobalHooks() error {
	globalHooks := c.moduleManager.GetGlobalHooksInOrder(module_manager.KubeEvents)

	for _, globalHookName := range globalHooks {
		globalHook, _ := c.moduleManager.GetGlobalHook(globalHookName)

		for _, config := range globalHook.Config.OnKubernetesEvents {
			err := c.kubeEventsManager.AddMonitor("", config.Monitor)
			if err != nil {
				return fmt.Errorf("run kube monitor for hook %s: %s", globalHook.Name, err)
			}
			c.GlobalHooks[config.Monitor.Metadata.ConfigId] = &kube_event.KubeEventHook{
				HookName:     globalHook.Name,
				ConfigName:   config.ConfigName,
				AllowFailure: config.AllowFailure,
			}
		}
		//
		//for _, desc := range MakeKubeEventHookDescriptors(globalHook, &globalHook.Config.HookConfig) {
		//	configId, err := c.kubeEventsManager.Run(desc.EventTypes, desc.Kind, desc.Namespace, desc.Selector, desc.ObjectName, desc.JqFilter, desc.Debug)
		//	if err != nil {
		//		return err
		//	}
		//	c.GlobalHooks[configId] = desc
		//
		//	rlog.Debugf("MAIN: run informer %s for global hook %s", configId, globalHook.Name)
		//}
	}

	return nil
}

// EnableModuleHooks starts kube events informers for all module hooks
func (c *kubernetesHooksController) EnableModuleHooks(moduleName string) error {
	for _, enabledModuleName := range c.EnabledModules {
		if enabledModuleName == moduleName {
			// already enabled
			return nil
		}
	}

	moduleHooks, err := c.moduleManager.GetModuleHooksInOrder(moduleName, module_manager.KubeEvents)
	if err != nil {
		return err
	}

	for _, moduleHookName := range moduleHooks {
		moduleHook, _ := c.moduleManager.GetModuleHook(moduleHookName)

		for _, config := range moduleHook.Config.OnKubernetesEvents {
			err := c.kubeEventsManager.AddMonitor("", config.Monitor)
			if err != nil {
				return fmt.Errorf("run kube monitor for hook %s: %s", moduleHook.Name, err)
			}
			c.ModuleHooks[config.Monitor.Metadata.ConfigId] = &kube_event.KubeEventHook{
				HookName:     moduleHook.Name,
				ConfigName:   config.ConfigName,
				AllowFailure: config.AllowFailure,
			}
		}

		//for _, desc := range MakeKubeEventHookDescriptors(moduleHook, &moduleHook.Config.HookConfig) {
		//	configId, err := c.kubeEventsManager.Run(desc.EventTypes, desc.Kind, desc.Namespace, desc.Selector, desc.ObjectName, desc.JqFilter, desc.Debug)
		//	if err != nil {
		//		return err
		//	}
		//	c.ModuleHooks[configId] = desc
		//
		//	rlog.Debugf("MAIN: run informer %s for module hook %s", configId, moduleHook.Name)
		//}
	}

	c.EnabledModules = append(c.EnabledModules, moduleName)

	// Start informers for new monitors
	c.kubeEventsManager.Start()
	return nil
}

// DisableModuleHooks stops all monitors for all hooks in module
func (c *kubernetesHooksController) DisableModuleHooks(moduleName string) error {
	// TODO remove EnabledModules index. ConfigId is now in  moduleHook.Config.OnKubernetesEvents[].Monitor.Metadata.ConfigId
	// loop through module hooks and check if configId is in c.ModuleHooks, stop monitor and delete a map item.
	moduleEnabledInd := -1
	for i, enabledModuleName := range c.EnabledModules {
		if enabledModuleName == moduleName {
			moduleEnabledInd = i
			break
		}
	}
	if moduleEnabledInd < 0 {
		return nil
	}
	// remove name from enabled modules index
	c.EnabledModules = append(c.EnabledModules[:moduleEnabledInd], c.EnabledModules[moduleEnabledInd+1:]...)

	disabledModuleHooks, err := c.moduleManager.GetModuleHooksInOrder(moduleName, module_manager.KubeEvents)
	if err != nil {
		return err
	}

	for configId, desc := range c.ModuleHooks {
		for _, disabledModuleHookName := range disabledModuleHooks {
			if desc.HookName == disabledModuleHookName {
				err := c.kubeEventsManager.StopMonitor(configId)
				if err != nil {
					return err
				}

				delete(c.ModuleHooks, configId)

				break
			}
		}
	}

	return nil
}

// HandleEvent creates a task from kube event
func (c *kubernetesHooksController) HandleEvent(kubeEvent kube_events_manager.KubeEvent) ([]task.Task, error) {
	res := make([]task.Task, 0)

	globalHook, hasGlobalHook := c.GlobalHooks[kubeEvent.ConfigId]
	moduleHook, hasModuleHook := c.ModuleHooks[kubeEvent.ConfigId]
	if !hasGlobalHook && !hasModuleHook {
		return nil, fmt.Errorf("Possible a bug: kubernets event '%s/%s/%s %s' is received, but no hook is found", kubeEvent.Namespace, kubeEvent.Kind, kubeEvent.Name, kubeEvent.Event)
	}

	var taskType task.TaskType
	var kubeHook *kube_event.KubeEventHook
	if hasGlobalHook {
		kubeHook = globalHook
		taskType = task.GlobalHookRun
	} else {
		kubeHook = moduleHook
		taskType = task.ModuleHookRun
	}

	switch kubeEvent.Type {
	case "Synchronization":
		// Send all objects
		objList := make([]interface{}, 0)
		for _, obj := range kubeEvent.Objects {
			objList = append(objList, interface{}(obj))
		}
		bindingContext := make([]module_manager.BindingContext, 0)
		bindingContext = append(bindingContext, module_manager.BindingContext{
			BindingContext: hook.BindingContext{
				Binding: kubeHook.ConfigName,
				Type:    kubeEvent.Type,
				Objects: objList,
			},
		})

		newTask := task.NewTask(taskType, kubeHook.HookName).
			WithBinding(module_manager.KubeEvents).
			WithBindingContext(bindingContext).
			WithAllowFailure(kubeHook.AllowFailure)

		res = append(res, newTask)
	default:
		bindingContext := make([]module_manager.BindingContext, 0)
		for _, kEvent := range kubeEvent.WatchEvents {
			bindingContext = append(bindingContext, module_manager.BindingContext{
				BindingContext:	hook.BindingContext{
				Binding:    kubeHook.ConfigName,
				Type:       "Event",
				WatchEvent: kEvent,

				Namespace: kubeEvent.Namespace,
				Kind:      kubeEvent.Kind,
				Name:      kubeEvent.Name,

				Object:       kubeEvent.Object,
				FilterResult: kubeEvent.FilterResult,
			},
			})
		}

		newTask := task.NewTask(taskType, kubeHook.HookName).
			WithBinding(module_manager.KubeEvents).
			WithBindingContext(bindingContext).
			WithAllowFailure(kubeHook.AllowFailure)

		res = append(res, newTask)
	}


	//var desc *kube_event.KubeEventHook
	//var taskType task.TaskType

	//if moduleDesc, hasKey := c.ModuleHooks[kubeEvent.ConfigId]; hasKey {
	//	desc = moduleDesc
	//	taskType = task.ModuleHookRun
	//} else if globalDesc, hasKey := c.GlobalHooks[kubeEvent.ConfigId]; hasKey {
	//	desc = globalDesc
	//	taskType = task.GlobalHookRun
	//}

	//if desc != nil && taskType != "" {
	//	bindingName := desc.Name
	//	if desc.Name == "" {
	//		bindingName = module_manager.ContextBindingType[module_manager.KubeEvents]
	//	}
	//
	//	bindingContext := make([]module_manager.BindingContext, 0)
	//	for _, kEvent := range kubeEvent.Events {
	//		bindingContext = append(bindingContext, module_manager.BindingContext{
	//			Binding:           bindingName,
	//			ResourceEvent:     kEvent,
	//			ResourceNamespace: kubeEvent.Namespace,
	//			ResourceKind:      kubeEvent.Kind,
	//			ResourceName:      kubeEvent.Name,
	//		})
	//	}
	//
	//	newTask := task.NewTask(taskType, desc.HookName).
	//		WithBinding(module_manager.KubeEvents).
	//		WithBindingContext(bindingContext).
	//		WithAllowFailure(desc.Config.AllowFailure)
	//
	//	res = append(res, newTask)
	//} else {
	//	return nil, fmt.Errorf("Unknown kube event: no such config id '%s' registered", kubeEvent.ConfigId)
	//}

	return res, nil
}
