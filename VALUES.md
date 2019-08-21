# Values storage

The Addon-operator provides storage for the values that will be passed to the helm chart. You may find out more about the chart values concept in the helm documentation: [values files](https://helm.sh/docs/chart_template_guide/#values-files). Global and module hooks  have access to the values in the storage and can change them.

The values storage is a hash-like data structure. The `global` key contains all global values – they are passed to every hook and available to all helm charts. Only global hooks may change global values.

The other keys must be [names of modules](MODULES.md#module-structure) in the form of camelCase. Every key stores the object with module values – they are only available to the hooks and enabled script of this module as well as its helm chart. Only module hooks can change the values of the module.

> **Note:** You cannot get the values of another module within the module hook. Shared values should be global values for now (#9).

The values can be represented as:

- a structure (including empty structure)
- a list (including empty list)

Structures and lists must be JSON-compatible since hooks receive values at runtime as JSON files (see [using values in hook](#using-values-in-hook)).

> **Note:** each module has addditional key with `Enabled` suffix and boolean value for enable and disable the module; this key is handled by [modules discovery](LIFECYCLE.md#modules-discovery) process.

# values.yaml

On start-up, the Addon-operator loads values into storage from `values.yaml` files:

- $MODULES_DIR/values.yaml
- `values.yaml` files in modules directories — only the values from key with camelCase name of the module

An example of global values in $MODULES_DIR/values.yaml:

```
global:
  param1: value1
  param2: value2
simpleModule:
  modParam1: 
```

An example of module values in $MODULES_DIR/001-simple-module/values.yaml:

```
simpleModule:
  modParam1: value1
  modParam2: value2
```

# ConfigMap/addon-operator

There is a key `global` in the ConfigMap/addon-operator that contains global values and a keys with module values. The values stored in these keys as a yaml-coded strings. Values in the ConfigMap/addon-operator override the values that loaded from values.yaml files.

Addon-operator monitors changes in the ConfigMap/addon-operator and starts [modules discovery](LIFECYCLE.md#modules-discovery) process in the case of parameters change.

An example of ConfigMap/addon-operator:

```
data:
  global: |                 # vertical bar is required here
    param1: newValue
    param3: valu3
  simpleModule: |           # module name should be in camelCase
    modParam2: newValue2
  anotherModule: "false"    # `false' value disables a module
```

# Update values

Hooks have the ability to update values in the storage. In order to do that a hook returns a [JSON Patch](http://jsonpatch.com/).

A hook can update values in the ConfigMap/addon-operator so that the updated values would be available after the restart of the Addon-operator (long-term update). For example, you may store generated passwords or certificates.

Patch for a long-term update is returned via the $CONFIG_VALUES_JSON_PATCH_PATH file and after hook execution Addon-operator immediately apply this patch to the values in ConfigMap/addon-operator.

Another option is to update values for a time while Addon-operator process is running. For example, you may store the results of the discovery of cluster resources or parameters.

Patch for temporary updates is returned via $VALUES_JSON_PATCH_PATH file and remains in the Addon-operator memory.

# Merged values

When the hook or `enabled` script should be executed, or helm chart is going to be installed, Addon-operator generates a merged set of values. This merged set combines:
* global values from values.yaml files and ConfigMap/addon-operator;
* module values from the values.yaml files and ConfigMap/addon-operator;
* patches for the temporary update are applied.

The merged values are passed as the temporary JSON file to hooks or `enabled` script and as the temporary values.yaml file to the helm installing the chart.

# Using values in hook

When hook is triggered by an event, the values are passed to it via JSON files. Hook can use environment variables to get paths of those files:

$CONFIG_VALUES_PATH — this file contains values from the ConfigMap/addon-operator.

$VALUES_PATH — this file contains merged values.

For global hooks, only the global values are available.

For module hooks the global values and the module values are available. Also, the `enabledModules` field is added to the `global` values in the $VALUES_PATH file. It contains the list of all enabled modules in order of execution (see [module lifecycle](LIFECYCLE.md#module-lifecycle)).

To change the values, hook must return JSON patches via the result files. Hook can use environment variables to get paths of those files:

$CONFIG_VALUES_JSON_PATCH_PATH — hook should write a patch for ConfigMap/addon-operator into this file.

$VALUES_JSON_PATCH_PATH — hook should write a patch for a temporary update of parameters into this file.

# Using values in `enabled` scripts

The `enabled` script works with values in the read-only mode. It receives values in JSON files. Script can use environment variables to get paths of those files:

$CONFIG_VALUES_PATH — this file contains values from  ConfigMap/addon-operator.

$VALUES_PATH — this file contains merged values.

The `enabledModules` field with the list of previously enabled modules is added to the `global` key in $VALUES_PATH file.


# Using values in helm charts

Helm chart of the module has access to a merged values similar to the $VALUES_PATH but without `enabledModules` field.

The helm's variable `.Values` allows you to use values in the templates:

```
{{ .Values.global.param1 }}

{{ .Values.moduleName.modParam2 }}
```


# Example

Let’s assume the following values are defined:

```
$ cat modules/values.yaml:

global:
  param1: 100
  param2: "Yes"

$ cat modules/01-some-module/values.yaml

someModule:
  param1: "String"

$ kubectl -n addon-operator get cm/addon-operator -o yaml

data:
  global: |
    param1: 200
  someModule: |
    param1: "Long string"
    param2: "FOO"
```

Addon-operator generates the following files with values:

```
$ cat $CONFIG_VALUES_PATH

{"global":{
    "param1":200
}, "someModule":{
    "param1":"Long string",
    "param2": "FOO"
}}

$ cat $VALUES_PATH

{"global":{
    "param1":200,
    "param2": "YES"
}, "someModule":{
    "param1":"Long string", 
    "param2": "FOO"
}}

```

Let’s hook adds a new value with the help of a JSON patch:

```
$ cat /modules/001-some-module/hooks/hook.sh

#!/usr/bin/env bash
...
cat > $CONFIG_VALUES_JSON_PATCH_PATH <<EOF
  [{"op":"add", "path":"/someModule/param3", "value":"newValue"}]
EOF
...
```

Now the ConfigMap/addon-operator has the following content:

```
data:
  global: |
    param1: 200
  someModule: |
    param1: "Long string"
    param2: "FOO"
    param3: "newValue"
```

The next time the hook is executed, Addon-operator would generate the following files with values:

```
$ cat $CONFIG_VALUES_PATH

{"global":{
     "param1":200
},
"someModule":{
    "param1":"Long string",
    "param2": "FOO",
    "param3": "newValue"
}}

$ cat $VALUES_PATH

{"global":{
    "param1":200,
    "param2": "YES"
}, "someModule":{
    "param1":"Long string",
    "param2": "FOO",
    "param3": "newValue"
}}
```

Helm chart template string `replicas: {{ .Values.global.param1 }}` would generate the string `replicas: 200`. As you can see, the value "100" from the values.yaml is replaced by "200" from the ConfigMap/addon-operator.

