# Addon-operator metrics

> **TODO** This document must be updated! 


Shell-operator implement prometheus target at /metrics endpoint. Default port is 9115.

__addon_operator_hook_allowed_errors__

__addon_operator_hook_errors__

These metrics are counters of hook execution errors. There is a label `hook` with a name of the erroneous hook. Errors of hooks with `allowFailure: true` setting are in a separate metric to ease alert definitions.


__addon_operator_tasks_queue_length__

A gauge with a length of a working queue. Can be used for alerting about long running hooks.


__addon_operator_live_ticks__

A counter that increments every 10 seconds. Can be used for alerting about shell-operator malfunctioning.
