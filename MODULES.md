# Modules


Module is a wrapper layer for helm. This layer solve some issues that are critical when helm charts are used to deploy cluster components or *addons*:

- helm has only simple feature discovery for Kubernetes clusters. `onStartup` and `beforeHelm` hooks can be used to discover Kubernetes features and customize values for helm chart.
- helm release can get stuck in case of first fail. Addon-operator check this situation and delete single failed release before helm install.
- helm has no ability to detect another releases to deploy components dependently. `enabled` script has access to previously enabled modules to detect if parent is enabled.

Also modules provides:

- continuous discovery: values for component's chart can be changed and the component will be upgraded.
- dynamic enable: modules can be enabled or disabled dynamically on changes in Kubernetes cluster.

## Structure of a module

Modules are directories under WORKING_DIR/modules. The typical module structure is:

```
001-module
├── .helmignore
├── Chart.yaml
├── enabled
├── hooks
│   └── module-hooks.sh
├── README.md
├── templates
│   └── daemon-set.yaml
└── values.yaml
```

Addon-operator specific files are:

`hooks` directory — a place for module hooks

`enabled` script — script that is executed to determine if module should be enabled.

`values.yaml` — file with values for helm chart. This file must contain one root key with module name in camelCase and a value:
- a string "false" to disable module by default
- a map or an array

`README.md` — description of a module. It is always good to have one.

`.helmignore`, `Chart.yaml`, `templates` directory and other files are a helm chart.

## Environment of a module

Addon-operator provides mechanisms to store and retrieve values for hooks and helm chart. Before running hook or helm, addon-operator merges values from several sources and create effective set of values.

Values are stored in:
- `values.yaml`
- `cm/addon-operator`
- json patches from hooks

Module can be in two states: enabled or disabled. This state determined from:
- `values.yaml`
- `cm/addon-operator`
- `enabled` script

If merged values from `values.yaml` and `cm/addon-operator` contains "false", then module is considered disabled. If result is a map or array, even empty one, then `enabled` script is executed to take a final decision. 

More on values and enabled script you can get from [VALUES.md](VALUES.md) document.

## Module's lifecycle

Different parts of module can be run as a response to different addon-operator event. Let's see how the module behave on addon-operator start.

The first phase is `loading`. Addon-operator determines configurations of module hooks.

The second phase is `running`. Addon-operator start a process that called modules discovery to determine which modules are enabled. For each enabled module addon-operator creates tasks to run a module.

Module run is a process of sequential execution of `onStartup` hook, `beforeHelm` hook, `helm` and `afterHelm` or `afterDeleteHelm` hooks.

`onStartup` module hook run once for enabled module at start or if disabled module become enabled.

`helm` phase of module run is an execution of helm to install module's chart or to delete or purge existed release.

After all modules are run, addon-operator start watch for events.

`schedule` and `onKubernetesEvent` can run particular module hooks. These hooks can return json patch to change global or module section in `cm/addon-operator`.

Changing of module values in `cm/addon-operator` trigger a module run: `beforeHelm` hook, `helm`, `afterHelm` hooks.

Changing of global values in `cm/addon-operator` triggers a discover process: if a list of enabled modules is changed, then all modules are run.

Modules are run one by one to enable dependent execution. If module run ends with error in hook or helm, then `addon_operator_module_run_errors` counter is increased and module will run again after 5s delay.

## Values example

Values is the most obscure part of modules. Let's see it in action.

Consider that we have a simple module:

```
001-simple-one-module
├── Chart.yaml
├── hooks
│   └── module-hooks.sh
├── templates
│   └── daemon-set.yaml
└── values.yaml
```

```
# values.yaml
simpleOneModule:
  param1: value_1
  param2: value_2
```

```
$ kubectl get cm/addon-operator -o yaml

...
data:
...
  global:
    globParam1: globalValue1
  simpleOneModule: |
    param3: value_3
    param2: newValue_1
...

```

When module is run, beforeHelm hook from module-hooks.yaml will get this in $VALUES_PATH file:
```
{"global":{
   "globParam1": "globalValue1"
 },
 "simpleOneModule": {
   "param1": "value_1",
   "param2": "newValue_1",
   "param3": "value_3"
 }
}
```

Hook can write a JSON patch into $VALUES_JSON_PATCH_PATH:

```
{"op": "replace", "path":"simpleOneModule.param2", "value":"patchedValue_2"}
```

Then effective values for helm should be:

```
global:
  globParam1: globalValue1
simpleOneModule:
  param1: value_1
  param2: patchedValue_2
  param3: value_3
```

These values can be used in templates:

```
...
kind: DaemonSet
...
    env:
    - name: GLOBAL_PARAM_1
      value: {{ .Values.global.globParam1 }}
    - name: APP_PARAM_1
      value: {{ .Values.simpleOneModule.param1 }}
...
```

Hook can output patch into $CONFIG_VALUES_JSON_PATCH_PATH to update cm/addon-operator and store values between addon-operator restarts.


## Enabled script example

`enabled` script can be used to programmatically disable module. `VALUES_PATH` file contains values for module and `MODULE_ENABLED_RESULT` file should be populate with true or false string to enabled or disable module.

The module in previous example can be modified that it will be disabled if param2 is equal to "stopMePlease":

```
$ cat enabled

#!/usr/bin/env bash

param2=$(jq -r '.simpleOneModule.param2' $VALUES_PATH)

if [[ $param2 == "stopMePlease" ]] ; then
  echo "false"
else
  echo "true"
fi 
```

