# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""Real-fixture fact sets with a documented run-variant normalisation.

Two runs over identical content differ in exactly these fields, each minted per
run in the git collector:

  fact_id                facts.StableID over the run's source_run_id and the
                         generation id derived from it (the fact builders hash
                         the generation they belong to into the id)
  payload.source_run_id  facts.StableID("GitCollectorSnapshotRun", {component,
                         observed_at: the batch's wall clock, selected paths}),
                         collector/repo/git/source_processing.go:151
  payload.indexed_at     stamped at parse time; collector/repo/git/
                         phase0_pinned_ref_shim_test.go documents it as the one
                         wall-clock field of a git fact

fact_id is compared through stable_fact_key, the run-invariant identity of the
same fact. Everything else in the payload is hashed. The raw dump
(export/real_facts.tsv) is kept for every run so this can be audited, and
differing_top_level_keys proves, from two runs, that the normalised fields are the
only differing ones.
"""

import hashlib
import json
import os
from collections import defaultdict
from typing import Any

RUN_VARIANT_PAYLOAD_FIELDS = ("indexed_at", "source_run_id")


def _read(path: str) -> list[tuple[str, str, str, Any]]:
  rows = []
  if not os.path.exists(path):
    return rows
  with open(path, encoding="utf-8") as handle:
    for line in handle:
      parts = line.rstrip("\n").split("\t", 3)
      if len(parts) < 4:
        continue
      rows.append((parts[0], parts[1], parts[2], json.loads(parts[3])))
  return rows


def real_fact_sets(path: str) -> dict[str, str]:
  """Per-scope hash over (stable_fact_key, payload hash minus run-variant fields)."""
  by_scope: dict[str, list[str]] = defaultdict(list)
  for scope_id, _fact_id, stable_key, payload in _read(path):
    if isinstance(payload, dict):
      payload = {k: v for k, v in payload.items() if k not in RUN_VARIANT_PAYLOAD_FIELDS}
    digest = hashlib.sha256(json.dumps(payload, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
    by_scope[scope_id].append("%s:%s" % (stable_key, digest))
  return {scope_id: hashlib.sha256(",".join(sorted(items)).encode()).hexdigest()
          for scope_id, items in sorted(by_scope.items())}


def effective_fact_sets(path: str) -> dict[str, str]:
  """Per scope: the effective fact set across the generation chain.

  The latest non-tombstone version of each stable_fact_key, taken from the newest
  generation (by ingest time) that carries the key, hashed with the same
  normalisation as real_fact_sets. A control whose later generations are deltas and
  a treatment arm whose later generations are full re-observations (a paced
  generation was superseded unprojected, so there was no projected baseline) end
  with the same effective set when nothing was lost.
  """
  latest: dict[tuple[str, str], tuple[float, bool, Any]] = {}
  if os.path.exists(path):
    with open(path, encoding="utf-8") as handle:
      for line in handle:
        parts = line.rstrip("\n").split("\t", 4)
        if len(parts) < 5:
          continue
        scope_id, ingested, tombstone, key, payload_text = parts
        entry = (float(ingested), tombstone == "t", json.loads(payload_text))
        current = latest.get((scope_id, key))
        if current is None or entry[0] >= current[0]:
          latest[(scope_id, key)] = entry
  by_scope: dict[str, list[str]] = defaultdict(list)
  for (scope_id, key), (_ts, tombstone, payload) in latest.items():
    if tombstone:
      continue
    if isinstance(payload, dict):
      payload = {k: v for k, v in payload.items() if k not in RUN_VARIANT_PAYLOAD_FIELDS}
    digest = hashlib.sha256(json.dumps(payload, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
    by_scope[scope_id].append("%s:%s" % (key, digest))
  return {scope_id: hashlib.sha256(",".join(sorted(items)).encode()).hexdigest()
          for scope_id, items in sorted(by_scope.items())}


def fact_counts(path: str) -> dict[str, dict[str, int]]:
  """Per scope: fact count and distinct stable-key count (they must be equal)."""
  facts: dict[str, int] = defaultdict(int)
  keys: dict[str, set[str]] = defaultdict(set)
  for scope_id, _fact_id, stable_key, _payload in _read(path):
    facts[scope_id] += 1
    keys[scope_id].add(stable_key)
  return {s: {"facts": facts[s], "distinct_stable_keys": len(keys[s])} for s in sorted(facts)}


def differing_top_level_keys(path_a: str, path_b: str) -> list[str]:
  """Top-level payload keys whose value differs between two runs, matched by
  (scope, stable_fact_key). Over identical content this must be exactly the
  normalised payload fields."""
  def index(path: str) -> dict[tuple[str, str], Any]:
    return {(s, k): p for s, _f, k, p in _read(path)}
  left, right = index(path_a), index(path_b)
  differing: set[str] = set()
  for key in set(left) & set(right):
    a, b = left[key], right[key]
    if isinstance(a, dict) and isinstance(b, dict):
      for field in set(a) | set(b):
        if a.get(field) != b.get(field):
          differing.add(field)
  return sorted(differing)
