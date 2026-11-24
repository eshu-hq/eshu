#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""Writes the manifest.json of one replay run.

lib/arm.sh (replay_write_manifest) passes every field as a REPLAY_MF_<NAME> environment
variable, so the manifest is built from plain values instead of an unquoted heredoc that
the shell expands (the heredoc budget gate). The field names, order of keys (sorted) and
JSON types are those of the heredoc this file replaced: integers for the counts, a bool
for the completion step, null for an unset selection base, strings for the rest, and the
sha256 of every eshu-* binary in REPLAY_MF_BIN.

Usage: write_manifest.py <out.json>   (REPLAY_MF_* set by the caller)
"""
import hashlib
import json
import os
import sys

STRINGS = ("ARM", "TTL", "THRESHOLDS", "GLOBAL_ADMISSION", "GLOBAL_MARKS", "SOURCE_A", "SOURCE_B", "A_SHAPE", "B_CADENCE",
           "LOAD1_AT_START", "POSTGRES", "NEO4J", "COMMIT", "STARTED")
INTS = ("REP", "A_REDUCER_ROWS", "A_PROJECTOR_GENERATIONS", "B_GENERATIONS", "ENGAGED_WINDOW_S", "WORKERS_PROJECTOR",
        "WORKERS_INGESTER_PROJECTOR")


def env(name: str) -> str:
  return os.environ["REPLAY_MF_" + name]


def main(out: str) -> None:
  bindir = env("BIN")
  binaries = {}
  for name in sorted(os.listdir(bindir)):
    if name.startswith("eshu-"):
      with open(os.path.join(bindir, name), "rb") as handle:
        binaries[name] = hashlib.sha256(handle.read()).hexdigest()
  strings = {name: env(name) for name in STRINGS}
  ints = {name: int(env(name)) for name in INTS}
  selection_base = env("SELECTION_BASE_S")
  manifest = {
      "binaries": binaries, "arm": strings["ARM"], "rep": ints["REP"], "ttl": strings["TTL"],
      "completion_step": bool(int(env("COMPLETE"))), "thresholds": strings["THRESHOLDS"],
      "global_admission": strings["GLOBAL_ADMISSION"], "global_marks": strings["GLOBAL_MARKS"],
      "source_a": strings["SOURCE_A"], "source_b": strings["SOURCE_B"], "a_shape": strings["A_SHAPE"],
      "a_reducer_rows": ints["A_REDUCER_ROWS"], "a_projector_generations": ints["A_PROJECTOR_GENERATIONS"],
      "b_generations": ints["B_GENERATIONS"], "b_cadence": strings["B_CADENCE"],
      "workers": {"projector": ints["WORKERS_PROJECTOR"], "ingester_projector": ints["WORKERS_INGESTER_PROJECTOR"]},
      "engaged_window_s": ints["ENGAGED_WINDOW_S"], "selection_base_s": None if selection_base == "None" else int(selection_base),
      "load1_at_start": strings["LOAD1_AT_START"], "postgres": strings["POSTGRES"], "neo4j": strings["NEO4J"],
      "commit": strings["COMMIT"], "started": strings["STARTED"],
  }
  with open(out, "w", encoding="utf-8") as handle:
    json.dump(manifest, handle, indent=2, sort_keys=True)


if __name__ == "__main__":
  main(sys.argv[1])
