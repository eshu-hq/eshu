#!/usr/bin/env bash
# Wrapper around `npm run lint` that runs the production ESLint flat config
# against the configured paths and exits non-zero on any reported violation.
#
# Used by the `lint` job in .github/workflows/frontend.yml (issue #3763 O3)
# and by local scripts that want a friendly one-liner for the gate.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

if [ ! -x "${repo_root}/node_modules/.bin/eslint" ]; then
  printf 'verify-eslint-config: eslint binary not installed; run npm ci first\n' >&2
  exit 1
fi

printf 'verify-eslint-config: running eslint with flat config at %s/eslint.config.js\n' "${repo_root}"
exec "${repo_root}/node_modules/.bin/eslint" .
