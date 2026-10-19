#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""Summarise the per-run P1 timing CSVs in out/ (#7766 evidence).

Usage: python3 -I summarize_p1.py [out_dir]   (default: ./out next to this file)
Percentiles are nearest-rank, so at n=10 and n=20 every p99 is the maximum.
"""
import csv
import glob
import math
import os
import statistics
import sys


def pct(values, p):
    values = sorted(values)
    i = max(0, min(len(values) - 1, math.ceil(len(values) * p) - 1))
    return values[i]


def main():
    here = os.path.dirname(os.path.abspath(__file__))
    out_dir = sys.argv[1] if len(sys.argv) > 1 else os.path.join(here, "out")
    for path in sorted(glob.glob(os.path.join(out_dir, "p1_time_*.csv"))):
        base = os.path.basename(path)
        exploratory = "rollback" in base or base == "p1_time_R5000_design.csv"
        with open(path, newline="") as handle:
            lines = [line for line in handle if not line.startswith("#")]
        data = list(csv.DictReader(lines))
        if not data:
            continue
        total = [float(d["lock_to_commit_ms"]) for d in data]

        def mean(key):
            return statistics.mean(float(d[key]) for d in data)

        print(
            f"{base:46s} n={len(data):2d} gens={data[0]['generations']:>6s} "
            f"min={min(total):7.1f} p50={pct(total, .5):7.1f} "
            f"p90={pct(total, .9):7.1f} p99={pct(total, .99):7.1f} "
            f"max={max(total):7.1f} | mean ms: recheck={mean('recheck_ms'):5.1f} "
            f"7a={mean('7a_ms'):6.1f}({data[0]['7a_rows']}r) 7b={mean('7b_ms'):4.1f} "
            f"7c={mean('7c_ms'):6.1f} 7d={mean('7d_ms'):6.1f}({data[0]['7d_rows']}r) "
            f"7e={mean('7e_ms'):4.1f} 7f={mean('7f_ms'):4.1f} 7g={mean('7g_ms'):4.1f} "
            f"commit={mean('commit_ms'):5.1f} {'(exploratory)' if exploratory else ''}"
        )


if __name__ == "__main__":
    main()
