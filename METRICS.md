# Addon-operator metrics

Addon-operator implements prometheus target at /metrics endpoint. Default port is 9115.

__addon_operator_live_ticks__

A counter that increments every 10 seconds. Can be used for alerting about shell-operator malfunctioning.

__addon_operator_tasks_queue_length__

A gauge with a length of a working queue. Can be used for alerting about long running hooks.


__addon_operator_module_discover_errors__
 
A counter of errors in dynamic modules discovery. Counter increases on errors of `enabled` script execution, errors of module hooks configuration, errors of listing helm releases or in case of Kubernetes API errors.

__addon_operator_module_run_errors{module=x}__
 
A counter of module run errors.

__addon_operator_module_delete_errors{module=x}__
 
A counter of module deletion errors.

__addon_operator_module_hook_errors{module=x, hook=y}__

A counter of module hook execution errors. It counts errors for hooks with disabled `allowFailure` (no key in configuration or explicit `allowFailure: false`).

__addon_operator_module_hook_allowed_errors{module=x, hook=y}__
 
A counter of module hook execution errors. It counts errors for hooks that allowed to fail — `allowFailure: true`.

__addon_operator_global_hook_errors{hook=y}__

A counter of global hook execution errors. It counts errors for hooks with disabled `allowFailure` (no key in configuration or explicit `allowFailure: false`).

__addon_operator_global_hook_allowed_errors{hook=y}__

A counter of global hook execution errors. It counts errors for hooks that allowed to fail — `allowFailure: true`.
