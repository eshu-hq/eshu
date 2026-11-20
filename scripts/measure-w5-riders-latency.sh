#!/usr/bin/env bash
# Wave-5 rider latency gate (Refs eshu-hq/eshu#9669, satellite
# eshu-hq/eshu-api#101).
#
# Owns the judgment-latency comparison the census assigns the core lane:
# the pin-backed Wave-5 rider judgments must not be slower than the
# static-default lanes they shadow. Hermetic by design: both legs run
# against the same in-process muxes with canned/static admissions, so the
# numbers isolate judgment-path cost (mux match plus 404/405 page), not
# backend or network time. The pinned-image leg against a real deployment
# rides flip prep.
#
# Method: go test benchmarks BenchmarkW5RidersJudgment (one leg per rider
# spelling) vs BenchmarkW5RidersStaticJudgment (internal/query package),
# -count=5, take the median ns/op. FAIL when a pin-backed leg exceeds its
# static-default twin by more than 50%. Generous: every leg does one
# in-memory mux judgment with zero admission contact, so any real pathology
# (a double read, a body re-read, an uncached load) exceeds it loudly
# while scheduling noise does not.
#
# Usage: bash scripts/measure-w5-riders-latency.sh
# Needs: go (version in go.mod).
set -euo pipefail

cd "$(dirname "$0")/../go"

bench() {
  go test ./internal/query/ -run '^$' -bench "$1" -benchtime=2000x -count=5 2>/dev/null | \
    awk '$1 ~ /^Benchmark/ && $4 == "ns/op" {print $3}'
}

median() {
  sort -n | awk '{a[NR]=$1} END {if (NR%2==1) print a[(NR+1)/2]; else print (a[NR/2]+a[NR/2+1])/2}'
}

check_leg() {
  local leg="$1" pinned="$2" static="$3"
  if [[ -z "$pinned" || -z "$static" ]]; then
    echo "FAIL: no benchmark samples for leg ${leg}" >&2
    exit 1
  fi
  # shellcheck disable=SC2004
  local ratio
  ratio="$(awk -v p="$pinned" -v s="$static" 'BEGIN {printf "%.2f", p/s}')"
  local over
  over="$(awk -v p="$pinned" -v s="$static" 'BEGIN {print (p > s*1.5) ? 1 : 0}')"
  echo "leg ${leg}: pinned=${pinned}ns static=${static}ns ratio=${ratio} (fail past 1.50)"
  if [[ "$over" == "1" ]]; then
    echo "FAIL: pinned leg ${leg} regresses past 1.50x of static" >&2
    exit 1
  fi
}

pin_r1="$(bench 'BenchmarkW5RidersJudgment/r1-patch-bundles' | median)"
pin_r2="$(bench 'BenchmarkW5RidersJudgment/r2-slash-policy' | median)"
pin_r3="$(bench 'BenchmarkW5RidersJudgment/r3-slash-providers' | median)"
static_r1="$(bench 'BenchmarkW5RidersStaticJudgment/r1-patch-bundles' | median)"
static_r2="$(bench 'BenchmarkW5RidersStaticJudgment/r2-slash-policy' | median)"
static_r3="$(bench 'BenchmarkW5RidersStaticJudgment/r3-slash-providers' | median)"

check_leg r1-patch-bundles "$pin_r1" "$static_r1"
check_leg r2-slash-policy "$pin_r2" "$static_r2"
check_leg r3-slash-providers "$pin_r3" "$static_r3"

echo "w5-riders-latency: all legs within budget"
