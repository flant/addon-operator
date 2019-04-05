# Addon-operator

Addon-operator is a ...


## Quickstart

> You need to have a Kubernetes cluster, and the kubectl must be configured to communicate with your cluster.

Steps to setup Addon-operator in your cluster are:
- build an image with your global hooks and modules
- create necessary RBAC objects (for onKubernetesEvent binding)
- run Deployment with a built image

### Build an image with your global hooks and modules

TODO

### Install addon-operator in a cluster

TODO

## Hook binding types

### Shell-operator style hooks

[__onStartup__](HOOKS.md#onstartup)

This binding has only one parameter: order of execution. Hooks are loaded at start and then hooks with onStartup binding are executed in order defined by parameter.

Example `hook --config`:

```
{"onStartup":10}
```

[__schedule__](HOOKS.md#schedule)

This binding is for periodical running of hooks. Schedule can be defined with granularity of seconds.

Example `hook --config` with 2 schedules:

```
{
  "schedule": [
   {"name":"every 10 min",
    "schedule":"*/10 * * * * *",
    "allowFailure":true
   },
   {"name":"Every Monday at 8:05",
    "schedule":"* 5 8 * * 1"
    }
  ]
}
```

[__onKubernetesEvent__](HOOKS.md#onKubernetesEvent)

This binding defines a subset of Kubernetes objects that Shell-operator will monitor and a [jq](https://github.com/stedolan/jq/) filter for their properties.

Example of `hook --config`:

```
{
  "onKubernetesEvent": [
  {"name":"Execute on changes of namespace labels",
   "kind": "namespace",
   "event":["update"],
   "jqFilter":".metadata.labels"
  }]
}
```

### Global hooks

### Module hooks

## Prometheus target

Addon-operator provides `/metrics` endpoint. More on this in [METRICS](METRICS.md) document.

## Examples

More examples can be found in [examples](/examples/) directory.

## License

Apache License 2.0, see [LICENSE](LICENSE).
