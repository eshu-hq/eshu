#!/usr/bin/env bash
#
# test-verify-bench-regression.sh — contract mirror for verify-bench-regression.sh
# and the bench regression CI wiring. Static checks plus functional checks that
# feed synthetic baseline/current result files (no benchmarks run) so the
# benchstat regression parser is exercised deterministically: a >10% sec/op
# regression is caught, a small change is not, and advisory vs enforce behaves.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/verify-bench-regression.sh"
refresh="${repo_root}/scripts/refresh-bench-baseline.sh"
workflow="${repo_root}/.github/workflows/bench.yml"
weekly="${repo_root}/.github/workflows/bench-baseline-refresh.yml"

fail=0
note() { printf 'test-verify-bench-regression: %s\n' "$*" >&2; }
check() { if [[ "$2" -ne 0 ]]; then note "FAIL: $1"; fail=1; fi; }

command -v rg >/dev/null 2>&1 || { note "missing required tool: rg"; exit 2; }
command -v benchstat >/dev/null 2>&1 || { note "missing required tool: benchstat"; exit 2; }

# --- Static contract ----------------------------------------------------------
[[ -f "${script}" ]]; check "verify-bench-regression.sh exists" $?
[[ -x "${script}" ]]; check "verify-bench-regression.sh executable" $?
bash -n "${script}"; check "verify-bench-regression.sh syntax" $?
[[ -x "${refresh}" ]]; check "refresh-bench-baseline.sh executable" $?
bash -n "${refresh}"; check "refresh-bench-baseline.sh syntax" $?
rg -q "benchstat" "${script}"; check "gate uses benchstat" $?
rg -q -- "-format csv" "${script}"; check "gate parses benchstat csv" $?
rg -q "BENCH_REGRESSION_ENFORCE" "${script}"; check "gate has enforce toggle" $?
# Clean-filter lib: keeps app log output out of the benchstat input.
clean_lib="${repo_root}/scripts/lib/benchstat-clean.sh"
[[ -f "${clean_lib}" ]]; check "benchstat-clean lib exists" $?
bash -n "${clean_lib}"; check "benchstat-clean lib syntax" $?
rg -q "benchstat_clean_filter" "${script}"; check "gate filters generated current via the lib" $?
rg -q "benchstat_clean_filter" "${refresh}"; check "refresh filters the baseline via the lib" $?
# The committed baseline must already be clean: no app-log timestamps.
if rg -q "20[0-9][0-9]/[0-9]" "${baseline:=${repo_root}/testdata/benchmarks/baseline.txt}"; then
	note "FAIL: committed baseline contains app-log lines (timestamps); regenerate via refresh-bench-baseline.sh"
	fail=1
fi
[[ -f "${baseline:=${repo_root}/testdata/benchmarks/baseline.txt}" ]]; check "committed baseline.txt exists" $?

# Workflow wiring: PR regression job + weekly refresh schedule.
[[ -f "${workflow}" ]]; check "bench.yml exists" $?
rg -q "verify-bench-regression.sh" "${workflow}"; check "bench.yml runs the regression gate" $?
if rg -q "BENCH_COUNT: 1" "${workflow}"; then
	check "bench regression workflow uses the bounded one-pass sample" 0
else
	check "bench regression workflow uses the bounded one-pass sample" 1
fi
if rg -q "BENCH_COUNT: 6" "${workflow}"; then
	check "bench regression workflow avoids the six-pass sample that exceeds CI budget" 1
else
	check "bench regression workflow avoids the six-pass sample that exceeds CI budget" 0
fi
[[ -f "${weekly}" ]]; check "weekly refresh workflow exists" $?
rg -q "schedule:" "${weekly}"; check "weekly workflow is scheduled" $?
rg -q "refresh-bench-baseline.sh" "${weekly}"; check "weekly workflow refreshes the baseline" $?
if rg -F -q 'BENCH_COUNT="${BENCH_COUNT:-1}"' "${script}"; then
	check "gate generation defaults to the bounded one-pass sample" 0
else
	check "gate generation defaults to the bounded one-pass sample" 1
fi
if rg -F -q 'BENCH_COUNT="${BENCH_COUNT:-1}"' "${refresh}"; then
	check "baseline refresh defaults to the same bounded one-pass sample" 0
else
	check "baseline refresh defaults to the same bounded one-pass sample" 1
fi

# --- Functional: benchstat regression parser ---------------------------------
tmp="$(mktemp -d)"; trap 'rm -rf "${tmp}" 2>/dev/null || true' EXIT
# A baseline with one benchmark at ~100ns, count=6. The body lives in
# scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes the entire
# heredoc body to a pipe before forking the reader, and macOS's 512-byte
# pipe buffer deadlocks on any body over that size (#5074).
cat "${repo_root}/scripts/lib/test-verify-bench-regression-base.txt" >"${tmp}/base.txt"
# A regressed current: ~130ns (+30%).
sed 's/100.0 ns\/op/130.0 ns\/op/' "${tmp}/base.txt" >"${tmp}/cur_regress.txt"
# A near-flat current: ~101ns.
sed 's/100.0 ns\/op/101.0 ns\/op/' "${tmp}/base.txt" >"${tmp}/cur_flat.txt"

# Regression, advisory (default): reports but exits 0.
if BENCH_BASELINE="${tmp}/base.txt" BENCH_CURRENT="${tmp}/cur_regress.txt" \
	"${script}" >"${tmp}/out_adv.log" 2>&1; then adv_rc=0; else adv_rc=$?; fi
[[ "${adv_rc}" -eq 0 ]]; check "regression is advisory by default (exit 0)" $?
rg -q "regressions > 10% vs baseline" "${tmp}/out_adv.log"; check "advisory run reports the regression" $?

# Regression, enforced: exits non-zero.
if BENCH_BASELINE="${tmp}/base.txt" BENCH_CURRENT="${tmp}/cur_regress.txt" BENCH_REGRESSION_ENFORCE=true \
	"${script}" >"${tmp}/out_enf.log" 2>&1; then enf_rc=0; else enf_rc=$?; fi
[[ "${enf_rc}" -ne 0 ]]; check "regression fails when enforced (exit != 0)" $?

# Flat change, enforced: passes (benchstat reports ~ for an insignificant +1%).
if BENCH_BASELINE="${tmp}/base.txt" BENCH_CURRENT="${tmp}/cur_flat.txt" BENCH_REGRESSION_ENFORCE=true \
	"${script}" >"${tmp}/out_flat.log" 2>&1; then flat_rc=0; else flat_rc=$?; fi
[[ "${flat_rc}" -eq 0 ]]; check "near-flat change passes even when enforced" $?

if [[ "${fail}" -ne 0 ]]; then note "contract mirror FAILED"; exit 1; fi
note "contract mirror passed"
