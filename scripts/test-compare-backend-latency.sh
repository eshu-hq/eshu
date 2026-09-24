#!/usr/bin/env bash
# test-compare-backend-latency.sh - test mirror for
# scripts/compare-backend-latency.sh. Runs without Docker, Postgres, or a Go
# build: a GREEN pair of planted fixture reports renders the comparison
# table, and RED on a differing identity field, a planted Postgres-work
# mismatch (the route is listed as non-comparable, not compared), and
# runs < 3. Each RED case is checked for the SPECIFIC reason it must fail,
# not just a nonzero exit, so a script that refuses everything cannot pass
# this mirror by accident.
set -euo pipefail
export LC_ALL=C

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/compare-backend-latency.sh"
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

fail() {
	printf 'test-compare-backend-latency: FAIL: %s\n' "$*" >&2
	exit 1
}

# base_report BACKEND -- a minimal valid schema-version-1 report with two
# comparable routes (GET /a, GET /b), written to "${work}/${1}.json".
# The template lives in scripts/lib/test-compare-backend-latency-report.json
# (the heredoc budget gate caps inline heredocs at 512 bytes).
fixture="${repo_root}/scripts/lib/test-compare-backend-latency-report.json"
base_report() {
	local backend="$1" p95_a="$2" p95_b="$3"
	jq --arg backend "${backend}" --argjson a "${p95_a}" --argjson b "${p95_b}" '
		.identity.backend = $backend
		| .routes[0].warm |= (.p95_ms = $a | .max_ms = $a | .run_p95_max_ms = $a)
		| .routes[1].warm |= (.p95_ms = $b | .max_ms = $b | .run_p95_max_ms = $b)
	' "${fixture}" >"${work}/${backend}.json"
}

base_report nornicdb 13 7
base_report neo4j 26 4

# --- GREEN: a matching identity pair renders the table with both routes
# compared, no NON-COMPARABLE rows. ---
out="$(bash "${script}" "${work}/nornicdb.json" "${work}/neo4j.json")" || fail "GREEN pair did not render"
printf '%s\n' "${out}" | rg -qF '| GET /a | 10.00 | 13.00 | 10.00 | 26.00 | 2x | 80/80 |' || fail "GET /a row missing or wrong:\n${out}"
printf '%s\n' "${out}" | rg -qF '| GET /b | 5.00 | 7.00 | 5.00 | 4.00 | 0.57x | 80/80 |' || fail "GET /b row missing or wrong:\n${out}"
printf '%s\n' "${out}" | rg -q 'NON-COMPARABLE' && fail "GREEN pair must have no NON-COMPARABLE rows:\n${out}"

# --- RED: --out writes the same bytes as stdout. ---
bash "${script}" --out "${work}/table.md" "${work}/nornicdb.json" "${work}/neo4j.json"
cmp -s <(printf '%s\n' "${out}") "${work}/table.md" || fail "--out differs from stdout"

expect_exit_and_reason() {
	local want_exit="$1" want_reason="$2"
	shift 2
	local got_exit=0 got_output
	got_output="$("$@" 2>&1)" || got_exit=$?
	[[ "${got_exit}" -eq "${want_exit}" ]] || fail "exit ${got_exit}, want ${want_exit}: $* -- output: ${got_output}"
	printf '%s' "${got_output}" | rg -qF "${want_reason}" || fail "output did not name the reason (${want_reason}): ${got_output}"
}

# --- RED: a differing identity field (seed_options.total_scopes) refuses
# with exit 2, naming the field. ---
jq '.identity.seed_options.total_scopes = 801' "${work}/neo4j.json" >"${work}/neo4j-scopes.json"
expect_exit_and_reason 2 "seed_options.total_scopes" \
	bash "${script}" "${work}/nornicdb.json" "${work}/neo4j-scopes.json"

# --- RED: a differing eshu_commit refuses, naming the field. ---
jq '.identity.eshu_commit = "other-commit"' "${work}/neo4j.json" >"${work}/neo4j-commit.json"
expect_exit_and_reason 2 "eshu_commit" \
	bash "${script}" "${work}/nornicdb.json" "${work}/neo4j-commit.json"

# --- RED: a differing api_binary_sha256 refuses, naming the field. ---
jq '.identity.api_binary_sha256 = "different-sha"' "${work}/neo4j.json" >"${work}/neo4j-sha.json"
expect_exit_and_reason 2 "api_binary_sha256" \
	bash "${script}" "${work}/nornicdb.json" "${work}/neo4j-sha.json"

# --- RED: runs < 3 on either leg refuses. ---
jq '.identity.runs = 2' "${work}/neo4j.json" >"${work}/neo4j-runs2.json"
expect_exit_and_reason 2 "identity.runs=2" \
	bash "${script}" "${work}/nornicdb.json" "${work}/neo4j-runs2.json"
jq '.identity.runs = 1' "${work}/nornicdb.json" >"${work}/nornicdb-runs1.json"
expect_exit_and_reason 2 "identity.runs=1" \
	bash "${script}" "${work}/nornicdb-runs1.json" "${work}/neo4j.json"

# --- Planted Postgres-work mismatch: the route is listed NON-COMPARABLE,
# not compared -- the OTHER route must still compare normally, so this is
# not the same as a hard refusal. ---
jq '.routes[0].work.calls = 900' "${work}/neo4j.json" >"${work}/neo4j-work.json"
out="$(bash "${script}" "${work}/nornicdb.json" "${work}/neo4j-work.json")" || fail "planted work mismatch must not refuse the whole comparison"
printf '%s\n' "${out}" | rg -qF '| GET /a | - | - | - | - | - | NON-COMPARABLE (Postgres work differs) |' \
	|| fail "GET /a must be listed NON-COMPARABLE (Postgres work differs):\n${out}"
printf '%s\n' "${out}" | rg -qF '| GET /b | 5.00 | 7.00 | 5.00 | 4.00 | 0.57x | 80/80 |' \
	|| fail "GET /b must still compare normally despite GET /a's work mismatch:\n${out}"

# --- Planted status mismatch: same non-comparable-but-not-refused shape. ---
jq '.routes[1].status = 500' "${work}/neo4j.json" >"${work}/neo4j-status.json"
out="$(bash "${script}" "${work}/nornicdb.json" "${work}/neo4j-status.json")" || fail "planted status mismatch must not refuse the whole comparison"
printf '%s\n' "${out}" | rg -qF 'NON-COMPARABLE (status 200 vs 500)' || fail "status mismatch not reported:\n${out}"

# --- A route missing entirely from one leg is NON-COMPARABLE, not an error. ---
jq '.routes += [{"route":"GET /only-neo4j","exercised":true,"status":200,"metered":true,"work":{"calls":1,"rows":1,"blks":1},"warm":{"n":80,"p50_ms":1,"p95_ms":1,"min_ms":1,"max_ms":1,"stddev_ms":0,"run_p95_min_ms":1,"run_p95_max_ms":1}}]' \
	"${work}/neo4j.json" >"${work}/neo4j-extra-route.json"
out="$(bash "${script}" "${work}/nornicdb.json" "${work}/neo4j-extra-route.json")" || fail "an extra route on one leg must not refuse the comparison"
printf '%s\n' "${out}" | rg -qF 'GET /only-neo4j | - | - | - | - | - | NON-COMPARABLE (missing on left)' \
	|| fail "extra route not reported as NON-COMPARABLE (missing on left):\n${out}"

# --- A zero left-leg p95 must not abort the WHOLE table (jq division by
# zero would otherwise kill every route's render, not just the degenerate
# one): the route is listed NON-COMPARABLE and the other route still
# compares, and the script exits 0. ---
jq '.routes[0].warm.p95_ms = 0' "${work}/nornicdb.json" >"${work}/nornicdb-zerop95.json"
out="$(bash "${script}" "${work}/nornicdb-zerop95.json" "${work}/neo4j.json")" || fail "a zero left p95 must not abort the whole comparison"
printf '%s\n' "${out}" | rg -qF '| GET /a | - | - | - | - | - | NON-COMPARABLE (left p95 is 0ms; ratio is undefined) |' \
	|| fail "GET /a with a zero left p95 must render NON-COMPARABLE, not crash the table:\n${out}"
printf '%s\n' "${out}" | rg -qF '| GET /b | 5.00 | 7.00 | 5.00 | 4.00 | 0.57x | 80/80 |' \
	|| fail "GET /b must still compare normally despite GET /a's zero p95:\n${out}"

# --- A non-integer identity.runs refuses (exit 2, naming the field) instead
# of letting bash's `[[ -ge ]]` throw its own uncaught arithmetic error. ---
jq '.identity.runs = 3.5' "${work}/neo4j.json" >"${work}/neo4j-runs-float.json"
expect_exit_and_reason 2 "identity.runs must be an integer" \
	bash "${script}" "${work}/nornicdb.json" "${work}/neo4j-runs-float.json"

# --- Malformed input is refused, not silently mis-rendered. ---
printf '{"not":"a report"}' >"${work}/bad.json"
expect_exit_and_reason 2 "not a schema-version-1 latency report" \
	bash "${script}" "${work}/bad.json" "${work}/neo4j.json"

# --- Usage error for the wrong argument count. ---
expect_exit_and_reason 2 "usage:" bash "${script}" "${work}/nornicdb.json"

printf 'test-compare-backend-latency: PASS\n'
