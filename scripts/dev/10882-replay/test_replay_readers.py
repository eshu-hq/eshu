# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""Seeded RED/GREEN proof for the replay readers (replay_state, replay_facts,
replay_prom): the pieces that turn raw run files into the numbers the checker
judges. Each test plants input where a naive reading would pass and asserts the
reader does not.

Run: python3 -I scripts/dev/10882-replay/test_replay_readers.py
"""

import json
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import replay_facts  # noqa: E402  pylint: disable=wrong-import-position
import replay_prom  # noqa: E402  pylint: disable=wrong-import-position
import replay_state  # noqa: E402  pylint: disable=wrong-import-position


def _file(text: str) -> str:
  handle = tempfile.NamedTemporaryFile("w", delete=False, encoding="utf-8")
  with handle:
    handle.write(text)
  return handle.name


def _sample(ts, engaged, engaged_at="NA", updated_at="NA", graph="0"):
  return "\t".join([str(ts), "t" if engaged else "f", engaged_at, updated_at, "0", "0", "0", "0", "0", "6", graph, "20.0"]) + "\n"


SAMPLE_HEADER = ("ts\tlatch_engaged\tlatch_engaged_at\tlatch_updated_at\tcontent_files_a\tcontent_entities_a\t"
                 "projector_succeeded_a\tk_visible_retrying_a\twait_rows\treal_generations\tgraph_nodes_a\tload1\n")


class SelectionTest(unittest.TestCase):

  def _generations(self, rows):
    return _file("".join("\t".join(r) + "\n" for r in rows))

  def test_each_commit_is_matched_by_its_own_sha(self):
    events = [["100.0", "fixture_push", "replay/fixture-01", "engaged", "shaE"],
              ["160.0", "fixture_push", "replay/fixture-01", "after_release", "shaR"]]
    path = self._generations([["s1", "g1", "shaE", "190.0", "t", "superseded", "f"],
                              ["s1", "g2", "shaR", "175.0", "t", "active", "t"]])
    out = replay_state.selection(events, path)
    self.assertEqual(out["engaged"]["min_s"], 90.0)
    self.assertEqual(out["after_release"]["max_s"], 15.0)
    self.assertEqual(out["_final"], {"active_sha_is_last_push": True, "scopes": 1})

  def test_red_a_commit_that_never_became_a_generation_is_missing_not_satisfied(self):
    events = [["100.0", "fixture_push", "replay/fixture-01", "engaged", "shaE"],
              ["160.0", "fixture_push", "replay/fixture-01", "after_release", "shaR"]]
    path = self._generations([["s1", "g1", "shaE", "190.0", "t", "active", "t"]])  # only the late engaged commit exists
    out = replay_state.selection(events, path)
    self.assertEqual(out["after_release"]["missing"], 1)
    self.assertFalse(out["_final"]["active_sha_is_last_push"])


class EngagementTest(unittest.TestCase):

  def test_times_come_from_the_latch_row_not_the_sample_clock(self):
    path = _file(SAMPLE_HEADER + _sample(1003.2, True, "1000.5", "1000.5") + _sample(1006.5, True, "1000.5", "1000.5")
                 + _sample(1009.9, False, "NA", "1008.7"))
    engaged = replay_state.engagement(replay_state.read_samples(path))
    self.assertEqual(engaged["engage_at"], 1000.5)
    self.assertEqual(engaged["release_at"], 1008.7)
    self.assertFalse(engaged["reengaged"])

  def test_red_a_second_engaged_interval_is_reengagement(self):
    path = _file(SAMPLE_HEADER + _sample(1, True, "1", "1") + _sample(2, False, "NA", "2") + _sample(3, True, "3", "3")
                 + _sample(4, False, "NA", "4"))
    self.assertTrue(replay_state.engagement(replay_state.read_samples(path))["reengaged"])

  def test_red_a_missing_graph_reading_stays_none_never_zero(self):
    samples = replay_state.read_samples(_file(SAMPLE_HEADER + _sample(1, False, graph="NA")))
    self.assertIsNone(samples[0]["graph"])


class AccountingTest(unittest.TestCase):

  def test_paced_items_attempts_and_superseded_by_source(self):
    rows = [["s1", "g1", "shaA", "f", "superseded", "2", "2", "git", "0", ""],
            ["s1", "g2", "shaB", "t", "succeeded", "1", "0", "git", "0", ""],
            ["s2", "g3", "shaC", "t", "succeeded", "3", "2", "git", "0", ""],
            ["s3", "g4", "", "t", "succeeded", "1", "0", "hosted", "0", ""]]
    out = replay_state.projector_accounting(_file("".join("\t".join(r) + "\n" for r in rows)), {"A": "git", "B": "hosted"})
    self.assertEqual(out["paced_items"], {"A": 2})
    self.assertEqual(out["paced_wait_sum"], {"A": 4, "B": 0})
    self.assertEqual(out["attempt_count_changes"], 0)
    self.assertEqual(out["superseded_scopes"], 1)
    self.assertEqual(out["superseded_without_succeeded_active"], [])

  def test_red_attempt_budget_spent_unsettled_and_superseded_without_a_succeeded_item(self):
    rows = [["s1", "g1", "a", "f", "superseded", "1", "0", "git", "0", ""],
            ["s1", "g2", "b", "t", "retrying", "1", "0", "git", "0", ""],
            ["s2", "g3", "c", "t", "succeeded", "3", "0", "git", "0", ""]]
    out = replay_state.projector_accounting(_file("".join("\t".join(r) + "\n" for r in rows)), {"A": "git", "B": "hosted"})
    self.assertEqual(out["attempt_count_changes"], 1)
    self.assertEqual(out["unsettled"], 1)
    self.assertEqual(out["superseded_without_succeeded_active"], ["s1"])

  def test_intents_are_split_into_active_and_union(self):
    rows = [["s1", "d1", "g1", "f", "pending", "git"], ["s1", "d2", "g2", "t", "pending", "git"]]
    out = replay_state.intents(_file("".join("\t".join(r) + "\n" for r in rows)), {"A": "git", "B": "hosted"})
    self.assertEqual(out["active"]["A"], ["s1:d2"])
    self.assertEqual(out["union"]["A"], ["s1:d1", "s1:d2"])


class FactsTest(unittest.TestCase):

  def _dump(self, run, **override):
    payload = {"language": "go", "indexed_at": "t-" + run, "source_run_id": "run-" + run}
    payload.update(override)
    rows = [("scope-1", "id-%s-1" % run, "file:a.go", payload), ("scope-1", "id-%s-2" % run, "repo:r", {"name": "r"})]
    return _file("".join("%s\t%s\t%s\t%s\n" % (s, f, k, json.dumps(p)) for s, f, k, p in rows))

  def test_run_variant_fields_do_not_change_the_hash_and_are_the_only_differing_keys(self):
    a, b = self._dump("a"), self._dump("b")
    self.assertEqual(replay_facts.real_fact_sets(a), replay_facts.real_fact_sets(b))
    self.assertEqual(replay_facts.differing_top_level_keys(a, b), ["indexed_at", "source_run_id"])
    self.assertEqual(replay_facts.fact_counts(a)["scope-1"], {"facts": 2, "distinct_stable_keys": 2})

  def test_red_a_real_payload_change_changes_the_hash_and_shows_as_a_differing_key(self):
    a, b = self._dump("a"), self._dump("b", language="rust")
    self.assertNotEqual(replay_facts.real_fact_sets(a), replay_facts.real_fact_sets(b))
    self.assertIn("language", replay_facts.differing_top_level_keys(a, b))


class PromTest(unittest.TestCase):

  def test_consult_share_within_100ms_comes_from_the_bucket_and_engaged_counter_sums(self):
    text = "\n".join([
        'eshu_dp_reducer_admission_source_consult_duration_seconds_bucket{outcome="ok",source_system="git",le="0.05"} 10',
        'eshu_dp_reducer_admission_source_consult_duration_seconds_bucket{outcome="ok",source_system="git",le="0.1"} 40',
        'eshu_dp_reducer_admission_source_consult_duration_seconds_bucket{outcome="ok",source_system="git",le="+Inf"} 100',
        'eshu_dp_reducer_admission_source_consult_duration_seconds_count{outcome="ok",source_system="git"} 100',
        'eshu_dp_workflow_claim_pacing_engaged_total{pacing_signal="reducer_source",domain="git"} 1',
        'eshu_dp_workflow_claim_pacing_engaged_total{pacing_signal="reducer_source",domain="git"} 1',
        'eshu_dp_workflow_claim_pacing_engaged_total{pacing_signal="reducer_source",domain="other"} 5'])
    path = _file(text)
    rows = replay_prom.parse_prom(path)
    stats = replay_prom.consult_stats(rows, {"A": "git", "B": "hosted"})["A"]
    self.assertAlmostEqual(stats["within_100ms"], 0.4)  # 40 of 100 inside the edge: the p50 is NOT within 100 ms
    self.assertEqual(replay_prom.engaged_counter(rows, "git"), 2.0)


if __name__ == "__main__":
  unittest.main()
