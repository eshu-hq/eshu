# Rollback DRAFT (`#8852`, `#4047` cutover)

**DRAFT — rehearsal plan, not an executed runbook.** Authority for a
source scope and generation must rest with exactly one producer at a
time. The shared rollback script and runbook live under `#8895` and in
`docs/public/operate/collector-cutover-rollback.md`; below is the
AWS-cloud binding of its six steps.

## Trigger

Any cutover failure: parity mismatch, admission errors,
projection/graph divergence, missing telemetry, or expired/revoked grant
blocking emission.

## Steps

### 1. Stop new external claims

Halt the claim dispatcher targets for `collector_kind = aws` so no new
`Claim` reaches the external producer. In-flight work items keep their
leases; do not delete them. One claim covers one
`(account, region, service_kind)` tuple, so confirm the halt across
every configured tuple — a halt that misses one service kind leaves
that lane claiming externally.

### 2. Drain or expire in-flight leases

Wait for in-flight external results to land, or let the lease TTL
expire and allow the control store to release them. Confirm zero
`claimed` work items for `aws` before proceeding — the workflow
control store is the source of truth, not the producer's logs.
Per-account limiter slots (`service.go:105-107`) drain with the claims;
do not force leases with raw SQL (it bypasses the fencing token).

### 3. Revoke the external grant

```bash
for kind in aws_resource aws_relationship aws_tag_observation aws_dns_record aws_image_reference aws_security_group_rule aws_iam_permission aws_resource_policy_permission aws_warning; do
  eshu component revoke-grant dev.eshu.collector.aws-cloud \
    --component-home ~/.eshu/components \
    --version 0.1.0 \
    --kind "${kind}" \
    --scope aws
done
for kind in ec2_instance_posture rds_instance_posture s3_bucket_posture s3_external_principal_grant; do
  eshu component revoke-grant dev.eshu.collector.aws-cloud \
    --component-home ~/.eshu/components \
    --version 0.1.0 \
    --kind "${kind}" \
    --scope aws
done
```

Revocation fails the next external emission closed, so a stray retry
cannot re-establish dual authority.

### 4. Restore the in-tree producer

Reactivate the in-tree `collector-aws-cloud` deployment (claimed-live
or `-mode=cassette` for diagnosis) with the last supported
configuration, and re-enable the in-tree producer in values (reverse of
the cutover values change). In-tree needs no grant: it commits through
the claimed-service ingestion store plus the scan-status committer
under core admission. Pagination checkpoints resume fenced per
generation (`checkpoint/`, `ExpireStale`); stale rows from the external
attempt cannot be picked up because the generation fence moved on.

### 5. Replay from the last committed generation

Resume claiming from the last generation the core committed — never from
the external producer's last attempt. Generation IDs are reused on retry
(`awsruntime/source.go:267-269`, `GenerationID == SourceRunID`), so
replay is idempotent; duplicate delivery is safe by stable key
(`envelope.go`, `*_envelope.go`, `FactID` folds kind + key + scope +
generation). The empty-successor backstop still applies: a scope that
legitimately emptied replays only with its `allow_empty_successor`
attestation intact (`config.go:44-47`).

### 6. Verify no dual authority, no lost facts

- Exactly one producer holds each scope: query claimed work items and
  recent committed generations for `collector_kind = aws`; every
  generation's `collector_instance_id` must name the restored producer,
  across all 13 kinds and every `(account, region, service_kind)` tuple.
- Fact parity: committed kind counts per scope equal the pre-cutover
  baseline for all 13 kinds; canonical projection and graph truth agree
  (`aws_cloud_runtime_drift` reducer domain, `cloud_asset_resolution`
  projection hook, `GET /api/v0/cloud/inventory` read surface).
- Read truth: cloud inventory queries return the baseline rows for
  resources, relationships, posture, and grants.
- Telemetry: `eshu_dp_aws_*` counters resume from the in-tree binary
  (API calls, throttles, assume-role failures, resources/relationships
  emitted, checkpoint events, freshness events, scan-status stale
  fences, scan duration, claim concurrency); the grant-denial path is
  quiet (no external emissions attempted).

## Rehearsal requirement

Rollback is proven only by rehearsal: cut over a non-production scope
(one `(account, region, service_kind)` tuple), revoke, restore, replay,
and verify the checks above before production cutover. Record the
rehearsal evidence on the cutover issue. Cutover order holds for
rollback too: AWS restores independently of Jira (`#8858`) and
PagerDuty (`#8863`), but a joint incident re-verifies all three
activations before either is re-attempted.
