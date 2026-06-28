#!/usr/bin/env bash
#
# test-run-go-benchmarks.sh — contract mirror for run-go-benchmarks.sh and the
# bench.yml workflow (B-1 / #3794).
#
# Static checks (cheap, rg-based) assert the producer keeps its benchstat-ready,
# credential-free shape and that the workflow wires it in and uploads the
# artifact. A fast functional check runs the producer against one tiny hermetic
# benchmark package to prove it actually emits a results file with benchmark
# rows and exits 0 with no backend configured.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
producer="${repo_root}/scripts/run-go-benchmarks.sh"
workflow="${repo_root}/.github/workflows/bench.yml"

fail=0
note() { printf 'test-run-go-benchmarks: %s\n' "$*" >&2; }
check() {
	# check "<description>" <condition-exit-status>
	if [[ "$2" -ne 0 ]]; then
		note "FAIL: $1"
		fail=1
	fi
}

command -v rg >/dev/null 2>&1 || { note "missing required tool: rg"; exit 2; }

# --- Static contract: producer ------------------------------------------------
[[ -f "${producer}" ]]; check "run-go-benchmarks.sh exists" $?
[[ -x "${producer}" ]]; check "run-go-benchmarks.sh is executable" $?

rg -q -- "-run='\^\\\$'" "${producer}"; check "producer disables unit tests with -run='^\$'" $?
rg -q -- "-benchmem" "${producer}"; check "producer requests -benchmem" $?
rg -q -- "-bench=" "${producer}"; check "producer runs -bench" $?
rg -q "BENCH_OUTPUT" "${producer}"; check "producer honors BENCH_OUTPUT" $?
rg -q "tee " "${producer}"; check "producer tees results to the output file" $?

# Portability: macOS ships bash 3.2, which lacks `mapfile`. Auto-discovery must
# use a portable read loop so the producer runs on a developer laptop too.
if rg -q -P '^(?!\s*#).*\bmapfile\b' "${producer}"; then
	note "FAIL: producer uses mapfile (bash 4+ only); use a portable read loop"
	fail=1
fi

# Credential-free: the producer must not bake in a Postgres/NornicDB DSN, so
# backend-bound benchmarks self-skip on a plain runner.
if rg -q "ESHU_[A-Z_]*DSN=|ESHU_POSTGRES_DSN=|ESHU_NEO4J|bolt://|postgres://" "${producer}"; then
	note "FAIL: producer sets a backend DSN (must stay credential-free)"
	fail=1
fi
# And it must actively clear inherited backend DSNs so an ambient
# ESHU_POSTGRES_DSN cannot turn the sweep into one that hits a live database.
rg -q "env_unset|[-]u .*ESHU_POSTGRES_DSN| -u " "${producer}"; check "producer clears inherited backend DSNs" $?
rg -q "ESHU_POSTGRES_DSN" "${producer}"; check "producer names ESHU_POSTGRES_DSN among cleared vars" $?

# --- Static contract: workflow ------------------------------------------------
[[ -f "${workflow}" ]]; check "bench.yml workflow exists" $?
rg -q "scripts/run-go-benchmarks.sh" "${workflow}"; check "workflow invokes the producer" $?
rg -q "scripts/test-run-go-benchmarks.sh" "${workflow}"; check "workflow runs this mirror" $?
rg -q "upload-artifact@v4" "${workflow}"; check "workflow uploads results as an artifact" $?
rg -q "pull_request" "${workflow}"; check "workflow triggers on pull_request" $?
# B-1 must NOT gate on regressions — that is B-2 (#3795). Guard against a
# benchstat gate sneaking into this workflow before B-2 lands. Match only
# non-comment lines so the explanatory comment naming B-2 does not trip it.
if rg -q -P '^(?!\s*#).*benchstat' "${workflow}"; then
	note "FAIL: bench.yml invokes benchstat — regression gating belongs to B-2 (#3795), not B-1"
	fail=1
fi

# --- Functional check: producer emits results, credential-free ----------------
tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT
out="${tmp_root}/bench-results.txt"

# Run only one small hermetic package, once, with no DSN in the environment.
if env -u ESHU_POSTGRES_DSN -u ESHU_REDUCER_CLAIM_BENCH_DSN \
	BENCH_OUTPUT="${out}" \
	BENCH_PACKAGES="./internal/facts" \
	BENCH_PATTERN="BenchmarkValidateSchemaVersion" \
	BENCH_TIME="1x" \
	BENCH_COUNT="1" \
	"${producer}" >/dev/null 2>&1; then
	check "producer exits 0 on a hermetic package" 0
else
	check "producer exits 0 on a hermetic package" 1
fi
[[ -f "${out}" ]]; check "producer writes the results file" $?
if [[ -f "${out}" ]]; then
	rg -q "^BenchmarkValidateSchemaVersion" "${out}"; check "results file contains benchmark rows" $?
fi

# Hermeticity under a hostile environment: export a bogus backend DSN and confirm
# the postgres benchmark still self-skips (exit 0) instead of dialing it. If the
# runner failed to clear the inherited DSN, go test would try to connect and fail.
out2="${tmp_root}/bench-results-dsn.txt"
if ESHU_POSTGRES_DSN="postgres://invalid:invalid@127.0.0.1:1/none" \
	ESHU_REDUCER_CLAIM_BENCH_DSN="postgres://invalid:invalid@127.0.0.1:1/none" \
	BENCH_OUTPUT="${out2}" \
	BENCH_PACKAGES="./internal/storage/postgres" \
	BENCH_PATTERN="BenchmarkReducerQueueClaimDeepQueue" \
	BENCH_TIME="1x" \
	BENCH_COUNT="1" \
	"${producer}" >/dev/null 2>&1; then
	check "producer stays hermetic with an inherited backend DSN (bench skips, exit 0)" 0
else
	check "producer stays hermetic with an inherited backend DSN (bench skips, exit 0)" 1
fi

if [[ "${fail}" -ne 0 ]]; then
	note "contract mirror FAILED"
	exit 1
fi
note "contract mirror passed"
