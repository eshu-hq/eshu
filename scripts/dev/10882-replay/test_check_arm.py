# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""Seeded RED/GREEN proof for check_arm.py (the replay pass/fail gate).

GREEN: a treatment arm that matches the control passes every check.
RED:   each planted violation must fail exactly the check that names it. The
       required pair is a planted A write during engagement and a missing
       generation; the rest pin every other arm of the gate so none passes by
       accident (empty graph reading, selection not stretched, a commit that
       never came back, intents missing from the union, build identity, an
       inconclusive tolerance, one-sided AC5, T2 and T3 specifics).

Run: python3 -I scripts/dev/10882-replay/test_check_arm.py
"""

import copy
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import check_arm  # noqa: E402  pylint: disable=wrong-import-position

SHAS = {"binaries": {"eshu-projector": "p1", "eshu-ingester": "i1"}}


def _base(arm: str = "C") -> dict:
  return {
      "arm": arm, "rep": 1,
      "manifest": {"commit": "abc", "binaries": dict(SHAS["binaries"]), "source_a": "git", "source_b": "hosted",
                   "workers": {"projector": 4, "ingester_projector": 4}, "ttl": "0s", "a_reducer_rows": 13000},
      "build": {"commit": "abc", "binaries": {"eshu-projector": {"sha256": "p1", "vcs_revision": "", "vcs_modified": ""},
                                              "eshu-ingester": {"sha256": "i1", "vcs_revision": "abc", "vcs_modified": "false"}}},
      "graph_reading": {"samples": 40, "missing_samples": 0, "final_counts_present": True},
      "timeouts": [], "released_pre_write": 0,
      "b": {"queue_wait_p50_s": 0.50, "queue_wait_p95_s": 1.20, "claims_per_s": 2.0},
      "deferrals": {"source_behind": {}, "attempt_count_changes": 0, "dead_letters": 0, "admission_timeouts": 0},
      "paced": {"paced_items": {}, "paced_wait_sum": {}, "superseded_scopes": 0, "superseded_without_succeeded_active": []},
      "writes": {"first": {"graph": 0, "content": 0, "succeeded": 0}, "last_engaged": None,
                 "end": {"graph": 1400, "content": 144, "succeeded": 54}, "at_t120": {"graph": 900, "content": 90, "succeeded": 40}},
      "final": {
          "generations": {"scope-a-1": "g1", "scope-b-1": "g3", "git-1": "sha-1"},
          "real_fact_sets": {"git-1": "h1", "git-2": "h2"}, "effective_fact_sets": {"git-1": "e1", "git-2": "e2"},
          "active_is_delta": {"git-1": True, "git-2": True}, "graph_counts": {"File": 36, "Repository": 3},
          "intent_active": {"A": ["git-1:d1", "git-1:d2"]}, "intent_union": {"A": ["git-1:d1", "git-1:d2", "git-1:d3"]},
          "content_rows": 36, "projector_unsettled": 0, "fixture_scopes": 6, "active_sha_is_last_push": True,
      },
  }


def _control(wait_p50: float = 0.50) -> dict:
  control = _base("C")
  control["b"]["queue_wait_p50_s"] = wait_p50
  return control


def _treatment() -> dict:
  arm = copy.deepcopy(_base("T1"))
  arm["deferrals"]["source_behind"] = {"A": 12}
  arm["released_pre_write"] = 108
  arm["paced"] = {"paced_items": {"A": 54}, "paced_wait_sum": {"A": 108}, "superseded_scopes": 6,
                  "superseded_without_succeeded_active": []}
  arm["manifest"]["selection_base_s"] = 20
  arm["selection"] = {"engaged": {"min_s": 55.0, "max_s": 80.0, "missing": 0},
                      "after_release": {"min_s": 1.0, "max_s": 6.0, "missing": 0}}
  arm["engaged"] = {"engage_at": "2026-11-23T00:00:10Z", "release_at": "2026-11-23T00:02:10Z", "engaged_counter": 1.0,
                    "reengaged": False}
  arm["writes"] = {"first": {"graph": 0, "content": 0, "succeeded": 0}, "last_engaged": {"graph": 0, "content": 0, "succeeded": 0},
                   "end": {"graph": 1400, "content": 144, "succeeded": 54}, "at_t120": None}
  arm["consult"] = {"A": {"count": 300, "p50_s": 0.04, "p95_s": 0.09, "within_100ms": 0.9}}
  return arm


def _failed(checks: list) -> list:
  return [c.name for c in checks if not c.passed]


def _run(kind: str, arm: dict, controls: list) -> list:
  return _failed(check_arm.run_checks(kind, arm, controls))


def _controls() -> list:
  return [_control(0.49), _control(0.50), _control(0.51)]


class TreatmentTest(unittest.TestCase):

  def test_green_matching_treatment_passes(self):
    self.assertEqual(_run("treatment", _treatment(), _controls()), [])

  def test_red_planted_a_write_during_engagement_fails(self):
    arm = _treatment()
    arm["writes"]["last_engaged"]["graph"] = 3
    self.assertEqual(_run("treatment", arm, _controls()), ["zero A graph, content and projection writes while engaged"])

  def test_red_missing_generation_fails(self):
    arm = _treatment()
    del arm["final"]["generations"]["scope-a-1"]
    self.assertEqual(_run("treatment", arm, _controls()), ["committed generation set (source commit sha)"])

  def test_red_changed_real_fact_hash_fails(self):
    arm = _treatment()
    arm["final"]["effective_fact_sets"]["git-1"] = "other"
    failed = _run("treatment", arm, _controls())
    self.assertEqual(len(failed), 1)
    self.assertTrue(failed[0].startswith("effective real git fixture fact sets"))

  def test_active_generation_differences_are_reported_not_judged(self):
    # The smoke case: the control's active generation is a delta (11 facts), the
    # treatment arm's is a full re-observation (50 facts), the effective sets equal.
    arm = _treatment()
    arm["final"]["real_fact_sets"] = {"git-1": "full-1", "git-2": "full-2"}
    arm["final"]["intent_active"] = {"A": ["git-1:d1", "git-1:d2", "git-1:d4"]}
    arm["final"]["active_is_delta"] = {"git-1": False, "git-2": False}
    checks = check_arm.run_checks("treatment", arm, _controls())
    self.assertEqual(_failed(checks), [])
    row = next(c for c in checks if c.name.startswith("real fact sets of the ACTIVE"))
    self.assertIn("differ", row.detail)
    self.assertIn("arm=[False]", row.detail)

  def test_red_empty_graph_reading_fails(self):
    arm = _treatment()
    arm["graph_reading"]["missing_samples"] = 3
    self.assertEqual(_run("treatment", arm, _controls()), ["graph reading present in every sample (no swallowed Neo4j error)"])
    arm = _treatment()
    arm["writes"]["last_engaged"]["graph"] = None  # an empty reading must not read as zero
    self.assertEqual(_run("treatment", arm, _controls()), ["zero A graph, content and projection writes while engaged"])

  def test_red_no_positive_control_after_release_fails(self):
    arm = _treatment()
    arm["writes"]["end"]["graph"] = 0  # the probe never saw a write: zero would be vacuous
    self.assertEqual(_run("treatment", arm, _controls()), ["positive control: A graph writes after release are above 0"])

  def test_red_latch_not_released_or_reengaged_fails(self):
    arm = _treatment()
    arm["engaged"]["release_at"] = None
    self.assertEqual(_run("treatment", arm, _controls()), ["latch engaged and released (latch row times)"])
    arm = _treatment()
    arm["engaged"]["reengaged"] = True
    self.assertEqual(_run("treatment", arm, _controls()), ["no re-engagement (one engaged interval, one latch engaged_at)"])
    arm = _treatment()
    arm["engaged"]["engaged_counter"] = 3.0  # several workers record one transition: reported, not judged
    self.assertEqual(_run("treatment", arm, _controls()), [])

  def test_red_selection_not_stretched_and_late_commit_not_restored_fail(self):
    arm = _treatment()
    arm["selection"]["engaged"]["min_s"] = 2.0
    self.assertEqual(len(_run("treatment", arm, _controls())), 1)
    arm = _treatment()
    arm["selection"]["after_release"]["max_s"] = 120.0  # the late engaged commit cannot stand in for it
    self.assertEqual(len(_run("treatment", arm, _controls())), 1)
    arm = _treatment()
    arm["selection"]["after_release"].update({"max_s": None, "missing": 1})  # a due repository never came back
    self.assertEqual(len(_run("treatment", arm, _controls())), 1)

  def test_red_too_few_paced_items_fails(self):
    arm = _treatment()
    arm["paced"]["paced_items"]["A"] = 6
    self.assertEqual(_run("treatment", arm, _controls()), ["at least as many distinct paced A projector items as projector workers"])

  def test_red_dead_letter_timeout_and_attempt_change_fail(self):
    arm = _treatment()
    arm["deferrals"].update({"dead_letters": 1, "admission_timeouts": 2, "attempt_count_changes": 1})
    self.assertEqual(sorted(_run("treatment", arm, _controls())),
                     sorted(["no new dead letters", "no admission_timeout", "attempt_count - source_paced_waits == 1 on every succeeded item"]))

  def test_red_unsettled_queue_and_swallowed_timeout_fail(self):
    arm = _treatment()
    arm["final"]["projector_unsettled"] = 2
    arm["timeouts"] = [{"what": "projector queue to settle", "limit_s": "240"}]
    self.assertEqual(sorted(_run("treatment", arm, _controls())),
                     sorted(["projector queue settled (0 unsettled)", "no wait timed out"]))

  def test_red_superseded_scope_without_succeeded_active_item_fails(self):
    arm = _treatment()
    arm["paced"]["superseded_without_succeeded_active"] = ["git-3"]
    self.assertEqual(_run("treatment", arm, _controls()), ["superseded items: every scope's active-generation projector item succeeded"])

  def test_red_intent_union_missing_a_pair_the_control_has_fails(self):
    arm = _treatment()
    arm["final"]["intent_union"] = {"A": ["git-1:d1", "git-1:d2"]}  # d3 only ever came from a paced generation
    self.assertEqual(_run("treatment", arm, _controls()), ["reducer intents (scope, domain) over every generation, per source"])

  def test_red_build_identity_fails(self):
    for mutate in (lambda a: a["manifest"].update(commit=""),
                   lambda a: a["manifest"]["binaries"].update({"eshu-projector": "other"}),
                   lambda a: a["build"]["binaries"]["eshu-ingester"].update(vcs_modified="true"),
                   lambda a: a["build"]["binaries"]["eshu-ingester"].update(vcs_revision="zzz")):
      arm = _treatment()
      mutate(arm)
      self.assertEqual(_run("treatment", arm, _controls()), ["build identity: one commit, every binary pinned"])

  def test_red_vacuity_guards_fail(self):
    for mutate in (lambda a: a["final"].update(fixture_scopes=5),
                   lambda a: a["final"].update(active_sha_is_last_push=False),
                   lambda a: a["final"].update(intent_union={"B": ["x:y"]})):
      arm = _treatment()
      mutate(arm)
      failed = _run("treatment", arm, _controls())
      self.assertIn("fixture vacuity guards: 6 scopes, each at the last pushed sha, git-side intents present", failed)

  def test_red_close_gate_consult_outside_100ms_fails(self):
    arm = _treatment()
    arm["consult"]["A"]["within_100ms"] = 0.3
    self.assertEqual(_run("treatment", arm, _controls()), ["close gate: consult p50 within 100 ms (le=0.1 bucket share >= 50%)"])
    arm = _treatment()
    arm["manifest"]["ttl"] = "5s"  # the gate is read on T1 at TTL 0 only
    arm["consult"]["A"]["within_100ms"] = 0.3
    self.assertEqual(_run("treatment", arm, _controls()), [])


class Ac5Test(unittest.TestCase):

  def test_red_b_slowdown_beyond_tolerance_fails(self):
    arm = _treatment()
    arm["b"]["queue_wait_p50_s"] = 0.80
    self.assertEqual(_run("treatment", arm, _controls()), ["B queue_wait_p50_s not degraded"])

  def test_b_improvement_is_not_a_failure(self):
    arm = _treatment()
    arm["b"]["queue_wait_p50_s"] = 0.20  # one-sided: faster than the control is fine
    self.assertEqual(_run("treatment", arm, _controls()), [])

  def test_red_claim_rate_drop_fails(self):
    arm = _treatment()
    arm["b"]["claims_per_s"] = 1.0
    self.assertEqual(_run("treatment", arm, _controls()), ["B claims_per_s not degraded"])

  def test_red_tolerance_above_half_the_control_mean_is_inconclusive(self):
    controls = [_control(0.20), _control(0.50), _control(0.80)]  # spread 0.60 -> tolerance 1.2 = 240%
    arm = _treatment()
    checks = check_arm.run_checks("treatment", arm, controls)
    row = next(c for c in checks if c.name == "B queue_wait_p50_s not degraded")
    self.assertFalse(row.passed)
    self.assertIn("INCONCLUSIVE", row.detail)
    self.assertIn("% of the control mean", row.detail)

  def test_red_b_deferral_or_paced_wait_fails(self):
    arm = _treatment()
    arm["deferrals"]["source_behind"]["B"] = 1
    arm["paced"]["paced_wait_sum"]["B"] = 3
    self.assertEqual(sorted(_run("treatment", arm, _controls())),
                     sorted(["B has zero source_behind deferrals", "B has zero paced waits"]))


class ControlTest(unittest.TestCase):

  def test_control_set_needs_three_equal_repetitions(self):
    self.assertEqual(_run("controlset", {}, _controls()), [])
    self.assertEqual(_run("controlset", {}, _controls()[:2]), ["at least 3 control repetitions"])
    drifted = _controls()
    drifted[2]["final"]["graph_counts"] = {"File": 30}
    self.assertEqual(_run("controlset", {}, drifted), ["control rep 3 vs rep 1: graph counts per label"])
    blind = _controls()
    blind[1]["writes"]["at_t120"]["graph"] = 0  # a control whose probe saw no write: the zero-write proof would be vacuous
    self.assertEqual(_run("controlset", {}, blind),
                     ["control rep 2: positive control: unpaced A graph nodes at t+120 s are above the first sample"])
    broken = _controls()
    broken[0]["graph_reading"]["missing_samples"] = 1
    self.assertEqual(_run("controlset", {}, broken),
                     ["control rep 1: graph reading present in every sample (no swallowed Neo4j error)"])

  def test_c0_checks_positive_control_and_final_state(self):
    arm = copy.deepcopy(_control())
    arm["arm"] = "C0"
    self.assertEqual(_run("control", arm, _controls()), [])
    arm["writes"]["at_t120"]["graph"] = 0
    self.assertEqual(_run("control", arm, _controls()),
                     ["positive control: unpaced A graph nodes at t+120 s are above the first sample"])


class HostedTest(unittest.TestCase):

  def _t2(self) -> tuple:
    arm = _treatment()
    arm["hosted"] = {"planted": 24, "at_engagement_end": {"pending": 24, "claims": 0, "attempts": 0},
                     "final": {"completed": 24, "expired": 0}}
    c2 = _control()
    c2["hosted"] = {"at_engagement_end": {"claims": 24}}
    return arm, [c2, c2, c2]

  def test_t2_claim_accounting(self):
    arm, controls = self._t2()
    self.assertEqual(_run("t2", arm, controls), [])
    arm["hosted"]["at_engagement_end"]["claims"] = 3
    self.assertEqual(_run("t2", arm, controls), ["zero claims while engaged"])
    arm, controls = self._t2()
    arm["hosted"]["final"]["completed"] = 23
    self.assertEqual(_run("t2", arm, controls), ["each item claimed and completed after release"])

  def test_t2_needs_the_c2_positive_control_and_final_state(self):
    arm, controls = self._t2()
    controls[0]["hosted"]["at_engagement_end"]["claims"] = 0  # C2's collector claimed nothing: zero claims proves nothing
    self.assertEqual(_run("t2", arm, controls), ["positive control: the C2 hosted collector claimed during the same window"])
    arm, controls = self._t2()
    arm["final"]["graph_counts"] = {"File": 1}
    self.assertEqual(_run("t2", arm, controls), ["graph counts per label"])


class T3Test(unittest.TestCase):

  def _t3(self) -> dict:
    arm = _treatment()
    arm["engaged"] = {"engage_at": None, "release_at": None, "engaged_counter": 0.0, "latch_row_for_a": False}
    arm["released_pre_write"] = 0
    arm["deferrals"]["source_behind"] = {}
    arm["census"] = {"classes": {"parked_wait_unwoken": 6500, "readiness_gated": 6500, "takeable": 48}}
    arm["writes"] = {"first": {"graph": 0, "content": 0, "succeeded": 0}, "last_engaged": None,
                     "end": {"graph": 1400, "content": 144, "succeeded": 54}, "at_t120": None}
    return arm

  def test_green_t3(self):
    self.assertEqual(_run("t3", self._t3(), _controls()), [])

  def test_red_latch_row_census_and_releases_fail(self):
    arm = self._t3()
    arm["engaged"]["latch_row_for_a"] = True
    self.assertEqual(_run("t3", arm, _controls()), ["no latch row for source A and engaged counter 0"])
    arm = self._t3()
    arm["census"]["classes"]["parked_wait_unwoken"] = 100
    self.assertEqual(_run("t3", arm, _controls()), ["census matches the seed (parked + readiness-gated == seeded A rows)"])
    arm = self._t3()
    arm["released_pre_write"] = 2
    self.assertEqual(_run("t3", arm, _controls()), ["zero pre-write releases and zero A deferrals"])
    arm = self._t3()
    arm["consult"]["A"]["count"] = 0
    self.assertEqual(_run("t3", arm, _controls()), ["consults ran on source A (engaged scrape)"])


if __name__ == "__main__":
  unittest.main()
