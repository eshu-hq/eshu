#!/usr/bin/env bash
#
# Fixture sanitization scan (#3554, registered #7918) for the answer-demo
# fixture directories.
#
# Forbids real repository names (the `api-node-` convention names), machine
# user paths, and the environment and organization tokens in fixture-bearing
# trees. The environment and organization tokens come from
# scripts/lib/private-identifier-pattern.sh, the one definition shared with
# scripts/verify-no-private-identifiers.sh (#7803). Real hashed ids stay on
# the private denylist outside the repository (arbiter ruling a8-2, #7803):
# the public pattern cannot list them because generic fixture shapes would
# also match. Registered as the `answer-demo-fixtures-sanitized` gate in
# specs/ci-gates.d/gates-02.yaml; CI runs it in verify-agent-hygiene.yml's
# "Agent hygiene gate" job. The seeded RED/GREEN mirror is
# scripts/test-verify-answer-demo-fixtures-sanitized.sh.
#
# ESHU_FIXTURE_SCRUBBER_ROOT overrides the repository root for the test
# mirror only; production runs always scan the checkout holding this script.
set -euo pipefail

ROOT="${ESHU_FIXTURE_SCRUBBER_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
cd "$ROOT"

# shellcheck source=scripts/lib/private-identifier-pattern.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/private-identifier-pattern.sh"

targets=(
	"apps/console/src"
	"apps/console/prototype"
	"go/internal/parser/hcl"
	"go/internal/parser/rust"
	# go/internal/query is a whole-directory target since the #7803 sweep
	# cleaned its environment tokens in comments (#7803 batch 2); the two
	# former file targets and the old service/visualization subtrees stay
	# covered without a path exception.
	"go/internal/query"
)

pattern='api-node-|/Users/|TF__[A-Z]{2}\b|@[[:lower:]]{3}/|'"${PRIVATE_IDENTIFIER_PATTERN}"

# Fail closed like verify-no-private-identifiers.sh: rg exits 1 for "no
# match" and 2 or more for an error, and an error is a failure, never a
# clean scan. A missing target directory must not read as a pass.
scan_out=""
rc=0
scan_out="$(rg -n "$pattern" "${targets[@]}")" || rc=$?
if [ "$rc" -eq 0 ]; then
	printf '%s\n' "$scan_out"
	printf 'answer demo fixture sanitization check failed\n' >&2
	exit 1
elif [ "$rc" -gt 1 ]; then
	printf 'answer demo fixture sanitization: ripgrep failed (exit %d); content was not verified\n' "$rc" >&2
	exit 2
fi

printf 'answer demo fixture sanitization check passed\n'
