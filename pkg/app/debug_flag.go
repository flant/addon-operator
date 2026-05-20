package app

import "github.com/spf13/cobra"

// DefineDebugUnixSocketFlag registers the --debug-unix-socket flag on cmd,
// binding it to the package-level DebugUnixSocket variable. Debug
// sub-commands (queue, hook, config, etc.) use this to locate the operator's
// debug socket without depending on shell-operator's app globals.
func DefineDebugUnixSocketFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&DebugUnixSocket, "debug-unix-socket", DebugUnixSocket, "A path to a unix socket for a debug endpoint.")
	_ = cmd.Flags().MarkHidden("debug-unix-socket")
}
