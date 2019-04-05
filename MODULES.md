# Modules


Module is a wrapper layer for helm. This layer solve some issues critical to use helm chart to deploy cluster components or *addons*:

- helm has only simple feature discovery of Kubernetes clusters. `onStartup` and `beforeHelm` hooks can be used to discover Kubernetes features and customize values for helm chart.
- helm release can get stuck in case of first fail. Addon-operator check this situation and delete single failed release before helm install.
- helm has no ability to detect another releases to deploy components dependently. `enabled` script has access to previously enabled modules to detect if parent is enabled.

Also modules provides:

- continuous discovery: values for component's chart can be changed and the component will be upgraded.
- dynamic enable: modules can be enabled or disabled dynamically on changes in Kubernetes cluster.

## Structure of a module

Modules are directories under WORKING_DIR/modules. The typical module structure is:

```

```

Module consists of:

- 