# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""Final-state comparison against the control, judged by shape (arbiter ruling).

The active generation of a fixture repository is not always comparable between
two arms: in the control (C) the later generations are deltas over a projected
full base; in an arm whose first generation was paced and superseded unprojected
there is no projected baseline, so its later generations are full re-observations.
A scope therefore has a shape, (active generation is a delta, sha of its projected
full base), and is judged against the reference whose shape it matches:

  same shape as C   -> the strict checks: active fact sets and both intent sets
  full at the final sha while C is a delta -> equal to CF (the reference arm that
                       ends with a full active generation at the final sha), C's
                       active intents a subset of the arm's, intent union equal to
                       C's, sha chain equal to C's
  neither            -> FAIL, with the rows

In every arm each full generation at sha1 equals C's strictly. There is no merged
"effective" set and no copy of the product's delta overlay here.
"""

from typing import Any

from checks_lib import MIN_CONTROL_REPS, Check, check_control_positive, check_hygiene, diff_maps, get


# The repository fact differs between a delta and a full generation in exactly these
# keys (collector/repo/git/content/envelopes.go); everything else must be equal.
DELTA_REPOSITORY_KEYS = ("delta_generation", "delta_relative_paths", "delta_deleted_relative_paths", "parsed_file_count")


def _shape(result: dict[str, Any], scope: str) -> tuple[bool, str] | None:
  info = get(result, "final.shapes", {}).get(scope)
  return None if info is None else (bool(info["is_delta"]), info["base_sha"])


def _scope_intents(result: dict[str, Any], scope: str, view: str) -> set[str]:
  out: set[str] = set()
  for keys in get(result, "final.intent_%s" % view, {}).values():
    out |= {k for k in keys if k.startswith(scope + ":")}
  return out


def _active_facts(result: dict[str, Any], scope: str) -> dict[str, str]:
  info = get(result, "final.shapes", {}).get(scope)
  if info is None:
    return {}
  return get(result, "final.generation_facts", {}).get(scope, {}).get(info["active_sha"], {})


def _eq(want: Any, got: Any) -> bool:
  return want == got


def check_shape_judged(arm: dict[str, Any], control: dict[str, Any], cf: dict[str, Any] | None) -> list[Check]:
  """Active facts, active intents, intent union and sha chain, judged by shape."""
  failures: dict[str, list[str]] = {"facts": [], "intents": [], "union": [], "chain": [], "shape": []}
  detail: list[str] = []
  for scope in sorted(get(control, "final.shapes", {})):
    a, c = _shape(arm, scope), _shape(control, scope)
    f = _shape(cf, scope) if cf else None
    if a is None:
      failures["shape"].append("%s: no active generation" % scope)
      continue
    chain_ok = get(arm, "final.shapes", {})[scope]["sha_chain"] == get(control, "final.shapes", {})[scope]["sha_chain"]
    if a == c:
      detail.append("%s: same shape as C (delta=%s)" % (scope[-8:], c[0]))
      if not _eq(_active_facts(control, scope), _active_facts(arm, scope)):
        failures["facts"].append(scope)
      if _scope_intents(control, scope, "active") != _scope_intents(arm, scope, "active"):
        failures["intents"].append(scope)
    elif cf is not None and f is not None and a == f and not a[0]:
      detail.append("%s: full at the final sha, matches CF (C is a delta)" % scope[-8:])
      if not _eq(_active_facts(cf, scope), _active_facts(arm, scope)):
        failures["facts"].append(scope)
      if not _scope_intents(control, scope, "active") <= _scope_intents(arm, scope, "active"):
        failures["intents"].append(scope)
      if not chain_ok:
        failures["chain"].append(scope)
    else:
      failures["shape"].append("%s: arm shape %s matches neither C %s nor CF %s" % (scope, a, c, f))
    if _scope_intents(control, scope, "union") != _scope_intents(arm, scope, "union"):
      failures["union"].append(scope)
  return [
      Check("every fixture scope matches the shape of C or of CF", not failures["shape"], "; ".join(failures["shape"]) or
            "; ".join(detail)),
      Check("active-generation fact sets equal the reference of the same shape (3-field normalisation)",
            not failures["facts"], "failing scopes=%s" % failures["facts"]),
      Check("active-generation intents equal C (same shape) or are a superset of C's (full at the final sha)",
            not failures["intents"], "failing scopes=%s" % failures["intents"]),
      Check("reducer intents (scope, domain) over every generation equal C's", not failures["union"],
            "failing scopes=%s" % failures["union"]),
      Check("sha chain equals C's where the arm is full at the final sha", not failures["chain"],
            "failing scopes=%s" % failures["chain"]),
  ]


def check_sha1_full(arm: dict[str, Any], control: dict[str, Any]) -> list[Check]:
  """Each full generation at sha1 equals C's sha1 full strictly (every arm)."""
  bad = []
  for scope, info in sorted(get(control, "final.shapes", {}).items()):
    sha1 = info["sha_chain"][0] if info["sha_chain"] else None
    want = get(control, "final.generation_facts", {}).get(scope, {}).get(sha1)
    got = get(arm, "final.generation_facts", {}).get(scope, {}).get(sha1)
    if want is None or got != want:
      bad.append(scope)
  return [Check("the sha1 full generation of every scope equals C's strictly", not bad, "failing scopes=%s" % bad)]


def check_graph_and_content(arm: dict[str, Any], control: dict[str, Any]) -> list[Check]:
  """Graph counts per label, relationship counts per type, content rows and identity."""
  checks = []
  for path, label in (("final.graph_counts", "graph counts per label"),
                      ("final.relationship_counts", "relationship counts per type"),
                      ("final.content_identity", "content identity sets (files by repo, path and content hash; entities by id)")):
    want, got = get(control, path, {}), get(arm, path, {})
    checks.append(Check(label, bool(want) and want == got, diff_maps(want, got) if want else "control has no %s" % path))
  want, got = get(control, "final.content_rows"), get(arm, "final.content_rows")
  checks.append(Check("content rows", want is not None and want == got, "control=%s arm=%s" % (want, got)))
  checks.append(Check("committed generation set (source commit sha)",
                      bool(get(control, "final.generations")) and get(control, "final.generations") == get(arm, "final.generations"),
                      diff_maps(get(control, "final.generations", {}), get(arm, "final.generations", {}))))
  return checks


def check_final_state(arm: dict[str, Any], control: dict[str, Any], cf: dict[str, Any] | None = None) -> list[Check]:
  """Final committed state against the control, judged by shape."""
  return (check_shape_judged(arm, control, cf) + check_sha1_full(arm, control) + check_graph_and_content(arm, control))


def check_cf_reference(control: dict[str, Any], cf: dict[str, Any]) -> list[Check]:
  """CF against C: C's active delta rows are present in CF with equal payload, and the
  repository fact differs only in the delta keys (printed)."""
  missing, unequal, repo_notes, repo_bad = [], [], [], []
  for scope, info in sorted(get(control, "final.shapes", {}).items()):
    f = get(cf, "final.shapes", {}).get(scope)
    if f is None or f["is_delta"]:
      missing.append("%s: CF has no full active generation" % scope)
      continue
    c_rows = get(control, "final.generation_facts", {}).get(scope, {}).get(info["active_sha"], {})
    f_rows = get(cf, "final.generation_facts", {}).get(scope, {}).get(f["active_sha"], {})
    for key, digest in c_rows.items():
      if key.startswith("repository:"):
        continue
      if key not in f_rows:
        missing.append("%s %s" % (scope[-8:], key))
      elif f_rows[key] != digest:
        unequal.append("%s %s" % (scope[-8:], key))
    c_repo = get(control, "final.repository_payloads", {}).get(scope, {}).get(info["active_sha"], {})
    f_repo = get(cf, "final.repository_payloads", {}).get(scope, {}).get(f["active_sha"], {})
    differing = sorted(k for k in set(c_repo) | set(f_repo) if c_repo.get(k) != f_repo.get(k))
    repo_notes.append("%s: %s" % (scope[-8:], differing))
    repo_bad += ["%s %s" % (scope[-8:], k) for k in differing if k not in DELTA_REPOSITORY_KEYS]
  return [
      Check("C's active delta rows are present in CF with equal payload", not missing and not unequal,
            "missing=%s unequal=%s" % (missing[:5], unequal[:5])),
      Check("the repository fact differs between C and CF only in the delta keys", not repo_bad,
            "differing keys per scope (printed): %s; not allowed=%s" % ("; ".join(repo_notes), repo_bad[:5])),
  ]


def check_control_set(controls: list[dict[str, Any]]) -> list[Check]:
  """At least three control repetitions, equal to each other in final state."""
  checks = [Check("at least %d control repetitions" % MIN_CONTROL_REPS, len(controls) >= MIN_CONTROL_REPS,
                  "have %d" % len(controls))]
  for index, control in enumerate(controls, start=1):
    for check in check_hygiene(control) + check_control_positive(control):
      checks.append(Check("control rep %d: %s" % (index, check.name), check.passed, check.detail))
  for index, other in enumerate(controls[1:], start=2):
    for check in check_final_state(other, controls[0]):
      checks.append(Check("control rep %d vs rep 1: %s" % (index, check.name), check.passed, check.detail))
  return checks
