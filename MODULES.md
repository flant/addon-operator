# Modules

# Module scructure

```
001-module
├── .helmignore
├── Chart.yaml
├── enabled
├── hooks
│   └── module-hooks.sh
├── README.md
├── templates
│   └── daemon-set.yaml
└── values.yaml
```

`hooks` directory — a place for module hooks

`enabled` script — script that is executed to determine if module should be enabled.

`values.yaml` — file with values for helm chart. This file must contain one root key with module name in camelCase and a value:
- a string "false" to disable module by default
- a map or an array

`README.md` — description of a module. It is always good to have one.

`.helmignore`, `Chart.yaml`, `templates` directory and other files are a helm chart.

# Helm execution specifics

## values.yaml

## Chart.yaml

## Releases deduplication

# Workarounds for Helm issues

# Next

- [addon-operator lifecycle](LIFECYCLE.md)
- [values](VALUES.md)
- [hooks](HOOKS.md)
