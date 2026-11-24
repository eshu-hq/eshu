#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""Per-item queue wait of the healthy source B, from a replay results root.

The registered AC5 clause compares per-run percentiles of B's projector queue
wait against the control repetitions. That comparison cannot resolve 10%: the
projector polls once a second and the feeder commits once a second, so a run's
p50 is one draw of a drifting phase. This table reads the same queue timestamps
per ITEM instead (claim time minus enqueue time of every succeeded B projector
item, from export/projector_items.tsv), so the arbiter's pooled reading can be
reproduced from the committed raw rows: items, max wait, items above one second,
and the mean of the per-run p50 and p95, per arm and pooled treatment against
pooled control.

It judges nothing. Usage: b_item_table.py <results-root> [--burst]

With --burst it also prints, for the arms whose B is git (C2, T2, T2TTL), the
waits of the six-fixture burst pushed at the start of the engaged window, so the
head-of-line effect of A's first paced re-claim wave can be read per run.
"""
from __future__ import annotations

import glob
import json
import os
import sys
from collections import defaultdict
from typing import Any

HOSTED_SOURCE = "dev.eshu.collector.synth-8877-cutover"
# projector_items.tsv columns written by lib/collect.sh.
COL_STATUS, COL_SOURCE, COL_CREATED, COL_CLAIMED = 4, 7, 10, 11
BURST_BEFORE_S, BURST_AFTER_S = 1.0, 3.0
FIXTURE_SCOPE_PREFIX = "git-repository-scope:"
# The arms the pooled reading compares: the hosted-B treatments against the two
# hosted-B controls. CF and CG are a reference and a reported reading, not controls.
POOL_TREATMENT_ARMS = ("T1", "T1TTL", "T3", "T3TTL")
POOL_CONTROL_ARMS = ("C", "C0")


def percentile(values: list[float], fraction: float) -> float:
  """Linear-interpolated percentile, the same definition as percentile_cont."""
  if not values:
    return 0.0
  ordered = sorted(values)
  position = fraction * (len(ordered) - 1)
  low = int(position)
  high = min(low + 1, len(ordered) - 1)
  return ordered[low] + (ordered[high] - ordered[low]) * (position - low)


def read_rows(path: str) -> list[list[str]]:
  with open(path, encoding="utf-8") as handle:
    return [line.rstrip("\n").split("\t") for line in handle if line.strip()]


def b_waits(run_dir: str, source_b: str) -> list[tuple[float, float, str]]:
  """(created_epoch, wait_seconds, scope_id) of every succeeded source-B projector item."""
  out = []
  for row in read_rows(os.path.join(run_dir, "export", "projector_items.tsv")):
    if len(row) < 13 or row[COL_STATUS] != "succeeded" or row[COL_SOURCE] != source_b or not row[COL_CLAIMED]:
      continue
    created, claimed = float(row[COL_CREATED]), float(row[COL_CLAIMED])
    out.append((created, claimed - created, row[0]))
  return out


def summarize(waits: list[float]) -> dict[str, Any]:
  return {"items": len(waits), "max": max(waits, default=0.0), "above_1s": sum(1 for w in waits if w > 1.0),
          "p50": percentile(waits, 0.5), "p95": percentile(waits, 0.95)}


def load_runs(results: str) -> list[dict[str, Any]]:
  runs = []
  for run_dir in sorted(glob.glob(os.path.join(results, "*-rep*"))):
    path = os.path.join(run_dir, "export", "projector_items.tsv")
    manifest_path = os.path.join(run_dir, "manifest.json")
    if not (os.path.isfile(path) and os.path.isfile(manifest_path)):
      continue
    with open(manifest_path, encoding="utf-8") as handle:
      manifest = json.load(handle)
    source_b = manifest.get("source_b", "")
    rows = b_waits(run_dir, source_b)
    runs.append({"name": os.path.basename(run_dir), "arm": manifest.get("arm", ""),
                 "treatment": manifest.get("thresholds") not in (0, "0", False, None, ""),
                 "source_b": source_b, "dir": run_dir, "rows": rows})
  return runs


def per_arm_table(runs: list[dict[str, Any]]) -> list[str]:
  lines = ["arm        runs items max_s above_1s mean_run_p50_s mean_run_p95_s"]
  by_arm: dict[str, list[dict[str, Any]]] = defaultdict(list)
  for run in runs:
    by_arm[run["arm"]].append(run)
  for arm in sorted(by_arm):
    group = by_arm[arm]
    waits = [w for run in group for _, w, _ in run["rows"]]
    per_run = [summarize([w for _, w, _ in run["rows"]]) for run in group]
    lines.append("%-10s %4d %5d %5.2f %8d %14.3f %14.3f" % (
        arm, len(group), len(waits), max(waits, default=0.0), sum(1 for w in waits if w > 1.0),
        sum(r["p50"] for r in per_run) / len(per_run), sum(r["p95"] for r in per_run) / len(per_run)))
  return lines


def pooled(runs: list[dict[str, Any]], label: str) -> str:
  waits = [w for run in runs for _, w, _ in run["rows"]]
  per_run = [summarize([w for _, w, _ in run["rows"]]) for run in runs]
  if not per_run:
    return "%s: no runs" % label
  return "%s: runs=%d items=%d max=%.2f s above 1 s=%d; per-run p50 mean %.3f, p95 mean %.3f" % (
      label, len(runs), len(waits), max(waits, default=0.0), sum(1 for w in waits if w > 1.0),
      sum(r["p50"] for r in per_run) / len(per_run), sum(r["p95"] for r in per_run) / len(per_run))


def hosted_b_pooled(runs: list[dict[str, Any]]) -> list[str]:
  """The arbiter's reading: arms whose B is the hosted family, treatment against control."""
  hosted = [run for run in runs if run["source_b"] == HOSTED_SOURCE]
  return [pooled([r for r in hosted if r["arm"] in POOL_TREATMENT_ARMS], "hosted-B treatment (T1, T1TTL, T3, T3TTL)"),
          pooled([r for r in hosted if r["arm"] in POOL_CONTROL_ARMS], "hosted-B control (C, C0)")]


def burst_table(runs: list[dict[str, Any]]) -> list[str]:
  lines = ["run          burst items waits_s (the six-fixture push at the start of the engaged window)"]
  for run in runs:
    if run["source_b"] != "git":
      continue
    events = os.path.join(run["dir"], "events.tsv")
    if not os.path.isfile(events):
      continue
    push = next((float(r[0]) for r in read_rows(events) if len(r) > 3 and r[1] == "fixture_push" and r[3] == "engaged"), None)
    if push is None:
      continue
    waits = sorted(round(w, 2) for created, w, scope in run["rows"]
                   if scope.startswith(FIXTURE_SCOPE_PREFIX) and push - BURST_BEFORE_S <= created <= push + BURST_AFTER_S)
    lines.append("%-12s %5d %s" % (run["name"], len(waits), " ".join("%.2f" % w for w in waits)))
  return lines


def main(argv: list[str]) -> int:
  if len(argv) < 2:
    print(__doc__)
    return 2
  runs = load_runs(argv[1])
  print("== per-item B queue wait (succeeded B projector items; claim time minus enqueue time)")
  print("\n".join(per_arm_table(runs)))
  print("== pooled")
  print("\n".join(hosted_b_pooled(runs)))
  if "--burst" in argv:
    print("== burst")
    print("\n".join(burst_table(runs)))
  return 0


if __name__ == "__main__":
  raise SystemExit(main(sys.argv))
