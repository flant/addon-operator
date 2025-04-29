# Running Addon-operator

## Environment variables

**GLOBAL_HOOKS_DIR** — a directory with global hook files.

**MODULES_DIR** — paths separated by colon where modules are located.

**UNNUMBERED_MODULE_ORDER** — an integer number to use as the default order for modules without numbered prefix.

**ADDON_OPERATOR_NAMESPACE** — a required parameter with namespace where Addon-operator is deployed.

**ADDON_OPERATOR_CONFIG_MAP** — a name of ConfigMap to store values. Default is `addon-operator`.

Namespace and config map name are used to watch for ConfigMap changes. 

Example of container:

```
containers:
- image: addon-operator-image:latest
  env:
  - name: ADDON_OPERATOR_NAMESPACE
    valueFrom:
      fieldRef:
        fieldPath: metadata.namespace
  - name: ADDON_OPERATOR_CONFIG_MAP
    value: my-values   
```

With this variables Addon-operator would monitor ConfigMap/my-values object. 

**ADDON_OPERATOR_LISTEN_ADDRESS** — address for http server. Default is `0.0.0.0`

**ADDON_OPERATOR_LISTEN_PORT** — port for http server. Default is `9650`.

Addon-operator starts http server and listens on `ADDRESS:PORT`. There is a liveness probe and `/metrics` endpoint.

```
  env:
  ...
  - name: ADDON_OPERATOR_LISTEN_ADDRESS
    valueFrom:
      fieldRef:
        fieldPath: status.podIP
  - name: ADDON_OPERATOR_LISTEN_PORT
    value: 9090
  livenessProbe:
    httpGet:
      path: /healthz
      port: 9090      
``` 

**ADDON_OPERATOR_PROMETHEUS_METRICS_PREFIX** — a prefix for Prometheus metrics. Default is `addon_operator_`.

```
  env
  - name: ADDON_OPERATOR_PROMETHEUS_METRICS_PREFIX
    value: dev_cluster_  
```

```
curl localhost:9650/metrics

...
dev_cluster_live_ticks 32
...
```

**ADDON_OPERATOR_CRD_EXTRA_LABELS** – string with CRDs label pairs.  
For example: `heritage=my-app,scope=extra`  
Default is `heritage=addon-operator`.

**ADDON_OPERATOR_CRD_FILTER_PREFIXES** – String of filters for the CRD, separated by commas. Default is `doc-,_`.

### Kubernetes client settings

**KUBE_CONFIG** — a path to a kubernetes client config (~/.kube/config)

**KUBE_CONTEXT** — a context name in a kubernetes client config (similar to a `--context` flag of a kubectl)

**KUBE_CLIENT_QPS** and **KUBE_CLIENT_BURST** — qps and burst parameters to rate-limit requests to Kubernetes API server. Default qps is 5 and burst is 10 as in a [rest/config.go][config.go] file.

### Helm settings

Addon-operator works with helm by using internal lib

**HELM_IGNORE_RELEASE** — a name of the release that should not be treated as the module's release. Prevent self-destruction when addon-operator release is stored in the same namespace as releases for modules.

```
env:
- name: HELM_IGNORE_RELEASE
  value: {{ .Release.Name }}
```

**HELM_MONITOR_KUBE_CLIENT_QPS** — QPS for a rate limiter of a kubernetes client for Helm resources monitor.

**HELM_MONITOR_KUBE_CLIENT_BURST** — Burst for a rate limiter of a kubernetes client for Helm resources monitor.

### Logging settings

**LOG_TYPE** — Logging formatter type: `json`, `text` or `color`.

**LOG_LEVEL** — Logging level: `debug`, `info`, `error`.

**LOG_NO_TIME** — 'true' value will disable timestamp logging. Useful when output is redirected to logging system that already adds timestamps. Default is 'false'.

## Debug

Several tools are available for the debugging of addon-operator and hooks:

- You can get logs of an Addon-operator’s pod for analysis (by executing `kubectl logs -f po/POD_NAME`)
- You can set the environment variable `LOG_LEVEL=debug` to include detailed debugging data into logs
- Addon-operator inherits shell-operator's debug CLI interface and a UNIX socket HTTP endpoint. A path to the endpoint can be configured with `DEBUG_UNIX_SOCKET` environment variable, the default path is 	"/var/run/addon-operator/debug.socket".

Available debug commands:

```
addon-operator queue list [-o text|yaml|json]
    Dump tasks in all queues.

addon-operator global values [-o yaml|json]
    Dump current global values.

addon-operator global patches
    Dump current JSON patches for global values.

addon-operator global config [-o yaml|json]
    Dump global config values.

addon-operator module list [-o text|yaml|json]
    List available modules and their enabled status.

addon-operator module values [-o yaml|json] <module_name>
    Dump module values by name.

addon-operator module patches <module_name>
    Dump JSON patches for module values by name.

addon-operator module config [-o yaml|json] <module_name>
    Dump module config values by name.

addon-operator module resource-monitor [-o text|yaml|json]
    Dump resource monitors.
```

[config.go]: https://github.com/kubernetes/client-go/blob/v0.17.0/rest/config.go#L44
