# Addon-operator metrics

The Addon-operator implements Prometheus target at `/metrics` endpoint. The default port is `9650`.

* `addon_operator_module_hook_errors{module="module-name", hook="hook-name"}` – a counter of hooks’ execution errors with `allowFailure: false`.
* `addon_operator_module_hook_allowed_errors{module="module-name", hook="hook-name"}` – a counter of execution errors of module hooks with `allowFailure: true`.
* `addon_operator_global_hook_errors{hook="hook-name"}` – a counter of execution errors of global hooks with `allowFailure: false`.
* `addon_operator_global_hook_allowed_errors{hook="hook-name"}` – a counter of execution errors of global hooks with `allowFailure: true`.
* `addon_operator_module_discover_errors` – a counter of errors during the [modules discover](LIFECYCLE.md#modules-discover) process. It increases in these cases:
  * an 'enabled' script is executed with an error
  * a module hook return an invalid configuration
  * a call to the Kubernetes API ends with an error (for example, retrieving Helm releases).
* `addon_operator_module_run_errors{module=x}` – counter of errors on module [start-up](LIFECYCLE.md#modules-lifecycle).
* `addon_operator_module_delete_errors{module=x}` – counter of errors on module [deletion](LIFECYCLE.md#modules-lifecycle).
* `addon_operator_tasks_queue_length` – an indicator of a working queue length. This metric can be used to warn about stuck hooks. It has no labels.
* `addon_operator_live_ticks` – a counter that increases every 10 seconds. Used to verify that the main Addon-operator process is not stuck.
