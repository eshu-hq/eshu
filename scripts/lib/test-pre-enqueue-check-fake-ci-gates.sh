#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Fake `ci-gates` for scripts/dev/test-pre-enqueue-check.sh (#9094). The real
# binary is `go run ./cmd/ci-gates`; this stand-in serves the two context lists
# the check script reads, so a case can model a manifest without compiling Go:
#   ci-gates contexts --registry <path>                 -> contexts-full.txt
#   ci-gates contexts --registry <path> --ruleset-only  -> contexts-ruleset.txt
# Every call's argv is appended to $FIXTURES/ci-gates-calls.log, and anything
# other than `contexts` is refused so a new, unfixtured call fails the test.
set -euo pipefail
printf '%s\n' "$*" >>"${FIXTURES}/ci-gates-calls.log"
[[ "${1:-}" == contexts ]] || {
	echo "fake ci-gates: unsupported: $*" >&2
	exit 2
}
if [[ -f "${FIXTURES}/contexts.rc" ]]; then
	echo "fake ci-gates: simulated failure" >&2
	exit "$(cat "${FIXTURES}/contexts.rc")"
fi
for arg in "$@"; do
	if [[ "${arg}" == --ruleset-only ]]; then
		cat "${FIXTURES}/contexts-ruleset.txt"
		exit 0
	fi
done
cat "${FIXTURES}/contexts-full.txt"
