## onStartup global hooks example

Example of a global hook written as bash script.

### run

Build addon-operator image with custom scripts:

```
docker build -t "registry.mycompany.com/addon-operator:startup-global" .
docker push registry.mycompany.com/addon-operator:startup-global
```

Edit image in addon-operator-pod.yaml and apply manifests:

```
kubectl create ns example-startup-global
kubectl -n example-startup-global apply -f addon-operator-rbac.yaml
kubectl -n example-startup-global apply -f addon-operator-deploy.yaml
```

> Note: addon-operator-deploy.yaml use `hostNetwork: true` so tiller can listen on 127.0.0.1.  Use 
ADDON_OPERATOR_PROMETHEUS_LISTEN_PORT, ADDON_OPERATOR_TILLER_LISTEN_PORT and  ADDON_OPERATOR_TILLER_PROBE_LISTEN_PORT to assign different ports to run other examples. 

See in logs that hook.sh was run at startup:

```
kubectl -n example-startup-global logs deploy/addon-operator -c addon-operator -f
...
INFO     : Initializing global hooks ...
INFO     : INIT: global hook 'hook.sh' ...
...
INFO     : TASK_RUN GlobalHookRun@ON_STARTUP hook.sh
INFO     : Running global hook 'hook.sh' binding 'ON_STARTUP' ...
OnStartup global hook
...
```

### cleanup

```
kubectl delete clusterrolebinding/addon-operator
kubectl delete clusterrole/addon-operator
kubectl delete ns/example-startup-global
docker rmi registry.mycompany.com/addon-operator:startup-global
```
