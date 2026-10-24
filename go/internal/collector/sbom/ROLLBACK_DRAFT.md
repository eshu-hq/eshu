# Rollback DRAFT (`#8865`, `#4047` cutover)

**DRAFT — rehearsal plan, not an executed runbook.** Authority for a source
scope and generation must rest with exactly one producer at a time.

## Trigger

Any cutover failure: parity mismatch, admission errors, projection/graph
divergence, missing telemetry, or expired/revoked grant blocking emission.

## Steps

### 1. Stop new external claims

Halt the claim dispatcher targets for `collector_kind = sbom_attestation` so
no new `Claim` reaches the external producer. In-flight work items keep their
leases; do not delete them.

### 2. Drain or expire in-flight leases

Wait for in-flight external results to land, or let the lease TTL expire and
allow the control store to release them. Confirm zero `claimed` work items
for `sbom_attestation` before proceeding — the workflow control store
(`postgres.NewWorkflowControlStore`, `service.go:80`) is the source of
truth, not the producer's logs.

### 3. Revoke the external grant

```bash
for kind in document component dependency_relationship external_reference warning; do
  eshu component revoke-grant dev.eshu.collector.sbom-attestation \
    --component-home ~/.eshu/components \
    --version 0.1.0 \
    --kind "sbom.${kind}" \
    --scope sbom_attestation
done
for kind in statement slsa_provenance signature_verification; do
  eshu component revoke-grant dev.eshu.collector.sbom-attestation \
    --component-home ~/.eshu/components \
    --version 0.1.0 \
    --kind "attestation.${kind}" \
    --scope sbom_attestation
done
```

Revocation fails the next external emission closed, so a stray retry cannot
re-establish dual authority.

### 4. Restore the in-tree producer

Reactivate the in-tree `collector-sbom-attestation` deployment
(claimed-live, or `-mode=cassette` with a recorded generation for diagnosis)
with the last supported configuration. In-tree needs no grant: it commits
through `postgres.NewIngestionStore` (`service.go:47,76`) under core
admission.

### 5. Replay from the last committed generation

Resume claiming from the last generation the core committed — never from the
external producer's last attempt. Generation IDs are reused on retry
(`runtime/source.go:117-128`), so replay is idempotent; duplicate delivery
is safe by stable key (pinned by
`TestSBOM8865ParityDuplicateDeliveryIsStable`).

### 6. Verify no dual authority, no lost facts

- Exactly one producer holds each scope: query claimed work items and recent
  committed generations for `collector_kind = sbom_attestation`; every
  generation's `collector_instance_id` must name the restored producer.
- Fact parity: committed kind counts per scope equal the pre-cutover
  baseline across all 8 kinds (both `sbom_document` and `sbom_attestation`
  collector-kind values); attachment reducer and graph truth agree
  (`reducer/sbomattest`, `projector/sbom/attestation`).
- Read truth: `GET /api/v0/supply-chain/sbom-attestations/attachments` (and
  `/count`, `/inventory`) return the baseline rows.
- Telemetry: workflow failure class/details resume from the in-tree binary;
  the grant-denial path is quiet (no external emissions attempted).

## Rehearsal requirement

Rollback is proven only by rehearsal: cut over a non-production scope,
revoke, restore, replay, and verify the checks above before production
cutover. Record the rehearsal evidence on the cutover issue.
