# Cutover Checklist DRAFT — #4047 gate for #8857

DRAFT ONLY. No claim is stopped, no lease drained, no grant revoked, and no
generation replayed by this file. Each step records its command, observed
result, and exit code before the next step starts. Production acceptance,
deployment, and rollback remain NOT_CHECKED.

Preconditions: producer-grant manifest issued (#6709), standalone release
built from supported public contracts (#6710, #6708 SDK + #8851 physical
relocation), version-pair matrix filled (`version-matrix.sh`), mirror tests
green on both the in-tree path (this worktree) and the extracted path.

## 1. Stop claims on the replaced (in-tree) producer

- Quiesce the `grafana` collector instance: disable or delete its desired
  instance so the coordinator plans no new work items.
- Confirm no new `generation_id` is issued for
  `scope.CollectorGrafana` after the stop timestamp.
- Evidence: workflow work-item list filtered to the collector kind shows
  zero `pending`/`claimed` rows newer than the stop timestamp.

## 2. Drain or expire leases

- Wait out `ClaimLeaseTTL` (or expire leases explicitly) so no in-flight
  claim can still commit after the fence. Heartbeats stop with the
  quiesced instance; `HeartbeatInterval` stays below the lease TTL.
- Confirm in-flight work items reach terminal states (`completed`,
  `failed_*`, expired) with the in-tree producer identity only.
- Evidence: lease table shows no live lease for the in-tree
  `collector_instance_id`.

## 3. Revoke replaced authority

- `eshu component revoke-grant` every in-tree-held grant row for the 4
  `observability.*` grafana kinds at scope `grafana`.
- Confirm `eshu component grants --producer <in-tree>` reports each grant
  `revoked: true`, and that a trial emission under the revoked grant
  fails closed (`InvalidResult`, no facts committed).
- No-dual-authority gate: at no point may the in-tree and extracted
  producers both hold a live grant for the same kind+scope. Revocation
  of the replaced producer precedes activation of the replacement.

## 4. Replay the committed generation

- Activate the extracted producer (`grant-first-then-install`, then
  enable) and replay the last committed in-tree generation through the
  extracted path (cassette replay where credentials are absent, live
  fetch where present).
- Evidence: extracted-path envelopes preserve admitted kinds, stable
  keys, source scope, generations, fencing tokens, `reported`
  confidence, provenance, and redaction per the mirror tests
  (`extraction_prep_mirror_test.go` in both trees).

## 5. Verify exact truth

- Reducer: `obscoverage` projection from extracted-path facts matches
  the in-tree projection fact-for-fact (canonical graph truth).
- Query: HTTP API observability reads return identical rows for the
  replayed generation.
- MCP: observability read tools return identical truth.
- Concurrent safety: retry, duplicate delivery, ordering, cancellation,
  revoked authority mid-execution, stale claims, and partial/ambiguous
  failures show no lost facts and no dual commits.
- Rollback rehearsal: stop extracted claims, drain leases, revoke the
  extracted grant, restore the supported in-tree producer, replay the
  committed generation, re-verify exact truth — all without simultaneous
  authority.
- Only after all of the above plus independent review and stable
  exact-head CI may the superseded in-tree implementation be removed.
