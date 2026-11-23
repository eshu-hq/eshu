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


def active_is_delta(path: str) -> dict[str, bool]:
  """Per git scope: whether its active generation is a delta generation."""
  out: dict[str, bool] = {}
  for row in replay_state.read_tsv(path):
    if len(row) >= 7 and row[6] == "t":
      out[row[0]] = row[4] == "t"
  return out


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
      "final": {
          "generations": read_json(os.path.join(export, "generations.json"), {}),
          "real_fact_sets": replay_facts.real_fact_sets(facts_path),
          "effective_fact_sets": replay_facts.effective_fact_sets(os.path.join(export, "real_facts_chain.tsv")),
          "active_is_delta": active_is_delta(os.path.join(export, "generations_all.tsv")),
          "real_fact_counts": replay_facts.fact_counts(facts_path),
          "normalized_fields": {"fact_id": "compared via stable_fact_key",
                                "payload": list(replay_facts.RUN_VARIANT_PAYLOAD_FIELDS)},
          "graph_counts": gcounts, "content_rows": counters.get("content_rows"),
          "projector_unsettled": projector["unsettled"], "projector_superseded": counters.get("projector_superseded", 0),
          "intent_active": intents["active"], "intent_union": intents["union"],
          "active_sha_is_last_push": final_sel.get("active_sha_is_last_push"), "fixture_scopes": final_sel.get("scopes", 0),
      },
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
