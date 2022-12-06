module github.com/flant/addon-operator

go 1.16

require (
	github.com/davecgh/go-spew v1.1.1
	github.com/evanphx/json-patch v5.6.0+incompatible
	github.com/flant/kube-client v0.0.6
	github.com/flant/shell-operator v1.0.12
	github.com/go-chi/chi/v5 v5.0.7
	github.com/go-openapi/loads v0.19.5
	github.com/go-openapi/spec v0.19.8
	github.com/go-openapi/strfmt v0.19.5
	github.com/go-openapi/swag v0.19.14
	github.com/go-openapi/validate v0.19.12
	github.com/hashicorp/go-multierror v1.1.1
	github.com/kennygrant/sanitize v1.2.4
	github.com/onsi/gomega v1.20.1
	github.com/peterbourgon/mergemap v0.0.0-20130613134717-e21c03b7a721
	github.com/prometheus/client_golang v1.12.1
	github.com/segmentio/go-camelcase v0.0.0-20160726192923-7085f1e3c734
	github.com/sirupsen/logrus v1.8.1
	github.com/stretchr/testify v1.8.0
	github.com/tidwall/gjson v1.12.1
	github.com/tidwall/sjson v1.2.3
	go.uber.org/goleak v1.1.12
	gopkg.in/alecthomas/kingpin.v2 v2.2.6
	gopkg.in/satori/go.uuid.v1 v1.2.0
	gopkg.in/yaml.v3 v3.0.1
	helm.sh/helm/v3 v3.6.3
	k8s.io/api v0.21.14
	k8s.io/apimachinery v0.21.14
	k8s.io/client-go v0.21.14
	k8s.io/utils v0.0.0-20220728103510-ee6ede2d64ed
	rsc.io/letsencrypt v0.0.3 // indirect
	sigs.k8s.io/yaml v1.3.0
)

// Remove 'in body' from errors, fix for Go 1.16 (https://github.com/go-openapi/validate/pull/138).
replace github.com/go-openapi/validate => github.com/flant/go-openapi-validate v0.19.12-flant.0
