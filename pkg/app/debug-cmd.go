package app

import (
	"fmt"

	"gopkg.in/alecthomas/kingpin.v2"

	sh_app "github.com/flant/shell-operator/pkg/app"
	sh_debug "github.com/flant/shell-operator/pkg/debug"
)

func DefineDebugCommands(kpApp *kingpin.Application) {
	globalCmd := sh_app.CommandWithDefaultUsageTemplate(kpApp, "global", "manage global values")

	globalListCmd := globalCmd.Command("list", "List global hooks.").
		Action(func(c *kingpin.ParseContext) error {
			dump, err := Global(sh_debug.DefaultClient()).List(sh_debug.OutputFormat)
			if err != nil {
				return err
			}
			fmt.Println(string(dump))
			return nil
		})
	// -o json|yaml and --debug-unix-socket <file>
	AddOutputJsonYamlFlag(globalListCmd)
	sh_app.DefineDebugUnixSocketFlag(globalListCmd)

	globalValuesCmd := globalCmd.Command("values", "Dump current global values.").
		Action(func(c *kingpin.ParseContext) error {
			dump, err := Global(sh_debug.DefaultClient()).Values(sh_debug.OutputFormat)
			if err != nil {
				return err
			}
			fmt.Println(string(dump))
			return nil
		})
	// -o json|yaml and --debug-unix-socket <file>
	AddOutputJsonYamlFlag(globalValuesCmd)
	sh_app.DefineDebugUnixSocketFlag(globalValuesCmd)

	globalConfigCmd := globalCmd.Command("config", "Dump global config values.").
		Action(func(c *kingpin.ParseContext) error {
			dump, err := Global(sh_debug.DefaultClient()).Config(sh_debug.OutputFormat)
			if err != nil {
				return err
			}
			fmt.Println(string(dump))
			return nil
		})
	// -o json|yaml and --debug-unix-socket <file>
	AddOutputJsonYamlFlag(globalConfigCmd)
	sh_app.DefineDebugUnixSocketFlag(globalConfigCmd)

	globalPatchesCmd := globalCmd.Command("patches", "Dump global value patches.").
		Action(func(c *kingpin.ParseContext) error {
			dump, err := Global(sh_debug.DefaultClient()).Patches()
			if err != nil {
				return err
			}
			fmt.Println(string(dump))
			return nil
		})
	// --debug-unix-socket <file>
	sh_app.DefineDebugUnixSocketFlag(globalPatchesCmd)

	globalSnapshotsCmd := globalCmd.Command("snapshots", "Dump snapshots for all global hooks.").
		Action(func(c *kingpin.ParseContext) error {
			out, err := Global(sh_debug.DefaultClient()).Snapshots(sh_debug.OutputFormat)
			if err != nil {
				return err
			}
			fmt.Println(string(out))
			return nil
		})
	// -o json|yaml and --debug-unix-socket <file>
	AddOutputJsonYamlFlag(globalSnapshotsCmd)
	sh_app.DefineDebugUnixSocketFlag(globalSnapshotsCmd)

	moduleCmd := sh_app.CommandWithDefaultUsageTemplate(kpApp, "module", "List modules and dump their values")

	moduleListCmd := moduleCmd.Command("list", "List available modules and their enabled status.").
		Action(func(c *kingpin.ParseContext) error {
			modules, err := Module(sh_debug.DefaultClient()).List(sh_debug.OutputFormat)
			if err != nil {
				return err
			}
			fmt.Println(string(modules))
			return nil
		})
	// -o json|yaml|text and --debug-unix-socket <file>
	sh_debug.AddOutputJsonYamlTextFlag(moduleListCmd)
	sh_app.DefineDebugUnixSocketFlag(moduleListCmd)

	var moduleName string
	moduleValuesCmd := moduleCmd.Command("values", "Dump module values by name.").
		Action(func(c *kingpin.ParseContext) error {
			dump, err := Module(sh_debug.DefaultClient()).Name(moduleName).Values(sh_debug.OutputFormat)
			if err != nil {
				return err
			}
			fmt.Println(string(dump))
			return nil
		})
	moduleValuesCmd.Arg("module_name", "").Required().StringVar(&moduleName)
	// -o json|yaml and --debug-unix-socket <file>
	AddOutputJsonYamlFlag(moduleValuesCmd)
	sh_app.DefineDebugUnixSocketFlag(moduleValuesCmd)

	moduleRenderCmd := moduleCmd.Command("render", "Render module manifests.").
		Action(func(c *kingpin.ParseContext) error {
			dump, err := Module(sh_debug.DefaultClient()).Name(moduleName).Render()
			if err != nil {
				return err
			}
			fmt.Println(string(dump))
			return nil
		})
	moduleRenderCmd.Arg("module_name", "").Required().StringVar(&moduleName)
	AddOutputJsonYamlFlag(moduleRenderCmd)
	sh_app.DefineDebugUnixSocketFlag(moduleRenderCmd)

	moduleConfigCmd := moduleCmd.Command("config", "Dump module config values by name.").
		Action(func(c *kingpin.ParseContext) error {
			dump, err := Module(sh_debug.DefaultClient()).Name(moduleName).Config(sh_debug.OutputFormat)
			if err != nil {
				return err
			}
			fmt.Println(string(dump))
			return nil
		})
	moduleConfigCmd.Arg("module_name", "").Required().StringVar(&moduleName)
	// -o json|yaml and --debug-unix-socket <file>
	AddOutputJsonYamlFlag(moduleConfigCmd)
	sh_app.DefineDebugUnixSocketFlag(moduleConfigCmd)

	modulePatchesCmd := moduleCmd.Command("patches", "Dump module value patches by name.").
		Action(func(c *kingpin.ParseContext) error {
			dump, err := Module(sh_debug.DefaultClient()).Name(moduleName).Patches()
			if err != nil {
				return err
			}
			fmt.Println(string(dump))
			return nil
		})
	modulePatchesCmd.Arg("module_name", "").Required().StringVar(&moduleName)
	// --debug-unix-socket <file>
	sh_app.DefineDebugUnixSocketFlag(modulePatchesCmd)

	moduleResourceMonitorCmd := moduleCmd.Command("resource-monitor", "Dump resource monitors.").
		Action(func(c *kingpin.ParseContext) error {
			out, err := Module(sh_debug.DefaultClient()).Name(moduleName).ResourceMonitor(sh_debug.OutputFormat)
			if err != nil {
				return err
			}
			fmt.Println(string(out))
			return nil
		})
	moduleResourceMonitorCmd.Arg("module_name", "").StringVar(&moduleName)
	// -o json|yaml and --debug-unix-socket <file>
	AddOutputJsonYamlFlag(moduleResourceMonitorCmd)
	sh_app.DefineDebugUnixSocketFlag(moduleResourceMonitorCmd)

	moduleSnapshotsCmd := moduleCmd.Command("snapshots", "Dump snapshots for all hooks.").
		Action(func(c *kingpin.ParseContext) error {
			out, err := Module(sh_debug.DefaultClient()).Name(moduleName).Snapshots(sh_debug.OutputFormat)
			if err != nil {
				return err
			}
			fmt.Println(string(out))
			return nil
		})
	moduleSnapshotsCmd.Arg("module_name", "").Required().StringVar(&moduleName)
	// -o json|yaml and --debug-unix-socket <file>
	AddOutputJsonYamlFlag(moduleSnapshotsCmd)
	sh_app.DefineDebugUnixSocketFlag(moduleSnapshotsCmd)
}

func AddOutputJsonYamlFlag(cmd *kingpin.CmdClause) {
	cmd.Flag("output", "Output format: json|yaml.").Short('o').
		Default("yaml").
		EnumVar(&sh_debug.OutputFormat, "json", "yaml")
}

type GlobalRequest struct {
	client *sh_debug.Client
}

func Global(client *sh_debug.Client) *GlobalRequest {
	return &GlobalRequest{client: client}
}

func (gr *GlobalRequest) List(format string) ([]byte, error) {
	url := fmt.Sprintf("http://unix/global/list.%s", format)
	return gr.client.Get(url)
}

func (gr *GlobalRequest) Values(format string) ([]byte, error) {
	url := fmt.Sprintf("http://unix/global/values.%s", format)
	return gr.client.Get(url)
}

func (gr *GlobalRequest) Config(format string) ([]byte, error) {
	url := fmt.Sprintf("http://unix/global/config.%s", format)
	return gr.client.Get(url)
}

func (gr *GlobalRequest) Patches() ([]byte, error) {
	return gr.client.Get("http://unix/global/patches.json")
}

func (gr *GlobalRequest) Snapshots(format string) ([]byte, error) {
	url := fmt.Sprintf("http://unix/global/snapshots.%s", format)
	return gr.client.Get(url)
}

type ModuleRequest struct {
	client *sh_debug.Client
	name   string
}

func Module(client *sh_debug.Client) *ModuleRequest {
	return &ModuleRequest{client: client}
}

func (mr *ModuleRequest) List(format string) ([]byte, error) {
	url := fmt.Sprintf("http://unix/module/list.%s", format)
	return mr.client.Get(url)
}

func (mr *ModuleRequest) ResourceMonitor(format string) ([]byte, error) {
	url := fmt.Sprintf("http://unix/module/resource-monitor.%s", format)
	return mr.client.Get(url)
}

func (mr *ModuleRequest) Name(name string) *ModuleRequest {
	mr.name = name
	return mr
}

func (mr *ModuleRequest) Values(format string) ([]byte, error) {
	url := fmt.Sprintf("http://unix/module/%s/values.%s", mr.name, format)
	return mr.client.Get(url)
}

func (mr *ModuleRequest) Render() ([]byte, error) {
	url := fmt.Sprintf("http://unix/module/%s/render", mr.name)
	return mr.client.Get(url)
}

func (mr *ModuleRequest) Patches() ([]byte, error) {
	url := fmt.Sprintf("http://unix/module/%s/patches.json", mr.name)
	return mr.client.Get(url)
}

func (mr *ModuleRequest) Config(format string) ([]byte, error) {
	url := fmt.Sprintf("http://unix/module/%s/config.%s", mr.name, format)
	return mr.client.Get(url)
}

func (mr *ModuleRequest) Snapshots(format string) ([]byte, error) {
	url := fmt.Sprintf("http://unix/module/%s/snapshots.%s", mr.name, format)
	return mr.client.Get(url)
}
