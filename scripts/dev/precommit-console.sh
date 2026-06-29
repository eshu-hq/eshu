#!/usr/bin/env bash
# Local pre-push helper for the Eshu console (apps/console) gates that the CI
# "Console (apps/console)" job runs. It mirrors that job so a broken console —
# e.g. a per-page e2e selector that no longer matches the rendered DOM — fails
# at push time instead of on GitHub.
#
# Usage: scripts/dev/precommit-console.sh <e2e|all>
#   e2e  runs only the per-page mock e2e gate (npm run console:e2e:mock). This
#        is the gate that the unit-test suite (console:test) cannot catch,
#        because it exercises real route rendering in a headless browser.
#   all  runs the full CI Console job mirror: typecheck -> test -> build -> e2e.
#
# Design notes:
#   - The e2e gate needs a Chromium build. We install it idempotently via
#     `npx playwright install chromium` (a no-op once cached), matching the CI
#     "Install Playwright browsers" step. This is the only network step and is
#     skipped automatically once the browser is present.
#   - node_modules must already be installed (`npm ci`). The hook fails with a
#     clear message rather than silently running `npm ci`, so a contributor
#     never has a commit mutate their working tree behind their back.
#   - Set ESHU_SKIP_CONSOLE_E2E=1 to bypass (e.g. on a machine that genuinely
#     cannot run a headless browser); the bypass is logged so it is never silent.
set -euo pipefail

mode="${1:-e2e}"

script_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
repo_root="$(git rev-parse --show-toplevel 2>/dev/null || printf '%s\n' "${script_root}")"
cd "${repo_root}"

if [[ "${ESHU_SKIP_CONSOLE_E2E:-}" == "1" ]]; then
  echo "precommit-console: ESHU_SKIP_CONSOLE_E2E=1 set — skipping console gate (NOT verified locally)." >&2
  exit 0
fi

commit_exists() {
  git rev-parse --verify --quiet "$1^{commit}" >/dev/null
}

maybe_fetch_base_ref() {
  local base_name="$1"
  [[ -z "${base_name}" ]] && return 0
  git fetch --no-tags --depth=50 origin "${base_name}" >/dev/null 2>&1 || true
}

resolve_diff_left() {
  local base_ref="${ESHU_CONSOLE_E2E_BASE_REF:-origin/main}"
  local merge_base=""

  if [[ "${base_ref}" == origin/* ]]; then
    maybe_fetch_base_ref "${base_ref#origin/}"
  fi

  if commit_exists "${base_ref}"; then
    if merge_base="$(git merge-base "${base_ref}" HEAD 2>/dev/null)"; then
      printf '%s\n' "${merge_base}"
      return 0
    fi

    printf '%s\n' "${base_ref}"
    return 0
  fi

  if commit_exists "HEAD~1"; then
    printf '%s\n' "HEAD~1"
    return 0
  fi

  git hash-object -t tree /dev/null
}

has_console_changes() {
  local diff_left="$1"
  local file_path=""

  while IFS= read -r -d '' file_path; do
    [[ -n "${file_path}" ]] && return 0
  done < <(git diff --name-only -z --diff-filter=ACMRD "${diff_left}" HEAD -- apps/console)

  return 1
}

if ! has_console_changes "$(resolve_diff_left)"; then
  echo "precommit-console: no apps/console changes in this branch; skipping console gate."
  exit 0
fi

if [[ ! -d "${repo_root}/node_modules/playwright" || ! -d "${repo_root}/node_modules/vite" ]]; then
  echo "precommit-console: node_modules is missing or incomplete." >&2
  echo "  Run 'npm ci' at the repo root, then retry the push." >&2
  exit 1
fi

echo "precommit-console: ensuring Chromium is installed (idempotent)..."
npx playwright install chromium

run_e2e() {
  echo "precommit-console: running per-page mock e2e (84 pages)..."
  npm run console:e2e:mock
}

case "${mode}" in
  e2e)
    run_e2e
    ;;
  all)
    echo "precommit-console: typecheck..."
    npm run console:typecheck
    echo "precommit-console: unit tests..."
    npm run console:test
    echo "precommit-console: build..."
    npm run console:build
    run_e2e
    ;;
  *)
    echo "precommit-console: unknown mode '${mode}' (want: e2e|all)" >&2
    exit 2
    ;;
esac

echo "precommit-console: console gate passed."
