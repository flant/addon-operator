# Steps of addon-operator lifecycle

This document is intended to give a full view of how [hooks](HOOKS.md), [modules](MODULES.md), [values](VALUES.md), binding contexts, and queues are interlinked within the Addon-operator's [lifecycle](LIFECYCLE.md).

Startup steps:

<a name="global-onstartup"></a>1. execute global hooks with 'onStartup' binding ordered by the ORDER value (see [onStartup](HOOKS.md#onstartup))
  - input
    - binding context ($BINDING_CONTEXT_PATH temporary file)
      - `[{"binding":"onStartup"}]`
    - config ($CONFIG_VALUES_PATH temporary file)
      - 'global' section in ConfigMap
    - values ($VALUES_PATH temporary file)
      - 'global values' merged from:
        - 'global' section in modules/values.yaml
        - 'global' section in ConfigMap
        - patched with patches saved from previous global hooks
  - output
    - config patches ($CONFIG_VALUES_JSON_PATCH_PATH temporary file)
      - applied to ConfigMap just after the hook execution
    - values patches ($VALUES_JSON_PATCH_PATH temporary file)
      - saved in memory
  - events after execution
    - values changes do not trigger any event    
      
<a name="global-kubernetes-synchronization"></a>2. execute global hooks with 'kubernets' binding in alphabetic order (see [kubernetes](HOOKS.md#kubernetes))
  - a hook executes several times for each defined 'kubernetes' binding
  - input
    - binding context ($BINDING_CONTEXT_PATH temporary file)
      - `"type": "Synchronization"`
        - `"objects"` contains all existed objects
        - `"snapshots"` contains existed objects from previous bindings
    - config ($CONFIG_VALUES_PATH temporary file)
      - 'global' section in ConfigMap
    - values ($VALUES_PATH temporary file)
      - 'global values' merged from:
        - 'global' section in modules/values.yaml
        - 'global' section in ConfigMap
        - patched with patches saved from previous global hooks
  - output
    - config patches ($CONFIG_VALUES_JSON_PATCH_PATH temporary file)
      - applied to ConfigMap just after the hook execution
    - values patches ($VALUES_JSON_PATCH_PATH temporary file)
      - saved in memory
  - events after execution
    - values changes do not trigger an event

<a name="reload-all-modules"></a>'Reload all modules' steps:

<a name="global-beforeall"></a>3. execute global hooks with 'beforeAll' binding ordered by ORDER value (see [beforeAll](HOOKS.md#beforeall))
  - input
    - binding context ($BINDING_CONTEXT_PATH temporary file)
      - "snapshots" contains existed objects from all 'kubernetes' bindings of this hook
    - config ($CONFIG_VALUES_PATH temporary file)
      - 'global' section in ConfigMap
    - values ($VALUES_PATH temporary file)
      - 'global values' merged from:
        - 'global' section in modules/values.yaml
        - 'global' section in ConfigMap
        - patched with patches saved from previous global hooks
  - output
    - config patches ($CONFIG_VALUES_JSON_PATCH_PATH temporary file)
      - applied to ConfigMap just after the hook execution
    - values patches ($VALUES_JSON_PATCH_PATH temporary file)
      - saved in memory
  - events after execution
    - values changes do not trigger an event

<a name="discover-modules"></a>4. discover modules
  - get merged 'enabled' state from config for each module
    - false
    - '{moduleName}Enabled' value from modules/values.yaml
    - '{moduleName}Enabled' value from modules/{moduleName}/values.yaml
    - '{moduleName}Enabled' value from ConfigMap
  - run 'enabled' script if merged 'enabled' state is true
    - input
      - config ($CONFIG_VALUES_PATH temporary file)
        - 'global' section in ConfigMap
        - '{moduleName}' section in ConfigMap
      - values ($VALUES_PATH temporary file)
        - 'global values' merged from:
          - 'global' section in modules/values.yaml
          - 'global' section in ConfigMap
          - patched with patches saved from previous global hooks
          - extra field 'global.enabledModules' contains a list of previously enabled modules
        - 'module values' merged from:
          - '{moduleName}' section in modules/values.yaml
          - '{moduleName}' section in modules/{moduleName}/values.yaml
          - '{moduleName}' section in ConfigMap
          - patched with patches saved from previous module hooks
    - output
      - 'enabled' state ($MODULE_ENABLED_RESULT temporary file)
        - if hook return "false", hook state is disabled
  - no 'enabled' script
    - merged 'enabled' state is used
  - create 3 lists
    - modules to enable
    - modules to delete (disabled)
    - modules to purge (there is helm release, but no module directory)

<a name="module-run"></a>5. 'module run' for each enabled module
  - if startup or if module just become enabled
    - execute module hooks with 'onStartup' binding ordered by the ORDER value (see [onStartup](HOOKS.md#onstartup))
      - input
        - binding context ($BINDING_CONTEXT_PATH temporary file)
          - `{"binding":"onStartup"}`
        - config ($CONFIG_VALUES_PATH temporary file)
          - 'global' section in ConfigMap
          - '{moduleName}' section in ConfigMap
        - values ($VALUES_PATH temporary file)
          - 'global values' merged from:
            - 'global' section in modules/values.yaml
            - 'global' section in ConfigMap
            - patched with patches saved from previous global hooks
            - extra field 'global.enabledModules' contains a list of **all** enabled modules created by 'discover modules' step (4)
          - 'module values' merged from:
            - '{moduleName}' section in modules/values.yaml
            - '{moduleName}' section in modules/{moduleName}/values.yaml
            - '{moduleName}' section in ConfigMap
            - patched with patches saved from previous module hooks temporary file
      - output
        - config patches ($CONFIG_VALUES_JSON_PATCH_PATH temporary file)
          - applied to ConfigMap just after hook run
        - values patches ($VALUES_JSON_PATCH_PATH temporary file)
          - saved in memory
      - events after execution
        - values changes do not trigger an event
    - execute module hooks with 'kubernetes' bindings ordered in alphabetic order
      - a hook executes several times for each defined 'kubernetes' binding
      - input
        - binding context ($BINDING_CONTEXT_PATH temporary file)
          - `"type": "Synchronization"`
            - `"objects"` contains all existed objects
            - `"snapshots"` contains existed objects from previous bindings
        - config ($CONFIG_VALUES_PATH temporary file)
          - 'global' section in ConfigMap
          - '{moduleName}' section in ConfigMap
        - values ($VALUES_PATH temporary file)
          - 'global values' merged from:
            - 'global' section in modules/values.yaml
            - 'global' section in ConfigMap
            - patched with patches saved from previous global hooks
            - extra field 'global.enabledModules' contains a list of **all** enabled modules created by 'discover modules' step (4)
          - 'module values' merged from:
            - '{moduleName}' section in modules/values.yaml
            - '{moduleName}' section in modules/{moduleName}/values.yaml
            - '{moduleName}' section in ConfigMap
            - patched with patches saved from previous module hooks
      - output
        - config patches ($CONFIG_VALUES_JSON_PATCH_PATH temporary file)
          - applied to ConfigMap just after the hook execution
        - values patches ($VALUES_JSON_PATCH_PATH temporary file)
          - saved in memory
      - events after execution
        - values changes do not trigger an event
  - execute module hooks with 'beforeHelm' binding ordered by the ORDER value (see [beforeHelm](HOOKS.md#beforehelm))
    - input
      - binding context ($BINDING_CONTEXT_PATH temporary file)
        - `{"binding":"beforeHelm"}`
        - extra field `"snaphots"` contains existed objects from all 'kubernetes' bindings of this hook
      - config ($CONFIG_VALUES_PATH temporary file)
        - 'global' section in ConfigMap
        - '{moduleName}' section in ConfigMap
      - values ($VALUES_PATH temporary file)
        - 'global values' merged from:
          - 'global' section in modules/values.yaml
          - 'global' section in ConfigMap
          - patched with patches saved from previous global hooks
          - extra field 'global.enabledModules' contains a list of **all** enabled modules created by 'discover modules' step (4)
        - 'module values' merged from:
          - '{moduleName}' section in modules/values.yaml
          - '{moduleName}' section in modules/{moduleName}/values.yaml
          - '{moduleName}' section in ConfigMap
          - patched with patches saved from previous module hooks
    - output
       - config patches ($CONFIG_VALUES_JSON_PATCH_PATH temporary file)
        - applied to ConfigMap just after the hook execution
       - values patches ($VALUES_JSON_PATCH_PATH temporary file)
        - saved in memory
    - events after execution
      - values changes do not trigger an event
  - check if `helm upgrade` should run
    - get saved checksum from last release values
    - render templates
      - if checksum is changed → helm release should be upgraded
    - get helm resources defined in templates
      - if there are absent resources → helm release should be upgraded
  - run `helm upgrade --install`
    - values (unique file in a temporary directory)
      - 'global values' merged from:
        - 'global' section in modules/values.yaml
        - 'global' section in ConfigMap
        - patched with patches saved from previous global hooks
      - 'module values' merged from:
        - '{moduleName}' section in modules/values.yaml
        - '{moduleName}' section in /modules/{moduleName}/values.yaml
        - '{moduleName}' section in ConfigMap
        - patched with patches saved from previous module hooks
    - release name
      - module name in kebab-case
      - name from Chart.yaml is ignored
    - namespace
      - $ADDON_OPERATOR_NAMESPACE (see [RUNNING](RUNNING.md))
  - execute module hooks with 'afterHelm' binding ordered by the ORDER value (see [afterHelm](HOOKS.md#afterhelm))
    - input
      - binding context ($BINDING_CONTEXT_PATH temporary file)
        - `{"binding":"afterHelm"}`
        - extra field `"snaphots"` contains existed objects from all 'kubernetes' bindings of this hook
      - config ($CONFIG_VALUES_PATH temporary file)
        - 'global' section in ConfigMap
        - '{moduleName}' section in ConfigMap
      - values ($VALUES_PATH temporary file)
        - 'global values' merged from:
          - 'global' section in modules/values.yaml
          - 'global' section in ConfigMap
          - patched with patches saved from previous global hooks
          - extra field 'global.enabledModules' contains a list of **all** enabled modules created by 'discover modules' step (4)
        - 'module values' merged from:
          - '{moduleName}' section in modules/values.yaml
          - '{moduleName}' section in modules/{moduleName}/values.yaml
          - '{moduleName}' section in ConfigMap
          - patched with patches saved from previous module hooks
    - output
      - config patches ($CONFIG_VALUES_JSON_PATCH_PATH temporary file)
        - applied to ConfigMap just after the hook execution
      - values patches ($VALUES_JSON_PATCH_PATH temporary file)
        - saved in memory
    - events after execution
      - if module values are changed, restart 'module run'                
  
<a name="module-delete"></a>6. 'module delete' for each disabled module
  - run `helm delete --purge`
  - execute module hooks with 'afterDeleteHelm' binding ordered by the ORDER value (see [afterDeleteHelm](HOOKS.md#afterdeletehelm))
    - input
      - binding context ($BINDING_CONTEXT_PATH temporary file)
        - `{"binding":"afterDeleteHelm"}`
        - extra field `"snaphots"` contains existed objects from all 'kubernetes' bindings of this hook
      - config ($CONFIG_VALUES_PATH temporary file)
        - 'global' section in ConfigMap
        - '{moduleName}' section in ConfigMap
      - values ($VALUES_PATH temporary file)
        - 'global values' merged from:
          - 'global' section in modules/values.yaml
          - 'global' section in ConfigMap
          - patched with patches saved from previous global hooks
          - extra field 'global.enabledModules' contains a list of **all** enabled modules created by 'discover modules' step (4)
        - 'module values' merged from:
          - '{moduleName}' section in modules/values.yaml
          - '{moduleName}' section in modules/{moduleName}/values.yaml
          - '{moduleName}' section in ConfigMap
          - patched with patches saved from previous module hooks
    - output
      - config patches ($CONFIG_VALUES_JSON_PATCH_PATH temporary file)
        - applied to ConfigMap just after the hook execution
      - values patches ($VALUES_JSON_PATCH_PATH temporary file)
        - saved in memory
    - events after execution
      - values changes do not trigger an event
  
<a name="module-purge"></a>7. 'module purge' for each non-existent module  
  - run `helm delete --purge`
  
<a name="global-afterall"></a>8. execute global hooks with 'afterAll' binding ordered by the ORDER value (see [afterAll](HOOKS.md#afterall))
  - input
    - binding context ($BINDING_CONTEXT_PATH temporary file)
      - `{"binding":"afterAll"}`
      - extra field "snapshots" contains existed objects from all 'kubernetes' bindings of this hook
    - config ($CONFIG_VALUES_PATH temporary file)
      - 'global' section in ConfigMap
    - values ($VALUES_PATH temporary file)
      - 'global values' merged from:
        - 'global' section in modules/values.yaml
        - 'global' section in ConfigMap
        - patched with patches saved from previous global hooks
  - output
    - config patches ($CONFIG_VALUES_JSON_PATCH_PATH temporary file)
      - applied to ConfigMap just after the hook execution
    - values patches ($VALUES_JSON_PATCH_PATH temporary file)
      - saved in memory
  - events after execution
    - if values are changed, re-run 'Reload all modules' steps 3 to 8.

**Reaction to events**

<a name="global-kubernetes-event"></a>9. 'kubernetes' event for *global* hook (see [kubernetes](HOOKS.md#kubernetes))
  - hook execution is queued in "main" or in a named queue according to the binding configuration
  - queue handler runs a hook:
    - input
      - binding context ($BINDING_CONTEXT_PATH temporary file)
        - `"type": "Event"`
          - `"object"` contains a related object
          - `"filterResult"` contains a result of jqFilter
          - `"snapshots"` contains existed objects from other bindings
      - config ($CONFIG_VALUES_PATH temporary file)
        - 'global' section in ConfigMap
      - values ($VALUES_PATH temporary file)
        - 'global values' merged from:
          - 'global' section in modules/values.yaml
          - 'global' section in ConfigMap
          - patched with patches saved from previous global hooks
    - output
      - config patches ($CONFIG_VALUES_JSON_PATCH_PATH temporary file)
        - applied to ConfigMap just after the hook execution
      - values patches ($VALUES_JSON_PATCH_PATH temporary file)
        - saved in memory
    - events after execution
      - 'global values changed' if global section or *Enabled flags are changed

<a name="module-kubernetes-event"></a>10. 'kubernetes' event for *module* hook (see [kubernetes](HOOKS.md#kubernetes))
  - hook execution is queued in "main" or in a named queue according to the binding configuration
  - queue handler runs a hook:
    - input
      - binding context ($BINDING_CONTEXT_PATH temporary file)
        - `"type": "Event"`
          - `"object"` contains a related object
          - `"filterResult"` contains a result of jqFilter
          - `"snapshots"` contains existed objects from other bindings
      - config ($CONFIG_VALUES_PATH temporary file)
        - 'global' section in ConfigMap
        - '{moduleName}' section in ConfigMap
      - values ($VALUES_PATH temporary file)
        - 'global values' merged from:
          - 'global' section in modules/values.yaml
          - 'global' section in ConfigMap
          - patched with patches saved from previous global hooks
          - extra field 'global.enabledModules' contains a list of **all** enabled modules created by 'discover modules' step (4)
        - 'module values' merged from:
          - '{moduleName}' section in modules/values.yaml
          - '{moduleName}' section in modules/{moduleName}/values.yaml
          - '{moduleName}' section in ConfigMap
          - patched with patches saved from previous module hooks
    - output
      - config patches ($CONFIG_VALUES_JSON_PATCH_PATH temporary file)
        - applied to ConfigMap just after the hook execution
      - values patches ($VALUES_JSON_PATCH_PATH temporary file)
        - saved in memory
    - events after execution
      - 'global values changed' if global values are changed
      - 'modules values changed' if module values are changed

<a name="global-schedule-event"></a>11. 'schedule' event for *global* hook (see [schedule](HOOKS.md#schedule))
  - hook execution is queued in "main" or in a named queue according to the binding configuration
  - queue handler runs a hook:
    - input
      - binding context ($BINDING_CONTEXT_PATH temporary file)
        - `"snapshots"` contains existed objects from other bindings
      - config ($CONFIG_VALUES_PATH temporary file)
        - 'global' section in ConfigMap
      - values ($VALUES_PATH temporary file)
        - 'global values' merged from:
          - 'global' section in modules/values.yaml
          - 'global' section in ConfigMap
          - patched with patches saved from previous global hooks
    - output
      - config patches ($CONFIG_VALUES_JSON_PATCH_PATH temporary file)
        - applied to ConfigMap just after the hook execution
      - values patches ($VALUES_JSON_PATCH_PATH temporary file)
        - saved in memory
    - events after execution
      - 'global values changed' if global section or *Enabled flags are changed

<a name="module-schedule-event"></a>12. 'schedule' event for *module* hook (see [schedule](HOOKS.md#schedule))
  - hook execution is queued in "main" or in a named queue according to the binding configuration
  - queue handler runs a hook:
    - input
      - binding context ($BINDING_CONTEXT_PATH temporary file)
        - `"snapshots"` contains existed objects from other bindings
      - config ($CONFIG_VALUES_PATH temporary file)
        - 'global' section in ConfigMap
        - '{moduleName}' section in ConfigMap
      - values ($VALUES_PATH temporary file)
        - 'global values' merged from:
          - 'global' section in modules/values.yaml
          - 'global' section in ConfigMap
          - patched with patches saved from previous global hooks
          - extra field 'global.enabledModules' contains a list of **all** enabled modules created by 'discover modules' step (4)
        - 'module values' merged from:
          - '{moduleName}' section in modules/values.yaml
          - '{moduleName}' section in modules/{moduleName}/values.yaml
          - '{moduleName}' section in ConfigMap
          - patched with patches saved from previous module hooks
    - output
      - config patches ($CONFIG_VALUES_JSON_PATCH_PATH temporary file)
        - applied to ConfigMap just after the hook execution
      - values patches ($VALUES_JSON_PATCH_PATH temporary file)
        - saved in memory
    - events after execution
      - trigger 'global values changed' if global values are changed
      - trigger 'modules values changed' if module values are changed

<a name="global-values-changed"></a>13. 'global values changed' event (see [global hook](HOOKS.md#global-hook))
  - create 'Reload all modules' task in the "main" queue (steps 3 to 8)

<a name="module-values-changed"></a>14. 'module values changed' event (see [module hook](HOOKS.md#module-hook))
  - create 'module run' task in the "main" queue
    - step 5 without `onStartup` and `kubernetes@Synchronization` hooks
  
<a name="helm-resources-absent"></a>15. 'helm resources absent' event (see [auto-healing](MODULES.md#release-auto-healing))
  - create 'module run' task in the "main" queue
    - step 5 without `onStartup` and `kubernetes@Synchronization` hooks

<a name="configmap-changed"></a>16. ConfigMap is changed (see [ConfigMap/addon-operator](VALUES.md#configmapaddon-operator))
  - values in global section are changed
    - create 'Reload all modules' task in the "main" queue (steps 3 to 8)
  - *Enabled flags are changed
    - create 'Reload all modules' task in the "main" queue (steps 3 to 8)
  - values in modules sections are changed
    - create 'module run' task in the "main" queue
      - step 5 without `onStartup` and `kubernetes@Synchronization` hooks
