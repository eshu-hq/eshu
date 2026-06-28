#!/usr/bin/env bash
#
# verify-reducer-perf-gate.sh — B-9 (#3802): per-materialization-handler latency
# gate for the reducer.
#
# Splitting files (Epic D) does not make materialization faster. This gate locks
# the per-handler cost of the credential-free reducer materialization and
# correlation micro-benchmarks against an absolute ns/op budget so a handler
# regression is caught on main regardless of the repo/monorepo layout.
#
# It is complementary to the benchstat-relative bench-regression gate (B-2,
# #3795): that one flags a >10% sec/op drift vs a committed baseline; this one
# asserts each handler stays under a fixed ceiling derived from the locked
# baseline with documented headroom (see testdata/benchmarks/reducer-handler-budgets.txt).
#
# Hermetic by construction: the budgeted benchmarks are pure in-process
# extractors over fixture facts (no DSN, no Postgres/NornicDB). This script
# clears any inherited backend DSN so the run can never dial a live database.
#
# Enforcement: the committed budgets' baseline column was captured on
# darwin/arm64, NOT the CI ubuntu-latest runner class, so an absolute single-run
# ceiling there is only a like-for-like comparison once the baseline is
# recaptured on the enforcement runner. Until then the gate defaults to ADVISORY
# (report, exit 0) — same shared-runner-variance reasoning as the B-2 gate. Set
# REDUCER_PERF_ENFORCE=true (safe once the budgets are CI-refreshed via
# scripts/refresh-reducer-handler-budgets.sh) to turn a breach into a non-zero exit.
#
# Tunables (env):
#   REDUCER_BUDGETS         budget file       (default testdata/benchmarks/reducer-handler-budgets.txt)
#   REDUCER_PERF_RESULTS    pin a results file (skip running benchmarks; for the mirror)
#   REDUCER_PERF_PACKAGE    package to bench   (default ./internal/reducer)
#   REDUCER_PERF_BENCHTIME  -benchtime value   (default 100ms)
#   REDUCER_PERF_COUNT      -count value       (default 6)
#   REDUCER_PERF_TIMEOUT    go test -timeout   (default 20m)
#   REDUCER_PERF_ENFORCE    fail on breach     (default false = advisory)
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
budgets="${REDUCER_BUDGETS:-${repo_root}/testdata/benchmarks/reducer-handler-budgets.txt}"
results_pinned="${REDUCER_PERF_RESULTS:-}"
bench_package="${REDUCER_PERF_PACKAGE:-./internal/reducer}"
bench_time="${REDUCER_PERF_BENCHTIME:-100ms}"
bench_count="${REDUCER_PERF_COUNT:-6}"
bench_timeout="${REDUCER_PERF_TIMEOUT:-20m}"
enforce="${REDUCER_PERF_ENFORCE:-false}"

die() {
	printf 'verify-reducer-perf-gate: %s\n' "$*" >&2
	exit 1
}

command -v rg >/dev/null 2>&1 || die "missing required tool: rg"
command -v awk >/dev/null 2>&1 || die "missing required tool: awk"
[[ -f "${budgets}" ]] || die "budget file not found: ${budgets}"

# Parse the budget file into two parallel index arrays (bash 3.2 has no
# associative arrays). Each data row: benchmark<TAB>budget_ns<TAB>baseline_ns.
bench_names=()
bench_budgets=()
while IFS=$'\t' read -r name budget _rest; do
	[[ -z "${name}" || "${name}" == \#* ]] && continue
	[[ "${budget}" =~ ^[0-9]+$ ]] || die "non-numeric budget for ${name}: '${budget}'"
	bench_names+=("${name}")
	bench_budgets+=("${budget}")
done <"${budgets}"

[[ "${#bench_names[@]}" -gt 0 ]] || die "no benchmark budgets parsed from ${budgets}"

# Resolve the results. A caller-pinned REDUCER_PERF_RESULTS is used verbatim (the
# mirror feeds synthetic results); otherwise run the budgeted benchmarks once.
# Build an anchored alternation regexp from the budgeted names so only those run.
results="${results_pinned}"
if [[ -z "${results}" ]]; then
	command -v go >/dev/null 2>&1 || die "missing required tool: go"
	pattern=""
	for name in "${bench_names[@]}"; do
		pattern="${pattern:+${pattern}|}^${name}\$"
	done
	results="$(mktemp)"
	trap 'rm -f "${results}" 2>/dev/null || true' EXIT
	printf 'verify-reducer-perf-gate: running %d handler benchmarks (count=%s time=%s) in %s\n' \
		"${#bench_names[@]}" "${bench_count}" "${bench_time}" "${bench_package}" >&2
	# Clear inherited backend DSNs so the run stays hermetic regardless of the
	# ambient environment (-u is portable across GNU and BSD env).
	if ! (
		cd "${repo_root}/go" &&
			env -u ESHU_POSTGRES_DSN -u ESHU_REDUCER_CLAIM_BENCH_DSN \
				go test "${bench_package}" \
				-run='^$' \
				-bench="${pattern}" \
				-benchmem \
				-benchtime="${bench_time}" \
				-count="${bench_count}" \
				-timeout="${bench_timeout}"
	) >"${results}" 2>&1; then
		cat "${results}" >&2
		die "reducer handler benchmark run failed; see output above"
	fi
else
	[[ -f "${results}" ]] || die "REDUCER_PERF_RESULTS set but not found: ${results}"
fi

# For each budgeted benchmark, take the MEDIAN ns/op across the result samples
# (same statistic the budgets were derived from) and compare to the ceiling.
# Go benchmark line: "Benchmark<Name>-<P>\t<iters>\t<ns> ns/op\t...". Match on the
# exact name followed by the -P CPU suffix so a prefix name cannot capture a
# longer benchmark's samples.
breaches=""
missing=""
for i in "${!bench_names[@]}"; do
	name="${bench_names[$i]}"
	budget="${bench_budgets[$i]}"
	# `|| true` so an empty result set (no samples) does not abort the script
	# under `set -e`; the empty-median case is handled explicitly below.
	median="$(
		rg -N "^${name}-[0-9]+[[:space:]]" "${results}" 2>/dev/null |
			awk '{ for (j=1;j<=NF;j++) if ($(j+1)=="ns/op") print $j }' |
			sort -n |
			awk '{ a[NR]=$1 } END { if (NR==0) exit; if (NR%2) print a[(NR+1)/2]; else printf "%d\n", (a[NR/2]+a[NR/2+1])/2 }' || true
	)"
	if [[ -z "${median}" ]]; then
		missing="${missing}${missing:+ }${name}"
		continue
	fi
	if awk -v m="${median}" -v b="${budget}" 'BEGIN { exit !(m+0 > b+0) }'; then
		pct="$(awk -v m="${median}" -v b="${budget}" 'BEGIN { printf "%.1f", (m/b-1)*100 }')"
		breaches="${breaches}${breaches:+\n}  ${name}: median ${median} ns/op > budget ${budget} ns/op (+${pct}%)"
	else
		printf 'verify-reducer-perf-gate: OK %s median %s ns/op <= budget %s ns/op\n' \
			"${name}" "${median}" "${budget}" >&2
	fi
done

if [[ -n "${missing}" ]]; then
	die "no samples found for budgeted benchmark(s): ${missing} (did the benchmark get renamed? update ${budgets})"
fi

if [[ -n "${breaches}" ]]; then
	printf '\nverify-reducer-perf-gate: per-handler latency budget breaches:\n' >&2
	printf '%b\n' "${breaches}" >&2
	if [[ "${enforce}" == "true" ]]; then
		die "reducer per-handler latency gate failed (refresh the baseline if this is an intended change: scripts/refresh-reducer-handler-budgets.sh)"
	fi
	printf 'verify-reducer-perf-gate: ADVISORY (REDUCER_PERF_ENFORCE!=true) — not failing\n' >&2
	exit 0
fi

printf 'verify-reducer-perf-gate: all %d handlers within budget\n' "${#bench_names[@]}" >&2
