package app

import "github.com/spf13/cobra"

// DebugUnixSocket is the default path for the debug unix socket. It is used
// as the binding target for the --debug-unix-socket flag on debug sub-commands
// (global, module, etc.) that connect to a running addon-operator. For the
// start command, cfg.Debug.UnixSocket is preferred; see bindDebugFlags in
// app.go.
//
// The variable pre-dates the Config struct and is therefore kept around for
// backward compatibility with debug sub-commands that bind to it directly.
// The default is baked into the declaration so the global has a sensible
// value even when ApplyConfig has not been called (e.g. when a debug
// sub-command is invoked without going through the start command path).
//
// Mirrors the same DebugUnixSocket/ApplyConfig/DefineDebugUnixSocketFlag
// arrangement that shell-operator uses in its pkg/app/debug.go.
var DebugUnixSocket = DefaultDebugUnixSocket

// DefineDebugUnixSocketFlag registers the --debug-unix-socket flag on cmd,
// binding it to the package-level DebugUnixSocket variable. Debug
// sub-commands (global, module, config, etc.) use this to locate the
// operator's debug socket without depending on shell-operator's app globals.
func DefineDebugUnixSocketFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&DebugUnixSocket, "debug-unix-socket", DebugUnixSocket, "A path to a unix socket for a debug endpoint.")
	_ = cmd.Flags().MarkHidden("debug-unix-socket")
}
