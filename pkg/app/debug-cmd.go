package app

import (
	"fmt"

	"gopkg.in/alecthomas/kingpin.v2"

	sh_app "github.com/flant/shell-operator/pkg/app"
	sh_debug "github.com/flant/shell-operator/pkg/debug"
)

var outputFormat = "text"

func DefineDebugCommands(kpApp *kingpin.Application) {
	globalCmd := sh_app.CommandWithDefaultUsageTemplate(kpApp, "global", "manage global values")
	globalCmd.Flag("output", "Output format: json|yaml.").Short('o').
		Default("yaml").
		EnumVar(&outputFormat, "json", "yaml", "text")
	sh_app.DefineDebugUnixSocketFlag(globalCmd)
	// -o json|yaml|text

	globalCmd.Command("list", "List global hooks.").
		Action(func(_ *kingpin.ParseContext) error {
			dump, err := globalRequest(sh_debug.DefaultClient()).List(outputFormat)
			if err != nil {
				return err
			}
			fmt.Println(string(dump))
			return nil
		})

	globalCmd.Command("values", "Dump current global values.").
		Action(func(_ *kingpin.ParseContext) error {
			dump, err := globalRequest(sh_debug.DefaultClient()).Values(outputFormat)
			if err != nil {
				return err
			}
			fmt.Println(string(dump))
			return nil
		})

	globalCmd.Command("config", "Dump global config values.").
		Action(func(_ *kingpin.ParseContext) error {
			dump, err := globalRequest(sh_debug.DefaultClient()).Config(outputFormat)
			if err != nil {
				return err
			}
			fmt.Println(string(dump))
			return nil
		})

	globalCmd.Command("patches", "Dump global value patches.").
		Action(func(_ *kingpin.ParseContext) error {
			dump, err := globalRequest(sh_debug.DefaultClient()).Patches()
			if err != nil {
				return err
			}
			fmt.Println(string(dump))
			return nil
		})

	globalCmd.Command("snapshots", "Dump snapshots for all global hooks.").
		Action(func(_ *kingpin.ParseContext) error {
			out, err := globalRequest(sh_debug.DefaultClient()).Snapshots(outputFormat)
			if err != nil {
				return err
			}
			fmt.Println(string(out))
			return nil
		})

	moduleCmd := sh_app.CommandWithDefaultUsageTemplate(kpApp, "module", "List modules and dump their values")
	moduleCmd.Flag("output", "Output format: json|yaml.").Short('o').
		Default("yaml").
		EnumVar(&outputFormat, "json", "yaml", "text")
	sh_app.DefineDebugUnixSocketFlag(moduleCmd)

	moduleCmd.Command("list", "List available modules and their enabled status.").
		Action(func(_ *kingpin.ParseContext) error {
			modules, err := moduleRequest(sh_debug.DefaultClient()).List(outputFormat)
			if err != nil {
				return err
			}
			fmt.Println(string(modules))
			return nil
		})

	var (
		moduleName string
		showGlobal bool
	)

	moduleValuesCmd := moduleCmd.Command("values", "Dump module values by name.").
		Action(func(_ *kingpin.ParseContext) error {
			dump, err := moduleRequest(sh_debug.DefaultClient()).Name(moduleName).Values(outputFormat, showGlobal)
			if err != nil {
				return err
			}
			fmt.Println(string(dump))
			return nil
		})
	moduleValuesCmd.Arg("module_name", "").Required().StringVar(&moduleName)
	moduleValuesCmd.Flag("global", "Also show global values").Short('g').BoolVar(&showGlobal)

	var debug bool
	moduleRenderCmd := moduleCmd.Command("render", "Render module manifests.").
		Action(func(_ *kingpin.ParseContext) error {
			dump, err := moduleRequest(sh_debug.DefaultClient()).Name(moduleName).Render(debug)
			if err != nil {
				return err
			}
			fmt.Println(string(dump))
			return nil
		})
	moduleRenderCmd.Arg("module_name", "").Required().StringVar(&moduleName)
	moduleRenderCmd.Flag("debug", "enable debug mode").Default("false").BoolVar(&debug)

	moduleConfigCmd := moduleCmd.Command("config", "Dump module config values by name.").
		Action(func(_ *kingpin.ParseContext) error {
			dump, err := moduleRequest(sh_debug.DefaultClient()).Name(moduleName).Config(outputFormat, showGlobal)
			if err != nil {
				return err
			}
			fmt.Println(string(dump))
			return nil
		})
	moduleConfigCmd.Arg("module_name", "").Required().StringVar(&moduleName)
	moduleConfigCmd.Flag("global", "Also show global config").Short('g').BoolVar(&showGlobal)

	modulePatchesCmd := moduleCmd.Command("patches", "Dump module value patches by name.").
		Action(func(_ *kingpin.ParseContext) error {
			dump, err := moduleRequest(sh_debug.DefaultClient()).Name(moduleName).Patches()
			if err != nil {
				return err
			}
			fmt.Println(string(dump))
			return nil
		})
	modulePatchesCmd.Arg("module_name", "").Required().StringVar(&moduleName)

	moduleResourceMonitorCmd := moduleCmd.Command("resource-monitor", "Dump resource monitors.").
		Action(func(_ *kingpin.ParseContext) error {
			out, err := moduleRequest(sh_debug.DefaultClient()).Name(moduleName).ResourceMonitor(outputFormat)
			if err != nil {
				return err
			}
			fmt.Println(string(out))
			return nil
		})
	moduleResourceMonitorCmd.Arg("module_name", "").StringVar(&moduleName)

	moduleSnapshotsCmd := moduleCmd.Command("snapshots", "Dump snapshots for all hooks.").
		Action(func(_ *kingpin.ParseContext) error {
			out, err := moduleRequest(sh_debug.DefaultClient()).Name(moduleName).Snapshots(outputFormat)
			if err != nil {
				return err
			}
			fmt.Println(string(out))
			return nil
		})
	moduleSnapshotsCmd.Arg("module_name", "").Required().StringVar(&moduleName)
}

type cliGlobalSectionRequest struct {
	client *sh_debug.Client
}

func globalRequest(client *sh_debug.Client) *cliGlobalSectionRequest {
	return &cliGlobalSectionRequest{client: client}
}

func (gr *cliGlobalSectionRequest) List(format string) ([]byte, error) {
	url := fmt.Sprintf("http://unix/global/list.%s", format)
	return gr.client.Get(url)
}

func (gr *cliGlobalSectionRequest) Values(format string) ([]byte, error) {
	url := fmt.Sprintf("http://unix/global/values.%s", format)
	return gr.client.Get(url)
}

func (gr *cliGlobalSectionRequest) Config(format string) ([]byte, error) {
	url := fmt.Sprintf("http://unix/global/config.%s", format)
	return gr.client.Get(url)
}

func (gr *cliGlobalSectionRequest) Patches() ([]byte, error) {
	return gr.client.Get("http://unix/global/patches.json")
}

func (gr *cliGlobalSectionRequest) Snapshots(format string) ([]byte, error) {
	url := fmt.Sprintf("http://unix/global/snapshots.%s", format)
	return gr.client.Get(url)
}

type cliModuleSectionRequest struct {
	client *sh_debug.Client
	name   string
}

func moduleRequest(client *sh_debug.Client) *cliModuleSectionRequest {
	return &cliModuleSectionRequest{client: client}
}

func (mr *cliModuleSectionRequest) List(format string) ([]byte, error) {
	url := fmt.Sprintf("http://unix/module/list.%s", format)
	return mr.client.Get(url)
}

func (mr *cliModuleSectionRequest) ResourceMonitor(format string) ([]byte, error) {
	url := fmt.Sprintf("http://unix/module/resource-monitor.%s", format)
	return mr.client.Get(url)
}

func (mr *cliModuleSectionRequest) Name(name string) *cliModuleSectionRequest {
	mr.name = name
	return mr
}

func (mr *cliModuleSectionRequest) Values(format string, withGlobal bool) ([]byte, error) {
	url := fmt.Sprintf("http://unix/module/%s/values.%s?global=%t", mr.name, format, withGlobal)
	return mr.client.Get(url)
}

func (mr *cliModuleSectionRequest) Render(debug bool) ([]byte, error) {
	url := fmt.Sprintf("http://unix/module/%s/render?debug=%t", mr.name, debug)
	return mr.client.Get(url)
}

func (mr *cliModuleSectionRequest) Patches() ([]byte, error) {
	url := fmt.Sprintf("http://unix/module/%s/patches.json", mr.name)
	return mr.client.Get(url)
}

func (mr *cliModuleSectionRequest) Config(format string, withGlobal bool) ([]byte, error) {
	url := fmt.Sprintf("http://unix/module/%s/config.%s?global=%t", mr.name, format, withGlobal)
	return mr.client.Get(url)
}

func (mr *cliModuleSectionRequest) Snapshots(format string) ([]byte, error) {
	url := fmt.Sprintf("http://unix/module/%s/snapshots.%s", mr.name, format)
	return mr.client.Get(url)
}
