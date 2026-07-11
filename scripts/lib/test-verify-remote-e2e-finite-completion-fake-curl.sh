#!/usr/bin/env bash
set -euo pipefail

state_dir="${ESHU_REMOTE_E2E_TEST_STATE:?set ESHU_REMOTE_E2E_TEST_STATE}"
printf '%s\n' "$*" >>"${state_dir}/curl-targets"
case "$*" in
  *"/api/v0/index-status"*) cat "${state_dir}/index-status.json" ;;
  *"/api/v0/status/index"*) cat "${state_dir}/status-index.json" ;;
  *"/api/v0/package-registry/packages/count"*) cat "${state_dir}/package-count.json" ;;
  *"/api/v0/supply-chain/advisories/evidence"*) cat "${state_dir}/advisory-evidence.json" ;;
  *"/api/v0/supply-chain/impact/findings/count"*) cat "${state_dir}/impact-count.json" ;;
  *"/api/v0/supply-chain/security-alerts/reconciliations/count"*) cat "${state_dir}/security-alert-count.json" ;;
  *"/api/v0/supply-chain/sbom-attestations/attachments/count"*) cat "${state_dir}/sbom-count.json" ;;
  *"/api/v0/supply-chain/container-images/identities/count"*) cat "${state_dir}/container-image-count.json" ;;
  *) echo "unexpected curl target: $*" >&2; exit 2 ;;
esac
