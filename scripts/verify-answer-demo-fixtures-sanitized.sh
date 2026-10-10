#!/usr/bin/env bash
#
# Fixture sanitization scan (#3554) for the answer-demo fixture directories.
#
# NOT registered as a gate: it exits 1 on the current tree (employer-style
# repository names in apps/console/src test fixtures), so registering it would
# turn main red. Registering it, widening its targets, and fixing those
# fixtures is tracked in #7918. The environment and organization tokens come
# from scripts/lib/private-identifier-pattern.sh, the one definition shared
# with scripts/verify-no-private-identifiers.sh (#7803).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# shellcheck source=scripts/lib/private-identifier-pattern.sh
source "$ROOT/scripts/lib/private-identifier-pattern.sh"

targets=(
	"apps/console/src"
	"apps/console/prototype"
	"go/internal/parser/hcl"
	"go/internal/parser/rust"
)

pattern='api-node-|/Users/|TF__[A-Z]{2}\b|@[[:lower:]]{3}/|'"${PRIVATE_IDENTIFIER_PATTERN}"

if rg -n "$pattern" "${targets[@]}"; then
	printf 'answer demo fixture sanitization check failed\n' >&2
	exit 1
fi

printf 'answer demo fixture sanitization check passed\n'
