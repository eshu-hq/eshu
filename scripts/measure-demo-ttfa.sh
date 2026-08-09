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
PROJECT="${ESHU_DEMO_TTFA_PROJECT:-eshu-ttfa-$$}"
COMPOSE_FILE="docker-compose.demo.yaml"
OUT_DIR="${ESHU_DEMO_TTFA_OUT:-}"

usage() {
	printf 'usage: %s --mode cold|warm [--runs N] [--target 6m] [--project NAME]\n' "$0" >&2
	exit 2
}

while [ $# -gt 0 ]; do
	case "$1" in
	--mode) MODE="${2:-}"; shift 2 ;;
	--runs) RUNS="${2:-}"; shift 2 ;;
	--target) TARGET="${2:-}"; shift 2 ;;
	--project) PROJECT="${2:-}"; shift 2 ;;
	-h | --help) usage ;;
	*) printf 'unknown argument: %s\n' "$1" >&2; usage ;;
	esac
done

case "$MODE" in
cold | warm) ;;
*) printf 'error: --mode must be cold or warm\n' >&2; usage ;;
esac

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

# drop_demo_images removes the demo's images so the next run pays the real
# first-install cost. Only images this stack declares are touched.
drop_demo_images() {
	local img
	while IFS= read -r img; do
		[ -n "$img" ] || continue
		docker image rm -f "$img" >/dev/null 2>&1 || true
	done < <(demo_images)
}

teardown() {
	"$ESHU_BIN" demo down --project "$PROJECT" >/dev/null 2>&1 || true
}
trap teardown EXIT

printf 'demo TTFA measurement: mode=%s runs=%s project=%s\n' "$MODE" "$RUNS" "$PROJECT"
printf 'artifacts: %s\n\n' "$OUT_DIR"

failures=0
for run in $(seq 1 "$RUNS"); do
	teardown
	if [ "$MODE" = "cold" ]; then
		drop_demo_images
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
	ms="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["data"]["total_millis"])' "$envelope" 2>/dev/null || true)"
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
