# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""Checks shared by every arm kind of the #10882 replay checker.

Each function reads one assembled result (see assemble_result.py) and returns
Check rows. Nothing here retunes a threshold after the fact: the constants below
come from the arbiter ruling and are fixed before any run.
"""

from typing import Any

# AC5: degradation of B beyond the larger of 10% or twice the spread of the
# control repetitions fails; a tolerance above 50% of the control mean means the
# measurement cannot tell, which is reported as inconclusive and never a pass.
AC5_RELATIVE_TOLERANCE = 0.10
AC5_SPREAD_FACTOR = 2.0
AC5_INCONCLUSIVE_ABOVE = 0.50
# Selection: a commit pushed while engaged must wait longer than the base resync
# interval; one pushed after release must be selected within this many seconds.
SELECTION_RESTORED_WITHIN_S = 35.0
FIXTURE_SCOPES = 6
MIN_CONTROL_REPS = 3
# Close gate: consult p50 within 100 ms, judged on the le=0.1 bucket count.
CONSULT_WITHIN_100MS_SHARE = 0.5
# (higher is worse, lower is worse) for the B metrics AC5 compares.
B_METRICS = {"queue_wait_p50_s": "higher", "queue_wait_p95_s": "higher", "claims_per_s": "lower"}


class Check:
  """One named check and its outcome."""

  def __init__(self, name: str, passed: bool, detail: str) -> None:
    self.name = name
    self.passed = passed
    self.detail = detail

  def __str__(self) -> str:
    return "%s %s: %s" % ("PASS" if self.passed else "FAIL", self.name, self.detail)


def get(result: dict[str, Any], path: str, default: Any = None) -> Any:
  """Reads a dotted path from a nested dict."""
  node: Any = result
  for part in path.split("."):
    if not isinstance(node, dict) or part not in node:
      return default
    node = node[part]
  return node


def diff_maps(want: Any, got: Any) -> str:
  """Summarizes how two maps differ, bounded to a few keys."""
  if not isinstance(want, dict) or not isinstance(got, dict):
    return "equal" if want == got else "differ"
  missing = sorted(set(want) - set(got))
  extra = sorted(set(got) - set(want))
  changed = sorted(k for k in set(want) & set(got) if want[k] != got[k])
  parts = []
  for label, keys in (("missing", missing), ("extra", extra), ("changed", changed)):
    if keys:
      parts.append("%s %d (%s)" % (label, len(keys), ", ".join(str(k) for k in keys[:3])))
  return "; ".join(parts) or "equal"


def git_side(arm: dict[str, Any]) -> str:
  """Which of A or B is the git source in this arm."""
  return "A" if get(arm, "manifest.source_a") == "git" else "B"


def check_hygiene(arm: dict[str, Any]) -> list[Check]:
  """Build identity, an honest graph reading, a settled queue, no swallowed wait."""
  manifest = arm.get("manifest", {})
  build = arm.get("build", {})
  commit = manifest.get("commit", "")
  mismatched, stamped = [], 0
  for name, sha in sorted(manifest.get("binaries", {}).items()):
    entry = build.get("binaries", {}).get(name)
    if entry is None or entry.get("sha256") != sha:
      mismatched.append(name)
      continue
    revision = entry.get("vcs_revision", "")
    if revision:
      stamped += 1
    if (revision and revision != commit) or entry.get("vcs_modified") == "true":
      mismatched.append(name)
  identity_ok = bool(commit) and build.get("commit") == commit and bool(manifest.get("binaries")) and not mismatched
  reading = arm.get("graph_reading", {})
  return [
      Check("build identity: one commit, every binary pinned", identity_ok,
            "commit=%r build.json commit=%r binaries=%d unmatched=%s vcs-stamped=%d"
            % (commit, build.get("commit"), len(manifest.get("binaries", {})), mismatched, stamped)),
      Check("graph reading present in every sample (no swallowed Neo4j error)",
            reading.get("samples", 0) > 0 and reading.get("missing_samples", 1) == 0 and bool(reading.get("final_counts_present")),
            "samples=%s missing=%s final_counts=%s" % (reading.get("samples"), reading.get("missing_samples"),
                                                      reading.get("final_counts_present"))),
      Check("projector queue settled (0 unsettled)", int(get(arm, "final.projector_unsettled", -1)) == 0,
            "unsettled=%s" % get(arm, "final.projector_unsettled")),
      Check("no wait timed out", not arm.get("timeouts"), "timeouts=%s" % arm.get("timeouts")),
      Check("fixture vacuity guards: 6 scopes, each at the last pushed sha, git-side intents present",
            int(get(arm, "final.fixture_scopes", 0)) == FIXTURE_SCOPES and bool(get(arm, "final.active_sha_is_last_push"))
            and len(get(arm, "final.intent_union", {}).get(git_side(arm), [])) > 0,
            "scopes=%s active_is_last_push=%s git-side intents=%d" % (
                get(arm, "final.fixture_scopes"), get(arm, "final.active_sha_is_last_push"),
                len(get(arm, "final.intent_union", {}).get(git_side(arm), [])))),
  ]


def check_ac5(arm: dict[str, Any], controls: list[dict[str, Any]]) -> list[Check]:
  """AC5: source B is not degraded. One-sided, with the tolerance printed."""
  checks = [
      Check("B has zero source_behind deferrals", int(get(arm, "deferrals.source_behind", {}).get("B", 0)) == 0,
            "B=%s" % get(arm, "deferrals.source_behind", {}).get("B", 0)),
      Check("B has zero paced waits", int(get(arm, "paced.paced_wait_sum", {}).get("B", 0)) == 0,
            "B paced waits=%s" % get(arm, "paced.paced_wait_sum", {}).get("B", 0)),
  ]
  for metric, worse in B_METRICS.items():
    ref = [c["b"][metric] for c in controls if metric in c.get("b", {})]
    if not ref or metric not in arm.get("b", {}):
      checks.append(Check("B %s not degraded" % metric, False, "metric missing"))
      continue
    mean = sum(ref) / len(ref)
    spread = max(ref) - min(ref)
    tolerance = max(AC5_RELATIVE_TOLERANCE * abs(mean), AC5_SPREAD_FACTOR * spread)
    pct = 100.0 * tolerance / abs(mean) if mean else float("inf")
    got = arm["b"][metric]
    degraded = (got - mean) if worse == "higher" else (mean - got)
    detail = "arm=%.4f control mean=%.4f tolerance=%.4f (%.0f%% of the control mean)" % (got, mean, tolerance, pct)
    if pct > 100 * AC5_INCONCLUSIVE_ABOVE:
      checks.append(Check("B %s not degraded" % metric, False, "INCONCLUSIVE: " + detail))
    else:
      checks.append(Check("B %s not degraded" % metric, degraded <= tolerance, detail))
  return checks


# Product defaults the per-item completion bound is built from (not tuned here):
# the replay ReleaseAge (ESHU_SOURCE_BACKPRESSURE_RELEASE_AGE=15m in lib/arm.sh), the
# queue retry cap default (1 h, so min(cap, ReleaseAge) is the release age), the
# retry jitter fraction default 0.1 (runtime/retry_policy.go defaultRetryJitterFraction)
# and the projector claim poll interval (1 s, cmd/projector/runtime_wiring.go).
RELEASE_AGE_S = 900.0
RETRY_CAP_S = 3600.0
JITTER_FRACTION = 0.1
CLAIM_POLL_S = 1.0


def check_engagement(arm: dict[str, Any]) -> list[Check]:
  """The latch engaged once and released, read from the latch row and the binaries.

  One engaged interval, one distinct engaged_at, the row untouched after the
  release, the fleet-summed engaged counter exactly 1 and every process's active
  gauge 0 after release. The counter and gauge need the latch-write fix
  (drive/10882-latch-counter): before it every racing consult counts.
  """
  engaged = arm.get("engaged", {})
  gauges = engaged.get("gauge_final", [])
  return [
      Check("latch engaged and released (latch row times)",
            bool(engaged.get("engage_at")) and bool(engaged.get("release_at")),
            "engage=%s release=%s" % (engaged.get("engage_at"), engaged.get("release_at"))),
      Check("one engaged interval and one distinct engaged_at", not engaged.get("reengaged")
            and int(engaged.get("distinct_engagements", 0)) == 1,
            "reengaged=%s distinct engagements=%s" % (engaged.get("reengaged"), engaged.get("distinct_engagements"))),
      Check("latch row untouched after the release (updated_at at the second released sample == final dump)",
            engaged.get("updated_at_stable") is True, "stable=%s" % engaged.get("updated_at_stable")),
      Check("fleet-summed engaged counter == 1", float(engaged.get("engaged_counter", -1)) == 1.0,
            "engaged counter=%s" % engaged.get("engaged_counter")),
      Check("active gauge 0 in every final scrape", bool(gauges) and all(g == 0 for g in gauges), "gauges=%s" % gauges),
  ]


def check_item_timing(arm: dict[str, Any]) -> list[Check]:
  """No A item completes later than release + min(retry cap, ReleaseAge) x (1 + jitter
  fraction) + one claim poll interval; the numbers are reported per run."""
  items = arm.get("a_items", {})
  bound = min(RETRY_CAP_S, RELEASE_AGE_S) * (1 + JITTER_FRACTION) + CLAIM_POLL_S
  worst = items.get("max_after_release_s")
  return [Check("every A item completes within release + min(retry cap, ReleaseAge) x (1 + jitter) + one poll",
                worst is not None and worst <= bound,
                "bound=%.0f s; A items=%s max after release=%s s; paced items=%s between %s and %s s" % (
                    bound, items.get("items"), worst, items.get("paced_items"), items.get("paced_min_after_release_s"),
                    items.get("paced_max_after_release_s")))]


def check_zero_writes(arm: dict[str, Any]) -> list[Check]:
  """Absolute zero A writes while engaged, with the positive control after release."""
  writes = arm.get("writes", {})
  first, last, end = writes.get("first"), writes.get("last_engaged"), writes.get("end")
  if not first or not last or first["graph"] is None or last["graph"] is None:
    return [Check("zero A graph, content and projection writes while engaged", False,
                  "missing sampler rows (first=%s last_engaged=%s)" % (first, last))]
  delta = (last["graph"] - first["graph"], last["content"] - first["content"], last["succeeded"] - first["succeeded"])
  after = None if not end or end["graph"] is None else end["graph"] - last["graph"]
  return [
      Check("zero A graph, content and projection writes while engaged", delta == (0, 0, 0),
            "graph=%d content=%d succeeded=%d against the run's first sample" % delta),
      Check("positive control: A graph writes after release are above 0", after is not None and after > 0,
            "graph nodes after release=%s" % after),
  ]


def check_selection(arm: dict[str, Any]) -> list[Check]:
  """Git selection stretch, per pushed commit sha (arbiter 2.1)."""
  base = get(arm, "manifest.selection_base_s")
  if base is None:
    return []
  sel = arm.get("selection", {})
  engaged, after = sel.get("engaged", {}), sel.get("after_release", {})
  return [
      Check("selection stretched while engaged (every engaged commit later than the base interval)",
            engaged.get("missing", 1) == 0 and engaged.get("min_s") is not None and engaged["min_s"] > base,
            "min delay=%s s base=%s s missing=%s" % (engaged.get("min_s"), base, engaged.get("missing"))),
      Check("selection restored after release (every after-release commit within %.0f s)" % SELECTION_RESTORED_WITHIN_S,
            after.get("missing", 1) == 0 and after.get("max_s") is not None and after["max_s"] <= SELECTION_RESTORED_WITHIN_S,
            "max delay=%s s missing=%s" % (after.get("max_s"), after.get("missing"))),
  ]


def check_consult(arm: dict[str, Any], gate: bool) -> list[Check]:
  """Consult readings from the engaged scrape; the 100 ms gate on the bucket count."""
  a = get(arm, "consult.A", {})
  checks = [Check("consults ran on source A (engaged scrape)", float(a.get("count", 0)) > 0, "count=%s" % a.get("count"))]
  if gate:
    checks.append(Check("close gate: consult p50 within 100 ms (le=0.1 bucket share >= 50%)",
                        float(a.get("within_100ms", 0)) >= CONSULT_WITHIN_100MS_SHARE,
                        "share within 100 ms=%.2f p50=%.1f ms p95=%.1f ms (interpolated, reported)" % (
                            a.get("within_100ms", 0), 1000 * a.get("p50_s", 0), 1000 * a.get("p95_s", 0))))
  return checks


def check_control_positive(arm: dict[str, Any]) -> list[Check]:
  """C at t+120 s has A graph writes (the zero-write probe can see writes)."""
  writes = arm.get("writes", {})
  first, at120 = writes.get("first"), writes.get("at_t120")
  ok = bool(first and at120 and first["graph"] is not None and at120["graph"] is not None and at120["graph"] > first["graph"])
  return [Check("positive control: unpaced A graph nodes at t+120 s are above the first sample", ok,
                "first=%s at_t120=%s" % (None if not first else first["graph"], None if not at120 else at120["graph"]))]
