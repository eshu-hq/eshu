#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""Match the slow first canonical write of the burst to Neo4j stale-plan discards.

For every C2, T2 and T2TTL run under the given results roots it finds the first
canonical write of a fixture scope after the engaged push (and after the post-release
push) in projector.log, and counts the "Discarded stale query from the query cache"
lines that Neo4j's debug.log wrote inside that write's own window (+-20 ms). A slow
write that coincides with about a hundred discards names the planner replanning
cached statements when the node count moved; it does not show why a pacing arm
triggers it. Neo4j Community has no query log, so the debug.log is the only
statement-level signal.

Usage: neo4j_plan_discards.py <neo4j-debug.log> <results-root> [<results-root> ...]
"""
from __future__ import annotations

import bisect
import glob
import json
import os
import re
import sys
from datetime import datetime, timezone

ARMS = ("C2", "T2", "T2TTL")
WINDOW_S = 0.02
LOOKAHEAD_S = 10.0  # the first write of a push must start within this many seconds of it


def neo4j_epoch(text: str) -> float:
  return datetime.strptime(text[:23], "%Y-%m-%d %H:%M:%S.%f").replace(tzinfo=timezone.utc).timestamp()


def log_epoch(text: str) -> float:
  head, _, frac = text.replace("Z", "+00:00").partition(".")
  frac = frac.split("+")[0][:6].ljust(6, "0")
  return datetime.fromisoformat("%s.%s+00:00" % (head, frac)).timestamp()


def read_discards(path: str) -> tuple[list[float], list[str]]:
  rows = []
  with open(path, encoding="utf-8") as handle:
    for line in handle:
      try:
        row = json.loads(line)
      except ValueError:
        continue
      if "Discarded stale query" in row.get("message", ""):
        reason = re.search(r"Reason: (\w+)", row["message"])
        rows.append((neo4j_epoch(row["time"]), reason.group(1) if reason else "?"))
  rows.sort()
  return [t for t, _ in rows], [r for _, r in rows]


def fixture_writes(run: str) -> list[tuple[float, float, float]]:
  out = []
  with open(os.path.join(run, "projector.log"), encoding="utf-8") as handle:
    for line in handle:
      try:
        row = json.loads(line)
      except ValueError:
        continue
      if (row.get("message") == "projector runtime stage completed" and row.get("stage") == "canonical_write"
          and "git-repository-scope" in row.get("scope_id", "")):
        end = log_epoch(row["timestamp"])
        out.append((end - row["duration_seconds"], end, row["duration_seconds"]))
  return sorted(out)


def main(argv: list[str]) -> int:
  if len(argv) < 3:
    print(__doc__)
    return 2
  times, reasons = read_discards(argv[1])
  print("run          which          first write s  discards inside  reasons")
  for root in argv[2:]:
    for run in sorted(glob.glob(os.path.join(root, "*-rep*"))):
      arm = os.path.basename(run).split("-rep")[0]
      if arm not in ARMS:
        continue
      with open(os.path.join(run, "events.tsv"), encoding="utf-8") as handle:
        events = [line.rstrip("\n").split("\t") for line in handle]
      writes = fixture_writes(run)
      for which in ("engaged", "after_release"):
        push = next((float(e[0]) for e in events if len(e) > 3 and e[1] == "fixture_push" and e[3].strip() == which), None)
        first = next((w for w in writes if push is not None and push - 0.2 <= w[0] <= push + LOOKAHEAD_S), None)
        if first is None:
          print("%-12s %-14s %13s %16s  %s" % (os.path.basename(run), which, "not logged", "-", "no canonical_write line within %.0f s of the push" % LOOKAHEAD_S))
          continue
        start, end, duration = first
        lo, hi = bisect.bisect_left(times, start - WINDOW_S), bisect.bisect_right(times, end + WINDOW_S)
        kinds: dict[str, int] = {}
        for reason in reasons[lo:hi]:
          kinds[reason] = kinds.get(reason, 0) + 1
        print("%-12s %-14s %13.3f %16d  %s" % (os.path.basename(run), which, duration, hi - lo, kinds))
  return 0


if __name__ == "__main__":
  raise SystemExit(main(sys.argv))
