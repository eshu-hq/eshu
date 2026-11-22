#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""#11202 box 3: creation-time bucket arithmetic over generations.csv.

Reads the file written by census_generations.sh and prints numbers only (no scope
names: the input holds only a hash of each scope id). For bucket widths of 1 and 7
days, keyed on the generation's creation time (scope_generations.ingested_at):
rows by status and by scope kind, buckets with zero active rows, the age of active
generations weighted by rows, the K4 droppable share for RANGE by creation time
(all kinds, and a static split that keeps non-stacking kinds out of the buckets),
the K3 partition-count arithmetic, the K5 model, and the shape of the scopes with
exactly 11 and 24 superseded generations.

Non-eligible (K4): active, pending, failed, the first generation of its scope, or a
base that an active delta generation still needs. `--failed-eligible` treats failed
generations as eligible (a sensitivity, not the K4 reading).
Usage: python3 -I aggregate_buckets.py generations.csv [--failed-eligible]
"""
import collections
import csv
import datetime as dt
import math
import statistics
import sys

DAY = 86400.0
failed_eligible = "--failed-eligible" in sys.argv


def ts(s):
    return dt.datetime.fromisoformat(s.replace(" ", "T")) if s else None


G = []
for r in csv.reader(open(sys.argv[1])):
    kind, status, obs, ing, act, sup, n, h, is_delta, needed, rdesc, rasc = r
    G.append(dict(kind=kind, status=status, ing=ts(ing), act=ts(act), sup=ts(sup), rows=int(n), scope=h,
                  needed=needed == "t", rdesc=int(rdesc), rasc=int(rasc)))
NOW = max(g["ing"] for g in G)
EPOCH = min(g["ing"] for g in G)
for g in G:
    g["age"] = (NOW - g["ing"]).total_seconds() / DAY
    g["noneligible"] = (g["status"] in ("active", "pending") or (g["status"] == "failed" and not failed_eligible)
                        or g["rasc"] == 1 or g["needed"])
print(f"generations={len(G)} rows={sum(g['rows'] for g in G)} span_days={(NOW - EPOCH).total_seconds()/DAY:.1f} "
      f"failed_eligible={failed_eligible}")
sup_rows_kind = collections.Counter()
rows_kind = collections.Counter()
for g in G:
    rows_kind[g["kind"]] += g["rows"]
    if g["status"] == "superseded":
        sup_rows_kind[g["kind"]] += g["rows"]
NONSTACK = {k for k in rows_kind if rows_kind[k] and sup_rows_kind[k] / rows_kind[k] < 0.05}
print("non-stacking kinds (superseded rows < 5% of the kind's rows):",
      {k: f"{rows_kind[k]} rows, {100*sup_rows_kind[k]/rows_kind[k]:.1f}% superseded" for k in sorted(NONSTACK, key=lambda x: -rows_kind[x])[:5]})


def bucketize(width):
    b = collections.defaultdict(list)
    for g in G:
        idx = int((g["ing"] - EPOCH).total_seconds() // (width * DAY))
        b[idx].append(g)
    return b


def bucket_age(idx, width):
    end = EPOCH + dt.timedelta(days=(idx + 1) * width)
    return (NOW - end).total_seconds() / DAY


for width in (7, 1):
    B = bucketize(width)
    print(f"\n== bucket width {width} day(s): {len(B)} buckets in the window")
    ages = {i: bucket_age(i, width) for i in B}
    for min_age in (7, 30):
        old = [i for i in B if ages[i] >= min_age]
        zero_active = [i for i in old if not any(g["status"] == "active" for g in B[i])]
        rows_old = sum(g["rows"] for i in old for g in B[i])
        pinned = sum(g["rows"] for i in old for g in B[i] if g["noneligible"])
        sup_old = sum(g["rows"] for i in old for g in B[i] if g["status"] == "superseded")
        drop = sum(g["rows"] for i in old if not any(g["noneligible"] for g in B[i]) for g in B[i] if g["status"] == "superseded")
        # static split: non-stacking kinds leave the time buckets
        Bs = {i: [g for g in B[i] if g["kind"] not in NONSTACK] for i in old}
        sup_old_split = sum(g["rows"] for i in old for g in Bs[i] if g["status"] == "superseded")
        drop_split = sum(g["rows"] for i in old if not any(g["noneligible"] for g in Bs[i]) for g in Bs[i] if g["status"] == "superseded")
        sup_old_all_kinds = sup_old  # K4 stricter denominator for the split: all superseded rows in those buckets
        pinned_split = sum(g["rows"] for i in old for g in Bs[i] if g["noneligible"])
        rows_old_split = sum(g["rows"] for i in old for g in Bs[i])
        print(f"  aged >= {min_age} d: buckets={len(old)} with zero active rows={len(zero_active)} "
              f"with zero non-eligible rows={sum(1 for i in old if not any(g['noneligible'] for g in B[i]))}")
        print(f"    rows in these buckets={rows_old} superseded rows={sup_old} non-eligible (pinned) rows={pinned} "
              f"pinned share p={100*pinned/max(rows_old,1):.1f}%")
        print(f"    K4 droppable share, RANGE all kinds = {100*drop/max(sup_old,1):.1f}% ({drop} of {sup_old})")
        print(f"    K4 droppable share, static split: within buckets = {100*drop_split/max(sup_old_split,1):.1f}% "
              f"({drop_split} of {sup_old_split}); against all superseded rows incl. excluded kinds = "
              f"{100*drop_split/max(sup_old_all_kinds,1):.1f}%; pinned share in split buckets = "
              f"{100*pinned_split/max(rows_old_split,1):.1f}%")
    print(f"  {width}-day buckets, oldest first: age_days | rows in millions: active superseded failed pending | by kind: repository vulnerability_intelligence other")
    for i in sorted(B):
        c = collections.Counter()
        k = collections.Counter()
        for g in B[i]:
            c[g["status"]] += g["rows"]
            k[g["kind"] if g["kind"] in ("repository", "vulnerability_intelligence") else "other"] += g["rows"]
        print(f"    {ages[i]:6.1f} | {c['active']/1e6:6.2f} {c['superseded']/1e6:6.2f} {c['failed']/1e6:5.2f} {c['pending']/1e6:5.2f} | "
              f"{k['repository']/1e6:6.2f} {k['vulnerability_intelligence']/1e6:6.2f} {k['other']/1e6:5.2f}")
    kinds = collections.Counter()
    for i in B:
        for g in B[i]:
            if g["status"] == "active":
                kinds[g["kind"]] += 1
    print(f"  buckets with at least one active generation, aged >=30 d: "
          f"{sum(1 for i in B if ages[i] >= 30 and any(g['status']=='active' for g in B[i]))} of {sum(1 for i in B if ages[i] >= 30)}")

print("\n== age of active generations, weighted by rows (days since creation)")
act = [g for g in G if g["status"] == "active"]
tot = sum(g["rows"] for g in act)
for lim in (1, 7, 30):
    older = sum(g["rows"] for g in act if g["age"] > lim)
    print(f"  active rows in generations older than {lim} d: {older} ({100*older/tot:.1f}% of {tot})")
ws = sorted(((g["age"], g["rows"]) for g in act))
cum = 0
for q in (0.25, 0.5, 0.75, 0.9):
    cum = 0
    for a, r in ws:
        cum += r
        if cum >= q * tot:
            print(f"  rows-weighted age p{int(q*100)}: {a:.1f} d")
            break
print("  K5 model (lower bound of rows pinned at 90 days): active rows older than 30 d, by scope kind")
byk = collections.defaultdict(lambda: [0, 0])
for g in act:
    byk[g["kind"]][0] += g["rows"]
    if g["age"] > 30:
        byk[g["kind"]][1] += g["rows"]
for k, (t, o) in sorted(byk.items(), key=lambda kv: -kv[1][0])[:6]:
    print(f"    {k}: {o} of {t} active rows ({100*o/max(t,1):.1f}%)")
print(f"    all kinds: {sum(v[1] for v in byk.values())} of {tot} "
      f"({100*sum(v[1] for v in byk.values())/tot:.1f}% of active rows; {100*sum(v[1] for v in byk.values())/sum(g['rows'] for g in G):.1f}% of all rows)")

print("\n== K4 sensitivity: which reasons pin a bucket (buckets aged >= 7 d; droppable share of superseded rows)")
VARIANTS = {
    "ruling reading (active, pending, failed, first generation, delta base)":
        lambda g: g["status"] in ("active", "pending", "failed") or g["rasc"] == 1 or g["needed"],
    "no failed pin (active, pending, first generation, delta base)":
        lambda g: g["status"] in ("active", "pending") or g["rasc"] == 1 or g["needed"],
    "no first-generation pin (active, pending, failed, delta base)":
        lambda g: g["status"] in ("active", "pending", "failed") or g["needed"],
    "active and pending only":
        lambda g: g["status"] in ("active", "pending"),
    "active only":
        lambda g: g["status"] == "active",
}
for width in (7, 1):
    B = bucketize(width)
    old = [i for i in B if bucket_age(i, width) >= 7]
    for name, pin in VARIANTS.items():
        sup = sum(g["rows"] for i in old for g in B[i] if g["status"] == "superseded")
        free = [i for i in old if not any(pin(g) for g in B[i])]
        drop = sum(g["rows"] for i in free for g in B[i] if g["status"] == "superseded")
        pinned = sum(g["rows"] for i in old for g in B[i] if pin(g))
        allr = sum(g["rows"] for i in old for g in B[i])
        print(f"  width {width} d, {name}: buckets free of pins {len(free)} of {len(old)}; droppable share "
              f"{100*drop/max(sup,1):.1f}%; pinned share p={100*pinned/max(allr,1):.1f}%")
    for name in ("ruling reading (active, pending, failed, first generation, delta base)", "active only"):
        pin = VARIANTS[name]
        Bs = {i: [g for g in B[i] if g["kind"] not in NONSTACK] for i in old}
        sup_all = sum(g["rows"] for i in old for g in B[i] if g["status"] == "superseded")
        free = [i for i in old if not any(pin(g) for g in Bs[i])]
        drop = sum(g["rows"] for i in free for g in Bs[i] if g["status"] == "superseded")
        print(f"  width {width} d, STATIC SPLIT, {name}: buckets free of pins {len(free)} of {len(old)}; droppable share "
              f"of all superseded rows {100*drop/max(sup_all,1):.1f}%")
    reasons = collections.Counter()
    for i in old:
        for g in B[i]:
            if g["status"] == "active":
                reasons["active"] += g["rows"]
            elif g["status"] == "pending":
                reasons["pending"] += g["rows"]
            elif g["status"] == "failed":
                reasons["failed"] += g["rows"]
            elif g["rasc"] == 1:
                reasons["first generation (superseded)"] += g["rows"]
            elif g["needed"]:
                reasons["delta base (superseded)"] += g["rows"]
    print(f"  width {width} d rows pinned by reason in buckets aged >= 7 d: {dict(reasons)}")

print("\n== K3 arithmetic (leaf partitions at the policy horizon = ceil((6 + 90 + width) / width) x modulus)")
for width in (7, 1):
    leaves = math.ceil((6 + 90 + width) / width)
    print(f"  RANGE by creation time, {width}-day buckets: {leaves} leaves (limit 128: {'pass' if leaves <= 128 else 'KILL'})")
    print(f"  scope hash x time sub-parts, modulus 32, {width}-day buckets: {32*leaves} leaves ({'pass' if 32*leaves <= 128 else 'KILL'})")
rate = len([g for g in G if (NOW - g["ing"]).total_seconds() <= 30 * DAY]) / 30
print(f"  LIST per generation: generations created in 96 days at {rate:.0f}/day = {rate*96:.0f} leaves (KILL)")

print("\n== scopes with exactly N superseded generations (N = 11 and 24)")
by_scope = collections.defaultdict(list)
for g in G:
    by_scope[g["scope"]].append(g)
for n in (11, 24, 23, 12):
    sel = [v for v in by_scope.values() if sum(1 for g in v if g["status"] == "superseded") == n]
    if not sel:
        continue
    kinds = collections.Counter(v[0]["kind"] for v in sel)
    gaps, spans, per_gen_rows, total_gens = [], [], [], []
    for v in sel:
        t = sorted(g["ing"] for g in v)
        total_gens.append(len(v))
        spans.append((t[-1] - t[0]).total_seconds() / DAY)
        gaps += [(b - a).total_seconds() / 3600 for a, b in zip(t, t[1:])]
        per_gen_rows += [g["rows"] for g in v]
    first = [min(g["ing"] for g in v) for v in sel]
    newest = [(NOW - max(g["ing"] for g in v)).total_seconds() / DAY for v in sel]
    hours = collections.Counter(g["ing"].hour for v in sel for g in v)
    top_hour = hours.most_common(1)[0]
    print(f"        newest generation age p50={statistics.median(newest):.1f} d p90={sorted(newest)[int(.9*len(newest))]:.1f} d; "
          f"busiest hour of day (UTC) {top_hour[0]:02d}h holds {100*top_hour[1]/sum(hours.values()):.1f}% of creations")
    print(f"  N={n}: scopes={len(sel)} kinds={dict(kinds)} generations per scope p50={statistics.median(total_gens):.0f} "
          f"creation span p50={statistics.median(spans):.1f} d p90={sorted(spans)[int(.9*len(spans))]:.1f} d; "
          f"gap between consecutive generations p50={statistics.median(gaps):.1f} h p90={sorted(gaps)[int(.9*len(gaps))]:.1f} h; "
          f"rows per generation p50={statistics.median(per_gen_rows):.0f}")
    print(f"        first generation created: earliest {(NOW-min(first)).total_seconds()/DAY:.1f} d ago, latest {(NOW-max(first)).total_seconds()/DAY:.1f} d ago; "
          f"scopes with a failed generation: {sum(1 for v in sel if any(g['status']=='failed' for g in v))}")
