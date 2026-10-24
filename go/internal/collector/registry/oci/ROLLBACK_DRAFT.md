# Rollback DRAFT (`#8861`, `#4047` cutover)

**DRAFT — rehearsal plan, not an executed runbook.** Authority for a source
scope and generation must rest with exactly one producer at a time.

## Trigger

Any cutover failure: parity mismatch, admission errors, projection/graph
divergence, missing telemetry, or expired/revoked grant blocking emission.

## Steps

### 1. Stop new external claims

Halt the claim dispatcher targets for `collector_kind = oci_registry` so no
new `Claim` reaches the external producer. In-flight work items keep their
leases; do not delete them.

### 2. Drain or expire in-flight leases

Wait for in-flight external results to land, or let the lease TTL expire and
allow the control store to release them. Confirm zero `claimed` work items
for `oci_registry` before proceeding — the workflow control store
(`postgres.NewWorkflowControlStore`, `service.go:114`) is the source of
truth, not the producer's logs.

### 3. Revoke the external grant

```bash
for kind in repository image_tag_observation image_manifest image_index \
    image_descriptor image_referrer warning; do
  eshu component revoke-grant dev.eshu.collector.oci-registry \
    --component-home ~/.eshu/components \
    --version 0.1.0 \
    --kind "oci_registry.${kind}" \
    --scope oci_registry
done
```

Revocation fails the next external emission closed, so a stray retry cannot
re-establish dual authority.

### 4. Restore the in-tree producer

Reactivate the in-tree `collector-oci-registry` deployment (live or
`-mode=cassette` for diagnosis) with the last supported configuration.
In-tree needs no grant: it commits through `postgres.NewIngestionStore`
(`service.go:54,80,110`) under core admission.

### 5. Replay from the last committed generation

Resume polling/claiming from the last generation the core committed — never
from the external producer's last attempt. Generation IDs are reused on
retry (`claimed_source.go:25-60`), so replay is idempotent; duplicate
delivery is safe by stable key (`identity.go:17-61`).

### 6. Verify no dual authority, no lost facts

- Exactly one producer holds each scope: query claimed work items and recent
  committed generations for `collector_kind = oci_registry`; every
  generation's `collector_instance_id` must name the restored producer.
- Fact parity: committed kind counts per scope equal the pre-cutover
  baseline; canonical projection and graph truth agree
  (`projector/canonical/oci_registry.go`, `projector/decode/oci_registry.go`).
- Read truth: `GET /api/v0/images` and container-image query surfaces return
  the baseline rows.
- Telemetry: `eshu_dp_oci_registry_*` counters resume from the in-tree
  binary; the grant-denial path is quiet (no external emissions attempted).

## Rehearsal requirement

Rollback is proven only by rehearsal: cut over a non-production scope,
revoke, restore, replay, and verify the checks above before production
cutover. Record the rehearsal evidence on the cutover issue.
