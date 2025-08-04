<p align="center">
<img src="docs/src/image/logo-addon-operator-small.png" alt="addon-operator logo" />
</p>

<p align="center">
<a href="https://hub.docker.com/r/flant/addon-operator"><img src="https://img.shields.io/badge/docker-latest-2496ed.svg?logo=docker" alt="docker pull flant/addon-operator"/></a>
<a href="https://github.com/flant/addon-operator/discussions"><img src="https://img.shields.io/badge/GitHub-discussions-brightgreen" alt="GH Discussions"/></a>
<a href="https://t.me/kubeoperator"><img src="https://img.shields.io/badge/telegram-RU%20chat-179cde.svg?logo=telegram" alt="Telegram chat RU"/></a>
</p>

**Addon-operator** combines Helm charts with hooks and values storage to transform charts into smart modules that configure themselves and respond to changes in the cluster. It is a sister project for [shell-operator](https://github.com/flant/shell-operator) and is actively used in [Deckhouse Kubernetes Platform](https://github.com/deckhouse/deckhouse) to implement its modules.

# Features

- **Discovery of values** for Helm charts — parameters can be generated, calculated or retrieved from the cluster;
- **Continuous discovery** — parameters can be changed in response to cluster events;
- **Controlled Helm execution** — addon-operator monitors the Helm operation to ensure the Helm chart’s successful installation. Coming soon: use kubedog to track deploy status and more;
- **Custom extra actions before and after running Helm** as well as any other events via the hooks paradigm. See related [shell-operator capabilities](https://github.com/flant/shell-operator/blob/master/HOOKS.md).

Additionally, addon-operator provides:

- ease of maintenance of Kubernetes clusters: use the tools that Ops are familiar with to build your modules and hooks such as Bash, kubectl, Python, etc;
- the execution queue of modules and hooks that ensures the launch sequence and repeated execution in case of an error, which *simplifies programming of modules* and ensures *predictable outcome* of their operation;
- the possibility of *dynamic enabling/disabling* of a module (depending on detected parameters);
- the ability to tie *conditions of module activation* to the activation of other modules;
- *the unified ConfigMap* for the configuration of all settings;
- the ability to run Helm only if parameters have changed. In this case, `helm history` would output only releases with changes;
- *global hooks* for figuring out parameters and performing actions that affect several dependent modules;
- off-the-shelf *metrics* for monitoring via Prometheus.

# Documentation

Please see the [docs](https://flant.github.io/addon-operator/) for more in-depth information and supported features.

# Installation

You may use a prepared image [flant/addon-operator](https://hub.docker.com/r/flant/addon-operator) to install addon-operator in a cluster. The image comprises a binary `addon-operator` file as well as several required tools: `helm`, `kubectl`, `jq`, `bash`.

The installation incorporates the image building process with *files of modules and hooks*, applying the necessary RBAC rights and deploying the image in the cluster.

## Examples

To experiment with modules, hooks, and values we've prepared some [examples](/examples).

[Deckhouse Kubernetes Platform](https://deckhouse.io/) was an initial reason to create addon-operator, thus [its modules](https://github.com/deckhouse/deckhouse/tree/main/modules) might become a vital source of inspiration for implementing your own modules.

Sharing your examples of using addon-operator is much appreciated. Please, use the [relevant Discussions section](https://github.com/flant/addon-operator/discussions/categories/show-and-tell) for that.

# What's next?

Explore [shell-operator](https://github.com/flant/shell-operator) documentation, especially its [hooks](https://github.com/flant/shell-operator/blob/main/docs/src/HOOKS.md) section.

# Community

Please feel free to reach developers/maintainers and users via [GitHub Discussions](https://github.com/flant/addon-operator/discussions) for any questions regarding addon-operator.

You're also welcome to follow [@flant_com](https://twitter.com/flant_com) to stay informed about all our Open Source initiatives.

# License

Apache License 2.0, see [LICENSE](LICENSE).

# Kubernetes Resource Management Script

Этот скрипт предоставляет удобный способ управления Kubernetes ресурсами с возможностью создания, мутации и удаления подов, ConfigMaps, сервисов, а также управления лейблами на default storage class.

## Возможности

- ✅ Создание подов, ConfigMaps и сервисов
- ✅ Пакетное создание ресурсов
- ✅ Мутация ресурсов в цикле с добавлением лейблов
- ✅ Удаление ресурсов по типам или все сразу
- ✅ Управление лейблами на default storage class
- ✅ Резервное копирование ресурсов
- ✅ Цветной вывод для лучшего UX
- ✅ Обработка ошибок и валидация

## Требования

- `kubectl` настроен и подключен к кластеру
- Bash shell
- Доступ к namespace (по умолчанию `default`)

## Установка

1. Сделайте скрипт исполняемым:
```bash
chmod +x script.sh
```

2. Убедитесь, что у вас есть доступ к Kubernetes кластеру:
```bash
kubectl cluster-info
```

## Использование

### Основные команды

```bash
# Показать справку
./script.sh --help

# Создать ресурсы пакетно
./script.sh create-batch

# Мутировать ресурсы 5 раз
./script.sh mutate 5

# Удалить все поды
./script.sh delete pods

# Очистить все ресурсы, созданные скриптом
./script.sh cleanup

# Показать статус ресурсов
./script.sh status

# Создать резервную копию
./script.sh backup
```

### Использование в другом namespace

```bash
# Работать в namespace "my-namespace"
./script.sh -n my-namespace create-batch
./script.sh --namespace my-namespace cleanup
```

## Что создает скрипт

### ConfigMaps
- `app-config` - конфигурация приложения
- `db-config` - конфигурация базы данных

### Services
- `web-service` (порт 80 → 8080)
- `api-service` (порт 8080 → 3000)
- `db-service` (порт 5432 → 5432)

### Pods
- `web-app` (nginx:alpine) с подключенным app-config
- `api-app` (node:16-alpine) с подключенным app-config
- `database` (postgres:13-alpine) с подключенным db-config

### Storage Class
- Добавляет лейблы `managed-by=script` и `created-by=resource-manager` к default storage class

## Мутация ресурсов

При выполнении команды `mutate` скрипт:
1. Проходит по всем ресурсам (pods, services, configmaps)
2. Добавляет лейблы с номером итерации и временной меткой
3. Повторяет процесс указанное количество раз
4. Делает паузу между итерациями

Пример лейблов после мутации:
```yaml
labels:
  mutation-iteration: "3"
  mutated-at: "1703123456"
```

## Очистка ресурсов

Команда `cleanup`:
1. Удаляет лейблы с storage class
2. Удаляет все ресурсы с лейблами `app` или `mutation-iteration`
3. Удаляет локальные файлы ресурсов

## Структура файлов

```
addon-operator/
├── script.sh                    # Основной скрипт
├── config/
│   ├── app.properties          # Конфигурация приложения
│   └── database.properties     # Конфигурация БД
├── k8s-resources/              # Создаваемые YAML файлы
└── backups/                    # Резервные копии
```

## Примеры использования

### Полный цикл работы

```bash
# 1. Создать ресурсы
./script.sh create-batch

# 2. Проверить статус
./script.sh status

# 3. Мутировать ресурсы
./script.sh mutate 3

# 4. Создать резервную копию
./script.sh backup

# 5. Очистить все
./script.sh cleanup
```

### Работа с конкретными ресурсами

```bash
# Удалить только сервисы
./script.sh delete services

# Удалить только поды
./script.sh delete pods

# Удалить только ConfigMaps
./script.sh delete configmaps
```

## Безопасность

- Скрипт использует `set -euo pipefail` для безопасного выполнения
- Все операции выполняются с флагом `--ignore-not-found=true` где это возможно
- Резервные копии создаются перед критическими операциями
- Лейблы добавляются с префиксом для идентификации ресурсов скрипта

## Логирование

Скрипт использует цветной вывод для разных типов сообщений:
- 🟢 Зеленый - информационные сообщения
- 🟡 Желтый - предупреждения
- 🔴 Красный - ошибки
- 🔵 Синий - заголовки разделов

## Troubleshooting

### Проблема: "No default storage class found"
Решение: Убедитесь, что в кластере настроен default storage class:
```bash
kubectl get storageclass
```

### Проблема: "Permission denied"
Решение: Проверьте права доступа к namespace:
```bash
kubectl auth can-i create pods -n default
```

### Проблема: "Resource already exists"
Решение: Используйте команду cleanup перед повторным созданием:
```bash
./script.sh cleanup
./script.sh create-batch
```

## Лицензия

Apache License 2.0
