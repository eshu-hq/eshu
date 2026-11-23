#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""Proof for b_item_table.py: the per-item B queue wait table.

Synthetic run directories with known waits: the table must report the exact
item count, maximum, count above one second and per-run percentiles, pool only
the registered arms (CF and CG stay out), and pick the burst items by fixture
scope inside the push window.
"""
import json
import os
import tempfile
import unittest

import b_item_table as table

HOSTED = table.HOSTED_SOURCE


def make_run(root, name, arm, thresholds, source_b, waits, scope="replay-b-%d", push=None):
  run = os.path.join(root, name)
  os.makedirs(os.path.join(run, "export"))
  with open(os.path.join(run, "manifest.json"), "w", encoding="utf-8") as handle:
    json.dump({"arm": arm, "thresholds": thresholds, "source_b": source_b}, handle)
  rows = []
  for index, wait in enumerate(waits):
    created = 1000.0 + index
    rows.append("\t".join([scope % index if "%d" in scope else scope, "g", "sha", "t", "succeeded", "1", "0", source_b,
                           "0", "", "%.3f" % created, "%.3f" % (created + wait), "%.3f" % (created + wait)]))
  with open(os.path.join(run, "export", "projector_items.tsv"), "w", encoding="utf-8") as handle:
    handle.write("\n".join(rows) + "\n")
  if push is not None:
    with open(os.path.join(run, "events.tsv"), "w", encoding="utf-8") as handle:
      handle.write("%s\tfixture_push\treplay/fixture-01\tengaged\tabc\n" % push)
  return run


class BItemTableTest(unittest.TestCase):
  def test_percentile_is_linear_interpolated(self):
    self.assertAlmostEqual(table.percentile([1.0, 2.0, 3.0, 4.0], 0.5), 2.5)
    self.assertAlmostEqual(table.percentile([1.0, 2.0, 3.0, 4.0], 0.95), 3.85)
    self.assertEqual(table.percentile([], 0.5), 0.0)

  def test_per_arm_and_pooled_counts(self):
    with tempfile.TemporaryDirectory() as root:
      make_run(root, "C-rep1", "C", "", HOSTED, [0.5, 1.5, 0.25, 0.25])
      make_run(root, "T1-rep1", "T1", "ENGAGE=1h", HOSTED, [0.25, 0.25, 0.5, 2.0])
      make_run(root, "CF-rep1", "CF", "", HOSTED, [9.0, 9.0])  # a reference: must not enter the pool
      runs = table.load_runs(root)
      lines = table.hosted_b_pooled(runs)
      self.assertIn("treatment (T1, T1TTL, T3, T3TTL): runs=1 items=4 max=2.00 s above 1 s=1", lines[0])
      self.assertIn("control (C, C0): runs=1 items=4 max=1.50 s above 1 s=1", lines[1])

  def test_burst_takes_fixture_scopes_inside_the_push_window(self):
    with tempfile.TemporaryDirectory() as root:
      # Items are created at 1000, 1001, ...; the push is at 1001.0: only the fixture
      # scopes created in [push - 1, push + 3] count, never the feeder items.
      make_run(root, "T2-rep1", "T2", "ENGAGE=1h", "git", [0.5, 2.0, 1.0], scope="git-repository-scope:r%d", push=1001.0)
      make_run(root, "C2-rep1", "C2", "", "git", [0.1, 0.2], scope="replay-b-x", push=1001.0)
      lines = table.burst_table(table.load_runs(root))
      self.assertTrue(any(line.startswith("T2-rep1") and "0.50 1.00 2.00" in line for line in lines), lines)
      self.assertTrue(any(line.startswith("C2-rep1") and " 0 " in line for line in lines), lines)


if __name__ == "__main__":
  unittest.main()
