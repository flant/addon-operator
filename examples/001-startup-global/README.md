## onStartup global hooks example

Example of a global hook written as bash script.

### run

Build addon-operator image with custom scripts:

```
$ docker build -t "registry.mycompany.com/addon-operator:startup-global" .
$ docker push registry.mycompany.com/addon-operator:startup-global
```

Edit image in addon-operator-pod.yaml and apply manifests:

```
$ kubectl create ns example-startup-global
$ kubectl -n example-startup-global apply -f addon-operator-rbac.yaml
$ kubectl -n example-startup-global apply -f addon-operator-pod.yaml
```

See in logs that hook.sh was run:

```
$ kubectl -n example-startup-global logs -f po/addon-operator
...
INFO     : Initializing global hooks ...
INFO     : Initializing global hook 'global-hooks/hook.sh' ...
...
INFO     : TASK_RUN GlobalHookRun@ON_STARTUP global-hooks/hook.sh
INFO     : Running global hook 'global-hooks/hook.sh' binding 'ON_STARTUP' ...
OnStartup global hook
...
```

### cleanup

```
$ kubectl delete clusterrolebinding/addon-operator
$ kubectl delete clusterrole/addon-operator
$ kubectl delete ns/example-startup-global
$ docker rmi registry.mycompany.com/addon-operator:startup-global
```
