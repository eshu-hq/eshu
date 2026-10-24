# Rollback DRAFT (`#8854`, `#4047` cutover)

**DRAFT — rehearsal plan, not an executed runbook.** Authority for a
source scope and generation must rest with exactly one producer at a
time.

## Trigger

Any cutover failure: parity mismatch, admission errors, projection or
graph divergence, missing telemetry, or expired/revoked grant blocking
emission.

## Steps

### 1. Stop new external claims

Halt the claim dispatcher targets for `collector_kind = ci_cd_run` so
no new `Claim` reaches the external producer. In-flight work items keep
their leases; do not delete them.

### 2. Drain or expire in-flight leases

Wait for in-flight external results to land, or let the lease TTL
expire and allow the control store to release them. Confirm zero
`claimed` work items for `ci_cd_run` before proceeding — the workflow
control store (`postgres.NewWorkflowControlStore`, `service.go:116`)
is the source of truth, not the producer's logs.

### 3. Revoke the external grant

```bash
for kind in pipeline_definition run job step artifact trigger_edge environment_observation deployment_event warning; do
  eshu component revoke-grant dev.eshu.collector.cicd-run \
    --component-home ~/.eshu/components \
    --version 0.1.0 \
    --kind "ci.${kind}" \
    --scope ci_cd_run
done
```

Revocation fails the next external emission closed, so a stray retry
cannot re-establish dual authority.

### 4. Restore the in-tree producer

Reactivate the in-tree `collector-cicd-run` deployment (claimed-live
or `-mode=cassette` for diagnosis) with the last supported
configuration. In-tree needs no grant: it commits through
`postgres.NewIngestionStore` (`service.go:112`) under core admission.
GitHub watermark state survives in `cicd_run_watermarks` (migration
078), so gap detection resumes without a cold restart.

### 5. Replay from the last committed generation

Resume claiming from the last generation the core committed — never
from the external producer's last attempt. Generation IDs are reused
on retry, so replay is idempotent; duplicate delivery is safe by stable
key (`facts.StableID` in each fixture constructor).

### 6. Verify no dual authority, no lost facts

- Exactly one producer holds each scope: query claimed work items and
  recent committed generations for `collector_kind = ci_cd_run`; every
  generation's `collector_instance_id` names the restored producer.
- Fact parity: committed kind counts per scope equal the pre-cutover
  baseline; canonical projection and graph truth agree
  (`reducer/cicdrun`, `query/cicd` read model).
- Read truth: `GET /api/v0/ci-cd/run-correlations` returns the baseline
  rows.
- Telemetry: `eshu_dp_ci_cd_run_*` counters resume from the in-tree
  binary; the grant-denial path is quiet (no external emissions
  attempted).

## Rehearsal requirement

Rollback is proven only by rehearsal: cut over a non-production scope,
revoke, restore, replay, and verify the checks above before production
cutover. Record the rehearsal evidence on the cutover issue.
