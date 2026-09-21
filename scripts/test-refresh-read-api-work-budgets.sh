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

printf 'default\t1500\n\nGET /a\t1000\tnamed a\nGET /b\t1000\tnamed b\nGET /z\t1000\tnamed zero-base\n' >"${work}/named.txt"

cat >"${work}/r1.json" <<'JSON'
{"routes":[
 {"route":"GET /a","p95_ms":1,"work":{"calls":10,"blks":1000,"rows":4}},
 {"route":"GET /b","p95_ms":1,"work":{"calls":26,"blks":63019,"rows":25}},
 {"route":"GET /c","p95_ms":1,"work":{"calls":2,"blks":500,"rows":0}},
 {"route":"GET /z","p95_ms":1,"work":{"calls":1,"blks":1,"rows":0}}
]}
JSON
cat >"${work}/r2.json" <<'JSON'
{"routes":[
 {"route":"GET /a","p95_ms":1,"work":{"calls":12,"blks":900,"rows":5}},
 {"route":"GET /c","p95_ms":1,"work":{"calls":3,"blks":700,"rows":1}},
 {"route":"GET /d","p95_ms":1,"work":{"calls":1,"blks":1,"rows":0}}
]}
JSON

run() { bash "${script}" --named-from "${work}/named.txt" --baseline /dev/null "$@"; }
# --baseline /dev/null: these cases assert the FORMULAS, so they must not also
# depend on whatever the committed table happens to hold. The ratchet gets its
# own cases below.
run_ratcheted() { bash "${script}" --named-from "${work}/named.txt" "$@"; }

out1="$(run "${work}/r1.json" "${work}/r2.json")" || fail "renders a valid fixture"

# calls = ceil(max*1.25)+5, blks = ceil(max*3), rows = ceil(max*2) over the max
# across reports; the default row uses the max over the unnamed routes (c, d).
expect() {
	printf '%s\n' "${out1}" | rg -qxF -- "$1" || fail "missing line: $1"
}
expect "default	9	2100	2"
expect "GET /a	20	3000	10	GREEN-derived work guard"
expect "GET /b	38	189057	50	GREEN-derived work guard"
# A named route whose own work is at the noise floor (1 call, 1 block, 0 rows)
# would render 7/3/0, so one stray statement or row in the meter window would
# breach it. Named rows are floored at the default row's values (9/2100/2 here).
expect "GET /z	9	2100	2	GREEN-derived work guard"

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


# --- ratchet ---------------------------------------------------------------
# The guard that #6843 did not have: it raised these two rows from 74739 to
# 663066 blks in the same PR that caused the 8.9x work increase, and nothing
# objected, so the work gate stayed green while the route went 145ms -> 2.1s.

printf 'default\t9\t2100\t2\n\nGET /a\t20\t3000\t10\tGREEN-derived work guard\nGET /b\t38\t189057\t50\tGREEN-derived work guard\nGET /z\t9\t2100\t2\tGREEN-derived work guard\n' >"${work}/baseline.txt"

# GREEN: rendering the same reports against their own committed output passes.
run_ratcheted --baseline "${work}/baseline.txt" "${work}/r1.json" "${work}/r2.json" >/dev/null \
	|| fail "ratchet must allow an unchanged render"

# GREEN: growth at or under the threshold passes. /a is 3000 blks committed;
# 1000 -> 1400 raw renders 4200, exactly 1.4x, under 1.5x.
cat >"${work}/under.json" <<'JSON'
{"routes":[
 {"route":"GET /a","p95_ms":1,"work":{"calls":10,"blks":1400,"rows":4}},
 {"route":"GET /c","p95_ms":1,"work":{"calls":2,"blks":500,"rows":0}}
]}
JSON
run_ratcheted --baseline "${work}/baseline.txt" "${work}/under.json" >/dev/null \
	|| fail "ratchet must allow growth at or under the threshold"

# RED: the #6843 shape. /a's work jumps 8.9x, exactly as reducer_cloud_resource_identity
# and terraform_state_resource did, and the render must be refused.
cat >"${work}/regressed.json" <<'JSON'
{"routes":[
 {"route":"GET /a","p95_ms":1,"work":{"calls":10,"blks":8900,"rows":4}},
 {"route":"GET /c","p95_ms":1,"work":{"calls":2,"blks":500,"rows":0}}
]}
JSON
set +e
red_out="$(run_ratcheted --baseline "${work}/baseline.txt" "${work}/regressed.json" 2>&1 >/dev/null)"
red_rc=$?
set -e
[[ "${red_rc}" -eq 3 ]] || fail "seeded 8.9x regression must exit 3, got ${red_rc}"
printf '%s' "${red_out}" | rg -q 'RATCHET' || fail "refusal must name the ratchet"
printf '%s' "${red_out}" | rg -q 'GET /a: 3000 -> 26700 blks' || fail "refusal must show the route, old and new blks: ${red_out}"
printf '%s' "${red_out}" | rg -q 'accept-regression' || fail "refusal must name the escape hatch"

# GREEN: the same regression is allowed once the caller names it and an issue.
run_ratcheted --baseline "${work}/baseline.txt" \
	--accept-regression 'GET /a=#6843 graph-only labels now serve from fact truth' \
	"${work}/regressed.json" >/dev/null \
	|| fail "--accept-regression must allow the named route"

# The escape hatch must not be usable without saying why.
set +e
bash "${script}" --named-from "${work}/named.txt" --baseline "${work}/baseline.txt" \
	--accept-regression 'GET /a=' "${work}/regressed.json" >/dev/null 2>&1
empty_rc=$?
set -e
[[ "${empty_rc}" -ne 0 ]] || fail "--accept-regression must reject an empty reason"

# Accepting one route must not silently accept another.
cat >"${work}/two_regressed.json" <<'JSON'
{"routes":[
 {"route":"GET /a","p95_ms":1,"work":{"calls":10,"blks":8900,"rows":4}},
 {"route":"GET /b","p95_ms":1,"work":{"calls":26,"blks":630190,"rows":25}},
 {"route":"GET /c","p95_ms":1,"work":{"calls":2,"blks":500,"rows":0}}
]}
JSON
set +e
two_out="$(run_ratcheted --baseline "${work}/baseline.txt" \
	--accept-regression 'GET /a=#6843' "${work}/two_regressed.json" 2>&1 >/dev/null)"
two_rc=$?
set -e
[[ "${two_rc}" -eq 3 ]] || fail "an unaccepted second regression must still exit 3, got ${two_rc}"
printf '%s' "${two_out}" | rg -q 'GET /b' || fail "refusal must name the unaccepted route"
printf '%s' "${two_out}" | rg -q 'ACCEPTED GET /a' || fail "accepted route must be reported, not hidden"

# The threshold must admit the fix for the very regression it blocks. #6843's
# work must be refused (8.9x, above), but #6912's post-fix value must pass:
# entities scan ~24,893 + fact table ~2,112 + readiness ~= 27,005 raw blks
# renders 81,015, which is 1.08x the pre-#6843 committed 74,739. A ratchet that
# blocks its own fix is worse than no ratchet.
printf 'default\t9\t2100\t2\n\nGET /a\t13\t74739\t80\tGREEN-derived work guard\n' >"${work}/pre_regression.txt"
cat >"${work}/postfix.json" <<'JSON'
{"routes":[
 {"route":"GET /a","p95_ms":150,"work":{"calls":4,"blks":27005,"rows":44}},
 {"route":"GET /c","p95_ms":1,"work":{"calls":2,"blks":500,"rows":0}}
]}
JSON
postfix_out="$(run_ratcheted --baseline "${work}/pre_regression.txt" "${work}/postfix.json")" \
	|| fail "the ratchet must admit the legitimate post-fix budget (1.08x)"
printf '%s\n' "${postfix_out}" | rg -qxF -- "GET /a	10	81015	88	GREEN-derived work guard" \
	|| fail "post-fix row should render 81015"

# --- collapse direction ------------------------------------------------------
# A budget that COLLAPSES disarms a guard silently: nothing fails when it lands,
# and the next PR to touch the route breaches for no visible reason. Found on
# #6912: reverting #6843 deleted the seed that /cloud/inventory reads, dropping
# it 145851 -> 21 blks (6945x) in a PR about two unrelated routes.
# Mirrors the script's ratchet_drop_factor; the test asserts the ratio, not a number.
ratchet_drop_factor_test=20
printf 'default\t9\t2100\t2\n\nGET /a\t13\t145851\t16\tGREEN-derived work guard\n' >"${work}/collapse_base.txt"
cat >"${work}/collapsed.json" <<'JSON'
{"routes":[
 {"route":"GET /a","p95_ms":1,"work":{"calls":4,"blks":7,"rows":16}},
 {"route":"GET /c","p95_ms":1,"work":{"calls":2,"blks":500,"rows":0}}
]}
JSON
set +e
drop_out="$(run_ratcheted --baseline "${work}/collapse_base.txt" "${work}/collapsed.json" 2>&1 >/dev/null)"
drop_rc=$?
set -e
[[ "${drop_rc}" -eq 3 ]] || fail "a 6945x collapse must exit 3, got ${drop_rc}"
printf '%s' "${drop_out}" | rg -q 'COLLAPSED' || fail "refusal must name the collapse"
# Assert the ratio crosses the threshold, not the rendered number. The rendered
# value is fixture-dependent: named rows are floored at the default row, and the
# default tracks the max over UNNAMED routes. This fixture's unnamed GET /c reads
# 500 blks, so the floor is 1500 and the collapse renders 145851 -> 1500 (97x).
# The real #6912 case had a default row of 18, so ceil(7*3)=21 cleared the floor
# and rendered 145851 -> 21 (6945x). Both fire; only the ratio is invariant.
drop_new="$(printf '%s' "${drop_out}" | rg -o 'GET /a: 145851 -> [0-9]+ blks' | rg -o '> [0-9]+' | rg -o '[0-9]+')"
[[ -n "${drop_new}" ]] || fail "refusal must show the route and both values: ${drop_out}"
(( ratchet_drop_factor_test * drop_new < 145851 )) \
	|| fail "rendered ${drop_new} must be past the ${ratchet_drop_factor_test}x collapse threshold below 145851"
printf '%s' "${drop_out}" | rg -q 'corpus shrank' || fail "refusal must point at the corpus, which is the usual cause"

# A real optimisation must still pass. #6912's read-model fix drops these routes
# 663066 -> 81015, which is 8.2x and well inside the 20x factor.
printf 'default\t9\t2100\t2\n\nGET /a\t13\t663066\t88\tGREEN-derived work guard\n' >"${work}/opt_base.txt"
cat >"${work}/optimised.json" <<'JSON'
{"routes":[
 {"route":"GET /a","p95_ms":150,"work":{"calls":4,"blks":27005,"rows":44}},
 {"route":"GET /c","p95_ms":1,"work":{"calls":2,"blks":500,"rows":0}}
]}
JSON
run_ratcheted --baseline "${work}/opt_base.txt" "${work}/optimised.json" >/dev/null \
	|| fail "a genuine 8.2x optimisation must pass the collapse guard"

# The collapse is allowed once the caller names it and an issue.
run_ratcheted --baseline "${work}/collapse_base.txt" \
	--accept-drop 'GET /a=#6912 seed intentionally removed' "${work}/collapsed.json" >/dev/null \
	|| fail "--accept-drop must allow the named route"

# --accept-regression must NOT silence a collapse: they are different failures.
set +e
run_ratcheted --baseline "${work}/collapse_base.txt" \
	--accept-regression 'GET /a=#6912' "${work}/collapsed.json" >/dev/null 2>&1
wrong_flag_rc=$?
set -e
[[ "${wrong_flag_rc}" -eq 3 ]] || fail "--accept-regression must not silence a collapse"

# A typo'd --baseline must fail closed, not silently skip the ratchet.
set +e
bash "${script}" --named-from "${work}/named.txt" --baseline "${work}/no_such_file.txt" \
	"${work}/r1.json" >/dev/null 2>&1
typo_rc=$?
set -e
[[ "${typo_rc}" -ne 0 ]] || fail "an explicitly named missing baseline must not silently disable the ratchet"
# but /dev/null stays the documented way to render a first-ever table
run_ratcheted --baseline /dev/null "${work}/r1.json" >/dev/null \
	|| fail "--baseline /dev/null must remain a legitimate no-baseline render"

# Default-baseline resolution: with no --baseline, an existing --out file IS
# the baseline (the path CI takes). A regressed render must exit 3 and leave
# the file byte-identical -- the write happens only after the ratchet passes.
cp "${work}/baseline.txt" "${work}/live-out.txt"
set +e
bash "${script}" --named-from "${work}/named.txt" --out "${work}/live-out.txt" \
	"${work}/regressed.json" >/dev/null 2>&1
live_rc=$?
set -e
[[ "${live_rc}" -eq 3 ]] || fail "default-baseline regressed render exit ${live_rc}, want 3"
cmp -s "${work}/live-out.txt" "${work}/baseline.txt" \
	|| fail "a refused render must not touch the default-baseline --out file"

# The default row is ratcheted too: it is the fallback budget for every unnamed
# route, so inflating it disarms all of them at once.
printf 'default\t9\t100\t2\n\nGET /a\t20\t3000\t10\tGREEN-derived work guard\n' >"${work}/tight_default.txt"
set +e
def_out="$(run_ratcheted --baseline "${work}/tight_default.txt" "${work}/r1.json" "${work}/r2.json" 2>&1 >/dev/null)"
def_rc=$?
set -e
[[ "${def_rc}" -eq 3 ]] || fail "an inflated default row must trip the ratchet, got ${def_rc}"
printf '%s' "${def_out}" | rg -q '^  default: 100 -> ' || fail "refusal must name the default row: ${def_out}"

# calls and rows are ratcheted on the same thresholds as blks: an N+1 shows up in
# calls and a result explosion in rows, and guarding only blks would leave the
# same hole in two other columns.
printf 'default\t9\t2100\t2\n\nGET /a\t200\t3000\t400\tGREEN-derived work guard\n' >"${work}/counters_base.txt"
cat >"${work}/n_plus_one.json" <<'JSON'
{"routes":[
 {"route":"GET /a","p95_ms":1,"work":{"calls":800,"blks":1000,"rows":200}},
 {"route":"GET /c","p95_ms":1,"work":{"calls":2,"blks":500,"rows":0}}
]}
JSON
set +e
calls_out="$(run_ratcheted --baseline "${work}/counters_base.txt" "${work}/n_plus_one.json" 2>&1 >/dev/null)"
calls_rc=$?
set -e
[[ "${calls_rc}" -eq 3 ]] || fail "an N+1 (calls 200 -> 1005) must trip the ratchet, got ${calls_rc}"
printf '%s' "${calls_out}" | rg -q 'GET /a: 200 -> [0-9]+ calls' || fail "refusal must name the calls counter: ${calls_out}"

# Below the noise floor a ratio is meaningless: the default row's rows can go
# 2 -> 0 legitimately, and that must not read as a 100% collapse.
run_ratcheted --baseline "${work}/baseline.txt" "${work}/r1.json" "${work}/r2.json" >/dev/null \
	|| fail "small-value counters must not trip the ratchet as noise"

# A route absent from the baseline is new and has nothing to ratchet against.
printf 'default\t9\t2100\t2\n' >"${work}/empty_baseline.txt"
run_ratcheted --baseline "${work}/empty_baseline.txt" "${work}/r1.json" "${work}/r2.json" >/dev/null \
	|| fail "a route with no committed row must not trip the ratchet"

# The ratchet must not change the rendered bytes.
ratchet_out="$(run_ratcheted --baseline "${work}/baseline.txt" "${work}/r1.json" "${work}/r2.json")"
[[ "${ratchet_out}" == "${out1}" ]] || fail "the ratchet must gate output, never alter it"

printf 'test-refresh-read-api-work-budgets: PASS\n'
