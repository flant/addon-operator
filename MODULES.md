# Modules

# Module scructure

A module is a directory with files. Addon-operator searches for the modules directories in `/modules` or in the path specified by $MODULES_DIR variable. The module has the same name as the corresponding directory excluding the numeric prefix.


The file structure of the module’s directory:

```
/modules/001-simple-module
├── .helmignore
├── Chart.yaml
├── enabled
├── hooks
│   └── module-hooks.sh
├── README.md
├── templates
│   └── daemon-set.yaml
└── values.yaml
```

- `hooks` — directory with hooks;
- `enabled` — script that gets the status of module (is it enabled or not). See the [modules discovery](LIFECYCLE.md#modules-discovery) process;
- `Chart.yaml`, .helmignore, templates — files for the Helm chart;
- `README.md` — module description;
- `values.yaml` – default values for chart in a [special format](VALUES.md).

# Notes on how Helm is used

## values.yaml

Addon-operator does not use values.yaml as the only source of values for the chart. It generates a new file with a combined set of parameters (also mixing values from this file (see [VALUES](VALUES.md)).

## Chart.yaml

We recommend to define the `version` field in your chart.yaml as "0.0.1" and use VCS to control versions. We also recommend to explicitly specify the `name` field even despite it is ignored: Addon-operator passes the module name to the Helm as a release name.

## Releases deduplication

A module’s execution might be triggered by an event that does not change the parameters of the module (see [modules discovery](LIFECYCLE.md#modules-discovery)). Re-running Helm will lead to an "empty" release. To avoid this, Addon-operator compares parameters checksums and starts the installation of a Helm chart only if there are some changes.

# Workarounds for Helm issues

Helm badly handles failed chart installations [PR#4871](https://github.com/helm/helm/pull/4871). A workaround has been added to Addon-operator to reduce the number of manual interventions in such situations: automatic deletion of the single failed release. In the future, in addition to this mechanism, we plan to add a few improvements to the interaction with Helm. In particular, we plan to port related algorithms (how the interaction with Helm is done) from werf — [ROADMAP](https://github.com/flant/addon-operator/issues/17).

# Next

- [addon-operator lifecycle](LIFECYCLE.md)
- [values](VALUES.md)
- [hooks](HOOKS.md)

