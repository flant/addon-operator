<p align="center">
<img src="image/logo-addon-operator-small.png" alt="addon-operator logo" />
</p>

<p align="center">
<a href="https://hub.docker.com/r/flant/addon-operator"><img src="https://img.shields.io/badge/docker-latest-2496ed.svg?logo=docker" alt="docker pull flant/addon-operator"/></a>
<a href="https://github.com/flant/addon-operator/discussions"><img src="https://img.shields.io/badge/GitHub-discussions-brightgreen" alt="GH Discussions"/></a>
<a href="https://t.me/kubeoperator"><img src="https://img.shields.io/badge/telegram-RU%20chat-179cde.svg?logo=telegram" alt="Telegram chat RU"/></a>
</p>

# Installation

You may use a prepared image [flant/addon-operator][docker-hub] to install addon-operator in a cluster. The image comprises a binary `addon-operator` file as well as several required tools: `helm`, `tiller`, `kubectl`, `jq`, `bash`.

The installation incorporates the image building process with *files of modules and hooks*, applying the necessary RBAC rights and deploying the image in the cluster.

## Examples

To experiment with modules, hooks, and values we've prepared some [examples][examples].

[Deckhouse Kubernetes Platform][deckhouse] was an initial reason to create addon-operator, thus [its modules][deckhouse-modules] might become a vital source of inspiration for implementing your own modules.

Sharing your examples of using addon-operator is much appreciated. Please, use the [relevant Discussions section][discussions-sharing] for that.

# Community

Please feel free to reach developers/maintainers and users via [GitHub Discussions][discussions] for any questions regarding addon-operator.

You're also welcome to follow [@flant_com][twitter] to stay informed about all our Open Source initiatives.

# License

Apache License 2.0, see [LICENSE][license].

[deckhouse]: https://deckhouse.io/
[deckhouse-modules]: https://github.com/deckhouse/deckhouse/tree/main/modules
[discussions]: https://github.com/flant/addon-operator/discussions
[discussions-sharing]: https://github.com/flant/addon-operator/discussions/categories/show-and-tell
[docker-hub]: https://hub.docker.com/r/flant/addon-operator
[examples]: https://github.com/flant/addon-operator/tree/main/examples
[license]: https://github.com/flant/addon-operator/blob/main/LICENSE
[twitter]: https://twitter.com/flant_com
