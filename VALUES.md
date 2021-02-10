# Values storage

The Addon-operator provides the storage for the values that will be passed to the Helm chart. You may find out more about the chart values concept in the Helm documentation: [values files](https://helm.sh/docs/chart_template_guide/#values-files). Global and module hooks have access to the values in the storage and can change them.

The storage is a hash-like data structure. The `global` key contains all global values – they are passed to every hook and available to all Helm charts. Only global hooks may change global values.

The other keys must match the [module's name](MODULES.md#module-structure) converted to camelCase. Each key stores the object with module values. These values are only available to hooks, `enabled` script of this module, and to its Helm chart. Only module hooks can change the values of the module.

> **Note:** You cannot get the values of another module within the module hook. Shared values should be global values for now (#9).

Hook receives values via files on execution. These schemas can help you understand the flow of values for a global hook and for a module hook:

<p align="center">
<img width="407" src="docs/module_values_flow.png" alt="Flow of values for module hook" />
</p>

<p align="center">
<img width="407" src="docs/global_values_flow.png" alt="Flow of values for global hook" />
</p>

The values can be represented as:

- a structure (including empty structure)
- a list (including empty list)

Structures and lists must be JSON-compatible since hooks receive values at runtime as JSON files (see [using values in hook](#using-values-in-hook)).

> **Note:** each module has an additional key with `Enabled` suffix and a boolean value to enable or disable the module (e.g., `ingressNginxEnabled: false`). This key is handled by [modules discovery](LIFECYCLE.md#modules-discovery) process.

## `values.yaml`

On start-up, the Addon-operator loads values into storage from `values.yaml` files:

- `$MODULES_DIR/values.yaml`
- `values.yaml` files in modules directories — only the values from key with camelCase name of the module

An example of global values in `$MODULES_DIR/values.yaml`:

```yaml
global:
  param1: value1
  param2: value2
simpleModule:
  modParam1: value3
```

An example of module values in `$MODULES_DIR/001-simple-module/values.yaml`:

```yaml
simpleModule:
  modParam1: value1
  modParam2: value2
```

## ConfigMap/addon-operator

There is a key `global` in the ConfigMap/addon-operator that contains global values and the keys with module values. The values are stored in these keys as the YAML encoded strings. Values in the ConfigMap/addon-operator override the values loaded from `values.yaml` files.

The Addon-operator monitors changes in the ConfigMap/addon-operator and starts the 'reload all modules' process in case of global values changes or 'module run' process if only the module section is changed. See [LIFECYCLE](LIFECYCLE.md).

An example of ConfigMap/addon-operator:

```yaml
data:
  global: |                 # vertical bar is required here
    param1: newValue
    param3: valu3
  simpleModule: |           # module name should be in camelCase
    modParam2: newValue2
  anotherModule: "false"    # `false' value disables a module
```

## Update values

Hooks can update values in the storage. To do that the hook returns a [JSON Patch](http://jsonpatch.com/).

A hook can update values in the ConfigMap/addon-operator so that the updated values would be available after restarting the Addon-operator (long-term update). For example, you may store generated passwords or certificates.

Patch for a long-term update is returned via the `$CONFIG_VALUES_JSON_PATCH_PATH` file and after hook execution, the Addon-operator immediately applies this patch to the values in ConfigMap/addon-operator.

Another option is to store updated values for a period while the Addon-operator process is running. For example, you may store the results of the discovery of cluster resources or parameters.

Patch for temporary updates is returned via the `$VALUES_JSON_PATCH_PATH` file and remains in the Addon-operator volatile memory.

## Merged values

When the hook or `enabled` script is about to be executed, or a Helm chart is to be installed, the Addon-operator generates *a merged set of values*. This merged set combines:

- global values from `values.yaml` files and ConfigMap/addon-operator;
- module values from the `values.yaml` files and ConfigMap/addon-operator;
- patches for the temporary updates are applied.

The merged values are passed as the temporary JSON file to hooks or `enabled` script and as the temporary `values.yaml` file to the `helm install`.

## Using values in the hook

When the hook is triggered by an event, the values are passed to it via JSON files. The hook can use environment variables to get paths of those files:

- `$CONFIG_VALUES_PATH` — this file contains values from the ConfigMap/addon-operator.
- `$VALUES_PATH` — this file contains merged values.

For global hooks, only global values are available.

For module hooks the global values and the module values are available. Also, the `enabledModules` field is added to the `global` values in the `$VALUES_PATH` file. It contains the list of all enabled modules in the order of execution (see [module lifecycle](LIFECYCLE.md#module-lifecycle)).

To change the values, the hook must return JSON patches via the result files. The hook can use environment variables to get paths of those files:

- `$CONFIG_VALUES_JSON_PATCH_PATH` — hook should write a patch for ConfigMap/addon-operator into this file.
- `$VALUES_JSON_PATCH_PATH` — hook should write a patch for a temporary update of parameters into this file.

## Using the values in `enabled` scripts

The `enabled` script works with values in the read-only mode. It receives values in JSON files. The script can use environment variables to get paths of those files:

- `$CONFIG_VALUES_PATH` — this file contains values from ConfigMap/addon-operator.
- `$VALUES_PATH` — this file contains merged values.

The `enabledModules` field with the list of previously enabled modules is added to the `global` key in the `$VALUES_PATH` file.

## Using values in Helm charts

Helm chart of the module has access to the merged values similar to the `$VALUES_PATH` but without `enabledModules` field.

The Helm template's variable `.Values` allows you to use values in the templates:

```text
{{ .Values.global.param1 }}

{{ .Values.moduleName.modParam2 }}
```

## Example

Let’s assume the following values are defined:

```shell
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

The Addon-operator generates the following files with values:

```shell
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

A hook adds a new value with the help of a JSON patch:

```shell
$ cat /modules/001-some-module/hooks/hook.sh

#!/usr/bin/env bash
...
cat > $CONFIG_VALUES_JSON_PATCH_PATH <<EOF
  [{"op":"add", "path":"/someModule/param3", "value":"newValue"}]
EOF
...
```

Now the ConfigMap/addon-operator has the following content:

```shell
data:
  global: |
    param1: 200
  someModule: |
    param1: "Long string"
    param2: "FOO"
    param3: "newValue"
```

Next time the hook is executed, the Addon-operator would generate the following files with values:

```shell
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

Helm chart template
```replicas: {{ .Values.global.param1 }}```
would generate the string `replicas: 200`. As you can see, the value "100" from the values.yaml is replaced by "200" from the ConfigMap/addon-operator.


# Validation

The addon-operator supports OpenAPI schemas for config values and for effective values. These schemas should be stored in the `$GLOBAL_HOOKS_DIR/openapi` directory for global values and in the `$MODULES_DIR/<module-name>/openapi` directories for modules.

`openapi/config-values.yaml` is a schema for values merged from values.yaml, modules/values.yaml and the ConfigMap.

`openapi/values.yaml` is a schema for values merged from values.yaml, modules/values.yaml and the ConfigMap with applied values patches.

Validation occurs on startup, on ConfigMap changes, and after hook executions. If validation fails after hook execution, hook is restarted. If validation fails on startup, the addon-operator stops. If validation fails on ConfigMap changes, error is logged and no new tasks are queued.

> Note: Unlike the default behavior, the addon-operator sets `additionalProperties: false` if `additionalProperties` is not set.

## Example

```yaml
# /global/openapi/config-values.yaml

type: object
additionalProperties: false
required:
  - project
  - clusterName
minProperties: 2
properties:
  project:
    type: string
  clusterName:
    type: string
  clusterHostname:
    type: string
  discovery:
    type: object
```

This schema defines 2 required fields for 'global' values: `project` and `clusterName`. `clusterHostname` field is an optional string. `discovery` is an optional object with no restrictions on keys.

Consider this `ConfigMap/addon-operator` content:

```yaml
metadata:
...
data:
  global: |
    project: myProject
  moduleOne: |
    param1: value1
...
```

This ConfigMap has invalid 'global' values, and the addon-operator stops with an error on startup.

Consider valid `ConfigMap/addon-operator` and this config patch from global hook:

```json
[{"op":"add", "path":"/global/clusterHostname", "value":"{}"}]
```

This patch sets `clusterHostname` field in the 'global' section. It is not allowed because schema defines `clusterHostname` as a string. This situation is handled like a hook execution error, the hook stays in queue and restarts with exponential backoff (see [LIFECYCLE](LIFECYCLE.md#task-queues).

## Extending

Values are config values with applied patches, so schema in values.yaml should contain duplicates of properties from config-values.yaml schema. There is a technique with `allOf` to reduce duplicates: [1](https://github.com/json-schema-org/json-schema-spec/issues/348) [2](https://github.com/json-schema-org/json-schema-spec/issues/348), but it will not eliminate duplicates when `additionalProperties: false`. To overcome this problem, we implement custom property `x-extend` for values.yaml schema.

If values.yaml schema contains `x-extend` field, shell-operator extends fields in values.yaml schema with fields from config-values.yaml schema:
- definitions
- required
- properties
- patternProperties
- title
- description

Also, "x-*" properties copied from config-values.yaml schema.

### Example

Consider these OpenAPI schemas:

```yaml
# /global/openapi/config-values.yaml

type: object
additionalProperties: false
required:
  - project
  - clusterName
properties:
  project:
    type: string
  clusterName:
    type: string
  clusterHostname:
    type: string
```

```yaml
# /global/openapi/values.yaml

x-extend:
  schema: config-values.yaml
type: object
additionalProperties: false
required:
  - discovery
  - param1
properties:
  discovery:
    type: object
  param1:
    type: string
```

The addon-operator will validate values with this effective schema:

```yaml
# effective schema for values

type: object
additionalProperties: false
required:
  - project
  - clusterName
  - discovery
  - param1
properties:
  project:
    type: string
  clusterName:
    type: string
  clusterHostname:
    type: string
  discovery:
    type: object
  param1:
    type: string
```

## Defaults

The addon-operator respects `default` key in schemas and apply defaults when merge values.

### Example

Consider this schema for global values:

```yaml
# /global/openapi/values.yaml

x-extend:
  schema: config-values.yaml
type: object
additionalProperties: false
required:
  - param1
properties:
  discovery:
    type: object
    default:
      {}
  param1:
    type: string
```

The addon-operator will add `discovery` with empty object to values if no `discovery` key is present in the ConfigMap, `modules/values.yaml` or in patches.

## Required fields

There is a problem with `required` fields defined in `openapi/values.yaml`: values for Helm can be constructed by multiple hooks. Different hooks return different portions of `required` fields and validation will fail on hook execution. To define a contract for Helm values in this situation, the addon-operator implements `x-required-for-helm` to define required values for Helm. Values are checked before helm execution with `x-required-for-helm` array merged with `required`.

### Example

Suppose we have two hooks: one hook prepares a `param1` value and the second hook prepares a `param2` value. Helm required both fields, but we can't require both fields after each hook execution. `x-required-for-helm` to the rescue:

```yaml
# /global/openapi/values.yaml

type: object
x-required-for-helm:
  - param1
  - param2
properties:
  param1:
    type: string
  param2:
    type: string
```

The addon-operator will validate values *after each hook execution* with this effective schema:

```yaml
# effective schema for values

type: object
additionalProperties: false
properties:
  param1:
    type: string
  param2:
    type: string
```

The addon-operator will validate values *before Helm execution* with this effective schema:

```yaml
# effective schema for values

type: object
additionalProperties: false
required:
  - param1
  - param2
properties:
  param1:
    type: string
  param2:
    type: string
```
