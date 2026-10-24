#!/usr/bin/env bash
# Fake `helm` for test-collector-cutover-rollback.sh.
set -euo pipefail

state="${FAKE_STATE:?FAKE_STATE is required}"
printf 'helm %s\n' "$*" >>"${state}/calls.log"
if [[ -e "${state}/fail_helm" ]]; then
	printf 'Error: UPGRADE FAILED\n' >&2
	exit 1
fi
printf 'Release "%s" has been upgraded.\n' "${2:-release}"
