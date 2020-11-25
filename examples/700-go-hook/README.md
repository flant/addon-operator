## Example module with Go hook

This example contains a go_hooks.go file to illustrate possibilities of Go hooks.

In order to run addon-operator with Go hooks you need to compile hooks in addon-operator binary. register_go_hooks.go.tpl file contains an example of code needed to compile go hooks into addon-operator binary.

### run

1. clone addon-operator repo
2. Add paths with go hooks in register_go_hooks.go
3. Add register_go_hooks.go to cmd/addon-operator
4. Build image.

