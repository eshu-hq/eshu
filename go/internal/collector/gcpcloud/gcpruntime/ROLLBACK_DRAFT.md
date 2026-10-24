# Rollback DRAFT (`#8856`, `#4047` cutover)

**DRAFT — rehearsal plan, not an executed runbook.** Authority for a source
scope and generation must rest with exactly one producer at a time.

## Trigger

Any cutover failure: parity mismatch, admission errors, projection/graph
divergence, missing telemetry, or expired/revoked grant blocking emission.

## Steps

### 1. Stop new external claims

Halt the claim dispatcher targets for `collector_kind = gcp` so no new
`Claim` reaches the external producer. In-flight work items keep their
leases; do not delete them. (Mirror `gcp_iam_*` facts emitted under
`secrets_iam_posture` stamping halt with the same dispatcher stop once the
grant-decision lands; until then both kind sets are listed in the stop
filter.)

### 2. Drain or expire in-flight leases

Wait for in-flight external results to land, or let the lease TTL expire and
allow the control store to release them. Confirm zero `claimed` work items
for `gcp` before proceeding — the workflow control store
(`postgres.NewWorkflowControlStore`, `service.go:102`) is the source of
truth, not the producer's logs.

### 3. Revoke the external grant

```bash
for kind in cloud_resource cloud_relationship tag_observation \
    iam_policy_observation dns_record image_reference collection_warning; do
  eshu component revoke-grant dev.eshu.collector.gcp-cloud \
    --component-home ~/.eshu/components \
    --version 0.1.0 \
    --kind "gcp_${kind}" \
    --scope gcp
done
eshu component revoke-grant dev.eshu.collector.gcp-cloud \
  --component-home ~/.eshu/components \
  --version 0.1.0 \
  --kind gcp_cloud_resource \
  --scope gcp
# Plus one revocation per mirror kind from the grant draft.
```

Revocation fails the next external emission closed, so a stray retry cannot
re-establish dual authority.

### 4. Restore the in-tree producer

Reactivate the in-tree `collector-gcp-cloud` deployment (fixture,
claimed-live, or `-mode=cassette` for diagnosis) with the last supported
configuration. In-tree needs no grant: it commits through
`postgres.NewIngestionStore` (`service.go:62,98`) under core admission.

### 5. Replay from the last committed generation

Resume polling/claiming from the last generation the core committed — never
from the external producer's last attempt. Generation IDs are reused on
retry (`claimed_source.go:20-33`), so replay is idempotent; duplicate
delivery is safe by stable key (`generation.go:416-422`, envelope stable
keys). Fencing tokens advance monotonically per scope
(`GenerationTracker`); a replayed lower token yields a single stale warning,
never an overwrite.

### 6. Verify no dual authority, no lost facts

- Exactly one producer holds each scope: query claimed work items and recent
  committed generations for `collector_kind = gcp`; every generation's
  `collector_instance_id` must name the restored producer.
- Fact parity: committed kind counts per scope equal the pre-cutover
  baseline; canonical projection and graph truth agree
  (`projector/cloud/gcp/resource_materialization_intents.go`,
  `projector/cloud/inventory/reducer_intent.go`,
  `reducer/gcp_relationship_materialization.go`).
- Read truth: `GET /api/v0/cloud/inventory` and
  `GET /api/v0/secrets-iam/posture-summary` return the baseline rows.
- Telemetry: `eshu_dp_gcp_cloud_*` counters resume from the in-tree
  binary; the grant-denial path is quiet (no external emissions attempted).

## Rehearsal requirement

Rollback is proven only by rehearsal: cut over a non-production scope,
revoke, restore, replay, and verify the checks above before production
cutover. Record the rehearsal evidence on the cutover issue.
