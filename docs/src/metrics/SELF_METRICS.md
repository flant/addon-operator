# Metrics

* `addon_operator_binding_count{module="", hook=""}` — a gauge with bindings count for every hooks. Global hooks has empty "module" label.

* `addon_operator_config_values_errors_total{}` — a counter of ConfigMap validation errors after `kubectl edit`. See [validation](VALUES.md#validation).

* `addon_operator_global_hook_run_seconds{hook="", binding="", activation="", queue=""}` — a histogram with hook execution times. "hook" label is a name of the hook, "binding" is a binding name from configuration, "queue" is a queue name where hook is queued and "activation" is an event that triggers hook execution.
* `addon_operator_global_hook_run_errors_total{hook="", binding="", activation="", queue=""}` – this is the counter of hooks’ execution errors. It only tracks errors of hooks with the disabled `allowFailure` (i.e. respective key is omitted in the configuration or the `allowFailure: false` parameter is set). This metric has a "hook" label with the name of a failed hook.
* `addon_operator_global_hook_run_allowed_errors_total{hook="", binding="", activation="", queue=""}` – this is the counter of hooks’ execution errors. It only tracks errors of hooks that are allowed to exit with an error (the parameter `allowFailure: true` is set in the configuration). The metric has a "hook" label with the name of a failed hook.
* `addon_operator_global_hook_run_success_total{hook="", binding="", activation="", queue=""}` – this is the counter of hooks’ success execution. The metric has a "hook" label with the name of a succeeded hook.
* `addon_operator_global_hook_run_sys_cpu_seconds{hook="", binding="", activation="", queue=""}` — a histogram with global hook system cpu seconds.
* `addon_operator_global_hook_run_user_cpu_seconds{hook="", binding="", activation="", queue=""}` — a histogram with global hook user cpu seconds.
* `addon_operator_global_hook_run_max_rss_bytes{hook="", binding="", activation="", queue=""}` — a gauge with global hook max rss usage in bytes.

* `addon_operator_module_hook_run_seconds{module="", hook="", binding="", activation="", queue=""}` — a histogram with module hook execution times. "module" label is a name of the module, "hook" label is a name of the hook, "binding" is a binding name from configuration, "queue" is a queue name where hook is queued and "activation" is an event that triggers hook execution.
* `addon_operator_module_hook_run_errors_total{module="", hook="", binding="", activation="", queue=""}` – this is the counter of hooks’ execution errors. It only tracks errors of hooks with the disabled `allowFailure` (i.e. respective key is omitted in the configuration or the `allowFailure: false` parameter is set). This metric has a "hook" label with the name of a failed hook.
* `addon_operator_module_hook_run_allowed_errors_total{module="", hook="", binding="", activation="", queue=""}` – this is the counter of hooks’ execution errors. It only tracks errors of hooks that are allowed to exit with an error (the parameter `allowFailure: true` is set in the configuration). The metric has a "hook" label with the name of a failed hook.
* `addon_operator_module_hook_run_success_total{module="", hook="", binding="", activation="", queue=""}` – this is the counter of hooks’ success execution. The metric has a "hook" label with the name of a succeeded hook.
* `addon_operator_module_hook_run_sys_cpu_seconds{module="", hook="", binding="", activation="", queue=""}` — a histogram with module hook system cpu seconds.
* `addon_operator_module_hook_run_user_cpu_seconds{module="", hook="", binding="", activation="", queue=""}` — a histogram with module hook user cpu seconds.
* `addon_operator_module_hook_run_max_rss_bytes{module="", hook="", binding="", activation="", queue=""}` — a gauge with module hook max rss usage in bytes.

* `addon_operator_module_discover_errors_total` – a counter of errors during the [modules discover](LIFECYCLE.md#modules-discover) process. It increases in these cases:
    * an 'enabled' script is executed with an error
    * a module hook return an invalid configuration
    * a call to the Kubernetes API ends with an error (for example, retrieving Helm releases).
* `addon_operator_module_run_errors_total{module=x}` – counter of errors on module [start-up](LIFECYCLE.md#modules-lifecycle).
* `addon_operator_module_delete_errors_total{module=x}` – counter of errors on module [deletion](LIFECYCLE.md#modules-lifecycle).
* `addon_operator_module_run_seconds{module=""}` — a histogram with module execution timings.
* `addon_operator_module_helm_seconds{module="", activation=""}` — a histogram of module’s `helm upgrade` timings.
* `addon_operator_helm_operation_seconds{module="", activation="", operation=""}` — a histogram of different helm operations timings.

* `addon_operator_convergence_seconds{activation=onStartup}` — a counter of seconds spent to execute "reload all modules" processes. "activation=OnStartup" label value can be used to retrieve information about first "reload all modules" when operator starts.
* `addon_operator_convergence_total{activation=onStartup}` — a counter of "reload all modules" processes.

* `addon_operator_tasks_queue_length{queue=""}` – a gauge showing the length of the working queue. This metric can be used to warn about stuck hooks. It has the "queue" label with the queue name.

* `addon_operator_task_wait_in_queue_seconds_total{module="", hook="", binding="", queue=""}` — a counter with seconds that the task is elapsed in the queue.

* `addon_operator_live_ticks` – a counter that increases every 10 seconds. This metric can be used for alerting about an unhealthy Addon-operator. It has no labels.


* `addon_operator_kube_jq_filter_duration_seconds{module="", hook="", binding="", queue="", kind=""}` — a histogram with jq filter timings.

* `addon_operator_kube_event_duration_seconds{module="", hook="", binding="", queue="", kind=""}` — a histogram with kube event handling timings.

* `addon_operator_kube_snapshot_objects{module="", hook="", binding="", queue=""}` — a gauge with count of cached objects (the snapshot) for particular binding. "module" label is empty for global hook.

* `addon_operator_kube_snapshot_bytes{module="", hook="", binding="", queue=""}` — a gauge with size in bytes of cached objects for particular binding. Each cached object contains a Kubernetes object and/or result of jqFilter depending on the binding configuration. The size is a sum of the length of Kubernetes object in JSON format and the length of jqFilter‘s result in JSON format.

* `addon_operator_kubernetes_client_request_result_total` — a counter of requests made by kubernetes/client-go library.

* `addon_operator_kubernetes_client_request_latency_seconds` — a histogram with latency of requests made by kubernetes/client-go library.

* `addon_operator_tasks_queue_action_duration_seconds{queue_name="", queue_action=""}` — a histogram with measurements of low level queue operations. Use QUEUE_ACTIONS_METRICS="no" to disable this metric.
