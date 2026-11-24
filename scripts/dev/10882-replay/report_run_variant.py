#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""The run-variant proof of report.sh: two control runs of C differ only in the normalised payload fields. One PASS or FAIL line per later C repetition against C-rep1 (the differing top-level payload keys must be exactly replay_facts.RUN_VARIANT_PAYLOAD_FIELDS) and a FAIL line for a scope whose fact count differs from its distinct stable keys. Exits 1 on a FAIL.
A file rather than a heredoc: the heredoc budget gate.

Usage: report_run_variant.py <results-root>
"""
import glob, os, sys
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import replay_facts
runs = sorted(glob.glob(os.path.join(sys.argv[1], "C-rep*", "export", "real_facts.tsv")))
want = sorted(replay_facts.RUN_VARIANT_PAYLOAD_FIELDS)
bad = False
for other in runs[1:]:
  keys = replay_facts.differing_top_level_keys(runs[0], other)
  ok = keys == want
  bad |= not ok
  print("%s %s vs %s: differing top-level payload keys = %s (must be exactly %s)" % (
      "PASS" if ok else "FAIL", os.path.basename(os.path.dirname(os.path.dirname(runs[0]))),
      os.path.basename(os.path.dirname(os.path.dirname(other))), keys, want))
  for run in (runs[0], other):
    for scope, counts in replay_facts.fact_counts(run).items():
      if counts["facts"] != counts["distinct_stable_keys"]:
        bad = True
        print("FAIL fact count != distinct stable keys in %s %s: %s" % (run, scope, counts))
sys.exit(1 if bad else 0)
