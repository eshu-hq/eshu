#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

targets=(
	"apps/console/src"
	"apps/console/prototype"
	"go/internal/parser/hcl"
	"go/internal/parser/rust"
)

pattern='api-node-|/Users/|TF__[A-Z]{2}\b|\bbg-(prod|qa|dev)\b|\bops-qa\b|@[[:lower:]]{3}/'

if rg -n "$pattern" "${targets[@]}"; then
	printf 'answer demo fixture sanitization check failed\n' >&2
	exit 1
fi

printf 'answer demo fixture sanitization check passed\n'
