# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""Pass/fail checker for one replay arm (#10882, #11186, #11187).

Reads the result.json files that collect.sh writes and applies the acceptance
criteria of the arbiter ruling (.claude/arbiter-remaining-plan.md section 1 and
the forks ruling of 2026-11-22). This is a gate: it has a seeded RED/GREEN pair
in test_check_arm.py (one planted A write during engagement and one missing
generation must each fail it).

Usage:
  check_arm.py --arm RESULT [--control RESULT ...] [--kind treatment|control|t3]

--control is repeated once per control repetition; with three repetitions the
spread of arm C sets the AC5 tolerance. Exit code 0 means every check passed,
1 means at least one failed, 2 means a usage or input error.
"""

import argparse
import json
import sys
from typing import Any

# AC5: B's queue wait and claim rate may move by the larger of 10% or twice the
# spread of the control repetitions.
AC5_RELATIVE_TOLERANCE = 0.10
AC5_SPREAD_FACTOR = 2.0

# B metrics compared under AC5. Each is read from result["b"].
B_METRICS = ("queue_wait_p50_s", "queue_wait_p95_s", "claims_per_s")


class Check:
  """One named check and its outcome."""

  def __init__(self, name: str, passed: bool, detail: str) -> None:
    self.name = name
    self.passed = passed
    self.detail = detail

  def __str__(self) -> str:
    return "%s %s: %s" % ("PASS" if self.passed else "FAIL", self.name, self.detail)


def _get(result: dict[str, Any], path: str, default: Any = None) -> Any:
  """Reads a dotted path from a nested dict."""
  node: Any = result
  for part in path.split("."):
    if not isinstance(node, dict) or part not in node:
      return default
    node = node[part]
  return node


def _diff_maps(want: dict[str, Any], got: dict[str, Any]) -> str:
  """Summarizes how two maps differ, bounded to a few keys."""
  missing = sorted(set(want) - set(got))
  extra = sorted(set(got) - set(want))
  changed = sorted(k for k in set(want) & set(got) if want[k] != got[k])
  parts = []
  for label, keys in (("missing", missing), ("extra", extra), ("changed", changed)):
    if keys:
      parts.append("%s %d (%s)" % (label, len(keys), ", ".join(keys[:3])))
  return "; ".join(parts) or "equal"


def check_final_state(arm: dict[str, Any], control: dict[str, Any]) -> list[Check]:
  """Final committed state equals the control arm's (AC4 no-loss, arbiter 5-6)."""
  checks = []
  for path, label in (
      ("final.generations", "committed generation set"),
      ("final.real_fact_sets", "real git fixture fact sets (fact id + payload hash)"),
      ("final.graph_counts", "graph counts per label"),
      ("final.reducer_intents", "reducer intents for A and B (count + key set)"),
  ):
    want, got = _get(control, path, {}), _get(arm, path, {})
    if not want:
      checks.append(Check(label, False, "control has no %s" % path))
      continue
    checks.append(Check(label, want == got, _diff_maps(want, got)))
  for path, label in (
      ("final.content_rows", "content rows"),
      ("final.projector_unsettled", "projector items not succeeded"),
  ):
    want, got = _get(control, path), _get(arm, path)
    checks.append(Check(label, want is not None and want == got, "control=%s arm=%s" % (want, got)))
  return checks


def check_ac4(arm: dict[str, Any], control: dict[str, Any]) -> list[Check]:
  """AC4: a slowed source loses no work and resumes."""
  checks = []
  deferrals = _get(arm, "deferrals.source_behind", {})
  checks.append(Check(
      "A deferrals counted under reason=source_behind",
      deferrals.get("A", 0) > 0 or _get(arm, "released_pre_write", 0) > 0,
      "admission=%s pre-write releases=%s" % (deferrals.get("A", 0), _get(arm, "released_pre_write", 0))))
  engaged = _get(arm, "engaged", {})
  checks.append(Check(
      "latch engaged and released",
      bool(engaged.get("engage_at")) and bool(engaged.get("release_at")),
      "engage=%s release=%s" % (engaged.get("engage_at"), engaged.get("release_at"))))
  writes = _get(arm, "a_writes_during_engagement", {})
  total = sum(int(writes.get(k, 0)) for k in ("graph_nodes", "content_rows", "projector_succeeded"))
  checks.append(Check(
      "zero A graph, content and projection writes while engaged",
      total == 0, "graph=%s content=%s succeeded=%s" % (
          writes.get("graph_nodes"), writes.get("content_rows"), writes.get("projector_succeeded"))))
  checks.append(Check(
      "attempt counts unchanged", int(_get(arm, "deferrals.attempt_count_changes", 0)) == 0,
      "changes=%s" % _get(arm, "deferrals.attempt_count_changes")))
  dead = int(_get(arm, "deferrals.dead_letters", 0))
  dead_c = int(_get(control, "deferrals.dead_letters", 0))
  checks.append(Check("no new dead letters", dead <= dead_c, "arm=%d control=%d" % (dead, dead_c)))
  checks.append(Check(
      "no admission_timeout", int(_get(arm, "deferrals.admission_timeouts", 0)) == 0,
      "timeouts=%s" % _get(arm, "deferrals.admission_timeouts")))
  return checks + check_final_state(arm, control)


def check_ac5(arm: dict[str, Any], controls: list[dict[str, Any]]) -> list[Check]:
  """AC5: source B is unaffected."""
  checks = [
      Check("B has zero source_behind deferrals",
            int(_get(arm, "deferrals.source_behind", {}).get("B", 0)) == 0,
            "B=%s" % _get(arm, "deferrals.source_behind", {}).get("B", 0)),
  ]
  for metric in B_METRICS:
    ref = [c["b"][metric] for c in controls if metric in c.get("b", {})]
    if not ref or metric not in arm.get("b", {}):
      checks.append(Check("B %s within tolerance" % metric, False, "metric missing"))
      continue
    mean = sum(ref) / len(ref)
    spread = max(ref) - min(ref)
    tolerance = max(AC5_RELATIVE_TOLERANCE * abs(mean), AC5_SPREAD_FACTOR * spread)
    got = arm["b"][metric]
    checks.append(Check(
        "B %s within tolerance" % metric, abs(got - mean) <= tolerance,
        "arm=%.4f control mean=%.4f tolerance=%.4f" % (got, mean, tolerance)))
  return checks


def check_t3(arm: dict[str, Any]) -> list[Check]:
  """T3 (#11187): aged rows that are not takeable never hold the latch."""
  engaged = _get(arm, "engaged", {})
  claimable_a = int(_get(arm, "census.claimable_a_at_release", -1))
  never = not engaged.get("engage_at")
  released_clean = bool(engaged.get("release_at")) and claimable_a == 0
  return [
      Check("latch never engaged, or released with no claimable A row",
            never or released_clean,
            "engage=%s release=%s claimable_a_at_release=%s" % (
                engaged.get("engage_at"), engaged.get("release_at"), claimable_a)),
      Check("census lists the not-takeable rows by class", bool(_get(arm, "census.classes")),
            "classes=%s" % sorted(_get(arm, "census.classes", {}))),
  ]


def run_checks(kind: str, arm: dict[str, Any], controls: list[dict[str, Any]]) -> list[Check]:
  """Selects and runs the checks for one arm kind."""
  if kind == "t3":
    return check_t3(arm)
  if kind == "control":
    # The no-completion control: B and the final sets must equal arm C.
    return check_ac5(arm, controls) + check_final_state(arm, controls[0])
  return check_ac4(arm, controls[0]) + check_ac5(arm, controls)


def main(argv: list[str]) -> int:
  parser = argparse.ArgumentParser(description=__doc__)
  parser.add_argument("--arm", required=True, help="result.json of the arm under test")
  parser.add_argument("--control", action="append", default=[], help="result.json of a control repetition")
  parser.add_argument("--kind", choices=("treatment", "control", "t3"), default="treatment")
  args = parser.parse_args(argv)
  try:
    with open(args.arm, encoding="utf-8") as handle:
      arm = json.load(handle)
    controls = []
    for path in args.control:
      with open(path, encoding="utf-8") as handle:
        controls.append(json.load(handle))
  except (OSError, json.JSONDecodeError) as err:
    print("check_arm: %s" % err, file=sys.stderr)
    return 2
  if args.kind != "t3" and not controls:
    print("check_arm: --control is required for %s arms" % args.kind, file=sys.stderr)
    return 2
  checks = run_checks(args.kind, arm, controls)
  for check in checks:
    print(check)
  return 0 if all(c.passed for c in checks) else 1


if __name__ == "__main__":
  sys.exit(main(sys.argv[1:]))
