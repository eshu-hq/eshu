# tfstate Drift Tier-2 v2.5 Fixture

Fixture corpus for the Tier-2 v2.5 compose verifier:
`scripts/verify_tfstate_drift_compose_tier2_v25.sh`.

Tier-2 v1 covers buckets A, B, D, and E from one collector pass. This fixture
covers the two drift kinds that need generation history:

- Bucket C, `removed_from_state`: state serial 1 has a resource; serial 2 on
  the same lineage does not.
- Bucket F, `removed_from_config`: config generation 1 declares a resource;
  generation 2 removes it while state still carries it.

## Layout

| Path | Purpose |
| --- | --- |
| `repos_gen1/drift-c-tf-config/main.tf` | Bucket C gen-1 config, still declaring `aws_s3_bucket.cached`. |
| `repos_gen2/drift-c-tf-config/main.tf` | Bucket C gen-2 config, unchanged because the drift is state-side. |
| `repos_gen1/drift-f-tf-config/main.tf` | Bucket F gen-1 config, declaring `aws_s3_bucket.legacy`. |
| `repos_gen2/drift-f-tf-config/main.tf` | Bucket F gen-2 config, same backend with the resource removed. |
| `state_gen1/drift-c.tfstate` | Bucket C serial 1, lineage `...c3`, resource present. |
| `state_gen2/drift-c.tfstate` | Bucket C serial 2, same lineage, resource absent. |
| `state_gen1/drift-f.tfstate` | Bucket F serial 1, lineage `...f6`, resource present. |
| `state_gen2/drift-f.tfstate` | Bucket F serial 2, same lineage, resource still present. |
| `minio-init-gen1.sh` | Bucket creation plus gen-1 object upload. |
| `minio-init-gen2.sh` | Gen-2 object overwrite between collector passes. |

## What The Fixture Proves

The verifier runs two collector instances against gen-1 and gen-2 data while
keeping Postgres alive. That is load-bearing:

- The prior-state query can only emit `removed_from_state` when both serials
  exist on the same `state_snapshot:s3:<hash>` scope.
- The prior-config query can only emit `removed_from_config` when an active
  generation supersedes a prior generation on the same repo scope.
- Two collector instances give each pass a fresh Terraform-state run ID instead
  of relying on a planner retry of the same work item.

## Where It Is Asserted

`scripts/verify_tfstate_drift_compose_tier2_v25.sh` asserts non-zero counter
deltas for `drift_kind="removed_from_state"` and
`drift_kind="removed_from_config"` after the second pass drains
`config_state_drift` work.

If prior-generation selection changes, re-check this fixture against
`go/internal/storage/postgres/tfstate_drift_evidence_prior_config.go`,
`go/internal/storage/postgres/tfstate_drift_evidence_sql.go`, and the
classifier tests for the same drift kinds.
