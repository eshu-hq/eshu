# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""Turns one replay run directory into result.json for check_arm.py.

Inputs (written by the driver libs under the run directory): manifest.json,
samples.tsv, metrics/*.prom (final scrapes of the real binaries), export/*,
census-*.txt and events.tsv. Instrument readings (consult duration, deferrals)
come only from the binaries' /metrics scrapes; database and graph state come
from the sampler and the exports. Nothing here times anything itself.

Usage: assemble_result.py RUN_DIR
"""

import csv
import glob
import json
import math
import os
import re
import sys
from collections import defaultdict
from datetime import datetime, timezone
from typing import Any

_SERIES = re.compile(r"^([a-zA-Z_:][a-zA-Z0-9_:]*)(?:\{(.*)\})?\s+([-+0-9.eE]+|NaN|\+Inf)$")
_LABEL = re.compile(r'([a-zA-Z_][a-zA-Z0-9_]*)="((?:[^"\\]|\\.)*)"')


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


def load_metrics(run_dir: str) -> list[tuple[str, dict[str, str], float]]:
  """Reads the final scrape of every process."""
  rows = []
  for path in sorted(glob.glob(os.path.join(run_dir, "metrics", "*-final.prom"))):
    rows.extend(parse_prom(path))
  return rows


def consult_stats(rows: list, sources: dict[str, str]) -> dict[str, Any]:
  """Consult p50 and p95 per source (A, B) from the duration histogram."""
  hist: dict[str, dict[float, float]] = defaultdict(lambda: defaultdict(float))
  counts: dict[str, float] = defaultdict(float)
  for name, labels, value in rows:
    if labels.get("outcome") != "ok" or "source_system" not in labels:
      continue
    side = {v: k for k, v in sources.items()}.get(labels["source_system"], "other")
    if name.endswith("source_consult_duration_seconds_bucket"):
      le = float("inf") if labels["le"] == "+Inf" else float(labels["le"])
      hist[side][le] += value
    elif name.endswith("source_consult_duration_seconds_count"):
      counts[side] += value
  out = {}
  for side, buckets in hist.items():
    pairs = list(buckets.items())
    out[side] = {"count": counts[side], "p50_s": quantile(pairs, 0.5), "p95_s": quantile(pairs, 0.95)}
  return out


def deferrals(rows: list, sources: dict[str, str]) -> tuple[dict[str, float], float]:
  """source_behind deferral counts by side, and pre-write release total."""
  by_side: dict[str, float] = defaultdict(float)
  released = 0.0
  inverse = {v: k for k, v in sources.items()}
  for name, labels, value in rows:
    if name == "eshu_dp_reducer_admission_deferrals_total" and labels.get("reason") == "source_behind":
      by_side[inverse.get(labels.get("source_system", ""), "other")] += value
    elif name == "eshu_dp_projector_source_paced_retries_total":
      released += value
  return dict(by_side), released


def read_samples(path: str) -> list[dict[str, Any]]:
  """Reads samples.tsv rows, with numeric fields converted."""
  out = []
  with open(path, encoding="utf-8") as handle:
    for row in csv.DictReader(handle, delimiter="\t"):
      try:
        out.append({
            "ts": float(row["ts"]), "engaged": row["latch_engaged"] == "t",
            "content": int(row["content_files_a"]) + int(row["content_entities_a"]),
            "succeeded": int(row["projector_succeeded_a"]), "k": int(row["k_visible_retrying_a"]),
            "waits": int(row["wait_rows"]), "real_gen": int(row["real_generations"]),
            "graph": int(row["graph_nodes_a"] or 0),
        })
      except (KeyError, ValueError):
        continue
  return out


def iso(ts: float | None) -> str | None:
  return None if ts is None else datetime.fromtimestamp(ts, timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def engagement(samples: list[dict[str, Any]]) -> tuple[dict[str, Any], dict[str, int]]:
  """Engage and release times, and A's writes while the latch was engaged."""
  first = next((s for s in samples if s["engaged"]), None)
  if first is None:
    return {"engage_at": None, "release_at": None}, {"graph_nodes": 0, "content_rows": 0, "projector_succeeded": 0}
  engaged_rows = []
  release = None
  for s in samples:
    if s["ts"] < first["ts"]:
      continue
    if s["engaged"]:
      engaged_rows.append(s)
    else:
      release = s["ts"]
      break
  last = engaged_rows[-1]
  writes = {
      "graph_nodes": last["graph"] - first["graph"],
      "content_rows": last["content"] - first["content"],
      "projector_succeeded": last["succeeded"] - first["succeeded"],
  }
  return {"engage_at": iso(first["ts"]), "release_at": iso(release)}, writes


def selection(samples: list[dict[str, Any]], run_dir: str) -> list[dict[str, Any]]:
  """Per fixture-commit event: seconds until the new generations show up."""
  path = os.path.join(run_dir, "events.tsv")
  if not os.path.exists(path):
    return []
  out = []
  with open(path, encoding="utf-8") as handle:
    for line in handle:
      parts = line.rstrip("\n").split("\t")
      if len(parts) < 3 or parts[1] != "fixture_commit":
        continue
      at, repos = float(parts[0]), int(parts[2])
      base = max((s["real_gen"] for s in samples if s["ts"] <= at), default=0)
      first = next((s["ts"] for s in samples if s["ts"] > at and s["real_gen"] > base), None)
      done = next((s["ts"] for s in samples if s["ts"] > at and s["real_gen"] >= base + repos), None)
      out.append({"event": parts[3] if len(parts) > 3 else "", "commit_at": iso(at),
                  "delay_first_s": None if first is None else round(first - at, 1),
                  "delay_all_s": None if done is None else round(done - at, 1)})
  return out


def census_classes(path: str) -> dict[str, int]:
  """Parses a census.sh table into {class: rows}."""
  classes: dict[str, int] = {}
  if not os.path.exists(path):
    return classes
  with open(path, encoding="utf-8") as handle:
    for line in handle:
      parts = [p.strip() for p in line.split("|")]
      if len(parts) >= 5 and parts[0] and parts[0] not in ("class",) and not set(parts[0]) <= {"-", "+"}:
        try:
          classes[parts[0]] = int(parts[1])
        except ValueError:
          continue
  return classes


def read_json(path: str, default: Any) -> Any:
  if not os.path.exists(path):
    return default
  with open(path, encoding="utf-8") as handle:
    text = handle.read().strip()
  return json.loads(text) if text else default


def main(run_dir: str) -> int:
  manifest = read_json(os.path.join(run_dir, "manifest.json"), {})
  sources = {"A": manifest.get("source_a", "A"), "B": manifest.get("source_b", "B")}
  rows = load_metrics(run_dir)
  samples = read_samples(os.path.join(run_dir, "samples.tsv"))
  engaged, writes = engagement(samples)
  by_side, released = deferrals(rows, sources)
  export = os.path.join(run_dir, "export")
  counters = read_json(os.path.join(export, "counters.json"), {})
  intents = read_json(os.path.join(export, "reducer_intents.json"), {})
  graph_counts = {}
  gc_path = os.path.join(export, "graph_counts.tsv")
  if os.path.exists(gc_path):
    with open(gc_path, encoding="utf-8") as handle:
      for line in handle:
        parts = [p.strip() for p in (line.split(",") if "," in line else line.split())]
        if len(parts) == 2 and parts[1].isdigit():
          graph_counts[parts[0].strip()] = int(parts[1])
  census_end = census_classes(os.path.join(run_dir, "census-end.txt"))
  result = {
      "arm": manifest.get("arm"), "rep": manifest.get("rep"), "ttl": manifest.get("ttl"),
      "manifest": manifest, "engaged": engaged, "a_writes_during_engagement": writes,
      "released_pre_write": released,
      "deferrals": {
          "source_behind": by_side, "attempt_count_changes": counters.get("attempt_count_changes", 0),
          "dead_letters": counters.get("dead_letters", 0), "admission_timeouts": counters.get("admission_timeouts", 0),
      },
      "b": read_json(os.path.join(export, "b_stats.json"), {}),
      "consult": consult_stats(rows, sources),
      "report": {
          "k_peak": max((s["k"] for s in samples), default=0),
          "parked_rows_peak": max((s["waits"] for s in samples), default=0),
          "woken_holding_wait": census_classes(os.path.join(run_dir, "census-pre.txt")).get("woken_holding_wait", 0),
          "wait_table_rows_final": samples[-1]["waits"] if samples else 0,
      },
      "census": {"classes": census_classes(os.path.join(run_dir, "census-pre.txt")),
                 "classes_end": census_end, "claimable_a_at_release": census_end.get("takeable", 0)},
      "selection": selection(samples, run_dir),
      "hosted": {
          "planted": read_json(os.path.join(export, "hosted-final.json"), {}).get("planted", 0),
          "at_engagement_end": read_json(os.path.join(export, "hosted-at_engagement_end.json"), {}),
          "final": read_json(os.path.join(export, "hosted-final.json"), {}),
      },
      "final": {
          "generations": read_json(os.path.join(export, "generations.json"), {}),
          "real_fact_sets": read_json(os.path.join(export, "real_fact_sets.json"), {}),
          "graph_counts": graph_counts,
          "reducer_intents": {"A": intents.get("A", {}), "B": intents.get("B", {})},
          "content_rows": counters.get("content_rows"),
          "projector_unsettled": counters.get("projector_unsettled"),
      },
      "projector_superseded": counters.get("projector_superseded", 0),
  }
  with open(os.path.join(run_dir, "result.json"), "w", encoding="utf-8") as handle:
    json.dump(result, handle, indent=2, sort_keys=True)
  return 0


if __name__ == "__main__":
  if len(sys.argv) != 2:
    print(__doc__, file=sys.stderr)
    sys.exit(2)
  sys.exit(main(sys.argv[1]))
