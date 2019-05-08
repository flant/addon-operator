# Addon-operator [![docker pull flant/addon-operator](https://img.shields.io/badge/docker-latest-2496ed.svg?logo=docker)](https://hub.docker.com/r/flant/addon-operator) [![Slack chat EN](https://img.shields.io/badge/slack-EN%20chat-611f69.svg?logo=slack)](https://cloud-native.slack.com/messages/CJ13K3HFG) [![Telegram chat RU](https://img.shields.io/badge/telegram-RU%20chat-179cde.svg?logo=telegram)](https://t.me/shelloperator)

<img width="70" height="70" src="logo-addon.png" alt="Addon-operator logo" />


Addon-operator helps to manage additional components for Kubernetes cluster. It provides:
- __Adding cluster components with helm__: wrapper layer called *module* can be used to solve some helm issues, dynamically define chart values and maintain runtime dependencies between charts.
- __Dynamic values and continuous discovery__: install and remove components dynamically according to the changes of cluster objects.
- __Ease the maintenance of a Kubernetes clusters__: use the tools that Ops are familiar with to build your modules and hooks. It can be bash, python, ruby, kubectl, helm, etc.
- __Shell-operator capabilities__: onStartup, schedule, onKubernetesEvent types of hooks are available.

## Quickstart

Let's create a simple addon. For example, we need to set sysctl parameters for all nodes in a cluster. This can be done with DaemonsSet with privileged Pods:

```yaml
kind: DaemonSet
spec:
      - command: |
          while true; do
            sysctl -w vm.swappiness=0 ;
            sysctl -w kernel.numa_balancing=0 ;
            sysctl -w net.core.somaxconn=1000 ;
            sleep 600;
          done
        securityContext:
          privileged: true
```

It is a brief manifest just to give a clue what is a sysctl tuner. Full chart is available at [/example/101-module-sysctl-tuner](example/101-module-sysctl-tuner).


### Build an image with module

Use a prebuilt image [flant/addon-operator:latest](https://hub.docker.com/r/flant/addon-operator) and ADD modules directory under the `/modules` directory and global hooks directory under `/global-hooks`.

```dockerfile
FROM flant/addon-operator:latest
ADD global-hooks /global-hooks
ADD modules /modules
```

Build and push image to the Docker registry accessible by Kubernetes cluster.
```
docker build -t "registry.mycompany.com/addon-operator:example1" .
docker push registry.mycompany.com/addon-operator:example1
```

### Install Addon-operator in a cluster

In order to Addon-operator work, you have to setup [RBAC](https://kubernetes.io/docs/reference/access-authn-authz/rbac/):
```bash
$ kubectl create namespace addon-operator
$ kubectl create serviceaccount addon-operator \
  --namespace addon-operator
$ kubectl create clusterrolebinding addon-operator-ns-admin \
  --clusterrole=admin \
  --serviceaccount=addon-operator:addon-operator \
  --namespace=addon-operator
```

Put this manifest into the `addon-operator.yaml` file to describe a deployment based on the image you built:

```yaml
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: addon-operator
spec:
  selector:
    matchLabels:
      app: addon-operator
  template:
    metadata:
      labels:
        app: addon-operator
    spec:
      containers:
      - name: addon-operator
        image: registry.mycompany.com/addon-operator:example1
      serviceAccountName: addon-operator
```

Instantiate the `addon-operator` Deployment to start the Addon-operator:
```bash
kubectl create -n addon-operator -f addon-operator.yaml
```

Run `kubectl -n addon-operator logs -f deployment/addon-operator` to get the Addon-operator deployment log. It can contains following records:
```
... initialization phase log was skipped ...
... timestamps are removed ...
INFO     : QUEUE add ModuleRun sysctl-tuner
INFO     : TASK_RUN ModuleRun sysctl-tuner
INFO     : Running module hook '001-sysctl-tuner/hooks/module-hooks.sh' binding 'BEFORE_HELM' ...
Run 'beforeHelm' hook for sysctl-tuner
INFO     : Running helm upgrade for release 'sysctl-tuner' with chart '/tmp/addon-operator/sysctl-tuner.chart' in namespace 'example-module-sysctl-tuner' ...
INFO     : Helm upgrade for release 'sysctl-tuner' with chart '/tmp/addon-operator/sysctl-tuner.chart' in namespace 'example-module-sysctl-tuner' successful:
Release "sysctl-tuner" does not exist. Installing it now.
NAME:   sysctl-tuner
LAST DEPLOYED: Mon Apr 22 13:53:04 2019
NAMESPACE: example-module-sysctl-tuner
STATUS: DEPLOYED

RESOURCES:
==> v1beta1/DaemonSet
NAME          DESIRED  CURRENT  READY  UP-TO-DATE  AVAILABLE  NODE SELECTOR  AGE
sysctl-tuner  0        0        0      0           0          <none>         0s

==> v1/Pod(related)
NAME                READY  STATUS   RESTARTS  AGE
sysctl-tuner-l5pl5  0/1    Pending  0         0s
sysctl-tuner-tsftg  0/1    Pending  0         0s
sysctl-tuner-wpz99  0/1    Pending  0         0s
INFO     : Running module hook '001-sysctl-tuner/hooks/module-hooks.sh' binding 'AFTER_HELM' ...
Run 'afterHelm' hook for sysctl-tuner
...
```

As a result, sysctl parameters are applied to all nodes in cluster:

```
$ sysctl vm.swappiness   
vm.swappiness = 0
```

## What's next?

- Find out more on [modules and values](MODULES.md) in documentation.
- `/metrics` endpoint is implemented. See [METRICS](METRICS.md) for details.
- More examples can be found in [examples](/examples/) directory.

## License

Apache License 2.0, see [LICENSE](LICENSE).
