# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""Seeded RED/GREEN proof for check_arm.py (the replay pass/fail gate).

GREEN: a treatment arm that matches the control passes every check.
RED:   one planted A write during engagement, and one missing generation, must
       each fail the checker, naming the right check. Further seeded violations
       (B regression, dead letter, latch never released) pin the other arms of
       the gate so none passes by accident.

Run: python3 -I scripts/dev/10882-replay/test_check_arm.py
"""

import copy
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import check_arm  # noqa: E402  pylint: disable=wrong-import-position


def _control(wait_p50: float = 0.50) -> dict:
  return {
      "arm": "C",
      "b": {"queue_wait_p50_s": wait_p50, "queue_wait_p95_s": 1.20, "claims_per_s": 2.0},
      "deferrals": {"source_behind": {}, "attempt_count_changes": 0, "dead_letters": 0, "admission_timeouts": 0},
      "final": {
          "generations": {"scope-a-1": "g1", "scope-a-2": "g2", "scope-b-1": "g3"},
          "real_fact_sets": {"repo-1": "hash-1", "repo-2": "hash-2"},
          "graph_counts": {"File": 36, "Repository": 3},
          "reducer_intents": {"count": 4, "keys_hash": "k1"},
          "content_rows": 36,
          "projector_unsettled": 0,
      },
  }


def _treatment() -> dict:
  arm = copy.deepcopy(_control())
  arm["arm"] = "T1"
  arm["deferrals"]["source_behind"] = {"A": 12}
  arm["released_pre_write"] = 48
  arm["manifest"] = {"workers": {"projector": 4, "ingester_projector": 4}, "selection_base_s": 20}
  arm["selection"] = [{"event": "engaged", "delay_all_s": 55.0}, {"event": "after_release", "delay_all_s": 4.0}]
  arm["engaged"] = {"engage_at": "2026-11-22T21:00:00Z", "release_at": "2026-11-22T21:03:00Z"}
  arm["a_writes_during_engagement"] = {"graph_nodes": 0, "content_rows": 0, "projector_succeeded": 0}
  return arm


def _failed(checks: list) -> list:
  return [c.name for c in checks if not c.passed]


class CheckArmTest(unittest.TestCase):

  def test_green_matching_treatment_passes(self):
    controls = [_control(0.49), _control(0.50), _control(0.51)]
    checks = check_arm.run_checks("treatment", _treatment(), controls)
    self.assertEqual(_failed(checks), [])

  def test_red_planted_a_write_during_engagement_fails(self):
    arm = _treatment()
    arm["a_writes_during_engagement"]["graph_nodes"] = 3
    failed = _failed(check_arm.run_checks("treatment", arm, [_control()]))
    self.assertEqual(failed, ["zero A graph, content and projection writes while engaged"])

  def test_red_missing_generation_fails(self):
    arm = _treatment()
    del arm["final"]["generations"]["scope-a-2"]
    failed = _failed(check_arm.run_checks("treatment", arm, [_control()]))
    self.assertEqual(failed, ["committed generation set"])

  def test_red_changed_real_fact_hash_fails(self):
    arm = _treatment()
    arm["final"]["real_fact_sets"]["repo-1"] = "other"
    failed = _failed(check_arm.run_checks("treatment", arm, [_control()]))
    self.assertEqual(failed, ["real git fixture fact sets (fact id + payload hash)"])

  def test_red_latch_never_released_fails(self):
    arm = _treatment()
    arm["engaged"]["release_at"] = None
    self.assertIn("latch engaged and released", _failed(check_arm.run_checks("treatment", arm, [_control()])))

  def test_red_selection_not_stretched_or_not_restored_fails(self):
    arm = _treatment()
    arm["selection"][0]["delay_all_s"] = 2.0  # collected at once while engaged: no stretch
    self.assertEqual(_failed(check_arm.run_checks("treatment", arm, [_control()])), ["selection stretched while engaged"])
    arm = _treatment()
    arm["selection"][1]["delay_all_s"] = None  # a due repository never came back
    self.assertEqual(_failed(check_arm.run_checks("treatment", arm, [_control()])), ["selection restored after release"])

  def test_red_too_few_paced_items_fails(self):
    arm = _treatment()
    arm["released_pre_write"] = 6  # fewer than the 8 projector workers
    self.assertEqual(_failed(check_arm.run_checks("treatment", arm, [_control()])),
                     ["paced A projector items >= projector worker count"])

  def test_red_dead_letter_and_timeout_fail(self):
    arm = _treatment()
    arm["deferrals"]["dead_letters"] = 1
    arm["deferrals"]["admission_timeouts"] = 2
    failed = _failed(check_arm.run_checks("treatment", arm, [_control()]))
    self.assertEqual(sorted(failed), ["no admission_timeout", "no new dead letters"])

  def test_red_b_slowdown_beyond_tolerance_fails(self):
    arm = _treatment()
    arm["b"]["queue_wait_p50_s"] = 0.80  # +60% against a 10% bar and a tiny spread
    controls = [_control(0.49), _control(0.50), _control(0.51)]
    self.assertEqual(_failed(check_arm.run_checks("treatment", arm, controls)), ["B queue_wait_p50_s within tolerance"])

  def test_b_slowdown_inside_twice_the_control_spread_passes(self):
    arm = _treatment()
    arm["b"]["queue_wait_p50_s"] = 0.62  # +24%, but the control spread is 0.40, so tolerance is 0.80
    controls = [_control(0.30), _control(0.50), _control(0.70)]
    self.assertEqual(_failed(check_arm.run_checks("treatment", arm, controls)), [])

  def test_red_b_source_behind_deferral_fails(self):
    arm = _treatment()
    arm["deferrals"]["source_behind"]["B"] = 1
    self.assertEqual(_failed(check_arm.run_checks("treatment", arm, [_control()])), ["B has zero source_behind deferrals"])

  def test_t3_latch_never_engaged_passes_and_engaged_with_claimable_row_fails(self):
    arm = {"engaged": {}, "census": {"classes": {"parked_wait_unwoken": 13000}}}
    self.assertEqual(_failed(check_arm.run_checks("t3", arm, [])), [])
    arm = {"engaged": {"engage_at": "x", "release_at": "y"}, "census": {"claimable_a_at_release": 7, "classes": {"takeable": 7}}}
    self.assertEqual(_failed(check_arm.run_checks("t3", arm, [])),
                     ["latch never engaged, or released with no claimable A row"])


  def test_t2_claim_accounting(self):
    arm = _treatment()
    arm["hosted"] = {"planted": 24, "at_engagement_end": {"pending": 24, "claims": 0, "attempts": 0},
                     "final": {"completed": 24, "expired": 0}}
    self.assertEqual(_failed(check_arm.run_checks("t2", arm, [_control()])), [])
    claimed = copy.deepcopy(arm)
    claimed["hosted"]["at_engagement_end"]["claims"] = 3  # a claim got through while engaged
    self.assertEqual(_failed(check_arm.run_checks("t2", claimed, [_control()])), ["zero claims while engaged"])
    stuck = copy.deepcopy(arm)
    stuck["hosted"]["final"]["completed"] = 23  # one item never completed after release
    self.assertEqual(_failed(check_arm.run_checks("t2", stuck, [_control()])),
                     ["each item claimed and completed after release"])


if __name__ == "__main__":
  unittest.main()
