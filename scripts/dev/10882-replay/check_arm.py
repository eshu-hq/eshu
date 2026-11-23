# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""Pass/fail checker for one replay arm (#10882, #11186, #11187).

Reads the result.json files that assemble_result.py writes and applies the
acceptance criteria of the arbiter rulings (.claude/arbiter-remaining-plan.md
section 1, arbiter-replay-forks.md, arbiter-replay-go.md section 4). This is a
gate: it has seeded RED/GREEN cases in test_check_arm.py (a planted A write during
engagement, a missing generation, an empty graph reading, a missed commit, ...).

Usage:
  check_arm.py --arm RESULT [--control RESULT ...] [--kind KIND]

KIND is treatment (T1, T1TTL), control (C0), t2, t3 (T3, T3TTL), inflight (IF), controlset (only
the --control files: count and equality of the control repetitions) or cg (C at the
default global marks: reported, not judged). --control is repeated once per control
repetition; the first is the reference for final-state equality and all of them set
the AC5 tolerance. Exit code 0 means every check passed, 1 at least one failed,
2 a usage or input error.
"""

import argparse
import json
import os
import sys
from typing import Any

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from checks_lib import (  # noqa: E402  pylint: disable=wrong-import-position
    Check, check_ac5, check_consult, check_control_positive, check_engagement, check_hygiene,
    check_item_timing, check_selection, check_zero_writes, get)
from checks_final import check_cf_reference, check_control_set, check_final_state  # noqa: E402  pylint: disable=wrong-import-position


def check_ac4(arm: dict[str, Any], control: dict[str, Any], cf: dict[str, Any] | None) -> list[Check]:
  """AC4: a slowed source loses no work and resumes."""
  deferrals = get(arm, "deferrals.source_behind", {})
  released = get(arm, "released_pre_write", 0)
  workers = sum(int(v) for v in get(arm, "manifest.workers", {}).values())
  paced_a = int(get(arm, "paced.paced_items", {}).get("A", 0))
  checks = [
      Check("A pre-write releases counted (source paced)", released > 0,
            "pre-write releases=%s; admission-writer source_behind deferrals=%s (reported: that leg is judged by the "
            "in-flight arm, not here)" % (released, deferrals.get("A", 0))),
      Check("at least as many distinct paced A projector items as projector workers", workers > 0 and paced_a >= workers,
            "distinct paced A items=%s workers=%s" % (paced_a, workers)),
  ]
  checks += check_engagement(arm) + check_zero_writes(arm) + check_selection(arm)
  checks += [
      Check("attempt_count - source_paced_waits == 1 on every succeeded item",
            int(get(arm, "deferrals.attempt_count_changes", 0)) == 0,
            "violations=%s" % get(arm, "deferrals.attempt_count_changes")),
      Check("no new dead letters", int(get(arm, "deferrals.dead_letters", 0)) <= int(get(control, "deferrals.dead_letters", 0)),
            "arm=%s control=%s" % (get(arm, "deferrals.dead_letters"), get(control, "deferrals.dead_letters"))),
      Check("no admission_timeout", int(get(arm, "deferrals.admission_timeouts", 0)) == 0,
            "timeouts=%s" % get(arm, "deferrals.admission_timeouts")),
      Check("superseded items: every scope's active-generation projector item succeeded",
            not get(arm, "paced.superseded_without_succeeded_active", []),
            "superseded scopes=%s without a succeeded active item=%s" % (
                get(arm, "paced.superseded_scopes"), get(arm, "paced.superseded_without_succeeded_active"))),
  ]
  return checks + check_final_state(arm, control, cf)


def check_t2(arm: dict[str, Any], controls: list[dict[str, Any]], cf: dict[str, Any] | None = None) -> list[Check]:
  """T2: claim accounting for the hosted family paced at the claim grant.

  The synthetic hosted collector commits no facts, and pacing acts before the
  claim, so no partial commit exists to lose: the proof is that items pending at
  engagement get zero claims and no attempt change, none expire, and each is
  claimed and completes after release; the C2 control claims during the same window
  (the positive control) and the final state equals C2's.
  """
  engaged = get(arm, "hosted.at_engagement_end", {})
  final = get(arm, "hosted.final", {})
  planted = int(get(arm, "hosted.planted", 0))
  control_claims = int(get(controls[0], "hosted.at_engagement_end.claims", 0))
  checks = [
      Check("hosted items pending at engagement", planted > 0 and int(engaged.get("pending", 0)) == planted,
            "planted=%s pending=%s" % (planted, engaged.get("pending"))),
      Check("zero claims while engaged", int(engaged.get("claims", -1)) == 0, "claims=%s" % engaged.get("claims")),
      Check("positive control: the C2 hosted collector claimed during the same window", control_claims > 0,
            "C2 claims at window end=%s" % control_claims),
      Check("no attempt change while engaged", int(engaged.get("attempts", -1)) == 0,
            "attempt_total=%s" % engaged.get("attempts")),
      Check("none expired", int(final.get("expired", -1)) == 0, "expired=%s" % final.get("expired")),
      Check("each item claimed and completed after release", planted > 0 and int(final.get("completed", -1)) == planted,
            "completed=%s of %s" % (final.get("completed"), planted)),
  ]
  grant = arm.get("consult_claim_grant", {})
  checks.append(Check(
      "claim-grant consults ran on the hosted collector (collector_kind series, final scrape)",
      float(grant.get("count", 0)) > 0,
      "count=%s by collector_kind=%s p50=%.1f ms p95=%.1f ms share within 100 ms=%.2f (the per-source consult of "
      "'consult(A)' is the ingester and projector pre-write consult, not this one)" % (
          grant.get("count", 0), grant.get("by_kind", {}), 1000 * float(grant.get("p50_s", 0)),
          1000 * float(grant.get("p95_s", 0)), float(grant.get("within_100ms", 0)))))
  checks += check_engagement(arm) + check_zero_writes(arm)
  return checks + check_final_state(arm, controls[0], cf) + check_ac5(arm, controls)


def check_t3(arm: dict[str, Any], controls: list[dict[str, Any]], cf: dict[str, Any] | None = None) -> list[Check]:
  """T3 (#11187): aged rows that are not takeable never hold the latch."""
  census = arm.get("census", {}).get("classes", {})
  seeded = int(get(arm, "manifest.a_reducer_rows", -1))
  not_takeable = int(census.get("parked_wait_unwoken", 0)) + int(census.get("readiness_gated", 0))
  engaged = arm.get("engaged", {})
  checks = [
      Check("no latch row for source A and engaged counter 0",
            not engaged.get("latch_row_for_a") and float(engaged.get("engaged_counter", -1)) == 0.0
            and not engaged.get("engage_at"),
            "latch row for A=%s engaged counter=%s engage_at=%s" % (
                engaged.get("latch_row_for_a"), engaged.get("engaged_counter"), engaged.get("engage_at"))),
      Check("zero pre-write releases and zero A deferrals",
            float(arm.get("released_pre_write", -1)) == 0 and int(get(arm, "deferrals.source_behind", {}).get("A", 0)) == 0,
            "releases=%s A deferrals=%s" % (arm.get("released_pre_write"), get(arm, "deferrals.source_behind", {}).get("A", 0))),
      Check("census matches the seed (parked + readiness-gated == seeded A rows)", seeded > 0 and not_takeable == seeded,
            "census=%s seeded=%s classes=%s" % (not_takeable, seeded, sorted(census))),
  ]
  checks += check_consult(arm, gate=False)
  return checks + check_final_state(arm, controls[0], cf) + check_ac5(arm, controls)


def check_inflight(arm: dict[str, Any], controls: list[dict[str, Any]], cf: dict[str, Any] | None = None) -> list[Check]:
  """The in-flight arm (decision (g)): the admission-writer leg defers an item that is
  already past the pre-write consult, and that item finishes after the release.

  The aged A rows are inserted while a real-fixture projector item is claimed and
  blocked at its graph write, so the item's intent enqueue consults an engaged
  latch. Pass: at least one source_behind deferral for A, intents enqueued after the
  release by those items, every item settled, zero dead letters and admission
  timeouts, and the final state equals the control's (IFC).
  """
  inflight = arm.get("inflight", {})
  after = inflight.get("scopes_after_release") or []
  checks = [
      Check("admission-writer leg deferred A (source_behind deferrals >= 1)",
            int(get(arm, "deferrals.source_behind", {}).get("A", 0)) >= 1,
            "A deferrals=%s" % get(arm, "deferrals.source_behind", {}).get("A", 0)),
      Check("the deferred items enqueued their intents after the release",
            len(after) >= 1 and int(inflight.get("rows_after_release", 0)) >= 1,
            "scopes with intents enqueued after release=%d rows=%s" % (len(after), inflight.get("rows_after_release"))),
      Check("zero dead letters", int(get(arm, "deferrals.dead_letters", 0)) == 0,
            "dead letters=%s" % get(arm, "deferrals.dead_letters")),
      Check("zero admission_timeout", int(get(arm, "deferrals.admission_timeouts", 0)) == 0,
            "timeouts=%s" % get(arm, "deferrals.admission_timeouts")),
  ]
  checks += check_engagement(arm)
  return checks + check_final_state(arm, controls[0], cf)


def check_warm(arm: dict[str, Any], controls: list[dict[str, Any]], cf: dict[str, Any] | None = None) -> list[Check]:
  """The warm-source paced-delta case (replay ruling (c)): a source with a projected
  full base, a commit changing file X pushed while engaged (its delta is paced and
  superseded unprojected), a commit changing file Y pushed after the release. The
  final delta names both paths and the final state, content identity included,
  equals the control's."""
  bad, notes = [], []
  for scope, info in sorted(get(arm, "final.shapes", {}).items()):
    payload = get(arm, "final.repository_payloads", {}).get(scope, {}).get(info["active_sha"], {})
    paths = set(payload.get("delta_relative_paths") or [])
    notes.append("%s delta=%s paths=%s" % (scope[-8:], info["is_delta"], sorted(paths)))
    if not info["is_delta"] or not {"src/mod1.go", "src/mod2.go"} <= paths:
      bad.append(scope)
  checks = [
      Check("the final delta of every scope names both changed paths (X while engaged, Y after release)", not bad and bool(notes),
            "; ".join(notes[:3]) + ("; failing=%s" % bad if bad else "")),
      Check("a delta was paced and superseded unprojected while engaged",
            int(get(arm, "paced.superseded_scopes", 0)) >= 1 and float(arm.get("released_pre_write", 0)) >= 1,
            "superseded scopes=%s pre-write releases=%s" % (get(arm, "paced.superseded_scopes"), arm.get("released_pre_write"))),
  ]
  checks += check_engagement(arm) + check_zero_writes(arm, warm=True)
  checks += [Check("zero dead letters", int(get(arm, "deferrals.dead_letters", 0)) == 0,
                   "dead letters=%s" % get(arm, "deferrals.dead_letters")),
             Check("zero admission_timeout", int(get(arm, "deferrals.admission_timeouts", 0)) == 0,
                   "timeouts=%s" % get(arm, "deferrals.admission_timeouts"))]
  return checks + check_final_state(arm, controls[0], cf)


def run_checks(kind: str, arm: dict[str, Any], controls: list[dict[str, Any]], cf: dict[str, Any] | None = None) -> list[Check]:
  """Selects and runs the checks for one arm kind."""
  if kind == "controlset":
    return check_control_set(controls)
  if kind == "cg":
    return [Check("CG reported, not judged", True, "A deferrals=%s dead letters=%s admission timeouts=%s unsettled=%s B=%s" % (
        get(arm, "deferrals.source_behind"), get(arm, "deferrals.dead_letters"), get(arm, "deferrals.admission_timeouts"),
        get(arm, "final.projector_unsettled"), arm.get("b")))]
  if kind == "cfref":
    return check_cf_reference(controls[0], arm)
  checks = check_hygiene(arm)
  if kind == "inflight":
    return checks + check_inflight(arm, controls, cf)
  if kind == "warm":
    return checks + check_warm(arm, controls, cf)
  if kind == "t3":
    return checks + check_t3(arm, controls, cf)
  if kind == "t2":
    return checks + check_t2(arm, controls, cf)
  if kind == "control":
    # The no-completion control: unpaced like C, so B and the final sets equal C.
    return checks + check_control_positive(arm) + check_ac5(arm, controls) + check_final_state(arm, controls[0], cf)
  gate = get(arm, "manifest.ttl") == "0s"
  return checks + check_ac4(arm, controls[0], cf) + check_item_timing(arm) + check_consult(arm, gate=gate) + check_ac5(arm, controls)


def _load(path: str) -> dict[str, Any]:
  with open(path, encoding="utf-8") as handle:
    return json.load(handle)


def main(argv: list[str]) -> int:
  parser = argparse.ArgumentParser(description=__doc__)
  parser.add_argument("--arm", help="result.json of the arm under test")
  parser.add_argument("--control", action="append", default=[], help="result.json of a control repetition")
  parser.add_argument("--cf", help="result.json of the CF reference arm (a full active generation at the final sha)")
  parser.add_argument("--kind", choices=("treatment", "control", "t2", "t3", "inflight", "warm", "controlset", "cg", "cfref"), default="treatment")
  args = parser.parse_args(argv)
  try:
    arm = _load(args.arm) if args.arm else {}
    cf = _load(args.cf) if args.cf else None
    controls = [_load(path) for path in args.control]
  except (OSError, json.JSONDecodeError) as err:
    print("check_arm: %s" % err, file=sys.stderr)
    return 2
  if args.kind not in ("cg", "controlset") and not args.arm:
    print("check_arm: --arm is required for %s" % args.kind, file=sys.stderr)
    return 2
  if args.kind not in ("cg",) and not controls:
    print("check_arm: --control is required for %s" % args.kind, file=sys.stderr)
    return 2
  checks = run_checks(args.kind, arm, controls, cf)
  for check in checks:
    print(check)
  return 0 if all(c.passed for c in checks) else 1


if __name__ == "__main__":
  sys.exit(main(sys.argv[1:]))
