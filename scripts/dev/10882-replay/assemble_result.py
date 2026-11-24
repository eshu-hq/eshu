# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""Turns one replay run directory into result.json for check_arm.py.

Inputs (written by the driver libs under the run directory): manifest.json,
samples.tsv, events.tsv, metrics/*.prom (engaged and final scrapes of the real
binaries), export/*, census-*.txt, and ../build.json. Instrument readings
(consult duration, deferrals, the engaged counter) come only from the binaries'
/metrics scrapes; database and graph state come from the sampler and the exports.
Nothing here times anything itself.

Usage: assemble_result.py RUN_DIR
"""

import glob
import json
import os
import sys
from datetime import datetime, timezone
from typing import Any

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import replay_facts  # noqa: E402  pylint: disable=wrong-import-position
import replay_prom  # noqa: E402  pylint: disable=wrong-import-position
import replay_state  # noqa: E402  pylint: disable=wrong-import-position


def iso(ts: float | None) -> str | None:
  return None if ts is None else datetime.fromtimestamp(ts, timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def read_json(path: str, default: Any) -> Any:
  if not os.path.exists(path):
    return default
  with open(path, encoding="utf-8") as handle:
    text = handle.read().strip()
  return json.loads(text) if text else default


def census_classes(path: str) -> dict[str, int]:
  """Parses a census.sh table into {class: rows}."""
  classes: dict[str, int] = {}
  if not os.path.exists(path):
    return classes
  with open(path, encoding="utf-8") as handle:
    for line in handle:
      parts = [p.strip() for p in line.split("|")]
      if len(parts) >= 5 and parts[0] and parts[0] != "class" and not set(parts[0]) <= {"-", "+"}:
        try:
          classes[parts[0]] = int(parts[1])
        except ValueError:
          continue
  return classes


def graph_counts(path: str) -> dict[str, int]:
  counts: dict[str, int] = {}
  if not os.path.exists(path):
    return counts
  with open(path, encoding="utf-8") as handle:
    for line in handle:
      parts = [p.strip() for p in (line.split(",") if "," in line else line.split())]
      if len(parts) == 2 and parts[1].isdigit():
        counts[parts[0]] = int(parts[1])
  return counts


def latch_rows(path: str) -> list[dict[str, Any]]:
  rows = []
  for row in replay_state.read_tsv(path):
    if len(row) >= 5:
      rows.append({"signal": row[0], "domain": row[1], "engaged": row[2] == "t", "engaged_at": row[3], "updated_at": row[4]})
  return rows


def content_identity(path: str) -> dict[str, Any]:
  """Count and digest of the content identity sets: (repo, path, content hash) of
  every content file and the entity id of every content entity."""
  import hashlib
  files, entities = [], []
  for row in replay_state.read_tsv(path):
    if row[0] == "f":
      files.append("\t".join(row[1:4]))
    elif row[0] == "e":
      entities.append(row[1])
  digest = lambda rows: hashlib.sha256("\n".join(sorted(rows)).encode()).hexdigest()  # noqa: E731
  return {"files": len(files), "files_digest": digest(files), "entities": len(entities), "entities_digest": digest(entities)}


def updated_stable(samples: list[dict[str, Any]], rows: list[dict[str, Any]], source_a: str) -> bool | None:
  """The latch row's updated_at at the second released sample equals the final dump:
  nothing rewrote the row after the release (a stale consult would)."""
  released = [s for s in samples if not s["engaged"] and s["latch_updated_at"] is not None]
  final = next((float(r["updated_at"]) for r in rows if r["domain"] == source_a), None)
  if len(released) < 2 or final is None:
    return None
  return abs(released[1]["latch_updated_at"] - final) < 0.002


def final_state(export: str, sources: dict[str, str]) -> dict[str, Any]:
  """The committed end state of one run, from its exports alone (so it can be rebuilt
  from saved raw data, and tested on real exported rows)."""
  facts_path = os.path.join(export, "real_facts.tsv")
  chain_path = os.path.join(export, "real_facts_chain.tsv")
  intents = replay_state.intents(os.path.join(export, "reducer_rows.tsv"), sources)
  identity = content_identity(os.path.join(export, "content_ids.tsv"))
  return {
      "generations": read_json(os.path.join(export, "generations.json"), {}),
      "real_fact_sets": replay_facts.real_fact_sets(facts_path),
      "real_fact_counts": replay_facts.fact_counts(facts_path),
      "generation_facts": replay_facts.generation_facts(chain_path),
      "repository_payloads": replay_facts.repository_payloads(chain_path),
      "shapes": replay_state.shapes(os.path.join(export, "generations_all.tsv")),
      "relationship_counts": graph_counts(os.path.join(export, "relationship_counts.tsv")),
      "graph_counts": graph_counts(os.path.join(export, "graph_counts.tsv")),
      "content_identity": identity, "content_rows": identity["files"] + identity["entities"],
      "normalized_fields": {"fact_id": "compared via stable_fact_key", "payload": list(replay_facts.RUN_VARIANT_PAYLOAD_FIELDS)},
      "intent_active": intents["active"], "intent_union": intents["union"],
  }


def a_drain(export: str, events: list[list[str]]) -> dict[str, Any]:
  """A's last projector completion, relative to the end of the catch-up step."""
  drain = read_json(os.path.join(export, "a_drain.json"), {})
  done = float(drain.get("done_ts", 0)) or None
  caught = next((float(e[0]) for e in events if len(e) > 1 and e[1] == "catchup_done"), None)
  return {"items": drain.get("items", 0), "after_catchup_s": None if done is None or caught is None else round(done - caught, 1)}


def main(run_dir: str) -> int:
  manifest = read_json(os.path.join(run_dir, "manifest.json"), {})
  build = read_json(os.path.join(os.path.dirname(os.path.abspath(run_dir)), "build.json"), {})
  sources = {"A": manifest.get("source_a", "A"), "B": manifest.get("source_b", "B")}
  engaged_rows = replay_prom.load_metrics(run_dir, "engaged")
  final_rows = replay_prom.load_metrics(run_dir, "final")
  samples = replay_state.read_samples(os.path.join(run_dir, "samples.tsv"))
  events = replay_state.read_events(os.path.join(run_dir, "events.tsv"))
  export = os.path.join(run_dir, "export")
  counters = read_json(os.path.join(export, "counters.json"), {})
  started = next((float(e[0]) for e in events if len(e) > 1 and e[1] == "processes_started"), None)
  window = float(manifest.get("engaged_window_s") or 0)

  engaged = engagement(samples)
  engaged["engaged_counter"] = replay_prom.engaged_counter(final_rows, sources["A"])
  rows = latch_rows(os.path.join(export, "latch.tsv"))
  engaged["latch_row_for_a"] = any(r["domain"] == sources["A"] for r in rows)
  engaged["gauge_final"] = [replay_prom.active_gauge(replay_prom.parse_prom(p), sources["A"])
                            for p in sorted(glob.glob(os.path.join(run_dir, "metrics", "*-final.prom")))]
  engaged["updated_at_stable"] = updated_stable(samples, rows, sources["A"])
  writes = replay_state.write_points(samples, engaged, None if started is None else started + window)
  by_side, released = replay_prom.deferrals(final_rows, sources)
  intents = replay_state.intents(os.path.join(export, "reducer_rows.tsv"), sources)
  projector = replay_state.projector_accounting(os.path.join(export, "projector_items.tsv"), sources)
  facts_path = os.path.join(export, "real_facts.tsv")
  census_pre = census_classes(os.path.join(run_dir, "census-pre.txt"))
  census_end = census_classes(os.path.join(run_dir, "census-end.txt"))
  sel = replay_state.selection(events, os.path.join(export, "generations_all.tsv"))
  final_sel = sel.pop("_final", {})
  gcounts = graph_counts(os.path.join(export, "graph_counts.tsv"))
  loads = [s["load1"] for s in samples if s["load1"] is not None]
  release_epoch = engaged.get("release_at")
  result = {
      "arm": manifest.get("arm"), "rep": manifest.get("rep"), "ttl": manifest.get("ttl"),
      "manifest": manifest, "build": build, "engaged": {k: (iso(v) if k in ("engage_at", "release_at") else v)
                                                          for k, v in engaged.items()},
      "writes": writes,
      "graph_reading": {"missing_samples": sum(1 for s in samples if s["graph"] is None), "samples": len(samples),
                        "final_counts_present": bool(gcounts)},
      "released_pre_write": released, "paced": projector,
      "deferrals": {"source_behind": by_side, "attempt_count_changes": projector["attempt_count_changes"],
                    "dead_letters": counters.get("dead_letters", 0),
                    "admission_timeouts": counters.get("admission_timeouts", 0)},
      "b": read_json(os.path.join(export, "b_stats.json"), {}),
      "consult": replay_prom.consult_stats(engaged_rows, sources),
      "consult_final": replay_prom.consult_stats(final_rows, sources),
      "consult_claim_grant": replay_prom.claim_grant_consult(final_rows),
      "report": {"k_peak": max((s["k"] for s in samples), default=0),
                 "parked_rows_peak": max((s["waits"] for s in samples), default=0),
                 "woken_holding_wait": census_pre.get("woken_holding_wait", 0),
                 "wait_table_rows_final": samples[-1]["waits"] if samples else 0,
                 "load1_max": max(loads, default=None), "load1_end": next(
                     (float(e[2]) for e in reversed(events) if len(e) > 2 and e[1] == "run_done" and e[2]), None)},
      "census": {"classes": census_pre, "classes_end": census_end, "total": sum(census_pre.values()),
                 "claimable_a_at_release": census_end.get("takeable", 0)},
      "selection": sel,
      "hosted": {"planted": read_json(os.path.join(export, "hosted-final.json"), {}).get("planted", 0),
                 "at_engagement_end": read_json(os.path.join(export, "hosted-at_engagement_end.json"), {}),
                 "final": read_json(os.path.join(export, "hosted-final.json"), {})},
      "timeouts": [{"what": e[2], "limit_s": e[3]} for e in events if len(e) > 3 and e[1] == "wait_timeout"],
      "latch_rows": rows,
      "a_drain": a_drain(export, events),
      "a_items": replay_state.item_timing(os.path.join(export, "projector_items.tsv"), sources, release_epoch),
      "final": dict(final_state(export, sources), projector_unsettled=projector["unsettled"],
                    projector_superseded=counters.get("projector_superseded", 0),
                    active_sha_is_last_push=final_sel.get("active_sha_is_last_push"),
                    fixture_scopes=final_sel.get("scopes", 0)),
  }
  with open(os.path.join(run_dir, "result.json"), "w", encoding="utf-8") as handle:
    json.dump(result, handle, indent=2, sort_keys=True)
  return 0


def engagement(samples: list[dict[str, Any]]) -> dict[str, Any]:
  return replay_state.engagement(samples)


if __name__ == "__main__":
  if len(sys.argv) != 2:
    print(__doc__, file=sys.stderr)
    sys.exit(2)
  sys.exit(main(sys.argv[1]))
