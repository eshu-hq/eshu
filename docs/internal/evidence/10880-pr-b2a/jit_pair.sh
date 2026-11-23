#!/usr/bin/env bash
# usage: jit_pair.sh <dsn> <reads-dir> <out.tsv>
# One psql session per read. Per read: one warm-up, five runs with jit = on, five runs with
# jit = off, same session and corpus; prints the median of each and the change.
set -uo pipefail
dsn="$1"; dir="$2"; out="$3"
printf 'read\tjit_on_median_ms\tjit_off_median_ms\toff_vs_on\n' >"$out"
for name in v_list_status_pruned v_count_status_pruned v_count_none_pruned; do
  f="$dir/$name.sql"
  { printf '%s\n' '\timing on' 'SET jit = on;'
    for _ in 1 2 3 4 5 6; do cat "$f"; done
    printf '%s\n' 'SET jit = off;'
    for _ in 1 2 3 4 5; do cat "$f"; done; } >/tmp/b2a_jit.sql
  times=($(psql "$dsn" -X -q -o /dev/null -f /tmp/b2a_jit.sql 2>&1 | rg -o 'Time: [0-9.]+' | rg -o '[0-9.]+'))
  # times: 1 SET + 6 on + 1 SET + 5 off (the SET statements also print a time)
  on=($(printf '%s\n' "${times[@]:2:5}")); off=($(printf '%s\n' "${times[@]:8:5}"))
  med() { printf '%s\n' "$@" | sort -n | awk '{a[NR]=$1} END{print a[int((NR+1)/2)]}'; }
  mo=$(med "${on[@]}"); mf=$(med "${off[@]}")
  printf '%s\t%s\t%s\t%s\n' "$name" "$mo" "$mf" "$(awk -v a="$mo" -v b="$mf" 'BEGIN{printf "%.1f%%", 100*(b-a)/a}')" >>"$out"
done
