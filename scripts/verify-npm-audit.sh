#!/usr/bin/env bash
# verify-npm-audit.sh - blocking npm audit gate with a dated single-advisory
# allowlist (#10854). Used by the `audit` job in .github/workflows/frontend.yml
# in place of a bare `npm audit --audit-level=high`.
#
# Live mode (the only mode the workflow uses):
#   bash scripts/verify-npm-audit.sh
# runs `npm audit --json` and `npm audit --omit=dev --json`, then applies the
# rules in scripts/npm-audit-gate.mjs against specs/npm-audit-allowlist.v1.json
# using the real UTC clock. No environment variable moves the clock.
#
# Fixture mode (self-test only; needs BOTH report files and a test date):
#   bash scripts/verify-npm-audit.sh --fixture-all A.json --fixture-prod P.json \
#     --test-date YYYY-MM-DD [--allowlist L.json]
# --test-date and --allowlist are refused unless both fixture files are given.
#
# Exit: 0 pass, 1 gate failure, 2 usage error.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
gate="${repo_root}/scripts/npm-audit-gate.mjs"
allowlist="${repo_root}/specs/npm-audit-allowlist.v1.json"
fixture_all="" fixture_prod="" test_date="" allowlist_override=""

usage() {
  printf 'verify-npm-audit: %s\n' "$1" >&2
  exit 2
}

while [[ $# -gt 0 ]]; do
  [[ $# -ge 2 ]] || usage "flag $1 needs a value"
  case "$1" in
    --fixture-all) fixture_all="$2" ;;
    --fixture-prod) fixture_prod="$2" ;;
    --test-date) test_date="$2" ;;
    --allowlist) allowlist_override="$2" ;;
    *) usage "unknown flag $1" ;;
  esac
  shift 2
done

if [[ -n "${fixture_all}${fixture_prod}${test_date}${allowlist_override}" ]]; then
  [[ -n "${fixture_all}" && -n "${fixture_prod}" ]] || usage "--test-date and --allowlist need both --fixture-all and --fixture-prod"
  [[ -n "${test_date}" ]] || usage "fixture mode needs --test-date"
  [[ -z "${allowlist_override}" ]] || allowlist="${allowlist_override}"
  exec node "${gate}" --all "${fixture_all}" --prod "${fixture_prod}" --allowlist "${allowlist}" --now "${test_date}"
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT
cd "${repo_root}"

# npm audit exits 1 when it finds anything, so its status is not the signal;
# the gate parses the JSON and fails closed on missing or broken output.
npm audit --json >"${tmp_dir}/all.json" 2>"${tmp_dir}/all.err" || true
npm audit --omit=dev --json >"${tmp_dir}/prod.json" 2>"${tmp_dir}/prod.err" || true

node "${gate}" --all "${tmp_dir}/all.json" --prod "${tmp_dir}/prod.json" --allowlist "${allowlist}"
