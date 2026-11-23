#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Host-load evidence for a replay. Two modes:
#   load_watch.sh quiet <max_load1> <minutes> [deadline_epoch]
#       blocks until load1 has stayed below <max_load1> for <minutes> consecutive
#       minutes (sampled once a minute); exits 1 if the deadline passes first.
#   load_watch.sh sample <results-dir>
#       appends "epoch<TAB>load1" to <results-dir>/loadavg.tsv once a minute, until
#       killed. Started before the matrix and stopped after it; report.sh prints the
#       min, median and max.
set -euo pipefail

mode="${1:?usage: load_watch.sh quiet <max_load1> <minutes> [deadline_epoch] | sample <results-dir>}"
load1() { awk '{print $1}' /proc/loadavg; }

case "$mode" in
  quiet)
    max="${2:?max load1}" minutes="${3:?minutes}" deadline="${4:-0}"
    streak=0
    while ((streak < minutes)); do
      if awk -v l="$(load1)" -v m="$max" 'BEGIN{exit !(l < m)}'; then
        streak=$((streak + 1))
      else
        streak=0
      fi
      if ((deadline > 0 && $(date +%s) >= deadline)) && ((streak < minutes)); then
        echo "load_watch: load1 not below $max for $minutes minutes before the deadline (last load1 $(load1))" >&2
        exit 1
      fi
      ((streak >= minutes)) || sleep 60
    done
    echo "load_watch: load1 below $max for $minutes consecutive minutes"
    ;;
  sample)
    dir="${2:?results dir}"
    mkdir -p "$dir"
    while :; do
      printf '%s\t%s\n' "$(date +%s)" "$(load1)" >>"$dir/loadavg.tsv"
      sleep 60
    done
    ;;
  *) echo "unknown mode $mode" >&2; exit 2 ;;
esac
