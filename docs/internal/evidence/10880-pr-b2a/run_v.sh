#!/usr/bin/env bash
# usage: run_v.sh <dsn> <reads-dir> <out.tsv> [glob]
# Per statement: one warm-up run, then RUNS-1 timed runs through psql \timing (default
# RUNS=4); prints median and max milliseconds and the output line count. A statement
# whose warm-up exceeds SLOW_MS (default 60000) is not repeated: its single time is
# recorded. Each run is capped at TMO seconds (default 600). JIT stays at the server
# default (on); `show jit` is recorded in the header.
set -uo pipefail
dsn="$1"; dir="$2"; out="$3"; glob="${4:-*.sql}"
runs="${RUNS:-4}"; slow="${SLOW_MS:-60000}"; tmo="${TMO:-600}"
{ printf '# jit=%s shared_buffers=%s work_mem=%s\n' \
    "$(psql "$dsn" -Atc 'show jit')" "$(psql "$dsn" -Atc 'show shared_buffers')" "$(psql "$dsn" -Atc 'show work_mem')"
  printf 'read\tmedian_ms\tmax_ms\trows\tnote\n'; } >"$out"
one() { # prints the psql \timing milliseconds of one run, or TIMEOUT
  local t
  t="$(timeout "$tmo" psql "$dsn" -X -q -o /tmp/b2a_v_out.txt -c '\timing on' -f "$1" 2>&1 | rg -o 'Time: [0-9.]+' | rg -o '[0-9.]+' | tail -1)"
  [ -n "$t" ] && echo "$t" || echo TIMEOUT
}
for f in "$dir"/$glob; do
  name="$(basename "$f" .sql)"
  first="$(one "$f")"; rows="$(wc -l </tmp/b2a_v_out.txt)"
  if [ "$first" = TIMEOUT ]; then printf '%s\t>%s\t>%s\t%s\tTIMEOUT after %ss\n' "$name" "$((tmo*1000))" "$((tmo*1000))" "$rows" "$tmo" >>"$out"; continue; fi
  if awk -v t="$first" -v s="$slow" 'BEGIN{exit !(t>s)}'; then
    printf '%s\t%s\t%s\t%s\tsingle run (over %s ms)\n' "$name" "$first" "$first" "$rows" "$slow" >>"$out"; continue
  fi
  times=()
  for ((i = 1; i < runs; i++)); do times+=("$(one "$f")"); done
  read -r med max < <(printf '%s\n' "${times[@]}" | sort -n | awk '{a[NR]=$1} END{print a[int((NR+1)/2)], a[NR]}')
  printf '%s\t%s\t%s\t%s\twarm-up %s ms\n' "$name" "$med" "$max" "$rows" "$first" >>"$out"
done
