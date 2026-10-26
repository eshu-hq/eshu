# Rollback DRAFT (`#8860`, `#4047` cutover)

**DRAFT — rehearsal plan, not an executed runbook.** Authority for a source
scope and generation must rest with exactly one producer at a time.

## Trigger

Any cutover failure: parity mismatch, admission errors, projection/graph
divergence, missing telemetry, or expired/revoked grant blocking emission.

## Steps

### 1. Stop new external claims

Halt the claim dispatcher targets for `collector_kind = loki`
so no new `Claim` reaches the external producer. In-flight work items keep
their leases; do not delete them.

### 2. Drain or expire in-flight leases

Wait for in-flight external results to land, or let the lease TTL expire and
allow the control store to release them. Confirm zero `claimed` work items
for `loki` before proceeding — the workflow control store
(`postgres.NewWorkflowControlStore`, `service.go:76`) is the source of
truth, not the producer's logs.

### 3. Revoke the external grant

```bash
for kind in source_instance observed_log_signal observed_rule coverage_warning; do
  eshu component revoke-grant dev.eshu.collector.loki \
    --component-home ~/.eshu/components \
    --version 0.1.0 \
    --kind "observability.${kind}" \
    --scope loki
done
```

Revocation fails the next external emission closed, so a stray retry cannot
re-establish dual authority.

### 4. Restore the in-tree producer

Reactivate the in-tree `collector-loki` deployment (claimed-live
or `-mode=cassette` for diagnosis) with the last supported configuration.
In-tree needs no grant: it commits through `postgres.NewIngestionStore`
(`service.go:42,72`) under core admission.

### 5. Replay from the last committed generation

Resume claiming from the last generation the core committed — never from the
external producer's last attempt. Generation IDs are reused on retry
(`source.go:85-107`), so replay is idempotent; duplicate delivery is safe by
stable key (`envelope.go:329`).

### 6. Verify no dual authority, no lost facts

- Exactly one producer holds each scope: query claimed work items and recent
  committed generations for `collector_kind = loki`; every
  generation's `collector_instance_id` must name the restored producer.
- Fact parity: committed kind counts per scope equal the pre-cutover
  baseline; canonical projection and graph truth agree
  (`projector/observability/coverage/correlation_intents.go`,
  `factschema_decode_aws.go`).
- Read truth: `GET /api/v0/observability/coverage/correlations` returns the
  baseline rows.
- Telemetry: `eshu_dp_loki_*` counters resume from the in-tree
  binary; the grant-denial path is quiet (no external emissions attempted).

## Rehearsal requirement

Rollback is proven only by rehearsal: cut over a non-production scope,
revoke, restore, replay, and verify the checks above before production
cutover. Record the rehearsal evidence on the cutover issue.
