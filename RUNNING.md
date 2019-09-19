# Running Addon-operator

## Environment variables

**ADDON_OPERATOR_NAMESPACE** — a required parameter with namespace where Addon-operator is deployed.

**ADDON_OPERATOR_CONFIG_MAP** — a name of ConfigMap to store values. Default is `addon-operator`.

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


**ADDON_OPERATOR_TILLER_LISTEN_PORT** — a port used for communication with helm (-listen flag). Default is 44435.
**ADDON_OPERATOR_TILLER_PROBE_LISTEN_PORT** — a port used for Tiller probes (-probe-listen flag). Default is 44434.

Tiller starts as a subprocess and listens on 127.0.01 address. Defaults are good, but if Addon-operator should start with `hostNetwork: true`, then these variables will come in handy.
