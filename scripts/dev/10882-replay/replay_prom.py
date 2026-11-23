# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""Prometheus text parsing and the histogram readings the replay needs.

Instrument readings come only from the product binaries' /metrics scrapes. The
consult duration is read from the ENGAGED scrape (taken just before the catch-up
step), because the final scrape mixes in cheap post-release consults.
"""

import glob
import math
import os
import re
from collections import defaultdict
from typing import Any

_SERIES = re.compile(r"^([a-zA-Z_:][a-zA-Z0-9_:]*)(?:\{(.*)\})?\s+([-+0-9.eE]+|NaN|\+Inf)$")
_LABEL = re.compile(r'([a-zA-Z_][a-zA-Z0-9_]*)="((?:[^"\\]|\\.)*)"')

# The enablement gate edge: p50 at most 100 ms is judged on the le=0.1 bucket
# count, not on an interpolation inside a bucket (telemetry/
# instruments_source_consult.go).
GATE_EDGE_S = 0.1


def parse_prom(path: str) -> list[tuple[str, dict[str, str], float]]:
  """Parses Prometheus text exposition into (name, labels, value) rows."""
  rows = []
  with open(path, encoding="utf-8") as handle:
    for line in handle:
      if line.startswith("#") or not line.strip():
        continue
      match = _SERIES.match(line.strip())
      if not match:
        continue
      labels = dict(_LABEL.findall(match.group(2) or ""))
      value = float("inf") if match.group(3) == "+Inf" else float(match.group(3))
      rows.append((match.group(1), labels, value))
  return rows


def quantile(buckets: list[tuple[float, float]], q: float) -> float:
  """Linear-interpolated quantile from cumulative (le, count) buckets."""
  buckets = sorted(buckets)
  if not buckets or buckets[-1][1] == 0:
    return 0.0
  target = q * buckets[-1][1]
  prev_le, prev_count = 0.0, 0.0
  for le, count in buckets:
    if count >= target:
      if math.isinf(le) or count == prev_count:
        return prev_le
      return prev_le + (le - prev_le) * (target - prev_count) / (count - prev_count)
    prev_le, prev_count = le, count
  return prev_le


def load_metrics(run_dir: str, tag: str) -> list[tuple[str, dict[str, str], float]]:
  """Reads the scrape of every process with the given tag (engaged or final)."""
  rows = []
  for path in sorted(glob.glob(os.path.join(run_dir, "metrics", "*-%s.prom" % tag))):
    rows.extend(parse_prom(path))
  return rows


def _side(sources: dict[str, str], system: str) -> str:
  inverse = {v: k for k, v in sources.items()}
  return inverse.get(system, "other")


def consult_stats(rows: list, sources: dict[str, str]) -> dict[str, Any]:
  """Consult count, p50, p95 and the share inside the 100 ms edge, per source."""
  hist: dict[str, dict[float, float]] = defaultdict(lambda: defaultdict(float))
  counts: dict[str, float] = defaultdict(float)
  for name, labels, value in rows:
    if labels.get("outcome") != "ok" or "source_system" not in labels:
      continue
    side = _side(sources, labels["source_system"])
    if name.endswith("source_consult_duration_seconds_bucket"):
      le = float("inf") if labels["le"] == "+Inf" else float(labels["le"])
      hist[side][le] += value
    elif name.endswith("source_consult_duration_seconds_count"):
      counts[side] += value
  out = {}
  for side, buckets in hist.items():
    pairs = list(buckets.items())
    inside = sum(c for le, c in pairs if abs(le - GATE_EDGE_S) < 1e-9)
    out[side] = {"count": counts[side], "p50_s": quantile(pairs, 0.5), "p95_s": quantile(pairs, 0.95),
                 "within_100ms": (inside / counts[side]) if counts[side] else 0.0}
  return out


def deferrals(rows: list, sources: dict[str, str]) -> tuple[dict[str, float], float]:
  """source_behind admission deferrals by side, and the pre-write release total."""
  by_side: dict[str, float] = defaultdict(float)
  released = 0.0
  for name, labels, value in rows:
    if name == "eshu_dp_reducer_admission_deferrals_total" and labels.get("reason") == "source_behind":
      by_side[_side(sources, labels.get("source_system", ""))] += value
    elif name == "eshu_dp_projector_source_paced_retries_total":
      released += value
  return dict(by_side), released


def engaged_counter(rows: list, source_a: str) -> float:
  """Latch engagements recorded for source A, summed over every process."""
  total = 0.0
  for name, labels, value in rows:
    if (name == "eshu_dp_workflow_claim_pacing_engaged_total" and labels.get("pacing_signal") == "reducer_source"
        and labels.get("domain") == source_a):
      total += value
  return total


def active_gauge(rows: list, source_a: str) -> float | None:
  """The claim-pacing active gauge for source A in one scrape (0 after release).

  None when the scrape has no such series at all: a process that never ran the
  latch transition exposes none, and a missing series must never read as 0.
  """
  values = [value for name, labels, value in rows
            if name == "eshu_dp_workflow_claim_pacing_active" and labels.get("pacing_signal") == "reducer_source"
            and labels.get("domain") == source_a]
  return sum(values) if values else None


def claim_grant_consult(rows: list) -> dict[str, Any]:
  """The family consult at a coordinator claim grant: the histogram series that
  carry collector_kind instead of source_system. Count, p50, p95 and the share
  inside the 100 ms edge over every such series, plus the count per collector kind.
  Empty (count 0) when no process emitted the series, which is the T2 gap."""
  buckets: dict[float, float] = defaultdict(float)
  by_kind: dict[str, float] = defaultdict(float)
  total = 0.0
  for name, labels, value in rows:
    if labels.get("outcome") != "ok" or "collector_kind" not in labels:
      continue
    if name.endswith("source_consult_duration_seconds_bucket"):
      le = float("inf") if labels["le"] == "+Inf" else float(labels["le"])
      buckets[le] += value
    elif name.endswith("source_consult_duration_seconds_count"):
      by_kind[labels["collector_kind"]] += value
      total += value
  pairs = list(buckets.items())
  inside = sum(c for le, c in pairs if abs(le - GATE_EDGE_S) < 1e-9)
  return {"count": total, "by_kind": dict(by_kind), "p50_s": quantile(pairs, 0.5) if pairs else 0.0,
          "p95_s": quantile(pairs, 0.95) if pairs else 0.0, "within_100ms": (inside / total) if total else 0.0}
