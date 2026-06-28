#!/usr/bin/env bash
#
# run-go-benchmarks.sh — run the Go micro-benchmark suite credential-free and
# write a benchstat-readable results file.
#
# B-1 (#3794): this is the visibility layer. The CI workflow (.github/workflows/
# bench.yml) runs this on every PR and uploads the result file as an artifact so
# per-function ns/op / B/op / allocs/op are observable per change. There is NO
# regression gate here — B-2 (#3795) layers `benchstat` comparison against a
# committed baseline on top of this output.
#
# Hermetic by construction: backend-bound benchmarks (storage/postgres,
# reducer claim-latency, …) self-skip with b.Skip() when their DSN env var is
# unset, and the cypher graph-write benchmarks run against in-process no-op
# executors. This script deliberately sets no DSN, so a plain runner with no
# Postgres/NornicDB produces a clean, deterministic result file.
#
# Exit 0 when every benchmark that ran succeeded (skips are not failures).
# Non-zero when a benchmark fails to build or a b.Fatalf fires — that is a real
# breakage and should turn the workflow red even before B-2's gate exists.
#
# Tunables (env):
#   BENCH_OUTPUT   — results file path        (default: <repo>/bench-results.txt)
#   BENCH_PATTERN  — -bench regexp            (default: .)
#   BENCH_TIME     — -benchtime value         (default: 1x)
#   BENCH_COUNT    — -count value             (default: 1)
#   BENCH_PACKAGES — package list to benchmark (default: auto-discovered)
#   BENCH_TIMEOUT  — go test -timeout value   (default: 30m)
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output="${BENCH_OUTPUT:-${repo_root}/bench-results.txt}"
bench_pattern="${BENCH_PATTERN:-.}"
bench_time="${BENCH_TIME:-1x}"
bench_count="${BENCH_COUNT:-1}"
bench_timeout="${BENCH_TIMEOUT:-30m}"

die() {
	printf 'run-go-benchmarks: %s\n' "$*" >&2
	exit 1
}

command -v go >/dev/null 2>&1 || die "missing required tool: go"

# Resolve the package set. When BENCH_PACKAGES is set we honor it verbatim
# (whitespace-separated). Otherwise we auto-discover exactly the packages that
# contain a Go benchmark. This yields identical benchmark coverage to ./... but
# skips the hundreds of benchmark-less packages (e.g. collector/awscloud/services
# /*), whose per-package compile + test-binary startup — not the benchmarks —
# dominate a naive `-bench=. ./...` run. New benchmark packages are picked up
# automatically, so coverage cannot silently drift.
packages=()
if [[ -n "${BENCH_PACKAGES:-}" ]]; then
	read -r -a packages <<<"${BENCH_PACKAGES}"
else
	command -v rg >/dev/null 2>&1 || die "missing required tool: rg (needed to auto-discover benchmark packages; set BENCH_PACKAGES to skip discovery)"
	# Portable read loop (no mapfile — macOS ships bash 3.2, which lacks it).
	while IFS= read -r pkg; do
		[[ -n "${pkg}" ]] && packages+=("${pkg}")
	done < <(
		cd "${repo_root}/go" &&
			rg -l --glob '*_test.go' '^func Benchmark' |
			xargs -n1 dirname |
			sort -u |
			sed 's#^#./#'
	)
	[[ "${#packages[@]}" -gt 0 ]] || die "no benchmark packages discovered under go/"
fi

mkdir -p "$(dirname "${output}")"

printf 'run-go-benchmarks: bench=%s benchtime=%s count=%s packages=%d -> %s\n' \
	"${bench_pattern}" "${bench_time}" "${bench_count}" "${#packages[@]}" "${output}" >&2

# Backend-bound benchmarks self-skip only when their DSN env var is unset. A
# developer shell or CI job may already export these, which would turn the
# "credential-free" sweep into one that connects to (and creates/drops schemas
# on) a live database. Clear them for the subprocess so the run is hermetic
# regardless of the ambient environment. Keep this list in sync with the env
# vars that benchmark _test.go files read to gate backend access.
backend_dsn_vars=(ESHU_POSTGRES_DSN ESHU_REDUCER_CLAIM_BENCH_DSN)
env_unset=()
for v in "${backend_dsn_vars[@]}"; do
	env_unset+=(-u "${v}") # -u is portable across GNU and BSD env; --unset is GNU-only
done

# -run='^$' disables unit tests so only benchmarks execute. Stream to both the
# console (live CI log) and the results file (benchstat input / artifact). The
# go test exit status — not tee's — decides success, so capture it explicitly.
set -o pipefail
status=0
(
	cd "${repo_root}/go" &&
		env "${env_unset[@]}" \
			go test \
			-run='^$' \
			-bench="${bench_pattern}" \
			-benchmem \
			-benchtime="${bench_time}" \
			-count="${bench_count}" \
			-timeout="${bench_timeout}" \
			"${packages[@]}"
) 2>&1 | tee "${output}" || status=$?

if [[ "${status}" -ne 0 ]]; then
	die "benchmark run failed (exit ${status}); see ${output}"
fi

printf 'run-go-benchmarks: wrote %s\n' "${output}" >&2
