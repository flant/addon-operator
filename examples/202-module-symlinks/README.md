## Use multiple directories for modules with symlinks

Example of a multi directory setup. The 'backend' module resides in the 'module_dir_1' and the 'frontend' module resides in the 'module_storage' directory and has a symlink in the 'module_dir_2' directory.

```
├── module_dir_1
│   └── 001-backend
│       ├── Chart.yaml
│       ├── templates
│       │   ├── deploy-backend.yaml
...
├── module_dir_2
│   └── 002-frontend -> ../module_storage/002-frontend
...
├── module_storage
│   └── 002-frontend
│       ├── Chart.yaml
│       ├── templates
│       │   ├── deploy-frontend.yaml
...
```

MODULES_DIR environment variable is used to specify two directories:

```
- name: MODULES_DIR
  value: "/module_dir_1:/module_dir_2"
```

### Run

Build addon-operator image with modules:

```
docker build -t "localhost:5000/addon-operator:example-202" .
docker push localhost:5000/addon-operator:example-202
```

Edit image in addon-operator-deploy.yaml and apply manifests:

```
kubectl create ns example-202
kubectl -n example-202 apply -f deploy/cm.yaml
kubectl -n example-202 apply -f deploy/rbac.yaml
kubectl -n example-202 apply -f deploy/deployment.yaml
```

List Pods to see that 'frontend' and 'backend' are installed:

```
$ kubectl -n example-202 get po

NAME                              READY   STATUS    RESTARTS   AGE
addon-operator-5c9df6d4b8-pj9nt   1/1     Running   0          16s
backend-5b764d4464-rdxq4          1/1     Running   0          2m40s
frontend-d8677b8d4-8wkqh          1/1     Running   0          13s
```

See also enabled modules:

```
$ kubectl -n example-202 exec deploy/addon-operator -- /addon-operator module list

{"enabledModules":["backend","frontend"]}
```

### Cleanup

```
kubectl delete clusterrolebinding/addon-operator-202
kubectl delete clusterrole/addon-operator-202
kubectl delete ns/example-202
docker rmi localhost:5000/addon-operator:example-202
```
