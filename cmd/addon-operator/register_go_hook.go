package main

import (
	_ "github.com/flant/addon-operator/sdk"

	_ "github.com/flant/addon-operator/global-hooks"
	_ "github.com/flant/addon-operator/modules/001-test-batch-hook/hooks"
	_ "github.com/flant/addon-operator/modules/002-test-batch-hook/hooks"
)
