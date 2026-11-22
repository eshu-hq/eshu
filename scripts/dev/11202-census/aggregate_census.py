#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""Aggregate generations.csv (census_generations.sh) into numbers only.

Prints generations per scope by status, rows per generation by status, the lifetime
of superseded generations (activation to supersession), and creation rate per day.
Output carries no scope names: the input holds only a hash of each scope id.
Usage: python3 -I aggregate_census.py WORKDIR/generations.csv
"""
import collections
import csv
import datetime as dt
import sys


def ts(s):
    if not s:
        return None
    return dt.datetime.fromisoformat(s.replace(" ", "T"))


def pct(vals, q):
    if not vals:
        return 0
    vals = sorted(vals)
    return vals[min(len(vals) - 1, int(q * len(vals)))]


rows = list(csv.reader(open(sys.argv[1])))
gens = []
for r in rows:
    kind, status, obs, ing, act, sup, n, h = r
    gens.append((kind, status, ts(obs), ts(ing), ts(act), ts(sup), int(n), h))
print(f"generations={len(gens)} scopes={len({g[7] for g in gens})} fact rows covered={sum(g[6] for g in gens)}")
by_status = collections.Counter(g[1] for g in gens)
print("generations by status:", dict(by_status))
rows_by_status = collections.Counter()
for g in gens:
    rows_by_status[g[1]] += g[6]
tot = sum(rows_by_status.values()) or 1
print("rows by status:", {k: f"{v} ({100*v/tot:.1f}%)" for k, v in rows_by_status.items()})
per_scope = collections.defaultdict(collections.Counter)
for g in gens:
    per_scope[g[7]][g[1]] += 1
for st in ("active", "superseded", "failed", "pending"):
    vals = [c[st] for c in per_scope.values()]
    print(f"{st} generations per scope: mean={sum(vals)/len(vals):.2f} p50={pct(vals,.5)} p90={pct(vals,.9)} p99={pct(vals,.99)} max={max(vals)}")
sup_scopes = [c["superseded"] for c in per_scope.values()]
hist = collections.Counter(min(v, 25) for v in sup_scopes)
print("superseded generations per scope histogram (25 = 25 or more):", dict(sorted(hist.items())))
for st in ("active", "superseded", "failed", "pending"):
    vals = [g[6] for g in gens if g[1] == st]
    if vals:
        print(f"rows per {st} generation: n={len(vals)} p50={pct(vals,.5)} p90={pct(vals,.9)} p99={pct(vals,.99)} max={max(vals)} sum={sum(vals)}")
life = [(g[5] - g[4]).total_seconds() / 86400 for g in gens if g[1] == "superseded" and g[4] and g[5]]
print(f"superseded lifetime (activation to supersession, days): n={len(life)} p10={pct(life,.1):.2f} p50={pct(life,.5):.2f} p90={pct(life,.9):.2f} p99={pct(life,.99):.2f} max={max(life) if life else 0:.2f}")
ages = [(max(g[3] for g in gens) - g[3]).total_seconds() / 86400 for g in gens]
print(f"generation ingest span (days from oldest to newest): {max(ages):.1f}")
days = collections.Counter(g[3].date() for g in gens)
newest = max(days)
last = [days.get(newest - dt.timedelta(days=i), 0) for i in range(1, 31)]
print(f"generations created per day, last 30 complete days: mean={sum(last)/30:.0f} p50={pct(last,.5)} max={max(last)}")
sup_rows_per_day = collections.Counter()
for g in gens:
    if g[1] == "superseded":
        sup_rows_per_day[g[5].date() if g[5] else None] += g[6]
sup_rows_per_day.pop(None, None)
lastr = [sup_rows_per_day.get(newest - dt.timedelta(days=i), 0) for i in range(1, 31)]
print(f"rows retired (superseded) per day, last 30 complete days: mean={sum(lastr)/30:.0f} p50={pct(lastr,.5)} max={max(lastr)}")
by_kind = collections.defaultdict(lambda: collections.Counter())
for g in gens:
    by_kind[g[0]]["gens"] += 1
    by_kind[g[0]]["rows"] += g[6]
    if g[1] == "superseded":
        by_kind[g[0]]["sup_rows"] += g[6]
print("by scope kind (generations, rows, superseded rows):")
for k, c in sorted(by_kind.items(), key=lambda kv: -kv[1]["rows"])[:8]:
    print(f"  {k}: {c['gens']} {c['rows']} {c['sup_rows']}")
