#!/usr/bin/env bash
#
# refresh-bench-baseline.sh — B-2 (#3795): (re)capture testdata/benchmarks/baseline.txt by
# running the Go benchmark suite. Run on the enforcement runner class (the weekly
# bench-baseline-refresh workflow runs this on ubuntu-latest) so the committed
# baseline is a like-for-like comparison target for the per-PR regression gate.
#
# Uses the same count/benchtime as the gate (scripts/verify-bench-regression.sh)
# so baseline.txt and the PR's current.txt are captured identically. Review the
# diff before committing — a baseline bump is a reviewed claim that the new timing
# is the expected normal.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=scripts/lib/benchstat-clean.sh
. "${repo_root}/scripts/lib/benchstat-clean.sh"
baseline="${BENCH_BASELINE:-${repo_root}/testdata/benchmarks/baseline.txt}"

command -v rg >/dev/null 2>&1 || { printf 'refresh-bench-baseline: missing rg\n' >&2; exit 1; }
mkdir -p "$(dirname "${baseline}")"

# Capture raw output, then filter to clean benchstat input (the code under
# benchmark logs to stdout, which would otherwise bloat/corrupt the baseline).
raw="$(mktemp)"
trap 'rm -f "${raw}" 2>/dev/null || true' EXIT
BENCH_OUTPUT="${raw}" BENCH_COUNT="${BENCH_COUNT:-6}" BENCH_TIME="${BENCH_TIME:-100ms}" \
	bash "${repo_root}/scripts/run-go-benchmarks.sh"
benchstat_clean_filter "${raw}" "${baseline}"

printf 'refresh-bench-baseline: wrote %s — review the diff before committing\n' "${baseline}" >&2
