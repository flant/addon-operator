# Hooks

A hook is a script in any language you prefer with an added events configuration code.

# Global hooks initialization

# Module hooks initialization

# Bindings

## Overview

| Binding  | Global? | Module? | Info |
| ------------- | ------------- | --- | --- |
| [onStartup](#onstartup)↗  | ✓ | – | On Addon-operator startup |
| [onStartup](#onstartup)↗  | – |  ✓ | On first module run |
| [beforeAll](#beforeall)↗ | ✓ | – | Before run all modules | 
| [afterAll](#afterall)↗ | ✓ | – | After run all modules | 
| [beforeHelm](#beforehelm)↗ | – | ✓ | Before run helm install | 
| [afterHelm](#afterhelm)↗ | – | ✓ | After helm install | 
| [afterDeleteHelm](#afterdeletehelm)↗ | – | ✓ | After run helm delete | 
| [schedule](#schedule)↗ | ✓ | ✓ | Run on schedule | 
| [onKubernetesEvent](#onkubernetesevent)↗ | ✓ | ✓ | Run on event from Kubernetes | 

## onStartup

## beforeAll

## afterAll

## beforeHelm

## afterHelm

## afterDeleteHelm

## schedule

[schedule binding](https://github.com/flant/shell-operator/blob/master/HOOKS.md#schedule)

## onKubernetesEvent

[onKubernetesEvent binding](https://github.com/flant/shell-operator/blob/master/HOOKS.md#onKubernetesEvent)

# Execution on event

## Binding context
