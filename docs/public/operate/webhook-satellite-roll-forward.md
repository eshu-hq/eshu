<!-- docs-catalog
title: Roll forward a webhook satellite
description: Rebuilds the webhook satellite or the core webhook intake host forward at the pinned digest after the provider endpoint moves to the satellite, with the Postgres trigger tables as the accepted-delivery source of truth.
type: operate
audience: operator
entrypoint: false
landing: false
-->

# Roll forward a webhook satellite

Use this page when the extracted webhook satellite (`eshu-hq/eshu-webhook`) is
the provider-facing runtime and an outage needs recovery. The only supported
recovery after that point is to rebuild forward. Eshu does not rehearse
downgrades for this boundary; the owner policy is roll-forward plus rebuild
(`#8876`, owner ruling of 2026-10-30).

## Where this applies today

The provider endpoint has not moved. At the time of writing, on the
owner-designated QA environment (the issue `#8876` and the deployment
repository hold the current identifiers):

- The in-tree listener (`go/cmd/webhook-listener/`, `eshu-webhook-listener`)
  is still the provider-facing authority. The provider hostname is an
  HTTPRoute on the public gateway that routes to the listener Service.
- The satellite runs as a ClusterIP Deployment with no ingress or gateway
  route. It verifies provider signatures, normalizes, and submits to the core
  intake host (`go/cmd/webhook-intake`) through the intake host's ClusterIP
  Service.
- The core intake host serves `POST /api/v1/webhook/intake/{repository,
  incident,aws,gcp,actions}` with Bearer auth. It owns `EnsureSchema` for the
  five trigger tables and serves no provider routes.

Until an authority switch moves the provider endpoint to the satellite, the
pre-cutover recovery is the stateless stop and restore of the satellite while
the listener keeps authority. This page then does not apply. The authority
switch and the removal of the in-tree listener have not happened; this page is
the post-switch procedure.

The shared intake wire contract in `go/internal/webhook/intake/` stays in
tree. The intake host reuses it unchanged, so removing the listener must not
remove it.

## Preconditions

- The provider endpoint already points at the satellite.
- The satellite and the intake host hold no durable state of their own. Every
  accepted delivery is a row in the deployment Postgres; the satellite
  performs no SQL.
- Restore the identical image digest through the deployment repository that
  owns the Deployments. Never change the digest, Service type, or ingress
  during recovery.

## Source of truth for accepted deliveries

The five trigger tables in the deployment Postgres record what was accepted:
`webhook_refresh_triggers`, `cicd_actions_deliveries`,
`incident_freshness_triggers`, `aws_freshness_triggers`, and
`gcp_freshness_triggers`. A delivery with no row was never accepted.

Duplicates converge by the table's own key, not by a single delivery ID:

- Repository triggers upsert on `refresh_key`, which bumps `duplicate_count`
  and keeps one row.
- The incident, AWS, and GCP tables upsert on `freshness_key`.
- Actions deliveries key on `delivery_id`. An exact replay returns 202 with
  the original row intact. A different identity under the same delivery ID
  returns 409 and keeps the first row.

Pre-cutover parity proved these on the satellite-to-intake-host uplink, 16/16
with residue 0/0 (the run record is on `#8876`).

## Recover a lost delivery

A delivery that failed while the satellite or intake host was down has no row.
Do not insert rows or POST to the intake API by hand.

- GitHub does not automatically redeliver failed deliveries. The GitHub
  documentation says "GitHub does not automatically redeliver failed
  deliveries", and an operator can redeliver "webhook deliveries that occurred
  in the past 3 days" manually from the provider (GitHub.com; GitHub
  Enterprise Server keeps 7). Other providers differ, so check each one.
- Redelivery is safe because the keys above are idempotent.
- The `go/cmd/webhook-listener/` README states polling remains the
  authoritative backfill path for missed, dropped, or rejected deliveries.
  That README is deleted with the listener, so re-home this statement into the
  runtime docs in the same change that removes it. NOT_MEASURED: the
  worst-case freshness gap this leaves for each provider family.

## Procedure

1. Scope the outage with four probes.
   - Satellite `/healthz` failing: the satellite itself.
   - Satellite `/readyz` failing while its `/healthz` is 200: the satellite
     cannot reach the intake host's `/healthz`, which is a liveness check
     only. The intake host process is down or unreachable.
   - Intake host `/healthz` 200 but intake host `/readyz` failing: Postgres or
     the status schema. The intake host's `/readyz` carries the Postgres
     probe. In this state the satellite stays ready and keeps accepting
     provider requests, but intake submissions fail with 500, so providers see
     non-2xx. Fix Postgres first; rebuilding the satellite does not help.
   - Intake host `/healthz` failing: the intake host process.
2. Rebuild the failed Deployment forward through the deployment repository,
   at the identical pinned image digest, ClusterIP only, no ingress. Record
   the applied commit and the digest. A restarted intake host re-runs
   `EnsureSchema` before it serves; the DDL is idempotent.
3. Wait for rollout, then check `/healthz` and `/readyz` return 200 on both
   Deployments.
4. Re-prove the uplink with the 8-case parity run. The live script is not in
   the repository. The lane that ran it supplied `/tmp/full-parity-8876.py`
   on an operations runner (`#8876`, step-(e) comment), and it needs an owner
   grant for synthetic writes. The cases are: c1 valid push accepted with one
   row, c2 bad signature 401 with no row, c3 duplicate coalesces to one row
   with `duplicate_count` 1, c4 oversized body 413 with no row, c5 outage then
   retry heals to one row, c6 three queued rows claim in FIFO order, c7 intake
   auth, checked against the intake API itself (no token 401, bad token 401,
   valid token not 401; at the provider edge a wrong listener-to-core token
   surfaces as 500 so the provider redelivers, which the hermetic test
   `TestIntakeParityIntakeAuthFailureSurfaces500` proves), c8 Actions exact
   replay 202 twice and a conflicting identity 409 with one row. The pass bar
   was 16/16 with cleanup residue 0/0. The hermetic equivalent that lives in
   the repo is `go/cmd/webhook-listener/intake_parity_cases_test.go`.
   NOT_MEASURED: a run after the provider endpoint moves, because the live
   script also compared against an in-tree listener baseline that no longer
   exists then. Confirm the script's post-removal pass bar with the owner
   before relying on this step.
5. Reconcile: full-table row counts before and after must differ only by live
   provider traffic. The rehearsal run namespaced its synthetic rows as
   `parity-<run timestamp>-*`; delete any row left behind under the run's
   prefix and re-check until 0/0. The convention comes from the `#8876`
   rehearsal comment, not from the repository.

Rolling forward to a new digest, instead of restoring the pinned one, needs a
fresh parity run and an owner grant. This page has no branch for an outage
caused by the pinned image itself.

## Recovery time

NOT_MEASURED. No rebuild of the post-cutover topology has been timed.

The only timed sample is from the pre-cutover delete-path rehearsal (`#8876`):
the restore took 10 minutes 54 seconds, from the deployment merge to the
Deployment synced at 1/1, before any parity run. It is one sample of a
different path (satellite scaled to zero while the listener kept authority).
Time a real post-cutover rebuild and record it before you rely on any bound.

## When roll-forward fails

Do not improvise a downgrade. A red step 4 voids acceptance of the rebuilt
revision. File the outcome against `#9665`, the revival follow-up that owns
restoring the in-tree listener and switching the provider endpoint back. That
procedure is not written yet; until `#9665` is worked, a failed roll-forward
is an open incident, not a completed recovery.

## Evidence to record

Record the restored digests and applied commit, the rollout result, the
parity run tag with its 16/16 line, the cleanup delete counts with 0/0
residue, and the before and after full-table counts with the live-traffic
delta named.

## Related

- [Collector cutover rollback](collector-cutover-rollback.md) for in-tree
  collector hand-back.
- [Service runtimes](../deployment/service-runtimes-core.md) for the listener
  and intake host rows.
