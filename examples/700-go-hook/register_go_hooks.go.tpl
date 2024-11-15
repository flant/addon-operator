package main

import (
	_ "github.com/flant/addon-operator/sdk"

	_ "github.com/flant/addon-operator/examples/700-go-hook/global-hooks"
	_ "github.com/flant/addon-operator/examples/700-go-hook/modules/001-module-go-hooks/hooks"
)
