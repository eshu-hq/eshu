# Rollback DRAFT (`#8868`, `#4047` cutover)

**DRAFT — rehearsal plan, not an executed runbook.** Authority for a
source scope and generation must rest with exactly one producer at a
time.

## Trigger

Any cutover failure: parity mismatch, admission errors, projection or
graph divergence, missing telemetry, or expired/revoked grant blocking
emission.

## Steps

### 1. Stop new external claims

Halt the claim dispatcher targets for `collector_kind = terraform_state`
so no new `Claim` reaches the external producer. In-flight work items
keep their leases; do not delete them.

### 2. Drain or expire in-flight leases

Wait for in-flight external results to land, or let the lease TTL
expire and allow the control store to release them. Confirm zero
`claimed` work items for `terraform_state` before proceeding — the
workflow control store (`postgres.NewWorkflowControlStore`,
`service.go:115`) is the source of truth, not the producer's logs.

### 3. Revoke the external grant

```bash
for kind in snapshot resource output module provider_binding tag_observation warning; do
  eshu component revoke-grant dev.eshu.collector.terraform-state \
    --component-home ~/.eshu/components \
    --version 0.1.0 \
    --kind "terraform_state.${kind}" \
    --scope terraform_state
done
```

Revocation fails the next external emission closed, so a stray retry
cannot re-establish dual authority.

### 4. Restore the in-tree producer

Reactivate the in-tree `collector-terraform-state` deployment
(claimed-live or `-mode=cassette` for diagnosis) with the last supported
configuration. In-tree needs no grant: it commits through
`postgres.NewIngestionStore` (`service.go:84`) under core admission.
No watermark or cursor needs warming: re-resolution, prior snapshots,
backend facts, and git readiness are core-side (`service.go:93-99`), so
collection resumes without a cold restart. If the last committed
generation is an attested empty successor, restore with
`allow_empty_successor` attested (config or
`ESHU_TFSTATE_ALLOW_EMPTY_SUCCESSOR`); otherwise the store's
successor-emptiness backstop correctly refuses to replace a
content-bearing generation with an empty one.

### 5. Replay from the last committed generation

Resume claiming from the last generation the core committed — never
from the external producer's last attempt. Generation IDs are reused on
retry, so replay is idempotent; duplicate delivery is safe by stable key
(`facts.StableID("TerraformStateFact", …)` in
`terraformstate/parser.go:279-288`). Scope identity is deterministic
(`state_snapshot:<backend>:<locator_hash>`, `scope/tfstate.go:15-45`),
so the restored producer re-derives the same scope for the same state.

### 6. Verify no dual authority, no lost facts

- Exactly one producer holds each scope: query claimed work items and
  recent committed generations for `collector_kind = terraform_state`;
  every generation's `collector_instance_id` names the restored producer.
- Fact parity: committed kind counts per scope equal the pre-cutover
  baseline; canonical projection and graph truth agree
  (`projector/canonical/terraform_state.go`, reducer domain
  `config_state_drift`).
- Read truth: `GET /api/v0/iac/resources` returns the baseline rows.
- Telemetry: `eshu_dp_tfstate_*` counters resume from the in-tree
  binary; the grant-denial path is quiet (no external emissions
  attempted).

## Rehearsal requirement

Rollback is proven only by rehearsal: cut over a non-production scope,
revoke, restore, replay, and verify the checks above before production
cutover. Record the rehearsal evidence on the cutover issue.
