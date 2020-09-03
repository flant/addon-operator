package go_hooks

import (
	"fmt"
	"github.com/flant/addon-operator/sdk"
)

const moduleName = "node_manager"

func init() {
	sdk.Register(&JqFilterFunc{})
}

type NgsFilterResult struct {
	Annotations map[string]string `json:"annotations"`
	Labels      map[string]string `json:"labels"`
	Taints      []Taint           `json:"taints"`
}

type Taint struct {
	Some   string `json: ...`
	Fields string `json: ...`
	For    string `json: ...`
	Taint  string `json: ...`
}

type JqFilterFunc struct {
	sdk.CommonGoHook
}

func (s *JqFilterFunc) Metadata() sdk.HookMetadata {
	return sdk.HookMetadata{
		Name:   "jq_filter_func",
		Path:   "global-hooks/jq_filter_func",
		Global: true,
	}
}

func (s *JqFilterFunc) Config() *sdk.HookConfig {
	return s.CommonGoHook.Config(&sdk.HookConfig{
		MainHandler: func(input *sdk.BindingInput) (*sdk.BindingOutput, error) {
			return s.HandleEverything(input)
		},

		Schedule: []sdk.ScheduleConfig{
			{
				Crontab:              "*/10 * * * * *",
				AllowFailure:         true,
				IncludeSnapshotsFrom: []string{"name1", "name2"},
				Queue:                fmt.Sprintf("/modules/%s/handle_node_templates", moduleName),
				Group:                "",
				Handler: func(input *sdk.BindingInput) (*sdk.BindingOutput, error) {
					return s.HandleNgs(input)
				},
			},
		},

		Kubernetes: []sdk.KubernetesConfig{
			{
				Name:                         "ngs",
				ApiVersion:                   "",
				Kind:                         "",
				NameSelector:                 nil,
				NamespaceSelector:            nil,
				LabelSelector:                nil,
				FieldSelector:                nil,
				JqFilter:                     "",
				IncludeSnapshotsFrom:         nil,
				Queue:                        "",
				Group:                        "",
				ExecuteHookOnEvents:          nil,
				ExecuteHookOnSynchronization: false,
				WaitForSynchronization:       false,
				KeepFullObjectsInMemory:      false,
				AllowFailure:                 false,
				Handler: func(input *sdk.BindingInput) (*sdk.BindingOutput, error) {
					return nil, nil
				},
				FilterFunc: nil,
			},
		},

		OnStartup: &sdk.OrderedConfig{
			Order: 20,
			Handler: func(input *sdk.BindingInput) (*sdk.BindingOutput, error) {
				return nil, nil
			},
		},
		OnBeforeHelm: &sdk.OrderedConfig{
			Order: 20,
			Handler: func(input *sdk.BindingInput) (*sdk.BindingOutput, error) {
				return nil, nil
			},
		},
		OnAfterHelm: &sdk.OrderedConfig{
			Order: 20,
			Handler: func(input *sdk.BindingInput) (*sdk.BindingOutput, error) {
				return nil, nil
			},
		},
		OnAfterDeleteHelm: &sdk.OrderedConfig{
			Order: 20,
			Handler: func(input *sdk.BindingInput) (*sdk.BindingOutput, error) {
				return nil, nil
			},
		},

		OnBeforeAll: &sdk.OrderedConfig{
			Order: 20,
			Handler: func(input *sdk.BindingInput) (*sdk.BindingOutput, error) {
				return nil, nil
			},
		},
		OnAfterAll: &sdk.OrderedConfig{
			Order: 20,
			Handler: func(input *sdk.BindingInput) (*sdk.BindingOutput, error) {
				return nil, nil
			},
		},

		GroupHandlers: map[string]sdk.BindingHandler{
			"ngs": s.HandleGroupAzaza,
			"pods": func(input *sdk.BindingInput) (*sdk.BindingOutput, error) {
				return nil, nil
			},
			//	func(input *sdk.BindingInput) (out *sdk.HookOutput, err error) {
			//	// ...
			//	filterRes, err := ToTaints(input.BindingContext.Snapshots["ngs"][0].FilterResult)
			//
			//	return nil
			//},
		},
	})
}

/*
   jqFilter: |
	  {
		"name": .metadata.name,
		"nodeType": .spec.nodeType,
		"desired": {
		  "annotations": ((.spec.nodeTemplate // {}).annotations // {}),
		  "labels":      ((.spec.nodeTemplate // {}).labels // {}),
		  "taints":      (((.spec.nodeTemplate // {}).taints // []) | map(if .value == "" then del(.value) else . end ))
		}
	  }
*/

//func (s *JqFilterFunc) Run(input *sdk.HookInput) (output *sdk.HookOutput, err error) {
//	for _, bc := range input.BindingContextList {
//
//	}
//
//	return &sdk.HookOutput{}, nil
//}

func (s *JqFilterFunc) HandleNgs(input *sdk.BindingInput) (output *sdk.BindingOutput, err error) {
	return nil, nil
}

func (s *JqFilterFunc) HandleEverything(input *sdk.BindingInput) (output *sdk.BindingOutput, err error) {
	return nil, nil
}

func (s *JqFilterFunc) HandleGroupAzaza(input *sdk.BindingInput) (output *sdk.BindingOutput, err error) {
	return nil, nil
}
