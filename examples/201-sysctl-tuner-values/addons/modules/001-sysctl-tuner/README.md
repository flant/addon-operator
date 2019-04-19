sysctl-tuner module
===================

This module periodically applying sysctl parameters on nodes.

Module is run in DaemonSet in privileged containers and apply
parameters every 5 min.

Use cm/addon-operator to define additional parameters or to
override parameters in values.yaml

