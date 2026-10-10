#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq

# Exercise the actual orchestration order against the zero-residual invariant.
# The intentional operator-query failure must exist only after producer drains.
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
residual=0
documentation_proved=false
dead_letter_seeded=false
golden_documentation_run_proof() {
	[[ "${residual}" -eq 0 ]] || {
		printf 'documentation drain encountered intentional dead-letter fixture\n' >&2
		return 1
	}
	documentation_proved=true
}
seed_golden_dead_letter_fixture() {
	residual=1
	dead_letter_seeded=true
}
calls=()
while IFS= read -r call; do
	calls+=("${call}")
done < <(rg '^(golden_documentation_run_proof|seed_golden_dead_letter_fixture)$' \
	"${repo_root}/scripts/verify-golden-corpus-gate.sh")
[[ "${#calls[@]}" -eq 2 ]] || {
	printf 'expected exactly one documentation proof and dead-letter seed\n' >&2
	exit 1
}
for call in "${calls[@]}"; do
	"${call}"
done
[[ "${documentation_proved}" == true && "${dead_letter_seeded}" == true && "${residual}" -eq 1 ]]
printf 'test-golden-corpus-documentation-order: pass\n'
