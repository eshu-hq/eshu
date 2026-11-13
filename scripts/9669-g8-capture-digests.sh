#!/usr/bin/env bash
# G8 digest capture stub (#9669, part of #6707).
#
# DESIGN ONLY: without ESHU_G8_LIVE=1 it prints what would be captured.
# Live: prints repo:tag@digest for the served core artifact (go/cmd/api).
#
# Usage:
#   scripts/9669-g8-capture-digests.sh [--namespace eshu]
set -euo pipefail

ns="eshu"
if [[ "${1:-}" == "--namespace" ]]; then ns="${2:?}"; fi

if [[ "${ESHU_G8_LIVE:-}" != "1" ]]; then
  cat <<EOF
plan: kubectl -n ${ns} get deploy/eshu-extracted-api -o jsonpath='{.spec.template.spec.containers[*].image}'
plan: record repo:tag@digest for the restored core artifact containing go/cmd/api
NOT-EXECUTED: no cluster contact without ESHU_G8_LIVE=1.
EOF
  exit 0
fi

kubectl -n "${ns}" get deploy/eshu-extracted-api \
  -o jsonpath='{.spec.template.spec.containers[*].image}{"\n"}'
