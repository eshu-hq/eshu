#!/usr/bin/env bash
set -euo pipefail
url="${@: -1}"
case "${url}" in
  */debug/pprof/)
    printf 'pprof index\n'
    ;;
  */admin/status?format=json)
    if [ "${ESHU_FAKE_QUEUE_STATE:-terminal}" = "nonterminal" ]; then
        cat <<'JSON'
{"queue":{"outstanding":3,"pending":2,"in_flight":1,"retrying":1,"failed":0,"dead_letter":0,"overdue_claims":0},"retry_policies":[{"stage":"reducer","retry_delay":"1m"}],"vulnerability_sources":[{"source":"osv","terminal_status":"running","result_count":4,"warning_count":0}]}
JSON
        exit 0
    fi
    cat <<'JSON'
{"queue":{"outstanding":0,"pending":0,"in_flight":0,"retrying":0,"failed":0,"dead_letter":0,"overdue_claims":0},"retry_policies":[{"stage":"reducer","retry_delay":"1m"}],"vulnerability_sources":[{"source":"osv","terminal_status":"succeeded","result_count":4,"warning_count":0}]}
JSON
    ;;
  */api/v0/index-status)
    if [ "${ESHU_FAKE_QUEUE_STATE:-terminal}" = "nonterminal" ]; then
        cat <<'JSON'
{"status":"progressing","queue":{"outstanding":3,"pending":2,"in_flight":1,"retrying":1,"failed":0,"dead_letter":0},"health":{"state":"progressing"}}
JSON
        exit 0
    fi
    cat <<'JSON'
{"status":"healthy","queue":{"outstanding":0,"pending":0,"in_flight":0,"retrying":0,"failed":0,"dead_letter":0},"health":{"state":"healthy"}}
JSON
    ;;
  *)
    printf 'unexpected curl url: %s\n' "${url}" >&2
    exit 1
    ;;
esac
