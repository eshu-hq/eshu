# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""Readers for the sampler rows and the end-of-run exports of one replay run.

Everything here is a pure function of files the driver wrote, so it is tested
with seeded inputs (test_replay_state.py). Timestamps are epoch seconds.
"""

import csv
import os
from collections import defaultdict
from typing import Any


def _num(value: str) -> float | None:
  try:
    return float(value)
  except ValueError:
    return None


def read_samples(path: str) -> list[dict[str, Any]]:
  """Reads samples.tsv. A missing graph reading stays None, never 0."""
  out = []
  if not os.path.exists(path):
    return out
  with open(path, encoding="utf-8") as handle:
    for row in csv.DictReader(handle, delimiter="\t"):
      try:
        out.append({
            "ts": float(row["ts"]), "engaged": row["latch_engaged"] == "t",
            "latch_engaged_at": _num(row["latch_engaged_at"]), "latch_updated_at": _num(row["latch_updated_at"]),
            "content": int(row["content_files_a"]) + int(row["content_entities_a"]),
            "succeeded": int(row["projector_succeeded_a"]), "k": int(row["k_visible_retrying_a"]),
            "waits": int(row["wait_rows"]), "real_gen": int(row["real_generations"]),
            "graph": None if row["graph_nodes_a"] in ("NA", "") else int(row["graph_nodes_a"]),
            "load1": _num(row.get("load1", "")),
        })
      except (KeyError, ValueError):
        continue
  return out


def engagement(samples: list[dict[str, Any]]) -> dict[str, Any]:
  """Engage and release times from the latch row, re-engagement, and the writes
  of source A at the points the zero-write checks compare."""
  engaged_idx = [i for i, s in enumerate(samples) if s["engaged"]]
  # Engaged intervals: a run of engaged samples counts once, so a second interval
  # (release, then engage again) is re-engagement.
  intervals = sum(1 for i, s in enumerate(samples) if s["engaged"] and (i == 0 or not samples[i - 1]["engaged"]))
  if not engaged_idx:
    return {"engage_at": None, "release_at": None, "reengaged": False, "latch_row_seen": any(
        s["latch_engaged_at"] is not None or s["latch_updated_at"] is not None for s in samples)}
  first, last = samples[engaged_idx[0]], samples[engaged_idx[-1]]
  release = next((s for s in samples[engaged_idx[-1] + 1:] if not s["engaged"]), None)
  # The latch row's engaged_at identifies one engagement: a new value while
  # engaged is a new engagement even if no sample caught the gap between them.
  distinct = {s["latch_engaged_at"] for s in samples if s["engaged"] and s["latch_engaged_at"] is not None}
  return {
      "engage_at": first["latch_engaged_at"] or first["ts"], "distinct_engagements": max(len(distinct), 1),
      "release_at": None if release is None else (release["latch_updated_at"] or release["ts"]),
      "reengaged": intervals > 1 or len(distinct) > 1, "latch_row_seen": True,
      "first_engaged_ts": first["ts"], "last_engaged_ts": last["ts"],
  }


def write_points(samples: list[dict[str, Any]], engaged: dict[str, Any], t120: float | None) -> dict[str, Any]:
  """A's graph, content and succeeded counts at the run's first sample, at the
  last engaged sample, at the end, and at t+120 s (the positive control)."""
  def point(s: dict[str, Any] | None) -> dict[str, Any] | None:
    return None if s is None else {"graph": s["graph"], "content": s["content"], "succeeded": s["succeeded"], "ts": s["ts"]}
  first = samples[0] if samples else None
  last_engaged = None
  if "last_engaged_ts" in engaged:
    last_engaged = next((s for s in samples if s["ts"] == engaged["last_engaged_ts"]), None)
  at120 = None if t120 is None else next((s for s in samples if s["ts"] >= t120), None)
  return {"first": point(first), "last_engaged": point(last_engaged),
          "end": point(samples[-1] if samples else None), "at_t120": point(at120)}


def read_events(path: str) -> list[list[str]]:
  if not os.path.exists(path):
    return []
  with open(path, encoding="utf-8") as handle:
    return [line.rstrip("\n").split("\t") for line in handle if line.strip()]


def selection(events: list[list[str]], generations_path: str) -> dict[str, Any]:
  """Per pushed commit: seconds from the push to the ingest of the generation
  carrying that commit's sha. Per label (engaged, after_release): the delays of
  all repositories, with None for a commit that never became a generation."""
  ingested: dict[str, float] = {}
  active_sha: dict[str, str] = {}
  if os.path.exists(generations_path):
    with open(generations_path, encoding="utf-8") as handle:
      for line in handle:
        parts = line.rstrip("\n").split("\t")
        if len(parts) < 7:
          continue
        scope_id, _gen, sha, ingested_at, _delta, _status, active = parts[:7]
        if sha:
          ingested[sha] = float(ingested_at)
        if active == "t" and sha:
          active_sha[scope_id] = sha
  pushes: dict[str, list[dict[str, Any]]] = defaultdict(list)
  last_label_sha: dict[str, str] = {}
  for event in events:
    if len(event) >= 5 and event[1] == "fixture_push":
      push_ts, repo, label, sha = float(event[0]), event[2], event[3], event[4]
      delay = None if sha not in ingested else round(ingested[sha] - push_ts, 1)
      pushes[label].append({"repo": repo, "sha": sha, "delay_s": delay})
      last_label_sha[repo] = sha
  out: dict[str, Any] = {}
  for label, items in pushes.items():
    delays = [i["delay_s"] for i in items]
    known = [d for d in delays if d is not None]
    out[label] = {"commits": items, "min_s": min(known) if known else None, "max_s": max(known) if known else None,
                  "missing": sum(1 for d in delays if d is None)}
  out["_final"] = {"active_sha_is_last_push": bool(active_sha) and all(
      sha in set(last_label_sha.values()) for sha in active_sha.values()), "scopes": len(active_sha)}
  return out


def read_tsv(path: str) -> list[list[str]]:
  if not os.path.exists(path):
    return []
  with open(path, encoding="utf-8") as handle:
    return [line.rstrip("\n").split("\t") for line in handle if line.strip()]


def intents(path: str, sources: dict[str, str]) -> dict[str, Any]:
  """Reducer intents per source over the active generations and over all
  generations: the sets of "scope:domain" (arbiter 2.2)."""
  inverse = {v: k for k, v in sources.items()}
  active: dict[str, set[str]] = defaultdict(set)
  union: dict[str, set[str]] = defaultdict(set)
  for row in read_tsv(path):
    if len(row) < 6:
      continue
    scope_id, domain, _gen, is_active, _status, source = row[:6]
    side = inverse.get(source, "other")
    key = "%s:%s" % (scope_id, domain)
    union[side].add(key)
    if is_active == "t":
      active[side].add(key)
  return {"active": {k: sorted(v) for k, v in active.items()}, "union": {k: sorted(v) for k, v in union.items()}}


def projector_accounting(path: str, sources: dict[str, str]) -> dict[str, Any]:
  """Paced and superseded accounting by source, and the settled check."""
  inverse = {v: k for k, v in sources.items()}
  paced_items: dict[str, int] = defaultdict(int)
  paced_wait_sum: dict[str, int] = defaultdict(int)
  attempt_changes = 0
  unsettled = 0
  superseded: dict[str, str] = {}
  active_status: dict[str, str] = {}
  for row in read_tsv(path):
    if len(row) < 8:
      continue
    scope_id, _gen, _sha, is_active, status, attempts, paced, source = row[:8]
    side = inverse.get(source, "other")
    paced_n, attempts_n = int(paced), int(attempts)
    if paced_n > 0:
      paced_items[side] += 1
    paced_wait_sum[side] += paced_n
    if status == "succeeded" and attempts_n - paced_n != 1:
      attempt_changes += 1
    if status not in ("succeeded", "superseded"):
      unsettled += 1
    if status == "superseded":
      superseded[scope_id] = "superseded"
    if is_active == "t":
      active_status[scope_id] = status
  not_succeeded = sorted(s for s in superseded if active_status.get(s) != "succeeded")
  return {"paced_items": dict(paced_items), "paced_wait_sum": dict(paced_wait_sum),
          "attempt_count_changes": attempt_changes, "unsettled": unsettled,
          "superseded_scopes": len(superseded), "superseded_without_succeeded_active": not_succeeded}
