#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""Which projector worker held each burst item of the healthy source, and what it did before.

The six git-B fixture items pushed at the start of the engaged window wait in the
projector queue while source A's paced items are re-claimed. This reads one run's
saved files only (export/projector_items.tsv, events.tsv, projector.log) and prints,
for each burst item: enqueue time, claim time and wait (seconds after the push), the
worker that loaded its facts, and that worker's logged events from the push to the
claim: A items it released before writing ("source paced") and B items it finished.
It also prints the A paced releases per worker as time buckets, so a re-claim wave
and the burst can be placed on one clock. Reading only; it measures nothing new.

Usage: per_worker_timeline.py <run-dir>
"""
from __future__ import annotations

import json
import os
import sys
from collections import defaultdict
from typing import Any

FIXTURE = "git-repository-scope:"
PACED = "projector work released before projection writes: source paced"
LOAD = "projector work stage completed"


def read_tsv(path: str) -> list[list[str]]:
  with open(path, encoding="utf-8") as handle:
    return [line.rstrip("\n").split("\t") for line in handle if line.strip()]


def push_time(run: str) -> float:
  for row in read_tsv(os.path.join(run, "events.tsv")):
    if len(row) > 3 and row[1] == "fixture_push" and row[3] == "engaged":
      return float(row[0])
  raise SystemExit("no engaged fixture push in events.tsv")


def log_events(run: str) -> list[dict[str, Any]]:
  from datetime import datetime
  out = []
  with open(os.path.join(run, "projector.log"), encoding="utf-8") as handle:
    for line in handle:
      try:
        row = json.loads(line)
      except ValueError:
        continue
      if row.get("message") not in (PACED, LOAD) or "worker_id" not in row:
        continue
      stamp = row["timestamp"].replace("Z", "+00:00")
      head, _, frac = stamp.partition(".")
      frac = frac.split("+")[0][:6].ljust(6, "0")
      row["_t"] = datetime.fromisoformat("%s.%s+00:00" % (head, frac)).timestamp()
      row["_w"] = str(row["worker_id"])
      out.append(row)
  return out


def main(run: str) -> int:
  push = push_time(run)
  events = log_events(run)
  items = []
  for row in read_tsv(os.path.join(run, "export", "projector_items.tsv")):
    if len(row) < 13 or row[0][:len(FIXTURE)] != FIXTURE or row[4] != "succeeded" or not row[11]:
      continue
    created, claimed = float(row[10]), float(row[11])
    if push - 1 <= created <= push + 3:
      items.append((claimed, created, row[0], row[1]))
  items.sort()
  print("run %s: push at t=0; %d burst items" % (os.path.basename(run.rstrip("/")), len(items)))
  loads = {(e["scope_id"], e["generation_id"]): e for e in events if e["message"] == LOAD and e.get("stage") == "load_facts"}
  last_claim = max((c for c, _, _, _ in items), default=push)
  print("-- burst items (seconds after the push)")
  for claimed, created, scope, gen in items:
    load = loads.get((scope, gen))
    worker = load["_w"] if load else "?"
    before = [e for e in events if e["_w"] == worker and push - 1 <= e["_t"] <= claimed]
    paced = sum(1 for e in before if e["message"] == PACED)
    done_b = sum(1 for e in before if e["message"] == LOAD and e.get("source_system") == "git")
    print("%s enqueue +%.2f claim +%.2f wait %.2f s worker %s | before its claim (push..claim): %d A paced releases, %d B stages" % (
        scope[-8:], created - push, claimed - push, claimed - created, worker, paced, done_b))
  print("-- A paced releases per worker, per second after the push (until the last burst claim +2 s)")
  grid: dict[str, dict[int, int]] = defaultdict(lambda: defaultdict(int))
  for e in events:
    if e["message"] == PACED and push - 1 <= e["_t"] <= last_claim + 2:
      grid[e["_w"]][int(e["_t"] - push)] += 1
  for worker in sorted(grid):
    print("worker %s: %s" % (worker, " ".join("t+%d:%d" % (s, n) for s, n in sorted(grid[worker].items()))))
  total = sum(sum(v.values()) for v in grid.values())
  print("A paced releases in the window: %d" % total)
  return 0


if __name__ == "__main__":
  if len(sys.argv) != 2:
    print(__doc__)
    raise SystemExit(2)
  raise SystemExit(main(sys.argv[1]))
