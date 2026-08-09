#!/usr/bin/env bash
# Measure `eshu demo` time-to-first-answer (TTFA) over N runs in one mode.
#
# TTFA is measured by `eshu demo up` itself (invocation to first answer) and
# scored by `eshu demo-benchmark`. COLD and WARM are separate invocations of
# this script and are never averaged together: a blended number understates
# what a first-time installer waits through.
#
# The image cache is probed BEFORE each run and passed to the scorer, which
# fails the run when the observation contradicts --mode. After `demo up` the
# images are present either way, so probing afterwards would prove nothing.
#
# Usage:
#   scripts/measure-demo-ttfa.sh --mode warm --runs 3
#   scripts/measure-demo-ttfa.sh --mode cold --runs 3 --target 9m
set -euo pipefail

MODE=""
RUNS=3
TARGET=""
# The project name is deliberately STABLE, not per-run unique: Compose derives
# each built image name as <project>-<service>, so a fresh project name would
# make every run a cold build and warm mode unmeasurable. Override it only to
# run two measurements side by side, and expect the first run of a new project
# to be a build.
PROJECT="${ESHU_DEMO_TTFA_PROJECT:-eshu-ttfa}"
COMPOSE_FILE="docker-compose.demo.yaml"
OUT_DIR="${ESHU_DEMO_TTFA_OUT:-}"
PRUNE_BUILD_CACHE=0

# Defaults matching docs/public/reference/local-testing/demo-ttfa-benchmark.md.
# Keep the two in lockstep; they are the regression detectors for this lane.
DEFAULT_WARM_TARGET="${ESHU_DEMO_TTFA_WARM_TARGET:-5m}"
DEFAULT_COLD_TARGET="${ESHU_DEMO_TTFA_COLD_TARGET:-10m}"

usage() {
	printf 'usage: %s --mode cold|warm [--runs N] [--target 6m] [--project NAME] [--prune-build-cache]\n' "$0" >&2
	exit 2
}

while [ $# -gt 0 ]; do
	case "$1" in
	--mode) MODE="${2:-}"; shift 2 ;;
	--runs) RUNS="${2:-}"; shift 2 ;;
	--target) TARGET="${2:-}"; shift 2 ;;
	--project) PROJECT="${2:-}"; shift 2 ;;
	--prune-build-cache) PRUNE_BUILD_CACHE=1; shift ;;
	-h | --help) usage ;;
	*) printf 'unknown argument: %s\n' "$1" >&2; usage ;;
	esac
done

case "$MODE" in
cold | warm) ;;
*) printf 'error: --mode must be cold or warm\n' >&2; usage ;;
esac

# P2-4: an empty experiment must not report success. With --runs 0 the loop
# never executes, failures stays 0, and the script would print "all 0 runs
# passed" and exit 0.
case "$RUNS" in
'' | *[!0-9]*) printf 'error: --runs must be a positive integer\n' >&2; usage ;;
esac
if [ "$RUNS" -lt 1 ]; then
	printf 'error: --runs must be at least 1\n' >&2
	usage
fi

# The checked-in targets are enforced by default, so the documented command is
# the one that detects a regression. Omitting a target makes the timing
# criterion non-required, which would let an arbitrarily slow run print
# "all runs passed" -- the lane would exist and measure nothing.
if [ -z "$TARGET" ]; then
	if [ "$MODE" = "warm" ]; then
		TARGET="$DEFAULT_WARM_TARGET"
	else
		TARGET="$DEFAULT_COLD_TARGET"
	fi
	printf 'using default %s target %s (override with --target)\n' "$MODE" "$TARGET"
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

if [ -z "$OUT_DIR" ]; then
	OUT_DIR="$(mktemp -d)"
fi
mkdir -p "$OUT_DIR"

ESHU_BIN="${ESHU_BIN:-$repo_root/go/bin/eshu}"
if [ ! -x "$ESHU_BIN" ]; then
	printf 'error: %s is not executable; build it first (cd go && go build -o bin/eshu ./cmd/eshu)\n' \
		"$ESHU_BIN" >&2
	exit 1
fi

export ESHU_DEMO_PROJECT_NAME="$PROJECT"

# demo_images lists every image the demo stack needs, built and pulled alike.
# It is derived from Compose rather than hardcoded so a new service cannot
# silently fall outside the cold/warm classification.
demo_images() {
	docker compose -f "$COMPOSE_FILE" config --images
}

# observed_image_state reports "present" only when EVERY demo image is already
# local. One missing image means the run has to build or pull, which is what
# COLD measures.
observed_image_state() {
	local img
	while IFS= read -r img; do
		[ -n "$img" ] || continue
		if ! docker image inspect "$img" >/dev/null 2>&1; then
			printf 'absent\n'
			return 0
		fi
	done < <(demo_images)
	printf 'present\n'
}

# drop_demo_images removes the demo's image TAGS. On its own this is NOT a
# first-install cold run: the demo builds most of its images, and BuildKit keeps
# every layer, so the rebuild comes straight back out of cache. Measured on an
# idle 16-core host, tag-only "cold" came in 2.3% above warm (208.9s vs 204.2s)
# with 21.66GB of build cache still resident -- i.e. it measured almost nothing.
#
# --prune-build-cache drops that cache too, which is what a first install
# actually pays. It is opt-in because the cache is shared with everything else
# on the machine and reclaiming it is not this script's call to make silently.
drop_demo_images() {
	local img
	while IFS= read -r img; do
		[ -n "$img" ] || continue
		docker image rm -f "$img" >/dev/null 2>&1 || true
	done < <(demo_images)
}

# read_total_millis pulls ttfa_millis out of the scorer's own JSON verdict, so
# the summary depends on the eshu binary this script already requires.
read_total_millis() {
	"$ESHU_BIN" demo-benchmark --envelope "$1" --mode "$MODE" --json 2>/dev/null |
		tr ',' '\n' | sed -n 's/.*"ttfa_millis":[[:space:]]*\([0-9][0-9]*\).*/\1/p' | head -1
}

teardown() {
	"$ESHU_BIN" demo down --project "$PROJECT" >/dev/null 2>&1 || true
}
trap teardown EXIT

printf 'demo TTFA measurement: mode=%s runs=%s project=%s\n' "$MODE" "$RUNS" "$PROJECT"
printf 'artifacts: %s\n\n' "$OUT_DIR"

# Warm means "images already present". When they are not, one unmeasured
# priming run builds them, so the measured runs are genuinely warm instead of
# the first one silently paying build cost -- which the mode cross-check would
# reject anyway, correctly but unhelpfully.
if [ "$MODE" = "warm" ] && [ "$(observed_image_state)" = "absent" ]; then
	printf 'warm mode: demo images absent, priming with one unmeasured run\n'
	teardown
	"$ESHU_BIN" demo up --project "$PROJECT" --json >"$OUT_DIR/prime.json" 2>"$OUT_DIR/prime.err" || {
		printf 'priming run FAILED; see %s\n' "$OUT_DIR/prime.err" >&2
		exit 1
	}
	teardown
	printf 'priming complete\n\n'
fi

failures=0
for run in $(seq 1 "$RUNS"); do
	teardown
	if [ "$MODE" = "cold" ]; then
		drop_demo_images
		if [ "$PRUNE_BUILD_CACHE" = "1" ]; then
			# Not `|| true`. Pruning is the only thing separating a first
			# install from a cached rebuild, and the image tags are already
			# gone, so the probe would still report "absent" and the scorer
			# would accept a cached rebuild as a cold result.
			if ! docker builder prune -af >"$OUT_DIR/prune-$run.log" 2>&1; then
				printf 'run %s: build-cache prune FAILED; refusing to score a cached rebuild as a first install\n' \
					"$run" >&2
				printf '         see %s\n' "$OUT_DIR/prune-$run.log" >&2
				failures=$((failures + 1))
				continue
			fi
		else
			printf 'WARNING: build cache kept; this measures a cached rebuild, not a first install.\n'
			printf '         Pass --prune-build-cache for a true cold number.\n'
		fi
	fi

	observed="$(observed_image_state)"
	printf '=== run %s/%s (images observed before up: %s) ===\n' "$run" "$RUNS" "$observed"

	envelope="$OUT_DIR/demo-$MODE-$run.json"
	if ! "$ESHU_BIN" demo up --project "$PROJECT" --json >"$envelope" 2>"$OUT_DIR/demo-$MODE-$run.err"; then
		printf 'run %s: demo up FAILED; see %s\n' "$run" "$OUT_DIR/demo-$MODE-$run.err" >&2
		failures=$((failures + 1))
		continue
	fi

	score_args=(--envelope "$envelope" --mode "$MODE" --images "$observed")
	if [ -n "$TARGET" ]; then
		score_args+=(--target "$TARGET")
	fi
	if ! "$ESHU_BIN" demo-benchmark ${score_args[@]+"${score_args[@]}"}; then
		failures=$((failures + 1))
	fi
	printf '\n'
done

teardown

printf '=== %s summary (%s runs) ===\n' "$MODE" "$RUNS"
# Report every run's total, plus the median, so one warm outlier cannot be
# quietly presented as the number.
totals=()
for run in $(seq 1 "$RUNS"); do
	envelope="$OUT_DIR/demo-$MODE-$run.json"
	[ -s "$envelope" ] || continue
	# Read the total back through eshu rather than python3. The summary used to
	# shell out to python3 with stderr and the exit code discarded, so a missing
	# interpreter dropped rows from the table with no clue why.
	ms="$(read_total_millis "$envelope")"
	[ -n "$ms" ] || continue
	totals+=("$ms")
	printf '  run %s: %s ms\n' "$run" "$ms"
done

if [ "${#totals[@]}" -gt 0 ]; then
	printf '  median: %s ms\n' \
		"$(printf '%s\n' ${totals[@]+"${totals[@]}"} | sort -n | awk '{a[NR]=$1} END{print (NR%2)?a[(NR+1)/2]:int((a[NR/2]+a[NR/2+1])/2)}')"
fi

if [ "$failures" -gt 0 ]; then
	printf '\n%s run(s) FAILED the benchmark\n' "$failures" >&2
	exit 1
fi
printf '\nall %s runs passed\n' "$RUNS"
