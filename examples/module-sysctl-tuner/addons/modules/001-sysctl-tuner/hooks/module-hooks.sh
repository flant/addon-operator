#!/usr/bin/env bash

# A stub hook just to make sure that events are handled properly.

if [[ $1 == "--config" ]] ; then
  cat <<EOF
{"beforeHelm": 1,
"afterHelm": 1,
"afterDeleteHelm": 1
}
EOF
exit 0
fi

binging=$(jq '.[0].binding' "${BINDING_CONTEXT_PATH}")

echo "Run ${binding} hook of sysctl-tuner"
