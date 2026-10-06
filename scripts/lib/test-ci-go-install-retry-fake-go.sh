#!/usr/bin/env bash
# Fake `go` for scripts/test-ci-go-install-retry.sh. Only understands
# `install <pkg>`: appends "<args>" to FAKE_GO_LOG and its cwd to
# FAKE_GO_PWD_LOG once per invocation, counts invocations in
# FAKE_GO_COUNT_FILE, fails every invocation at or below FAKE_GO_FAIL_UNTIL
# (default 0, meaning "never fail"), and always fails an install of
# FAKE_GO_FAIL_PKG.
set -euo pipefail

if [[ "${1:-}" != "install" ]]; then
	exit 0
fi

printf '%s\n' "$*" >>"${FAKE_GO_LOG:?}"
pwd >>"${FAKE_GO_PWD_LOG:?}"
count_file="${FAKE_GO_COUNT_FILE:?}"
n=0
[[ -f "${count_file}" ]] && n="$(cat "${count_file}")"
n=$((n + 1))
printf '%s' "${n}" >"${count_file}"

if [[ -n "${FAKE_GO_FAIL_PKG:-}" && "${2:-}" == "${FAKE_GO_FAIL_PKG}" ]]; then
	echo "fake go: simulated 502 for ${2}" >&2
	exit 1
fi
if ((n <= ${FAKE_GO_FAIL_UNTIL:-0})); then
	echo "fake go: simulated proxy 502, attempt ${n}" >&2
	exit 1
fi
exit 0
