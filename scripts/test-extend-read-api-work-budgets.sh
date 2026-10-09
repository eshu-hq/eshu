#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${root}/scripts/extend-read-api-work-budgets.sh"
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT
fail() { printf 'test-extend-read-api-work-budgets: FAIL: %s\n' "$*" >&2; exit 1; }
reject() { if "$@" >"${work}/reject.out" 2>&1; then fail "accepted invalid input: $*"; fi; }

printf '# original GREEN provenance\ndefault\t13\t18\t14\n\nGET /old\t34\t55683\t76\tGREEN-derived work guard\n' >"${work}/baseline"
printf '# renderer output\ndefault\t14\t20\t15\n\nGET /b\t42\t19000\t82\tGREEN-derived work guard\nGET /a\t39\t18000\t80\tGREEN-derived work guard\n' >"${work}/rendered"
args=(--baseline "${work}/baseline" --rendered "${work}/rendered" --route 'GET /b' --route 'GET /a' --provenance '2 GREEN reports for #7881 on the enforced runner class')
bash "${script}" "${args[@]}" --out "${work}/out" || fail 'valid extension failed'
cmp -n "$(wc -c <"${work}/baseline")" "${work}/baseline" "${work}/out" || fail 'baseline bytes changed'
[[ "$(rg -c '^default\t' "${work}/out")" -eq 1 ]] || fail 'default changed'
[[ "$(rg -c '^GET /old\t' "${work}/out")" -eq 1 ]] || fail 'old row changed'
[[ "$(rg -c '^GET /[ab]\t' "${work}/out")" -eq 2 ]] || fail 'new rows missing'
[[ "$(rg -n '^GET /a\t' "${work}/out" | cut -d: -f1)" -lt "$(rg -n '^GET /b\t' "${work}/out" | cut -d: -f1)" ]] || fail 'new rows unsorted'
cp "${work}/out" "${work}/first"
bash "${script}" "${args[@]}" --out "${work}/out" || fail 'repeat failed'
cmp -s "${work}/first" "${work}/out" || fail 'different bytes on repeat'
reject bash "${script}" --baseline "${work}/baseline" --rendered "${work}/rendered" --route 'GET /old' --provenance ok
reject bash "${script}" --baseline "${work}/baseline" --rendered "${work}/rendered" --route 'GET /missing' --provenance ok
reject bash "${script}" --baseline "${work}/baseline" --rendered "${work}/rendered" --route 'GET /a' --route 'GET /a' --provenance ok
reject bash "${script}" --baseline "${work}/baseline" --rendered "${work}/rendered" --route $'GET /a\nGET /b' --provenance ok
reject bash "${script}" --baseline "${work}/baseline" --rendered "${work}/rendered" --route 'GET /a' --provenance $'forged\n# note'
printf 'GET /a\t39\t18000\t80\tGREEN-derived work guard\nGET /a\t39\t18000\t80\tGREEN-derived work guard\n' >"${work}/duplicate"
reject bash "${script}" --baseline "${work}/baseline" --rendered "${work}/duplicate" --route 'GET /a' --provenance ok
printf 'GET /a\tno\t18000\t80\tGREEN-derived work guard\n' >"${work}/malformed"
reject bash "${script}" --baseline "${work}/baseline" --rendered "${work}/malformed" --route 'GET /a' --provenance ok
printf 'default\t13\t18\t14\nGET /old\t34\t55683\t76\tGREEN-derived work guard\nGET /old\t34\t55683\t76\tGREEN-derived work guard\n' >"${work}/duplicate-baseline"
reject bash "${script}" --baseline "${work}/duplicate-baseline" --rendered "${work}/rendered" --route 'GET /a' --provenance ok
printf 'test-extend-read-api-work-budgets: PASS\n'
