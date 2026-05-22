# tfstate Drift Tier-2 E2E Fixture

Fixture corpus for `scripts/verify_tfstate_drift_compose_tier2.sh`.

The Tier-1 sibling at `../tfstate_drift/` seeds Postgres facts directly and
proves the reducer drift handler. Tier-2 proves the production wire from
Terraform backend facts through workflow planning, the real
`collector-terraform-state`, MinIO-hosted state files, Phase 3.5 enqueue, and
the same `config_state_drift` reducer.

## Layout

| Path | Purpose |
| --- | --- |
| `repos/drift-a-tf-config/main.tf` | Bucket A config: backend only. |
| `repos/drift-b-tf-config/main.tf` | Bucket B config: declares `aws_s3_bucket.declared`. |
| `repos/drift-d-tf-config-a/main.tf` | Bucket D config repo A: same backend as repo B. |
| `repos/drift-d-tf-config-b/main.tf` | Bucket D config repo B: same backend as repo A. |
| `repos/drift-e-tf-config/main.tf` | Bucket E config: declares `aws_s3_bucket.logs` with SSE `AES256`. |
| `state/drift-a.tfstate` | Bucket A state: carries `aws_s3_bucket.unmanaged`. |
| `state/drift-b.tfstate` | Bucket B state: zero resources. |
| `state/drift-d.tfstate` | Bucket D state: present but rejected before read because ownership is ambiguous. |
| `state/drift-e.tfstate` | Bucket E state: `aws_s3_bucket.logs` with SSE `aws:kms`. |
| `minio-init.sh` | Bucket creation plus object upload for the compose overlay. |

## What The Fixture Proves

- Bucket A emits `added_in_state`.
- Bucket B emits `added_in_config`.
- Bucket D emits an `ambiguous_backend_owner` warning and no drift counter.
- Bucket E emits `attribute_drift`.

Buckets C (`removed_from_state`) and F (`removed_from_config`) need two
collector generations and live in `v25/`.

## Where It Is Asserted

`scripts/verify_tfstate_drift_compose_tier2.sh` starts the compose overlay,
waits for `config_state_drift` work to drain, checks drift counters, and checks
the ambiguous-owner log. The script is the assertion source; this README only
documents which fixture rows feed those checks.

The `.tfstate` files are hand-written. They must keep the fields read by the
streaming parser: `version`, `lineage`, `serial`, and
`resources[].instances[].attributes`. Locator-hash rules are inherited from
the Tier-1 fixture.
