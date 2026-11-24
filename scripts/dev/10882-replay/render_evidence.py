# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""Renders the replay evidence record (markdown) from a results directory.

Every number comes from a run's result.json and the checker output; nothing is typed
in by hand. The fixed statements the rulings require (what the replay does not
exercise) are part of the template. Run report.sh first and pass its output file so
the record carries the checker's own PASS/FAIL lines.

Usage: render_evidence.py RESULTS_DIR [--report REPORT_TXT] > record.md
"""

import argparse
import glob
import json
import os
import statistics
import sys
from typing import Any

ARM_ORDER = ("C", "CF", "C0", "T1", "T1TTL", "C2", "T2", "T2TTL", "T3", "T3TTL", "CG", "IFC", "IF", "IWC", "IW")


def load(results: str) -> dict[str, dict[str, Any]]:
  out = {}
  for path in sorted(glob.glob(os.path.join(results, "*-rep*", "result.json"))):
    with open(path, encoding="utf-8") as handle:
      out[os.path.basename(os.path.dirname(path))] = json.load(handle)
  return out


def order_key(name: str) -> tuple[int, str]:
  arm = name.rsplit("-rep", 1)[0]
  return (ARM_ORDER.index(arm) if arm in ARM_ORDER else len(ARM_ORDER), name)


def row(name: str, r: dict[str, Any]) -> str:
  consult = r.get("consult", {}).get("A", {})
  rep = r.get("report", {})
  items = r.get("a_items", {})
  b = r.get("b", {})
  return "| %s | %s | %.1f | %.1f | %.2f | %s | %s | %s | %s | %s | %.3f / %.3f / %.2f |" % (
      name, r.get("ttl"), 1000 * consult.get("p50_s", 0), 1000 * consult.get("p95_s", 0), consult.get("within_100ms", 0),
      rep.get("k_peak"), rep.get("parked_rows_peak"), r.get("paced", {}).get("superseded_scopes"),
      items.get("max_after_release_s"), rep.get("load1_max"), b.get("queue_wait_p50_s", 0), b.get("queue_wait_p95_s", 0),
      b.get("claims_per_s", 0))


def main(argv: list[str]) -> int:
  parser = argparse.ArgumentParser(description=__doc__)
  parser.add_argument("results")
  parser.add_argument("--report", help="saved report.sh output to embed")
  args = parser.parse_args(argv)
  runs = load(args.results)
  if not runs:
    print("render_evidence: no results under %s" % args.results, file=sys.stderr)
    return 2
  build = {}
  path = os.path.join(args.results, "build.json")
  if os.path.exists(path):
    with open(path, encoding="utf-8") as handle:
      build = json.load(handle)
  loads: list[float] = []
  load_path = os.path.join(args.results, "loadavg.tsv")
  if os.path.exists(load_path):
    loads = [float(line.split("\t")[1]) for line in open(load_path, encoding="utf-8") if "\t" in line]
  print("# Source backpressure replay: evidence record (#10882, #11186, #11187)\n")
  print("Commit `%s`. Binaries built once from it; sha256 of each is in `build.json` of the results directory.\n" % build.get("commit"))
  print("## Host load during the run\n")
  print("One sample a minute: %s.\n" % ("min %.1f, median %.1f, max %.1f over %d samples" % (
      min(loads), statistics.median(loads), max(loads), len(loads)) if loads else "not recorded"))
  print("## Per run\n")
  print("| Run | TTL | consult p50 ms | consult p95 ms | share in le=0.1 | K peak | parked peak | superseded scopes | last A item after release s | load1 max | B wait p50 / p95 s / claims per s |")
  print("| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |")
  for name in sorted(runs, key=order_key):
    print(row(name, runs[name]))
  print("\nConsult figures are read from the engaged scrape of the product binaries' /metrics; B figures from the queue's own timestamps.\n")
  if args.report and os.path.exists(args.report):
    print("## Checker output\n\n<details>\n<summary>report.sh output</summary>\n\n```text")
    print(open(args.report, encoding="utf-8").read().rstrip())
    print("```\n\n</details>\n")
  print("## What the replay does not exercise\n")
  for line in (
      "No reducer ran. Release was driven by completing seeded rows through the production queue call.",
      "B's freshness is measured to the reducer-queue enqueue, not through reducer handlers.",
      "K and the parked counts come from the seed.",
      "T2 commits no hosted facts; pacing acts before the claim, so no partial commit exists to lose.",
      "The runs used `explicit` mode; the claims are scoped to explicit, githubOrg and gitlabGroup. The schedule leg is "
      "undefined for filesystem mode and startup refuses the pair (`TestWireSelectionBackpressureRefusesFilesystemMode`, "
      "`TestSelectRepositoriesFilesystemModeNeverDefersAnEditedRepository`). The first venue ran filesystem mode and was "
      "abandoned for that defect.",
      "The global admission marks were raised on the ingester in every arm except CG. The admission-writer leg and AC5 hold "
      "only while total reducer outstanding is below the ingester's high-water mark; the pre-write, claim-grant and "
      "selection legs do not depend on it.",
      "Seeded generations produce no reducer intents and no content rows, so the intent and content checks rest on the six "
      "real repositories.",
      "`fact_id` is compared through `stable_fact_key`; `payload.indexed_at` and `payload.source_run_id` are dropped.",
      "After release a paced projector item resumes only when its scheduled backoff expires, limited to min(retry cap, "
      "ReleaseAge) plus jitter per item. A source whose first generation is paced has no projected baseline, so each new "
      "commit is collected as a full generation and earlier ones are superseded unprojected.",
      "A matrix start at 2026-11-23T01:19Z was aborted before its first arm finished and discarded "
      "(`results-f7f5c72361-ABORTED-not-evidence`); no result from it was judged.",
      "Smoke figures are not evidence."):
    print("- %s" % line)
  return 0


if __name__ == "__main__":
  sys.exit(main(sys.argv[1:]))
