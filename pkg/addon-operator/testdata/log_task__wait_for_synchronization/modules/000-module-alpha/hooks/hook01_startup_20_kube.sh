#!/usr/bin/env bash

if [[ $1 == "--config" ]] ; then
cat <<EOF
configVersion: v1
kubernetes:
- name: monitor-pods
  kind: Pod
  queue: module-queue
EOF
else
  echo "hook_one"
  sleep 1
fi
