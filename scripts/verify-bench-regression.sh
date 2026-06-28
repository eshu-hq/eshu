#!/usr/bin/env bash
#
# verify-bench-regression.sh — B-2 (#3795): compare the current Go benchmark
# results against the committed baseline with benchstat and report any ns/op
# (sec/op) regression beyond a threshold.
#
# This layers a regression check on top of B-1's run-go-benchmarks.sh artifact.
# benchstat's own significance test (-alpha) decides whether a change is real;
# this script then fails (or warns) only on a *significant* sec/op regression
# larger than the threshold.
#
# Enforcement: a committed micro-benchmark baseline is only a like-for-like
# comparison when it was captured on the same runner class as the current run.
# The weekly bench-baseline-refresh workflow recaptures testdata/benchmarks/baseline.txt
# on ubuntu-latest for exactly that reason. Until a baseline is refreshed on the
# CI runner class, the check defaults to ADVISORY (report, do not fail) — the same
# shared-runner-variance reasoning as the B-11 macro gate. Set
# BENCH_REGRESSION_ENFORCE=true (the weekly-refreshed baseline makes this safe) to
# turn a regression into a non-zero exit.
#
# Tunables (env):
#   BENCH_BASELINE          baseline results file   (default testdata/benchmarks/baseline.txt)
#   BENCH_CURRENT           current results file     (default testdata/benchmarks/current.txt;
#                           generated via run-go-benchmarks.sh if absent)
#   BENCH_REGRESSION_PCT    regression threshold %   (default 10)
#   BENCH_ALPHA             benchstat significance   (default 0.05)
#   BENCH_REGRESSION_ENFORCE  fail on regression     (default false = advisory)
#   BENCH_TIME / BENCH_COUNT  passed to run-go-benchmarks.sh when generating
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=scripts/lib/benchstat-clean.sh
. "${repo_root}/scripts/lib/benchstat-clean.sh"
baseline="${BENCH_BASELINE:-${repo_root}/testdata/benchmarks/baseline.txt}"
# current_explicit tracks whether the caller pinned BENCH_CURRENT (the mirror
# feeds synthetic files); when it did, use it verbatim. Otherwise always
# (re)generate so the gate never compares against a stale current.txt.
current_explicit="${BENCH_CURRENT:-}"
current="${current_explicit:-${repo_root}/testdata/benchmarks/current.txt}"
threshold_pct="${BENCH_REGRESSION_PCT:-10}"
alpha="${BENCH_ALPHA:-0.05}"
enforce="${BENCH_REGRESSION_ENFORCE:-false}"

die() {
	printf 'verify-bench-regression: %s\n' "$*" >&2
	exit 1
}

command -v benchstat >/dev/null 2>&1 || die "missing required tool: benchstat (go install golang.org/x/perf/cmd/benchstat@latest)"
[[ -f "${baseline}" ]] || die "baseline not found: ${baseline} (commit one, or run scripts/refresh-bench-baseline.sh)"

# Resolve the current results. A caller-pinned BENCH_CURRENT is used verbatim
# (the mirror feeds synthetic files); otherwise always (re)generate so a stale
# current.txt from a previous run is never silently reused. Same shape as the
# baseline: run-go-benchmarks.sh auto-discovers the same packages, and count/
# benchtime match what the baseline was captured with.
if [[ -n "${current_explicit}" ]]; then
	[[ -f "${current}" ]] || die "BENCH_CURRENT set but not found: ${current}"
else
	printf 'verify-bench-regression: generating %s (count=%s time=%s)\n' \
		"${current}" "${BENCH_COUNT:-6}" "${BENCH_TIME:-100ms}" >&2
	mkdir -p "$(dirname "${current}")"
	# Capture raw, then filter to clean benchstat input (same as the baseline).
	raw="$(mktemp)"
	trap 'rm -f "${raw}" 2>/dev/null || true' EXIT
	BENCH_OUTPUT="${raw}" BENCH_COUNT="${BENCH_COUNT:-6}" BENCH_TIME="${BENCH_TIME:-100ms}" \
		bash "${repo_root}/scripts/run-go-benchmarks.sh"
	benchstat_clean_filter "${raw}" "${current}"
fi

# Human-readable comparison (also useful as a CI artifact / log).
printf '\n== benchstat (baseline vs current) ==\n' >&2
benchstat -alpha "${alpha}" "${baseline}" "${current}" >&2 || true

# Machine-readable comparison: parse the sec/op section's "vs base" column. A
# significant change is a signed percentage (e.g. +12.3%); an insignificant one
# is "~". A regression is a positive percentage above the threshold.
csv="$(benchstat -format csv -alpha "${alpha}" "${baseline}" "${current}" 2>/dev/null)" \
	|| die "benchstat failed to produce a CSV comparison"

regressions="$(
	printf '%s\n' "${csv}" | awk -v thr="${threshold_pct}" '
		# Enter the sec/op metric section at its header row; leave on a blank line.
		/,sec\/op,CI,sec\/op,CI,vs base,P$/ { insec=1; next }
		insec && /^[[:space:]]*$/ { insec=0 }
		insec {
			n=split($0, f, ",")
			name=f[1]
			vs=f[6]
			if (name=="" || name=="geomean") next
			# vs base like +12.3% (regression) / -8% (improvement) / ~ (no change)
			if (vs ~ /^\+[0-9.]+%$/) {
				pct=vs; sub(/%$/,"",pct); sub(/^\+/,"",pct)
				if (pct+0 > thr+0) printf "%s\t%s\n", name, vs
			}
		}
	'
)"

if [[ -n "${regressions}" ]]; then
	printf '\nverify-bench-regression: sec/op regressions > %s%% vs baseline:\n' "${threshold_pct}" >&2
	printf '%s\n' "${regressions}" | while IFS=$'\t' read -r name vs; do
		printf '  %s %s\n' "${name}" "${vs}" >&2
	done
	if [[ "${enforce}" == "true" ]]; then
		die "benchmark regression gate failed (set a fresh baseline if this is an intended change)"
	fi
	printf 'verify-bench-regression: ADVISORY (BENCH_REGRESSION_ENFORCE!=true) — not failing\n' >&2
	exit 0
fi

printf 'verify-bench-regression: no sec/op regression > %s%% vs baseline\n' "${threshold_pct}" >&2
