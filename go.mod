module github.com/flant/addon-operator

go 1.15

require (
	github.com/Masterminds/sprig/v3 v3.2.2 // indirect
	github.com/Masterminds/squirrel v1.5.0 // indirect
	github.com/davecgh/go-spew v1.1.1
	github.com/deislabs/oras v0.11.1 // indirect
	github.com/evanphx/json-patch v4.9.0+incompatible
	github.com/flant/kube-client v0.0.6
	github.com/flant/shell-operator v1.0.2
	github.com/go-chi/chi v4.0.3+incompatible
	github.com/go-openapi/spec v0.19.5
	github.com/go-openapi/strfmt v0.19.5
	github.com/go-openapi/swag v0.19.5
	github.com/go-openapi/validate v0.19.8
	github.com/hashicorp/go-multierror v1.0.0
	github.com/jmoiron/sqlx v1.3.1 // indirect
	github.com/kennygrant/sanitize v1.2.4
	github.com/lib/pq v1.10.0 // indirect
	github.com/mitchellh/copystructure v1.1.1 // indirect
	github.com/onsi/gomega v1.9.0
	github.com/peterbourgon/mergemap v0.0.0-20130613134717-e21c03b7a721
	github.com/prometheus/client_golang v1.7.1
	github.com/segmentio/go-camelcase v0.0.0-20160726192923-7085f1e3c734
	github.com/sirupsen/logrus v1.8.1
	github.com/stretchr/testify v1.7.0
	github.com/tidwall/gjson v1.7.5
	github.com/tidwall/sjson v1.1.6
	golang.org/x/text v0.3.5 // indirect
	gopkg.in/alecthomas/kingpin.v2 v2.2.6
	gopkg.in/satori/go.uuid.v1 v1.2.0
	gopkg.in/yaml.v3 v3.0.0-20200615113413-eeeca48fe776
	helm.sh/helm/v3 v3.4.2
	k8s.io/api v0.19.11
	k8s.io/apimachinery v0.19.11
	k8s.io/client-go v0.19.11
	k8s.io/utils v0.0.0-20201110183641-67b214c5f920
	rsc.io/letsencrypt v0.0.3 // indirect
	sigs.k8s.io/yaml v1.2.0
)

replace github.com/go-openapi/validate => github.com/flant/go-openapi-validate v0.19.4-0.20200313141509-0c0fba4d39e1 // branch: fix_in_body_0_19_7
