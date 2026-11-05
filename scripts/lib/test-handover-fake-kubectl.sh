#!/usr/bin/env bash
# Fake `kubectl` for test-claim-plane-cutover-handover.sh. A Deployment at
# zero replicas has no .status.replicas field, so the frozen shape is "0|"
# (empty status), exactly like the real jsonpath output. State lives in
# $STUB_STATE_DIR: spec (written by scale), scale.log (appended by scale).
set -euo pipefail

state_dir="${STUB_STATE_DIR:?}"
if printf '%s\n' "$@" | grep -q '^get$'; then
	if [[ -f "${state_dir}/spec" ]]; then
		printf '%s|\n' "$(cat "${state_dir}/spec")"
	else
		printf '%s|%s\n' "${STUB_SPEC:-2}" "${STUB_STATUS-2}"
	fi
	exit 0
fi
for arg in "$@"; do
	case "${arg}" in
	--replicas=*) printf '%s' "${arg#--replicas=}" >"${state_dir}/spec" ;;
	esac
done
printf '%s\n' "$*" >>"${STUB_SCALE_LOG:?}"
