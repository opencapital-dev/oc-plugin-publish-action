#!/usr/bin/env bash
# Offline test for the versions[] merge filter used by manifest-bump.sh.
# No network/gh — just exercises the jq transform.
set -euo pipefail

# The merge filter: normalize all to v-prefixed, dedupe, sort semver-descending.
FILTER='.versions = (
  (((.versions // []) + [$v]) | map("v" + sub("^v";"")) | unique)
  | sort_by(sub("^v";"") | split(".") | map(split("-")[0] | tonumber? // 0))
  | reverse
)'

got="$(echo '{"versions":["0.1.2","v0.1.10","0.1.3"]}' \
  | jq -c --arg v "v0.2.0" "$FILTER")"
want='{"versions":["v0.2.0","v0.1.10","v0.1.3","v0.1.2"]}'
[ "$got" = "$want" ] || { echo "FAIL append+sort: got $got want $want"; exit 1; }

# Idempotent: re-adding an existing version (bare vs v-prefixed) does not duplicate.
got="$(echo '{"versions":["v0.1.3","0.1.2"]}' \
  | jq -c --arg v "0.1.3" "$FILTER")"
want='{"versions":["v0.1.3","v0.1.2"]}'
[ "$got" = "$want" ] || { echo "FAIL idempotent: got $got want $want"; exit 1; }

echo "PASS"
