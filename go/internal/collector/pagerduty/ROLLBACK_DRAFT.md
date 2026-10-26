# Rollback DRAFT (`#8863`, `#4047` cutover)

**DRAFT — rehearsal plan, not an executed runbook.** Authority for a
source scope and generation must rest with exactly one producer at a
time. The shared rollback script and runbook live under `#8895`; below
is the PagerDuty binding of its six steps.

## Trigger

Any cutover failure: parity mismatch, admission errors,
projection/graph divergence, missing telemetry, or expired/revoked grant
blocking emission.

## Steps

### 1. Stop new external claims

Halt the claim dispatcher targets for `collector_kind = pagerduty` so
no new `Claim` reaches the external producer. In-flight work items keep
their leases; do not delete them.

### 2. Drain or expire in-flight leases

Wait for in-flight external results to land, or let the lease TTL
expire and allow the control store to release them. Confirm zero
`claimed` work items for `pagerduty` before proceeding — the workflow
control store is the source of truth, not the producer's logs.

### 3. Revoke the external grant

```bash
for kind in incident.record incident.lifecycle_event change.record; do
  eshu component revoke-grant dev.eshu.collector.pagerduty \
    --component-home ~/.eshu/components \
    --version 0.1.0 \
    --kind "${kind}" \
    --scope pagerduty
done
for kind in coverage_warning observed_pagerduty_service observed_pagerduty_integration; do
  eshu component revoke-grant dev.eshu.collector.pagerduty \
    --component-home ~/.eshu/components \
    --version 0.1.0 \
    --kind "incident_routing.${kind}" \
    --scope pagerduty
done
```

Revocation fails the next external emission closed, so a stray retry
cannot re-establish dual authority.

### 4. Restore the in-tree producer

Reactivate the in-tree `collector-pagerduty` deployment (claimed-live
or `-mode=cassette` for diagnosis) with the last supported
configuration, and re-enable the in-tree producer in values (reverse of
the cutover values change). In-tree needs no grant: it commits through
the claimed-service ingestion store under core admission.

### 5. Replay from the last committed generation

Resume claiming from the last generation the core committed — never from
the external producer's last attempt. Generation IDs are reused on retry
(`source.go:89`), so replay is idempotent; duplicate delivery is safe by
stable key (`envelope.go:198`, `providerStableKey`).

### 6. Verify no dual authority, no lost facts

- Exactly one producer holds each scope: query claimed work items and
  recent committed generations for `collector_kind = pagerduty`; every
  generation's `collector_instance_id` must name the restored producer.
- Fact parity: committed kind counts per scope equal the pre-cutover
  baseline for all 6 kinds; canonical projection and graph truth agree
  (`go/internal/projector/incident/routing/`,
  `incident_routing_materialization`).
- Read truth: `GET /api/v0/incidents/{incident_id}/context` returns the
  baseline rows.
- Telemetry: `eshu_dp_pagerduty_*` counters resume from the in-tree
  binary; the grant-denial path is quiet (no external emissions
  attempted).

## Rehearsal requirement

Rollback is proven only by rehearsal: cut over a non-production scope,
revoke, restore, replay, and verify the checks above before production
cutover. Record the rehearsal evidence on the cutover issue. Cutover
order holds for rollback too: PagerDuty restores independently of Jira
(`#8858`), but a joint incident re-verifies both activations before
either is re-attempted.
