#!/usr/bin/env bash
#
# refresh-reducer-handler-budgets.sh — B-9 (#3802): recapture the per-handler
# baseline column in testdata/benchmarks/reducer-handler-budgets.txt and recompute
# each ceiling as round(median * REDUCER_PERF_HEADROOM).
#
# Run on the enforcement runner class (CI ubuntu-latest) so the committed
# baseline/ceiling are a like-for-like comparison target for
# scripts/verify-reducer-perf-gate.sh, then flip the bench.yml job to
# REDUCER_PERF_ENFORCE=true. Review the diff before committing — a budget bump is
# a reviewed claim that the new timing is the expected normal.
#
# The benchmark NAME set is the source of truth: this script preserves exactly
# the rows already present in the budget file (it never adds or drops a handler).
# To add a handler, add its row by hand first, then run this to fill in numbers.
#
# Tunables (env):
#   REDUCER_BUDGETS         budget file        (default testdata/benchmarks/reducer-handler-budgets.txt)
#   REDUCER_PERF_BENCHTIME  -benchtime value    (default 100ms)
#   REDUCER_PERF_COUNT      -count value        (default 6)
#   REDUCER_PERF_HEADROOM   ceiling multiplier  (default 1.50)
#   REDUCER_PERF_PACKAGE    package to bench     (default ./internal/reducer)
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
budgets="${REDUCER_BUDGETS:-${repo_root}/testdata/benchmarks/reducer-handler-budgets.txt}"
bench_time="${REDUCER_PERF_BENCHTIME:-100ms}"
bench_count="${REDUCER_PERF_COUNT:-6}"
headroom="${REDUCER_PERF_HEADROOM:-1.50}"
bench_package="${REDUCER_PERF_PACKAGE:-./internal/reducer}"

die() {
	printf 'refresh-reducer-handler-budgets: %s\n' "$*" >&2
	exit 1
}

command -v go >/dev/null 2>&1 || die "missing required tool: go"
command -v rg >/dev/null 2>&1 || die "missing required tool: rg"
command -v awk >/dev/null 2>&1 || die "missing required tool: awk"
[[ -f "${budgets}" ]] || die "budget file not found: ${budgets}"

# Preserve the file's leading comment header verbatim (everything up to the first
# data row) so the documented rationale is not lost on a refresh.
header="$(awk '/^[^#[:space:]]/ { exit } { print }' "${budgets}")"

# Parse existing handler names (data rows) in order.
names=()
while IFS=$'\t' read -r name _budget _baseline; do
	[[ -z "${name}" || "${name}" == \#* ]] && continue
	names+=("${name}")
done <"${budgets}"
[[ "${#names[@]}" -gt 0 ]] || die "no handler rows parsed from ${budgets}"

# Run exactly the existing handler benchmarks (anchored alternation).
pattern=""
for name in "${names[@]}"; do
	pattern="${pattern:+${pattern}|}^${name}\$"
done

raw="$(mktemp)"
trap 'rm -f "${raw}" 2>/dev/null || true' EXIT
printf 'refresh-reducer-handler-budgets: benchmarking %d handlers (count=%s time=%s)\n' \
	"${#names[@]}" "${bench_count}" "${bench_time}" >&2
(
	cd "${repo_root}/go" &&
		env -u ESHU_POSTGRES_DSN -u ESHU_REDUCER_CLAIM_BENCH_DSN \
			go test "${bench_package}" \
			-run='^$' -bench="${pattern}" -benchmem \
			-benchtime="${bench_time}" -count="${bench_count}" -timeout=20m
) >"${raw}" 2>&1 || { cat "${raw}" >&2; die "benchmark run failed"; }

# Rewrite the file: header + recomputed data rows.
tmp_out="$(mktemp)"
printf '%s\n' "${header}" >"${tmp_out}"
for name in "${names[@]}"; do
	median="$(
		rg -N "^${name}-[0-9]+[[:space:]]" "${raw}" |
			awk '{ for (j=1;j<=NF;j++) if ($(j+1)=="ns/op") print $j }' |
			sort -n |
			awk '{ a[NR]=$1 } END { if (NR==0) exit; if (NR%2) print a[(NR+1)/2]; else printf "%d\n", (a[NR/2]+a[NR/2+1])/2 }' || true
	)"
	[[ -n "${median}" ]] || die "no samples for ${name} (renamed or build error?)"
	# ceiling = round(median * headroom) rounded to the nearest 100,000 ns.
	budget="$(awk -v m="${median}" -v h="${headroom}" 'BEGIN { c=m*h; printf "%d", (int(c/100000+0.5))*100000 }')"
	printf '%s\t%s\t%s\n' "${name}" "${budget}" "${median}" >>"${tmp_out}"
done

mv "${tmp_out}" "${budgets}"
printf 'refresh-reducer-handler-budgets: wrote %s — review the diff before committing\n' "${budgets}" >&2
