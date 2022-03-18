# Hooks

A hook is an executable file that the Addon-operator executes when some event occurs. It can be a script or a compiled program written in any programming language.

The Addon-operator pursues an agreement stating that the information is transferred to hooks via files and results of hook's execution are also stored in files. Paths to files are passed via environment variables. The output to stdout will be written to the log, except for the case with the configuration output (run with `--config` flag). Such an agreement simplifies the work with the input data and reporting the results of the hook execution.

## Global hooks

Global hooks are stored in the `$GLOBAL_HOOKS_DIR/hooks` directory. The Addon-operator recursively searches all executable files in it and runs them with the `--config` flag. Each hook prints its events binding configuration in JSON or YAML format to stdout. If the execution fails, the Addon-operator terminates with the code of 1.

Bindings from [shell-operator](https://github.com/flant/shell-operator) are available for global hooks: [onStartup](#onstartup), [schedule](#schedule) and [kubernetes](#kubernetes). The bindings to the events of the modules discovery process are also available: [beforeAll](#beforeall) and [afterAll](#afterall) (see [modules discovery](LIFECYCLE.md#modules-discovery)).

During execution, a global hook receives global values. These values can be modified by the hook to share data with global hooks, module hooks, and Helm templates. If the hook changes global values, the 'global values changed' event is generated and all modules are reloaded. For details on values storage, see [VALUES](VALUES.md). See also [an overview](LIFECYCLE.md#reload-all-modules) and [a detailed description](LIFECYCLE-STEPS.md#reload-all-modules) of 'Reload all modules' process.

## Module hook

Module hooks are executable files stored in the `hooks` subdirectory of the module. During the ['modules discovery'](LIFECYCLE.md#modules-discovery) process, if module appears to be enabled, the Addon-operator searches for executable files in `hooks` directory and executes them with `--config` flag. Each hook prints its event binding configuration in JSON or YAML format to stdout. The module discovery process restarts if an error occurs.

Bindings from [shell-operator](https://github.com/flant/shell-operator) are available for module hooks: [schedule](#schedule) and [kubernetes](#kubernetes). The bindings of the module lifecycle are also available: `onStartup`, `beforeHelm`, `afterHelm`, `afterDeleteHelm` — see [module lifecycle](LIFECYCLE.md#module-lifecycle).

During execution, a module hook receives global values and module values. Module values can be modified by the hook to share data with other hooks of the same module. If the hook changes module values, the 'module values changed' event is generated and then the module is reloaded. For details on values storage, see [VALUES](VALUES.md). See also a [module lifecycle](LIFECYCLE.md#module-lifecycle) and a [module run](LIFECYCLE-STEPS.md#module-run) detailed description.

## Bindings

### Overview

| Binding  | Global? | Module? | Info |
| ------------- | ------------- | --- | --- |
| [onStartup](#onstartup)↗  | ✓ | – | On Addon-operator startup |
| [onStartup](#onstartup)↗  | – |  ✓ | On first module's execution |
| [beforeAll](#beforeall)↗ | ✓ | – | Before any modules are executed|
| [afterAll](#afterall)↗ | ✓ | – | After all modules are executed|
| [beforeHelm](#beforehelm)↗ | – | ✓ | Before executing `helm install` |
| [afterHelm](#afterhelm)↗ | – | ✓ | After executing `helm install` |
| [afterDeleteHelm](#afterdeletehelm)↗ | – | ✓ | After executing `helm delete` |
| [schedule](#schedule)↗ | ✓ | ✓ | Run on schedule |
| [kubernetes](#kubernetes)↗ | ✓ | ✓ | Run on event from Kubernetes |

### onStartup

Example:

```yaml
configVersion: v1
onStartup: ORDER
```

Parameters:

- `ORDER` — an integer value that specifies an execution order. When added to the "main" queue, the hooks will be sorted by this value and then alphabetically by file name.

### beforeAll

Example:

```yaml
configVersion: v1
beforeAll: ORDER
```

Parameters:

- `ORDER` — an integer value that specifies an execution order. When added to the "main" queue, the hooks will be sorted by this value and then alphabetically by file name.

### afterAll

Example:

```yaml
configVersion: v1
afterAll: ORDER
```

Parameters:

- `ORDER` — an integer value that specifies an execution order. When added to the "main" queue, the hooks will be sorted by this value and then alphabetically by file name.

### beforeHelm

Example:

```yaml
configVersion: v1
beforeHelm: ORDER
```

Parameters:

- `ORDER` — an integer value that specifies an execution order. When added to the "main" queue, the hooks will be sorted by this value and then alphabetically by file name.

### afterHelm

Example:

```yaml
configVersion: v1
afterHelm: ORDER
```

Parameters:

- `ORDER` — an integer value that specifies an execution order. When added to the "main" queue, the hooks will be sorted by this value and then alphabetically by file name.

### afterDeleteHelm

Example:

```yaml
configVersion: v1
afterDeleteHelm: ORDER
```

Parameters:

- `ORDER` — an integer value that specifies an execution order. When added to the "main" queue, the hooks will be sorted by this value and then alphabetically by file name.


### schedule

See the [schedule binding](https://github.com/flant/shell-operator/blob/master/HOOKS.md#schedule) from the Shell-operator.

### kubernetes

See the [kubernetes binding](https://github.com/flant/shell-operator/blob/master/HOOKS.md#kubernetes) from the Shell-operator.

> Note: Addon-operator requires a ServiceAccount with the appropriate [RBAC](https://kubernetes.io/docs/reference/access-authn-authz/rbac/) permissions. See `addon-operator-rbac.yaml` files in [examples](/examples).

## Execution on event

When an event associated with a hook is triggered, Addon-operator executes the hook without arguments and passes the global or module values from the storage of the values via temporary files. In response, a hook could return JSON patches to modify values. The detailed description of the storage of the values is available in [VALUES](VALUES.md) document.

### Binding context

The binding context is a piece of information about the event which caused the hook execution.

The `$BINDING_CONTEXT_PATH` environment variable contains the path to a file with a JSON array of structures with the following fields:

- `binding` is a string from the `name` parameter for `schedule` or `kubernetes` bindings. Its value is a *binding type* if the parameter is not set and for other hooks. For example, the binding context for `beforeAll` binding type:

```json
[{"binding":"beforeAll"}]
```

The binding context for `schedule` and `kubernetes` hooks contains additional fields, described in Shell-operator [documentation](https://github.com/flant/shell-operator/blob/master/HOOKS.md#binding-context).

`beforeAll` and `afterAll` global hooks and `beforeHelm`, `afterHelm`, and `afterDeleteHelm` module hooks are executed with the binding context that includes a `snapshots` field, which contains all Kubernetes objects that match hook's `kubernetes` bindings configurations.

For example, a global hook with `kubernetes` and `beforeAll` bindings may have this configuration:

```yaml
configVersion: v1
beforeAll: 10
kubernetes:
- name: monitor-pods
  apiVersion: v1
  kind: Pod
  jqFilter: ".metadata.labels"
```

This hook will be executed *before* updating the Helm release with this binding context:

```yaml
[{"binding": "beforeAll",
"snapshots": {
  "monitor-pods": [
    {
      "object": {
        "kind": "Pod",
        "apiVersion": "v1",
        "metadata": {
          "name":"pod-1r62e3",
          "namespace":"default", ...},
        ...
      },
      "filterResult": {
        "label1": "label value",
        ...
      },
    },
    ...
    more pods
    ...
  ]
}
}]
```

### Synchronization for global hooks

[Synchronization](https://github.com/flant/shell-operator/blob/main/HOOKS.md#synchronization-binding-context) is a first run of global hooks with "kubernetes" bindings. As with the shell-operator, it occurs right after the success of the global "onStartup" hooks, but the following behaviour is slightly different. By default, the addon-operator waits for hooks with `executeHookOnSynchronization: true` to finish before proceeding to "beforeAll" hooks. You can skip this waiting for bindings in parallel queues by setting `waitForSynchronization: false`.

For example, a global hook with `kubernetes` and `beforeAll` bindings may have this configuration:

```yaml
configVersion: v1
beforeAll: 10
kubernetes:
- name: monitor-pods
  apiVersion: v1
  kind: Pod
  jqFilter: ".metadata.labels"
- name: monitor-nodes
  apiVersion: v1
  kind: Node
  jqFilter: ".metadata.labels"
  queue: config-map-handling-queue
  executeHookOnSynchronization: false
- name: monitor-cms
  apiVersion: v1
  kind: ConfigMap
  jqFilter: ".metadata.labels"
  queue: config-map-handling-queue
  waitForSynchronization: false
- name: monitor-secrets
  apiVersion: v1
  kind: Secret
  jqFilter: ".metadata.labels"
  queue: secrets-handling-queue
  executeHookOnSynchronization: false
  waitForSynchronization: false
```

This hook will be executed after "onStartup" as follows:

- Run hook with binding context for the "monitor-pods" binding in the "main" queue.
- Fill snapshot for the "monitor-nodes" binding, do not execute hook.
- Run in parallel: 1) hook with the "beforeAll" binding context in the "main" queue, 2) hook with the "monitor-cms" binding context in the "config-map-handling-queue" queue, and 3) fill snapshot for the "monitor-secrets" binding.

> Note: there is no guarantee that the "beforeAll" binding context contains snapshots with ConfigMaps and Secrets.

### Synchronization for module hooks

[Synchronization](https://github.com/flant/shell-operator/blob/main/HOOKS.md#synchronization-binding-context) is a first run of module hooks with "kubernetes" bindings after module enablement. It occurs right after the success of the module's "onStartup" hooks. By default, the addon-operator waits for hooks with `executeHookOnSynchronization: true` to finish before proceeding to "beforeHelm" hooks. You can skip this waiting for bindings in parallel queues by setting `waitForSynchronization: false`.

For example, a module hook with `kubernetes` and `beforeHelm` bindings may have this configuration:

```yaml
configVersion: v1
beforeHelm: 10
kubernetes:
- name: monitor-pods
  apiVersion: v1
  kind: Pod
  jqFilter: ".metadata.labels"
- name: monitor-nodes
  apiVersion: v1
  kind: Node
  jqFilter: ".metadata.labels"
  queue: config-map-handling-queue
  executeHookOnSynchronization: false
- name: monitor-cms
  apiVersion: v1
  kind: ConfigMap
  jqFilter: ".metadata.labels"
  queue: config-map-handling-queue
  waitForSynchronization: false
- name: monitor-secrets
  apiVersion: v1
  kind: Secret
  jqFilter: ".metadata.labels"
  queue: secrets-handling-queue
  executeHookOnSynchronization: false
  waitForSynchronization: false
```

This hook will be executed after "onStartup" as follows:

- Run hook with binding context for the "monitor-pods" binding in the "main" queue.
- Fill snapshot for the "monitor-nodes" binding, do not execute hook.
- Run in parallel: 1) hook with the "beforeHelm" binding context in the "main" queue, 2) hook with the "monitor-cms" binding context in the "config-map-handling-queue" queue, and 3) fill snapshot for the "monitor-secrets" binding.

> Note: there is no guarantee that the "beforeHelm" binding context contains snapshots with ConfigMaps and Secrets.

### Execution rate

Hook configuration has a `settings` section with parameters `executionMinPeriod` and `executionBurst`. These parameters are used to throttle hook executions and wait for more events in the queue. See section [execution rate](https://github.com/flant/shell-operator/blob/master/HOOKS.md#execution-rate) from the Shell-operator.
