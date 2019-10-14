# Hooks

A hook is an executable file that Addon-operator executes when some event occurs. It can be a script or a compiled program written in any programming language.

Addon-operator pursues an agreement stating that the information is transferred to hooks via files and results of hooks execution are also stored in files. Paths to files are passed via environment variables. The output to stdout will be written to the log, except for the case with the configuration output (run with `--config` flag). Such an agreement simplifies the work with the input data and reporting the results of the hook execution.

# Initialization of global hooks

Global hooks can read and modify values in the global values storage which is available to all modules (see [VALUES](VALUES.md)). The global hook performs actions and discovers the values required by several modules.

Global hooks are stored in the $GLOBAL_HOOKS_DIR directory. The Addon-operator recursively searches all executable files in it and runs them with the `--config` flag. Each hook prints its events binding configuration in the JSON format to stdout. If the execution fails, the Addon-operator terminates with the code of 1.

Bindings from [shell-operator](https://github.com/flant/shell-operator) are available for global hooks: [onStartup](#onstartup), [schedule](#schedule) and [onKubernetesEvent](#onkubernetesevent). The bindings to the events of the modules discovery process are also available: [beforeAll](#beforeall) and [afterAll](#afterall) (see [modules discovery](LIFECYCLE.md#modules-discovery)).

# Initialization of module hooks

Module hooks can get values from the global values storage. Also, they can read and modify values in the module values storage. For details on values storage, see [VALUES](VALUES.md). Hooks are initialized for all enabled modules within the process of [modules discovery](LIFECYCLE.md#modules-discovery).

Module hooks are executable files stored in the `hooks` subdirectory of module. During the modules discovery process Addon-operator searches for executable files in this directory and all found files are executed with `--config` flag. Each hook prints its event binding configuration in JSON format to stdout. The module discovery process restarts if an error occurs.

Bindings from [shell-operator](https://github.com/flant/shell-operator) are available for module hooks: [schedule](#schedule) and [onKubernetesEvent](#onkubernetesevent). The bindings of the module lifecycle are also available: `onStartup`, `beforeHelm`, `afterHelm`, `afterDeleteHelm` — see [module lifecycle](LIFECYCLE.md#module-lifecycle).

# Bindings

## Overview

| Binding  | Global? | Module? | Info |
| ------------- | ------------- | --- | --- |
| [onStartup](#onstartup)↗  | ✓ | – | On Addon-operator startup |
| [onStartup](#onstartup)↗  | – |  ✓ | On first module run |
| [beforeAll](#beforeall)↗ | ✓ | – | Before run all modules |
| [afterAll](#afterall)↗ | ✓ | – | After run all modules |
| [beforeHelm](#beforehelm)↗ | – | ✓ | Before run helm install |
| [afterHelm](#afterhelm)↗ | – | ✓ | After helm install |
| [afterDeleteHelm](#afterdeletehelm)↗ | – | ✓ | After run helm delete |
| [schedule](#schedule)↗ | ✓ | ✓ | Run on schedule |
| [onKubernetesEvent](#onkubernetesevent)↗ | ✓ | ✓ | Run on event from Kubernetes |

## onStartup

- Global hook execution on Addon-operator start-up.
- Module hook execution on the first run of enabled module.

Syntax:

```
{
  "onStartup": ORDER
}
```

Parameters:
- `ORDER` — the execution order (when added to the queue, the hooks will be sorted in the specified order, and then alphabetically). The value should be an integer.

## beforeAll

The execution of global hooks before modules discovery.

Syntax:

```
{
  "beforeAll": ORDER
}
```

Parameters:

- `ORDER` — the execution order (when added to the queue, the hooks will be sorted in the specified order, and then alphabetically). The value should be an integer.

## afterAll

The execution of global hooks after running and removing modules.

Syntax:

```
{
  "afterAll": ORDER
}
```

Parameters:

- `ORDER` — the execution order (when added to the queue, the hooks will be sorted in the specified order, and then alphabetically). The value should be an integer.

## beforeHelm

The execution of a module hook before the helm chart installation (see [module lifecycle](LIFECYCLE.md#module-lifecycle)).

```
{
  "beforeHelm": ORDER
}
```

Parameters:
- `ORDER` — the execution order (when added to the queue, the hooks will be sorted in the specified order, and then alphabetically). The value should be an integer.

## afterHelm

The execution of a module hook after the helm chart installation (see [module lifecycle](LIFECYCLE.md#module-lifecycle)).

```
{
  "afterHelm": ORDER
}
```

Parameters:
- `ORDER` — the execution order (when added to the queue, the hooks will be sorted in the specified order, and then alphabetically). The value should be an integer.

## afterDeleteHelm

The execution of a module hook after the helm chart deletion (see [module lifecycle](LIFECYCLE.md#module-lifecycle)).

```
{
  "afterDeleteHelm": ORDER
}
```

Parameters:
- `ORDER` — the execution order (when added to the queue, the hooks will be sorted in the specified order, and then alphabetically). The value should be an integer.


## schedule

[schedule binding](https://github.com/flant/shell-operator/blob/v1.0.0-beta.5/HOOKS.md#schedule).

## onKubernetesEvent

[onKubernetesEvent binding](https://github.com/flant/shell-operator/blob/v1.0.0-beta.5/HOOKS.md#onKubernetesEvent)

> Note: Addon-operator requires a ServiceAccount with the appropriate [RBAC](https://kubernetes.io/docs/reference/access-authn-authz/rbac/) permissions. See `addon-operator-rbac.yaml` files in [examples](/examples).

# Execution on event

When an event associated with a hook is triggered, Addon-operator executes the hook without arguments and passes the global or module values from the values storage via temporary files. In response, a hook could return JSON patches to modify values. The detailed description of the values storage is available at [VALUES](VALUES.md).

## Binding context

Binding context is information about the event which caused the hook execution.

The $BINDING_CONTEXT_PATH environment variable contains the path to a file with JSON array of structures with the following fields:

- `binding` is a string from the `name` parameter for `schecdule` or `onKubernetesEvent` or a binding type if parameter is not set and for other hooks. For example, binding context for `beforeAll` hook:

```json
[{"binding":"beforeAll"}]
```

There are some extra fields for `onKubernetesEvent` hooks:

- `resourceEvent` — the event type is identical to the values in the `event` parameter: "add", "update" or "delete".
- `resourceNamespace`, `resourceKind`, `resourceName` — the information about the Kubernetes object associated with an event.

For example, if you have the following binding configuration of a hook:

```json
{
  "schedule": [
  {
    "name": "incremental",
    "crontab": "0 2 */3 * * *",
    "allowFailure": true
  }]
}
```

... then at 12:02 it will be executed with the following binding context:

```json
[{ "binding": "incremental"}]
```

