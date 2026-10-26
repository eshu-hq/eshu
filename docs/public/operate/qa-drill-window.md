<!-- docs-catalog
title: QA drill window
description: Rehearses the standing cutover checklist on a QA environment so the first real cutover is not the first run.
type: operate
audience: operator
entrypoint: false
landing: false
-->

# QA drill window

Use this page to rehearse a cutover before the real one. A standing QA drill
window runs the same fixed cutover checklist every time a QA environment takes
a new image: render, sync, bootstrap order, fleet health, collector health,
then a forward-only rollback drill. The drill proves the procedure and the
evidence chain while the blast radius is small. The environment-specific runbook
with exact pins, digests, and sync commands lives in your GitOps deployment
repository next to the cutover procedure it rehearses.

Doctrine first: **forward-only + forward-fix.** Once a graph schema fingerprint
moves non-additively with an empty compatible list, a live pin-back to a
pre-migration image is not a supported escape — writers on the old schema
crash-loop, and the mirror fails by the same gate. Rollback means shipping a
fix forward. Revisit only with a real graph backup/restore story; keep the
pin-back steps as that future's drill script.

## Cadence and time box

- **Cadence:** every pin change to the QA environment, plus a standing calendar
  window. Name the first window explicitly; do not let the first drill coincide
  with the first risky cutover.
- **Time box (~2 h):** sync at T+0, evidence close after the first two
  coordinator plan windows (about 60 minutes where the reconcile interval is 30
  minutes), then a 30-minute rollback-rehearsal window. A collector that claims
  once per plan window cannot be proven healthy on a shorter window.

## Roles

Name a drill owner (calls GO/NO-GO at each gate, posts the rehearsal note), a
pin author (records image tag, digest, and release range), a bootstrap operator
(renders, reviews, creates, and watches the manual schema/bootstrap Job), a
collector prover (runs collector health and readback checks, captures
baselines), and the owner of the rehearsal-firing alert rule.

## Entry criteria

All must hold, else NO-GO:

1. The pin change is merged and the effective image pin is recorded.
2. The release range has been read for both schema axes: new numbered Postgres
   migration files, and the expected graph fingerprint at both ends versus the
   live marker.
3. The window is on the calendar, owners are named, and the rehearsal-firing
   rule is armed.
4. Truth baselines can be captured immediately pre-sync. A baseline from an
   earlier cutover day is stale — sources keep moving. Capture fresh baselines
   as the [truth checks page](collector-cutover-truth-checks.md#compare-against-a-baseline-captured-just-before-step-1)
   describes.

## Fixed cutover checklist

1. **Render pins (pre-sync, Git only).** Render what the deployer would sync
   with the real values, and confirm only workload image references change:
   reader refs, the schema-bootstrap hold, and the auto-sync setting stay
   intact. List what the range adds to the numbered migrations. Any unexpected
   render delta or unread range is NO-GO.
2. **Sync the pin change.** Sync the reviewed merge SHA selectively, with
   pruning. The sync applies Deployments and StatefulSets; it creates no
   schema Job. Operation not Succeeded means stop and diagnose before any
   bootstrap.
3. **Manual bootstrap order (bootstrap Job BEFORE writers).** The automated
   sync does not sequence graph writers behind schema bootstrap: writers that
   roll first crash-loop on the fingerprint mismatch. Render the chart's own
   schema-bootstrap Job as an unmanaged one-shot, review it, create it, and
   wait for Succeeded before restarting the rolled writers. Select the Job by
   its component label, not by a fixed name suffix. Confirm the writers report
   ready with zero restarts and no fingerprint errors in fresh logs.
4. **Fleet health.** Confirm the application is Synced and Healthy, every
   workload runs the new image digest with no crash-looping pods, claims stay
   bound, and the ingester workspace claim is untouched. Assert nonzero
   `spec.replicas` and `status.readyReplicas` for every restored producer:
   rollout status reports success at zero replicas, so a green rollout alone
   proves nothing.
5. **Collector health.** Check each extracted collector the same way the
   cutover procedure does: reconcile-container logs, no duplicate collector
   instance on the coordinator, enabled and claims-enabled flags set, fact
   counts and claim cadence at baseline, item-level write proof after a live
   source edit, and one real API plus one real MCP readback. The
   [collector cutover rollback runbook](collector-cutover-rollback.md) holds
   the verify and capture commands.
6. **Forward-only rollback drill (no live pin-back).** Do not sync the old
   pin. Run the rollback script in plan mode per collector kind — it prints the
   steps with exact commands and runs nothing — then tabletop the forward-fix
   path: what ships forward for a writer crash-loop. Record exit-code literacy
   for the script: 0 done, 2 usage error, 3 step failed, 4 refused, 5
   drain/replay timeout, 6 truth differs, 7 dual authority.
7. **Post the rehearsal note.** Within 24 hours, post sync and bootstrap
   receipts with SHAs and timestamps, the health census, the rollback-drill
   outcome, and the open threads filed and linked.

## Related

- [Collector cutover rollback](collector-cutover-rollback.md)
- [Collector cutover truth checks](collector-cutover-truth-checks.md)
- [Upgrade and rollback](../deploy/kubernetes/upgrades-rollbacks.md)
