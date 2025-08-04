#!/usr/bin/env bash

set -euo pipefail

NAMESPACE="pod-cannon"
POD_COUNT=1500

echo "Creating namespace $NAMESPACE (if not exists)..."
kubectl get namespace "$NAMESPACE" >/dev/null 2>&1 || kubectl create namespace "$NAMESPACE"

echo "Creating $POD_COUNT pods in namespace $NAMESPACE..."

for i in $(seq 1 $POD_COUNT); do
  cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: cannon-pod-$i
  namespace: $NAMESPACE
  labels:
    batch: "true"
spec:
  containers:
    - name: cannon
      image: busybox
      command: ["sh", "-c", "sleep 3600"]
EOF
done

echo "Waiting for all pods to be created..."
kubectl wait --for=condition=Ready pod -l batch=true -n "$NAMESPACE" --timeout=300s || true

echo "Modifying pods in a cycle (adding annotation)..."
for i in $(seq 1 $POD_COUNT); do
  kubectl annotate pod cannon-pod-$i -n "$NAMESPACE" "pod-cannon/modified=$(date +%s%N)" --overwrite
done

echo "Done."
