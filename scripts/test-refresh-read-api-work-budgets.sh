#!/usr/bin/env bash
# test-refresh-read-api-work-budgets.sh - test mirror for
# scripts/refresh-read-api-work-budgets.sh. Runs without Docker, Postgres, or a
# Go build: asserts the formulas on a fixture, byte-idempotency, input-order
# independence, and the failure paths (malformed report, no reports, nothing to
# derive the default row from).
set -euo pipefail
export LC_ALL=C

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/refresh-read-api-work-budgets.sh"
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

fail() {
	printf 'test-refresh-read-api-work-budgets: FAIL: %s\n' "$*" >&2
	exit 1
}

printf 'default\t1500\n\nGET /a\t1000\tnamed a\nGET /b\t1000\tnamed b\n' >"${work}/named.txt"

cat >"${work}/r1.json" <<'JSON'
{"routes":[
 {"route":"GET /a","p95_ms":1,"work":{"calls":10,"blks":1000,"rows":4}},
 {"route":"GET /b","p95_ms":1,"work":{"calls":26,"blks":63019,"rows":25}},
 {"route":"GET /c","p95_ms":1,"work":{"calls":2,"blks":500,"rows":0}}
]}
JSON
cat >"${work}/r2.json" <<'JSON'
{"routes":[
 {"route":"GET /a","p95_ms":1,"work":{"calls":12,"blks":900,"rows":5}},
 {"route":"GET /c","p95_ms":1,"work":{"calls":3,"blks":700,"rows":1}},
 {"route":"GET /d","p95_ms":1,"work":{"calls":1,"blks":1,"rows":0}}
]}
JSON

run() { bash "${script}" --named-from "${work}/named.txt" "$@"; }

out1="$(run "${work}/r1.json" "${work}/r2.json")" || fail "renders a valid fixture"

# calls = ceil(max*1.25)+5, blks = ceil(max*3), rows = ceil(max*2) over the max
# across reports; the default row uses the max over the unnamed routes (c, d).
expect() {
	printf '%s\n' "${out1}" | rg -qxF -- "$1" || fail "missing line: $1"
}
expect "default	9	2100	2"
expect "GET /a	20	3000	10	GREEN-derived work guard"
expect "GET /b	38	189057	50	GREEN-derived work guard"

printf '%s\n' "${out1}" | rg -q '^GET /c' && fail "unnamed route GET /c must not get its own row"

# Idempotent, and independent of report order.
out2="$(run "${work}/r1.json" "${work}/r2.json")"
cmp -s <(printf '%s\n' "${out1}") <(printf '%s\n' "${out2}") || fail "second run differs from the first"
out3="$(run "${work}/r2.json" "${work}/r1.json")"
cmp -s <(printf '%s\n' "${out1}") <(printf '%s\n' "${out3}") || fail "output depends on report order"

# --out writes the same bytes as stdout.
run --out "${work}/table.txt" "${work}/r1.json" "${work}/r2.json"
cmp -s <(printf '%s\n' "${out1}") "${work}/table.txt" || fail "--out differs from stdout"

# Negative cases.
expect_exit() {
	local want="$1"
	shift
	local got=0
	"$@" >/dev/null 2>&1 || got=$?
	[[ "${got}" -eq "${want}" ]] || fail "exit ${got}, want ${want}: $*"
}
printf '{"routes":[{"route":"GET /a","work":{"calls":"x"}}]}' >"${work}/bad.json"
expect_exit 2 bash "${script}" --named-from "${work}/named.txt" "${work}/bad.json"
printf 'not json' >"${work}/garbage.json"
expect_exit 2 bash "${script}" --named-from "${work}/named.txt" "${work}/garbage.json"
expect_exit 2 bash "${script}" --named-from "${work}/named.txt"
expect_exit 2 bash "${script}" --named-from "${work}/named.txt" "${work}/missing.json"
printf '{"routes":[{"route":"GET /a","work":{"calls":1,"blks":1,"rows":1}}]}' >"${work}/only-named.json"
expect_exit 3 bash "${script}" --named-from "${work}/named.txt" "${work}/only-named.json"

printf 'test-refresh-read-api-work-budgets: PASS\n'
