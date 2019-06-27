#!/bin/sh

set -e

shellOpVer=$(go list -m all | grep shell-operator | cut -d' ' -f 2-)
addonOpVer=$1

CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X 'github.com/flant/shell-operator/pkg/app.Version=$shellOpVer' -X 'github.com/flant/addon-operator/pkg/app.Version=$addonOpVer'  " -o addon-operator ./cmd/addon-operator
