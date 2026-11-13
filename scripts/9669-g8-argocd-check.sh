#!/usr/bin/env bash
# G8 ArgoCD Synced+Healthy check stub (#9669, part of #6707).
#
# DESIGN ONLY: read-only in every mode; never syncs, never patches.
#
# Usage:
#   scripts/9669-g8-argocd-check.sh --app <argocd-app>
#   (app name recorded in the rehearsal log on rehearsal day, never here)
set -euo pipefail

app="${ESHU_G8_APP:-<argocd-app>}"
if [[ "${1:-}" == "--app" ]]; then app="${2:?}"; fi

if [[ "${ESHU_G8_LIVE:-}" != "1" ]]; then
  cat <<EOF
plan: argocd app get ${app} --output json | jq '{sync: .status.sync.status, health: .status.health.status}'
plan: expect sync=Synced health=Healthy; paste output into the rehearsal log
NOT-EXECUTED: no cluster contact without ESHU_G8_LIVE=1.
EOF
  exit 0
fi

argocd app get "${app}" --output json \
  | jq -e '{sync: .status.sync.status, health: .status.health.status} | select(.sync == "Synced" and .health == "Healthy")'
