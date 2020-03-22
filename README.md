<p align="center">
<img width="407" src="docs/logo-addon.png" alt="Addon-operator logo" />
</p>

<p align="center">
<a href="https://hub.docker.com/r/flant/addon-operator"><img src="https://img.shields.io/badge/docker-latest-2496ed.svg?logo=docker" alt="docker pull flant/addon-operator"/></a>
<a href="https://cloud-native.slack.com/messages/CJ13K3HFG"><img src="https://img.shields.io/badge/slack-EN%20chat-611f69.svg?logo=slack" alt="Slack chat EN"/></a>
<a href="https://t.me/kubeoperator"><img src="https://img.shields.io/badge/telegram-RU%20chat-179cde.svg?logo=telegram" alt="Telegram chat RU"/></a>
</p>

The **Addon-operator** combines Helm charts with hooks and values storage to transform charts into smart modules that configure themselves and respond to changes in the cluster.

# Features

- **Discovery of values** for Helm charts — parameters can be generated, calculated or retrieved from the cluster;
- **Continuous discovery** — parameters can be changed in response to cluster events;
- **Controlled Helm execution** — Addon-operator monitors the Helm operation to ensure the Helm chart’s successful installation. Coming soon: use kubedog to track deploy status and more;
- **Custom extra actions before and after running Helm** as well as any other events via the hooks paradigm. See related [shell-operator capabilities](https://github.com/flant/shell-operator/blob/master/HOOKS.md).

Additionally, Addon-operator provides:

- ease of maintenance of Kubernetes clusters: use the tools that Ops are familiar with to build your modules and hooks such as bash, kubectl, python, etc;
- the execution queue of modules and hooks that ensures the launch sequence and repeated execution in case of an error, which *simplifies programming of modules* and ensures *predictable outcome* of their operation;
- the possibility of *dynamic enabling/disabling* of a module (depending on detected parameters);
- the ability to tie *conditions of module activation* to the activation of other modules;
- *the unified ConfigMap* for the configuration of all settings;
- the ability to run Helm only if parameters have changed. In this case, `helm history` would output only releases with changes;
- *global hooks* for figuring out parameters and performing actions that affect several dependent modules;
- off-the-shelf *metrics* for monitoring via Prometheus.

# Overview

## Hooks and Helm Values

Hooks are triggered by Kubernetes events and in response to other stimuli.

![Hooks are triggered by Kubernetes events](docs/readme-1.gif)

A hook is an executable file that can make changes to Kubernetes and set values of Helm (they are stored in the memory of Addon-operator) during execution

![A hook is an executable file](docs/readme-2.gif)

Hooks are a part of the module. Also, there is a Helm chart in the module. If the hook makes changes to values, then Addon-operator would upgrade the release of the Helm chart.

![Hook is a part of the module](docs/readme-3.gif)

## Modules

There can be many modules.

![Many modules](docs/readme-4.gif)

In addition to modules, the Addon-operator supports global hooks and global values. They have a storage of values. Global hooks are triggered by events and when active they can:

- Make changes to Kubernetes cluster
- Make changes to global values storage

![Global hooks and global values](docs/readme-5.gif)

If the global hook changes values in the global storage, then the Addon-operator triggers an upgrade of releases of all Helm charts.

![Changes in global values cause reinstallation](docs/readme-6.gif)

# Installation

You may use a prepared image [flant/addon-operator](https://hub.docker.com/r/flant/addon-operator) to install Addon-operator in a cluster. The image comprises a binary addon-operator file as well as several required tools: helm, tiller, kubectl, jq, bash.

The installation incorporates the image building process with *files of modules and hooks*, applying the necessary RBAC rights and deploying the image in the cluster.

To experiment with modules, hooks, and values we've prepared some [examples](/examples).

# What's next?

- Find out more on [lifecycle](LIFECYCLE.md) of Addon-operator and how to use [modules](MODULES.md), [hooks](HOOKS.md) and [values](VALUES.md).
- See [METRICS](METRICS.md) on how to monitor Addon-operator.
- Explore Shell-operator documentation, especially [hooks](https://github.com/flant/shell-operator/blob/master/HOOKS.md) section.
- Discover how to tune [deploy settings](RUNNING.md).

## License

Apache License 2.0, see [LICENSE](LICENSE).
