# Addon-operator [![docker pull flant/addon-operator](https://img.shields.io/badge/docker-latest-2496ed.svg?logo=docker)](https://hub.docker.com/r/flant/addon-operator) [![Slack chat EN](https://img.shields.io/badge/slack-EN%20chat-611f69.svg?logo=slack)](https://cloud-native.slack.com/messages/CJ13K3HFG) [![Telegram chat RU](https://img.shields.io/badge/telegram-RU%20chat-179cde.svg?logo=telegram)](https://t.me/shelloperator)

<img width="70" height="70" src="logo-addon.png" alt="Addon-operator logo" />


Addon-operator is an addon operator for Kubernetes.

This operator is not an operator for a _particular software product_ as prometheus-operator or kafka-operator. Addon-operator is an operator that helps create and manage custom addons suitable for various Kubernetes flavors.

Addon-operator has concepts of hooks and modules and is a further development of a [Shell-operator](https://github.com/flant/shell-operator/) — a tool for running event-driven scripts in a Kubernetes cluster.

Addon-operator provides:
- __Ease the maintenance of a Kubernetes clusters__: use the tools that Ops are familiar with to build your modules and hooks. It can be bash, python, ruby, kubectl, helm, etc.
- __Install cluster components with helm__: wrapper layer called *module* can be used to solve some helm issues, dynamically define chart values and maintain runtime dependencies between charts.
- __Dynamic values and continuous discovery__: install and remove components dynamically according to the changes of cluster objects.
- __Shell-operator capabilities__: onStartup, schedule, onKubernetes types of hooks are available in addon-operator.


Addon-operator has two component types: global hooks and modules.

A *global hook* is a script with the events configuration. Learn more about global hooks in a [HOOKS](HOOKS.md) document.

A *module* consists of a set of *module hooks* and an optional *helm chart*. Learn more about modules in a [MODULES](MODULES.md) document.

On start, Addon-operator goes through the following steps:
- search for global hooks in WORKING_DIR/global-hooks directory
- search for modules in WORKING_DIR/modules dicrectory
- run global hooks configuration phase
- run modules hooks configuration phase.

After initial phase complete, Addon-operator:
- run onStartup global hooks
- run beforeAll global hooks
- run enabled modules one-by-one with their helm specific hooks
- waits for events according hook configurations
- monitors for changes of modules configuration
- monitors changes in its own configuration.

=======
## Quickstart

> You need to have a Kubernetes cluster with [Helm](helm.sh) installed, and the kubectl must be configured to communicate with your cluster.

Steps to setup Addon-operator in your cluster are:
- build an image with your global hooks and modules
- install addon-operator in a cluster: 
  - create necessary RBAC objects (for onKubernetesEvent binding and to create resources with helm)
  - optionaly create a ConfigMap to store module configuration values
  - run Deployment with a built image

### Build an image with your global hooks and modules

TODO

### Install addon-operator in a cluster

Steps to setup Addon-operator in your cluster are:
- build an image with your modules and hooks
- create necessary RBAC objects (for onKubernetesEvent binding)
- run Pod with a built image

### Build an image with your modules and hooks

Create a folder for your project and create the `global-hooks` folder for global hooks, and the `modules` folder for modules.

```bash
mkdir global-hooks modules
```

Here is a simple global hook with the `onStartup` binding. Create the `global-hooks/hook.sh` file with the following content:
```bash
#!/usr/bin/env bash

if [[ $1 == "--config" ]] ; then
  echo '{"onStartup": 10}'
else
  echo "GLOBAL: Run simple onStartup global hook"
fi
```

Here is a simple module with `onStartup` and `schedule` bindings. Create the `010-simple-module/hooks` folder in the `modules` folder.

```bash
mkdir -p modules/010-simple-module/hooks
```

Create the `modules/010-simple-module/hooks/hook.sh` file with the following content:
```bash
#!/usr/bin/env bash

if [[ $1 == "--config" ]] ; then
  cat <<EOF
{"onStartup": 4,
 "schedule": [
    { "name":"every 10 min",
      "crontab":"*/20 * * * * *"
    }
  ]
}
EOF
else
  echo "Simple module hook."
fi
```

Make both hook files executable:
```
chmod +x global-hooks/hook.sh modules/010-simple-module/hooks/hook.sh
```

Use a prebuilt image [flant/addon-operator:latest](https://hub.docker.com/r/flant/addon-operator) with bash, kubectl, jq and addon-operator binaries as a base to build you own image. You just need to ADD your global-hook and modules folders under the `/addons/` folder of a new image.

Create the following Dockerfile in the folder of your project:
```dockerfile
FROM flant/addon-operator:latest
ADD global-hooks /addons/global-hooks
ADD modules /addons/modules
```

Build image and push it to the Docker registry accessible by Kubernetes cluster.
```
docker build -t "registry.mycompany.com/addon-operator:example1" .
docker push registry.mycompany.com/addon-operator:example1
```

### Install Addon-operator in a cluster

Before starting an Addon-operator you have to setup [RBAC](https://kubernetes.io/docs/reference/access-authn-authz/rbac/) in your Kubernetes cluster (learn more about an Addon-operator [security](#security)). Create the namespace `addon-operator` and the serviceAccount `addon-operator` with binding to the `view` role in a cluster scope and the `admin` role within the `addon-operator` namespace:
```bash
$ kubectl create namespace addon-operator
$ kubectl create serviceaccount addon-operator \
  --namespace addon-operator
$ kubectl create rolebinding addon-operator-ns-admin \
  --clusterrole=admin \
  --serviceaccount=addon-operator:addon-operator \
  --namespace=addon-operator
$ kubectl create clusterrolebinding addon-operator-cluster-view \
  --clusterrole=view \
  --serviceaccount=addon-operator:addon-operator
```

Put this manifest into the `addon-operator.yaml` file to describe a deployment based on the image you built:

```yaml
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: addon-operator
  namespace: addon-operator
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

Create the `addon-operator` deployment to start the Addon-operator:
```bash
kubectl create -n addon-operator -f addon-operator.yaml
```

Run `kubectl -n addon-operator logs -f deployment/addon-operator` to get the Addon-operator deployment log. It can contains following records:
```
... initialization phase log was skipped ...
... timestamps are removed ...
INFO     : MAIN: run main loop
INFO     : MAIN: add onStartup, beforeAll, module and afterAll tasks
INFO     : QUEUE add all GlobalHookRun@OnStartup
INFO     : QUEUE add all GlobalHookRun@BeforeAll, add DiscoverModulesState
INFO     : TASK_RUN GlobalHookRun@ON_STARTUP global-hooks/hook.sh
INFO     : Running global hook 'global-hooks/hook.sh' binding 'ON_STARTUP' ...
GLOBAL: Run simple onStartup global hook
INFO     : TASK_RUN DiscoverModulesState
INFO     : DISCOVER run `enabled` for [simple-module]
INFO     : DISCOVER enabled modules [simple-module]
INFO     : Initializing module 'simple-module' hooks ...
INFO     : Initializing hook '010-simple-module/hooks/hook.sh' ...
INFO     : QUEUE add ModuleRun simple-module
INFO     : TASK_RUN ModuleRun simple-module
INFO     : Running module hook '010-simple-module/hooks/hook.sh' binding 'ON_STARTUP' ...
Simple module hook.
INFO     : Running schedule manager entry '*/20 * * * * *' ...
INFO     : TASK_RUN ModuleHookRun@SCHEDULE 010-simple-module/hooks/hook.sh
INFO     : Running module hook '010-simple-module/hooks/hook.sh' binding 'SCHEDULE' ...
Simple module hook.
INFO     : Running schedule manager entry '*/20 * * * * *' ...
INFO     : TASK_RUN ModuleHookRun@SCHEDULE 010-simple-module/hooks/hook.sh
INFO     : Running module hook '010-simple-module/hooks/hook.sh' binding 'SCHEDULE' ...
Simple module hook.
```

As you can see, global hook was started once, before module run. Module hook has two bindings (onStartup and schedule), thus it is started once and then started on a schedule (once every 20 seconds).

## Hook binding types
>>>>>>> b5c9d12... ++

Shell-operator hooks can be used for both global hooks anc module hooks.

[__onStartup__](HOOKS.md#onstartup)

This binding can be used in **global** or **module** hooks and has only one parameter: order of execution. Hooks are loaded at start and then hooks with onStartup binding are executed in order defined by parameter.

Example `hook --config`:

```yaml
{"onStartup":10}
```

[__schedule__](HOOKS.md#schedule)

This binding can be used in **global** or **module** hooks. The schedule binding is for periodical running of hooks and can be defined with granularity of seconds.

Example `hook --config` with 2 schedules:

```yaml
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

This binding can be used in **global** or **module** hooks. This binding defines a subset of Kubernetes objects that Addon-operator will monitor and a [jq](https://github.com/stedolan/jq/) filter for their properties.

Example of `hook --config`:

```yaml
{
  "onKubernetesEvent": [
  {"name":"Execute on changes of namespace labels",
   "kind": "namespace",
   "event":["update"],
   "jqFilter":".metadata.labels"
  }]
}
```

### global hooks events

[__beforeAll__](HOOKS.md#beforeAll)

Hooks with this binding are triggered after all onStartup hooks have been triggered and before running all modules.

```yaml
{
  "beforeAll": ORDER
}
```

[__afterAll__](HOOKS.md#afterAll)

Hooks with this binding are triggered after running all modules.

```yaml
{
  "afterAll": ORDER
}
```

### module hooks events

[__beforeHelm__](HOOKS.md#beforeHelm)

Hook with this binding is triggered before running the module (before installing a helm chart).

```yaml
{
  "beforeHelm": ORDER
}
```

[__afterHelm__](HOOKS.md#afterHelm)

Hook with this binding is triggered after running the module (after installing a helm chart).

```yaml
{
  "afterHelm": ORDER
}
```

[__afterDeleteHelm__](HOOKS.md#afterDeleteHelm)

Hook with this binding is triggered after deleting the module (after running helm delete).

```yaml
{
  "afterDeleteHelm": ORDER
}
```

## Prometheus target

Addon-operator provides `/metrics` endpoint. More on this in [METRICS](METRICS.md) document.

## Security

Addon-operator requires at least clusterRole with rights to list pods and deployments. Also it requires rights according to your hooks. E.g. if you have a hook which creates a secret when a new namespace is created, you need to assign to Addon-operator's ServiceAccount rights to `get`, `list` and `watch` for namespaces and `get`, `list`, `patch` and `create` for secrets in any namespace.

In the production cluster it is not secure to give an unlimited privileges in the Kubernetes cluster to the Addon-operator, but in your test cluster this may be acceptable. If you have a demo cluster and you don't concern about security you can use the `cluster-admin` clusterRole with the unlimited rights for the ServiceAccount (change namespace if you need):

```
$ kubectl create namespace addon-operator
$ kubectl create serviceaccount addon-operator \
  --namespace addon-operator &&
$ kubectl create clusterrolebinding addon-operator-admin \
  --clusterrole=cluster-admin \
  --serviceaccount=addon-operator:addon-operator
```

## Examples

More examples can be found in [examples](/examples/) directory.

## License

Apache License 2.0, see [LICENSE](LICENSE).
