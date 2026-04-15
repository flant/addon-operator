package app

import (
	"fmt"

	"github.com/spf13/cobra"

	shapp "github.com/flant/shell-operator/pkg/app"
	sh_debug "github.com/flant/shell-operator/pkg/debug"
)

var outputFormat = "text"

func DefineDebugCommands(rootCmd *cobra.Command) {
	globalCmd := &cobra.Command{Use: "global", Short: "Manage global values."}
	globalCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "yaml", "Output format: json|yaml|text.")
	shapp.DefineDebugUnixSocketFlag(globalCmd)
	rootCmd.AddCommand(globalCmd)

	globalCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List global hooks.",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := sh_debug.DefaultClient()
			if err != nil {
				return err
			}
			dump, err := globalRequest(client).List(outputFormat)
			if err != nil {
				return err
			}
			fmt.Println(string(dump))
			return nil
		},
	})

	globalCmd.AddCommand(&cobra.Command{
		Use:   "values",
		Short: "Dump current global values.",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := sh_debug.DefaultClient()
			if err != nil {
				return err
			}
			dump, err := globalRequest(client).Values(outputFormat)
			if err != nil {
				return err
			}
			fmt.Println(string(dump))
			return nil
		},
	})

	globalCmd.AddCommand(&cobra.Command{
		Use:   "config",
		Short: "Dump global config values.",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := sh_debug.DefaultClient()
			if err != nil {
				return err
			}
			dump, err := globalRequest(client).Config(outputFormat)
			if err != nil {
				return err
			}
			fmt.Println(string(dump))
			return nil
		},
	})

	globalCmd.AddCommand(&cobra.Command{
		Use:   "patches",
		Short: "Dump global value patches.",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := sh_debug.DefaultClient()
			if err != nil {
				return err
			}
			dump, err := globalRequest(client).Patches()
			if err != nil {
				return err
			}
			fmt.Println(string(dump))
			return nil
		},
	})

	globalCmd.AddCommand(&cobra.Command{
		Use:   "snapshots",
		Short: "Dump snapshots for all global hooks.",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := sh_debug.DefaultClient()
			if err != nil {
				return err
			}
			out, err := globalRequest(client).Snapshots(outputFormat)
			if err != nil {
				return err
			}
			fmt.Println(string(out))
			return nil
		},
	})

	moduleCmd := &cobra.Command{Use: "module", Short: "List modules and dump their values."}
	moduleCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "yaml", "Output format: json|yaml|text.")
	shapp.DefineDebugUnixSocketFlag(moduleCmd)
	rootCmd.AddCommand(moduleCmd)

	moduleCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List available modules and their enabled status.",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := sh_debug.DefaultClient()
			if err != nil {
				return err
			}
			modules, err := moduleRequest(client).List(outputFormat)
			if err != nil {
				return err
			}
			fmt.Println(string(modules))
			return nil
		},
	})

	var (
		moduleName string
		showGlobal bool
	)

	moduleValuesCmd := &cobra.Command{
		Use:   "values <module_name>",
		Short: "Dump module values by name.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			moduleName = args[0]
			client, err := sh_debug.DefaultClient()
			if err != nil {
				return err
			}
			dump, err := moduleRequest(client).Name(moduleName).Values(outputFormat, showGlobal)
			if err != nil {
				return err
			}
			fmt.Println(string(dump))
			return nil
		},
	}
	moduleValuesCmd.Flags().BoolVarP(&showGlobal, "global", "g", false, "Also show global values.")
	moduleCmd.AddCommand(moduleValuesCmd)

	var (
		debug bool
		diff  bool
	)

	moduleRenderCmd := &cobra.Command{
		Use:   "render <module_name>",
		Short: "Render module manifests.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			moduleName = args[0]
			client, err := sh_debug.DefaultClient()
			if err != nil {
				return err
			}
			dump, err := moduleRequest(client).Name(moduleName).Render(debug, diff)
			if err != nil {
				return err
			}
			fmt.Println(string(dump))
			return nil
		},
	}
	moduleRenderCmd.Flags().BoolVar(&debug, "debug", false, "Enable debug mode.")
	moduleRenderCmd.Flags().BoolVar(&diff, "diff", false, "Enable diff mode (experimental, prints module resources drifted from the chart).")
	moduleCmd.AddCommand(moduleRenderCmd)

	moduleConfigCmd := &cobra.Command{
		Use:   "config <module_name>",
		Short: "Dump module config values by name.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			moduleName = args[0]
			client, err := sh_debug.DefaultClient()
			if err != nil {
				return err
			}
			dump, err := moduleRequest(client).Name(moduleName).Config(outputFormat, showGlobal)
			if err != nil {
				return err
			}
			fmt.Println(string(dump))
			return nil
		},
	}
	moduleConfigCmd.Flags().BoolVarP(&showGlobal, "global", "g", false, "Also show global config.")
	moduleCmd.AddCommand(moduleConfigCmd)

	modulePatchesCmd := &cobra.Command{
		Use:   "patches <module_name>",
		Short: "Dump module value patches by name.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			moduleName = args[0]
			client, err := sh_debug.DefaultClient()
			if err != nil {
				return err
			}
			dump, err := moduleRequest(client).Name(moduleName).Patches()
			if err != nil {
				return err
			}
			fmt.Println(string(dump))
			return nil
		},
	}
	moduleCmd.AddCommand(modulePatchesCmd)

	moduleResourceMonitorCmd := &cobra.Command{
		Use:   "resource-monitor [module_name]",
		Short: "Dump resource monitors.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				moduleName = args[0]
			}
			client, err := sh_debug.DefaultClient()
			if err != nil {
				return err
			}
			out, err := moduleRequest(client).Name(moduleName).ResourceMonitor(outputFormat)
			if err != nil {
				return err
			}
			fmt.Println(string(out))
			return nil
		},
	}
	moduleCmd.AddCommand(moduleResourceMonitorCmd)

	moduleSnapshotsCmd := &cobra.Command{
		Use:   "snapshots <module_name>",
		Short: "Dump snapshots for all hooks.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			moduleName = args[0]
			client, err := sh_debug.DefaultClient()
			if err != nil {
				return err
			}
			out, err := moduleRequest(client).Name(moduleName).Snapshots(outputFormat)
			if err != nil {
				return err
			}
			fmt.Println(string(out))
			return nil
		},
	}
	moduleCmd.AddCommand(moduleSnapshotsCmd)
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

func (mr *cliModuleSectionRequest) Render(debug, diff bool) ([]byte, error) {
	url := fmt.Sprintf("http://unix/module/%s/render?debug=%t&diff=%t", mr.name, debug, diff)
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
