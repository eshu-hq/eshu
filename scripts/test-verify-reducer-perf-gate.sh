#!/usr/bin/env bash
#
# test-verify-reducer-perf-gate.sh — contract mirror for verify-reducer-perf-gate.sh,
# refresh-reducer-handler-budgets.sh, the committed budget file, and the CI wiring
# (B-9 / #3802).
#
# Static checks assert the gate keeps its credential-free, advisory-then-blocking
# shape and that the workflow wires it in. Functional checks feed SYNTHETIC pinned
# results (no benchmarks run) so the median parser and breach logic are exercised
# deterministically: a breach is caught when enforced, is advisory by default, a
# within-budget run passes, and a missing-sample (renamed benchmark) fails closed.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/verify-reducer-perf-gate.sh"
refresh="${repo_root}/scripts/refresh-reducer-handler-budgets.sh"
budgets="${repo_root}/testdata/benchmarks/reducer-handler-budgets.txt"
workflow="${repo_root}/.github/workflows/bench.yml"

fail=0
note() { printf 'test-verify-reducer-perf-gate: %s\n' "$*" >&2; }
check() { if [[ "$2" -ne 0 ]]; then note "FAIL: $1"; fail=1; fi; }

command -v rg >/dev/null 2>&1 || { note "missing required tool: rg"; exit 2; }

# --- Static contract ----------------------------------------------------------
[[ -f "${script}" ]]; check "verify-reducer-perf-gate.sh exists" $?
[[ -x "${script}" ]]; check "verify-reducer-perf-gate.sh executable" $?
bash -n "${script}"; check "verify-reducer-perf-gate.sh syntax" $?
[[ -f "${refresh}" ]]; check "refresh-reducer-handler-budgets.sh exists" $?
[[ -x "${refresh}" ]]; check "refresh-reducer-handler-budgets.sh executable" $?
bash -n "${refresh}"; check "refresh-reducer-handler-budgets.sh syntax" $?
rg -q "REDUCER_PERF_ENFORCE" "${script}"; check "gate has enforce toggle (advisory by default)" $?
rg -q "median" "${script}"; check "gate compares the median ns/op" $?
# Credential-free: the gate must clear inherited backend DSNs, never set one.
rg -q -- "-u ESHU_POSTGRES_DSN" "${script}"; check "gate clears inherited Postgres DSN" $?
rg -q -- "-u ESHU_REDUCER_CLAIM_BENCH_DSN" "${script}"; check "gate clears inherited reducer claim DSN" $?
if rg -q "ESHU_[A-Z_]*DSN=|postgres://|bolt://" "${script}"; then
	note "FAIL: gate sets a backend DSN (must stay credential-free)"; fail=1
fi

# --- Budget file contract -----------------------------------------------------
[[ -f "${budgets}" ]]; check "budget file exists" $?
# Every data row must be name<TAB>budget_ns<TAB>baseline_ns with numeric columns.
bad_rows="$(awk -F'\t' '
	/^[#]/ || /^[[:space:]]*$/ { next }
	(NF!=3 || $2 !~ /^[0-9]+$/ || $3 !~ /^[0-9]+$/) { print NR": "$0 }
' "${budgets}")"
[[ -z "${bad_rows}" ]]; check "every budget row is name<TAB>budget_ns<TAB>baseline_ns" $?
# Each budget must be >= its recorded baseline (a ceiling below the measured
# baseline would be a guaranteed-flaky gate).
below_baseline="$(awk -F'\t' '
	/^[#]/ || /^[[:space:]]*$/ { next }
	(NF==3 && $2+0 < $3+0) { print $1 }
' "${budgets}")"
[[ -z "${below_baseline}" ]]; check "no budget is below its recorded baseline" $?
# At least one node-materialization, one edge-materialization, one correlation,
# and one fixpoint-cache handler are budgeted (coverage of the handler families).
rg -q "BenchmarkExtract.*NodeRows" "${budgets}"; check "budgets cover a node-materialization handler" $?
rg -q "BenchmarkExtract.*EdgeRows" "${budgets}"; check "budgets cover an edge-materialization handler" $?
rg -q "BenchmarkBuildServiceCatalogCorrelation" "${budgets}"; check "budgets cover a correlation handler" $?
rg -q "BenchmarkValueFlowFixpoint" "${budgets}"; check "budgets cover a value-flow fixpoint handler" $?

# --- Workflow wiring ----------------------------------------------------------
[[ -f "${workflow}" ]]; check "bench.yml exists" $?
rg -q "verify-reducer-perf-gate.sh" "${workflow}"; check "bench.yml runs the reducer perf gate" $?
rg -q "test-verify-reducer-perf-gate.sh" "${workflow}"; check "bench.yml runs this mirror" $?
rg -q "REDUCER_PERF_ENFORCE" "${workflow}"; check "bench.yml sets the enforce toggle" $?
rg -q "pull_request" "${workflow}"; check "bench.yml triggers on pull_request" $?

# --- Functional: median parser + breach logic via synthetic pinned results ----
tmp="$(mktemp -d)"; trap 'rm -rf "${tmp}" 2>/dev/null || true' EXIT

# A tiny two-handler budget file: one cheap (100000 ns), one expensive (200000).
cat >"${tmp}/budgets.txt" <<'TXT'
# synthetic test budgets
BenchmarkAlpha	100000	60000
BenchmarkBeta	200000	130000
TXT

# Synthetic results: Alpha within budget (median 60000), Beta OVER budget
# (median 250000 > 200000). The -P suffix mirrors real go bench output, and the
# medians come from three samples each so the median path (odd count) is used.
cat >"${tmp}/over.txt" <<'TXT'
goos: linux
goarch: amd64
pkg: example/reducer
BenchmarkAlpha-8   	   20000	     59000 ns/op	  100 B/op	  1 allocs/op
BenchmarkAlpha-8   	   20000	     60000 ns/op	  100 B/op	  1 allocs/op
BenchmarkAlpha-8   	   20000	     61000 ns/op	  100 B/op	  1 allocs/op
BenchmarkBeta-8    	   10000	    240000 ns/op	  200 B/op	  2 allocs/op
BenchmarkBeta-8    	   10000	    250000 ns/op	  200 B/op	  2 allocs/op
BenchmarkBeta-8    	   10000	    260000 ns/op	  200 B/op	  2 allocs/op
TXT

# All-within-budget results: Beta median 150000 < 200000.
sed 's/24[0-9]000 ns/140000 ns/; s/250000 ns/150000 ns/; s/260000 ns/160000 ns/' \
	"${tmp}/over.txt" >"${tmp}/within.txt"

# Missing-sample results: Beta dropped entirely (renamed benchmark scenario).
rg -v '^BenchmarkBeta' "${tmp}/over.txt" >"${tmp}/missing.txt"

run_gate() { # run_gate <results> <enforce> ; prints rc
	local rc=0
	REDUCER_BUDGETS="${tmp}/budgets.txt" REDUCER_PERF_RESULTS="$1" REDUCER_PERF_ENFORCE="$2" \
		"${script}" >"${tmp}/out.log" 2>&1 || rc=$?
	printf '%s' "${rc}"
}

# Breach, advisory (default): reports but exits 0.
rc="$(run_gate "${tmp}/over.txt" false)"
[[ "${rc}" -eq 0 ]]; check "breach is advisory by default (exit 0)" $?
rg -q "budget breaches" "${tmp}/out.log"; check "advisory run reports the breach" $?
rg -q "BenchmarkBeta: median 250000 ns/op > budget 200000" "${tmp}/out.log"; check "advisory run names the breaching handler + median" $?

# Breach, enforced: exits non-zero.
rc="$(run_gate "${tmp}/over.txt" true)"
[[ "${rc}" -ne 0 ]]; check "breach fails when enforced (exit != 0)" $?

# Within budget, enforced: passes.
rc="$(run_gate "${tmp}/within.txt" true)"
[[ "${rc}" -eq 0 ]]; check "within-budget run passes even when enforced" $?

# Missing sample (renamed benchmark): fails closed regardless of enforce.
rc="$(run_gate "${tmp}/missing.txt" false)"
[[ "${rc}" -ne 0 ]]; check "missing benchmark samples fail closed" $?
rg -q "no samples found" "${tmp}/out.log"; check "missing-sample run explains the failure" $?

if [[ "${fail}" -ne 0 ]]; then note "contract mirror FAILED"; exit 1; fi
note "contract mirror passed"
