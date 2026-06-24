#!/usr/bin/env bash
# manifest-bump.sh — append <VERSION> to the CALLER repo's own oc-plugin.json
# versions[] on main, after the signed image is in ghcr.io/<owner>/plugins/<id>.
# Own-repo write via GITHUB_TOKEN: no PAT, no PR, no cross-repo. Idempotent.
# CI-only (GitHub Actions ubuntu). Generates the manifest on first publish.
#
# Env (set by action.yml):
#   REPO       owner/repo of the caller (github.repository)
#   ID         plugin id (inputs.id)
#   OWNER      github owner (inputs.owner) — registry.namespace = <owner>/plugins
#   VERSION    tag, e.g. v0.1.4 (bare 0.1.4 tolerated)
#   PUBLISHER  publisher string for first-publish generate (default OpenCapital)
#   GH_TOKEN   GITHUB_TOKEN with contents:write on REPO
set -euo pipefail
: "${REPO:?REPO required}" "${ID:?ID required}" "${OWNER:?OWNER required}" "${VERSION:?VERSION required}"
: "${GH_TOKEN:?GH_TOKEN required}"
export GH_TOKEN
PUBLISHER="${PUBLISHER:-OpenCapital}"
FILE="oc-plugin.json"
VER="v${VERSION#v}"

# Current manifest + its blob sha on main (empty on first publish).
if cur="$(gh api "repos/${REPO}/contents/${FILE}?ref=main" --jq '.content' 2>/dev/null | base64 -d)" && [ -n "$cur" ]; then
  fsha="$(gh api "repos/${REPO}/contents/${FILE}?ref=main" --jq '.sha')"
else
  cur=""
  fsha=""
fi

if [ -z "$cur" ]; then
  cur="$(jq -n --arg id "$ID" --arg pub "$PUBLISHER" --arg ns "${OWNER}/plugins" '{
    schemaVersion: 1, pluginId: $id, publisher: $pub,
    registry: { host: "ghcr.io", namespace: $ns, publicURL: "https://ghcr.io" },
    versions: []
  }')"
fi

new="$(printf '%s' "$cur" | jq --arg v "$VER" '.versions = (
  (((.versions // []) + [$v]) | map("v" + sub("^v";"")) | unique)
  | sort_by(sub("^v";"") | split(".") | map(split("-")[0] | tonumber? // 0))
  | reverse
)')"

if [ "$(printf '%s' "$cur" | jq -S .)" = "$(printf '%s' "$new" | jq -S .)" ]; then
  echo "::notice::${ID} ${VER} already in versions[]; no commit"
  exit 0
fi

args=(-f "message=release: ${ID} ${VER}" -f "content=$(printf '%s\n' "$new" | base64 -w0)" -f "branch=main")
[ -n "$fsha" ] && args+=(-f "sha=${fsha}")
gh api "repos/${REPO}/contents/${FILE}" -X PUT "${args[@]}" >/dev/null
echo "::notice::committed ${ID} ${VER} to ${REPO}@main:${FILE}"
