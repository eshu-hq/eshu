#!/usr/bin/env bash
set -euo pipefail
url="${@: -1}"
case "${url}" in
  */debug/pprof/)
    printf 'pprof index\n'
    ;;
  */api/v0/index-status)
    if [ "${ESHU_FAKE_INDEX_STATUS_NON_TERMINAL:-0}" = "1" ]; then
      cat <<'JSON'
{"status":"degraded","queue":{"outstanding":0,"pending":1,"in_flight":0,"retrying":1,"failed":0,"dead_letter":1},"health":{"state":"degraded"}}
JSON
      exit 0
    fi
    cat <<'JSON'
{"status":"healthy","queue":{"outstanding":0,"pending":0,"in_flight":0,"retrying":0,"failed":0,"dead_letter":0},"health":{"state":"healthy"}}
JSON
    ;;
  */api/v0/*)
    if [ "${ESHU_FAKE_RUNTIME_PRIVATE_READBACK:-0}" = "1" ]; then
      cat <<'JSON'
{"repo":"example/private-service","package":"private-package","url":"https://example.invalid/private","token":"ghp_exampletoken","path":"/Users/example/repos/private-service"}
JSON
      exit 0
    fi
    printf '{"count":1,"total_findings":1,"total_reconciliations":1,"total_attachments":1,"total_identities":1}\n'
    ;;
  *)
    printf 'unexpected curl url: %s\n' "${url}" >&2
    exit 1
    ;;
esac
