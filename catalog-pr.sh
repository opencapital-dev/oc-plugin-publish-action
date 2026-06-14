#!/usr/bin/env bash
#
# catalog-pr.sh -- open a PR on the catalog repo adding <id>:<version> to a
# channel of plugins.json, run by the publish composite action AFTER the signed
# artifact is in plugins-staging. This is the "publish -> catalog PR" bridge:
# the PR's merge triggers plugin-promote-reconcile (cosign + footprint gated),
# which promotes staging -> trusted.
#
# No-op (exit 0) when CATALOG_TOKEN is empty, so publishing still works before
# the cross-repo token is configured -- the artifact is in staging regardless.
#
# Env (set by action.yml):
#   ID               plugin id (e.g. core-datasource)
#   VERSION          tag, e.g. v0.1.7 (a bare 0.1.7 is tolerated)
#   CATALOG_REPO     owner/repo holding plugins.json (e.g. opencapital-dev/opencapital)
#   CATALOG_CHANNEL  plugins (productive; merge = live) | preview (test channel)
#   CATALOG_TOKEN    PAT/App token with contents:write + pull-requests:write on CATALOG_REPO
#
set -euo pipefail

: "${ID:?ID required}" "${VERSION:?VERSION required}"
: "${CATALOG_REPO:?CATALOG_REPO required}" "${CATALOG_CHANNEL:?CATALOG_CHANNEL required}"
TOKEN="${CATALOG_TOKEN:-}"

if [ -z "$TOKEN" ]; then
  echo "::notice::CATALOG_TOKEN not set; skipping catalog PR (artifact is in plugins-staging)"
  exit 0
fi
export GH_TOKEN="$TOKEN"

case "$CATALOG_CHANNEL" in plugins|preview) ;; *) echo "ERROR: CATALOG_CHANNEL must be plugins|preview" >&2; exit 1 ;; esac

VER="v${VERSION#v}"                 # normalize to a single leading v
BRANCH="catalog/${ID}-${VER}"

cur="$(gh api "repos/${CATALOG_REPO}/contents/plugins.json" --jq '.content' | base64 -d)"

# Already listed in the target channel -> nothing to do (idempotent re-runs).
if printf '%s' "$cur" | jq -e --arg ch "$CATALOG_CHANNEL" --arg id "$ID" --arg v "$VER" \
     '(.[$ch][$id] // []) | index($v)' >/dev/null; then
  echo "::notice::${ID} ${VER} already in .${CATALOG_CHANNEL}; no PR opened"
  exit 0
fi

# Add the version to the target channel. plugins XOR preview: promoting to
# plugins drops the same version from preview so a version is never in both.
new="$(printf '%s' "$cur" | jq --arg ch "$CATALOG_CHANNEL" --arg id "$ID" --arg v "$VER" '
  .[$ch][$id] = (((.[$ch][$id] // []) + [$v]) | unique | reverse)
  | (if $ch == "plugins" then .preview[$id] = ((.preview[$id] // []) - [$v]) else . end)
')"

base="$(gh api "repos/${CATALOG_REPO}/git/ref/heads/main" --jq '.object.sha')"
# Create the branch at main, or hard-reset a stale same-name branch to main.
if ! gh api "repos/${CATALOG_REPO}/git/refs" -f ref="refs/heads/${BRANCH}" -f sha="$base" >/dev/null 2>&1; then
  gh api -X PATCH "repos/${CATALOG_REPO}/git/refs/heads/${BRANCH}" -f sha="$base" -F force=true >/dev/null
fi

fsha="$(gh api "repos/${CATALOG_REPO}/contents/plugins.json?ref=${BRANCH}" --jq '.sha')"
tmp="$(mktemp)"; printf '%s\n' "$new" > "$tmp"
gh api "repos/${CATALOG_REPO}/contents/plugins.json" -X PUT \
  -f message="catalog: add ${ID} ${VER} to .${CATALOG_CHANNEL}" \
  -f content="$(base64 -w0 "$tmp")" \
  -f sha="$fsha" -f branch="${BRANCH}" >/dev/null
rm -f "$tmp"

if gh pr create --repo "${CATALOG_REPO}" --base main --head "${BRANCH}" \
     --title "catalog: ${ID} ${VER} -> .${CATALOG_CHANNEL}" \
     --body "Automated on \`${ID}\` publish of \`${VER}\`. The signed artifact is in \`plugins-staging\`; merging promotes it via plugin-promote-reconcile (cosign + footprint gated)." \
     >/dev/null 2>&1; then
  echo "::notice::opened catalog PR: ${ID} ${VER} -> .${CATALOG_CHANNEL}"
else
  echo "::notice::catalog PR already open for ${BRANCH}"
fi
