#!/usr/bin/env bash
# G2 Wave-1 latency gate (Refs eshu-hq/eshu#9669, satellite
# eshu-hq/eshu-api#101).
#
# Owns the write-latency comparison the census assigns the core lane:
# the pin-backed Wave-1 legs must not be slower than the
# static-default lanes they shadow. Hermetic by design: both legs run
# against the same in-process canned/static admissions, so the numbers
# isolate handler-side cost (pin indirection), not backend or network
# time. The pinned-image leg against a real deployment rides flip prep.
#
# Method: go test benchmarks BenchmarkG2Wave1PinnedRead (one leg per
# wave family) vs BenchmarkG2Wave1StaticRead (internal/query
# package), -count=5, take the median ns/op. FAIL when a pin-backed
# leg exceeds its static-default twin by more than 50%. Generous:
# every leg does one bounded in-memory read, so any real pathology
# (a double read, a body re-read, an uncached load) exceeds it loudly
# while scheduling noise does not.
#
# Usage: bash scripts/measure-g2-wave1-latency.sh
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

pin_readiness="$(bench 'BenchmarkG2Wave1PinnedRead/readiness' | median)"
pin_fact="$(bench 'BenchmarkG2Wave1PinnedRead/fact' | median)"
pin_surface="$(bench 'BenchmarkG2Wave1PinnedRead/surface' | median)"
pin_rider="$(bench 'BenchmarkG2Wave1PinnedRead/rider-judgment' | median)"
static_readiness="$(bench 'BenchmarkG2Wave1StaticRead/readiness' | median)"
static_fact="$(bench 'BenchmarkG2Wave1StaticRead/fact' | median)"
static_surface="$(bench 'BenchmarkG2Wave1StaticRead/surface' | median)"
static_rider="$(bench 'BenchmarkG2Wave1StaticRead/rider-judgment' | median)"

check_leg readiness "$pin_readiness" "$static_readiness"
check_leg fact "$pin_fact" "$static_fact"
check_leg surface "$pin_surface" "$static_surface"
check_leg rider-judgment "$pin_rider" "$static_rider"

echo "g2-wave1-latency: all legs within budget"
