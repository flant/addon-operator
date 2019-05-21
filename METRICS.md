# Addon-operator metrics

Addon-operator implements Prometheus target at `/metrics` endpoint. Default port is 9115.

__addon_operator_module_hook_errors{module=”module-name”, hook="hook-name"}__

The counter of hooks’ execution errors. It only tracks errors of hooks with the disabled `allowFailure` (allowFailure: false).


__addon_operator_module_hook_allowed_errors{module=”module-name”, hook="hook-name"}__

The counter of execution errors of module hooks for which execution errors are allowed (allowFailure: true).


__addon_operator_global_hook_errors{hook="hook-name"}__

The counter of execution errors of global hooks for which execution errors are not allowed (allowFailure: false).


__addon_operator_global_hook_allowed_errors{hook="hook-name"}__

The counter of execution errors of global hooks for which execution errors are allowed (allowFailure: true).


__addon_operator_module_discover_errors__
A counter of errors during the [modules discover](LIFECYCLE.md#modules-discover) process. It increases every time when there are errors in the enabled-scripts running, configuration of module hooks, errors when viewing the helm releases or accessing the K8s API.


__addon_operator_module_run_errors{module=x}__
Counter of error on module [start-up](LIFECYCLE.md#modules-lifecycle).

__addon_operator_module_delete_errors{module=x}__
Counter of errors on module [deletion](LIFECYCLE.md#modules-lifecycle).


__addon_operator_tasks_queue_length__

An indicator of a working queue length. This metric can be used to warn about stuck hooks. It has no labels.

__addon_operator_live_ticks__

A counter that increases every 10 seconds.

