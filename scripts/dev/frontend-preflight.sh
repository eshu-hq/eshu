#!/usr/bin/env bash
# Frontend preflight (#4216): run the credential-free frontend gates that the
# changed paths select, via the shared gate registry — the local mirror of
# .github/workflows/frontend.yml (root site typecheck/test/build, console
# typecheck/test/build, console a11y, ESLint flat config, npm audit, console
# per-page e2e, and changed-file Prettier). Run it before pushing a frontend
# change so those failures surface locally instead of on CI.
#
# Usage:
#   scripts/dev/frontend-preflight.sh [--base <ref>]
#
# These gates need a working Node toolchain and installed dependencies. If
# node_modules is missing the underlying npm commands fail loudly with a clear
# error (run `npm ci` first) — they do not silently skip.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

base="origin/main"
if [[ "${1:-}" == "--base" && -n "${2:-}" ]]; then
	base="$2"
fi

if [[ ! -d "${repo_root}/node_modules" ]]; then
	printf 'frontend-preflight: node_modules missing — run `npm ci` at the repo root first.\n' >&2
fi

exec bash "${repo_root}/scripts/dev/run-selected-gates.sh" \
	--base "${base}" --tier pre-push --category frontend
