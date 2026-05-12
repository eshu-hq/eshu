# tfstate Drift Tier-2 E2E Fixture

Fixture corpus for `scripts/verify_tfstate_drift_compose_tier2.sh` (issue #187).
The Tier-1 sibling at `../tfstate_drift/` proves the drift handler given the
right facts by seeding Postgres directly. Tier-2 proves the wire from real
`eshu-collector-terraform-state` reads, through the workflow-coordinator's
`terraform_state` work plan, into the same drift handler.

## Layout

| Path | Purpose |
| --- | --- |
| `repos/drift-a-tf-config/main.tf` | Bucket A config: backend "s3" block only. |
| `repos/drift-b-tf-config/main.tf` | Bucket B config: declares `aws_s3_bucket.declared`. |
| `repos/drift-d-tf-config-a/main.tf` | Bucket D config repo "a": same s3 backend as repo "b". |
| `repos/drift-d-tf-config-b/main.tf` | Bucket D config repo "b": same s3 backend as repo "a". |
| `repos/drift-e-tf-config/main.tf` | Bucket E config: declares `aws_s3_bucket.logs` with SSE=AES256. |
| `state/drift-a.tfstate` | Bucket A state: carries `aws_s3_bucket.unmanaged`. |
| `state/drift-b.tfstate` | Bucket B state: zero resources. |
| `state/drift-d.tfstate` | Bucket D state: never read (resolver rejects before open). |
| `state/drift-e.tfstate` | Bucket E state: `aws_s3_bucket.logs` with SSE=aws:kms. |
| `minio-init.sh` | One-shot bucket + object loader for the compose overlay. |

## How the corpus drives the drift handler

1. The compose overlay at `docker-compose.tier2-tfstate.yaml` mounts
   `repos/` as `ESHU_FILESYSTEM_ROOT=/fixtures` for `bootstrap-index`,
   `ingester`, and `resolution-engine`. The Git collector emits
   `terraform_backends` facts from each `main.tf`.
2. The overlay also runs `minio` and a `minio-init` one-shot that creates
   `eshu-drift-a`, `eshu-drift-b`, `eshu-drift-d`, `eshu-drift-e` buckets and
   uploads each `.tfstate` to `prod/terraform.tfstate`.
3. The `workflow-coordinator` runs in active mode with claims enabled. Its
   `TerraformStateWorkPlanner` reads the `terraform_backends` facts via
   `terraformBackendCandidate` (s3-only filter at
   `go/internal/storage/postgres/tfstate_backend_facts.go:472`) and plans
   one `workflow_work_items` row per resolved backend.
4. The `collector-terraform-state` container claims each work item, reads the
   `.tfstate` from minio via the AWS SDK (`AWS_ENDPOINT_URL_S3` redirects the
   endpoint), and emits `terraform_state_snapshot` plus
   `terraform_state_resource` facts.
5. `bootstrap-index` reruns Phase 3.5, which finds the new
   `state_snapshot:*` scopes and enqueues `config_state_drift` intents.
6. The reducer runs `DomainConfigStateDrift` which increments
   `eshu_dp_correlation_drift_detected_total` with `drift_kind` labels
   `added_in_state`, `added_in_config`, `attribute_drift` and emits a
   WARN log with `failure_class="ambiguous_backend_owner"` for bucket D.

## Bucket coverage

Tier-2 covers buckets A, B, D, E. Buckets C (`removed_from_state`) and F
(`removed_from_config`) are deferred to a v2.5 follow-up because they need
two collector generations of the same state or repo to fire. The Tier-1
seed corpus already proves the loader logic for those drift kinds.

## minio addressing

The collector's AWS SDK v2 client defaults to virtual-hosted addressing
(`https://<bucket>.<endpoint-host>/...`). The compose overlay routes that
through compose-network aliases: `eshu-drift-a`, `eshu-drift-b`,
`eshu-drift-d`, and `eshu-drift-e` all resolve to the minio container, so
the SDK's `GET http://eshu-drift-a.minio:9000/prod/terraform.tfstate` works
without a path-style flag.

## Tfstate JSON shape

Hand-crafted, not produced by `terraform apply`. The streaming parser
(`go/internal/collector/terraformstate/parser.go`) reads `version`,
`lineage`, `serial`, and `resources[].instances[].attributes`. The lineage
and serial fields are validated against the generation the collector
constructs from the parsed values, so any unique UUID-shaped lineage plus
a positive serial works. Keep the lineage values distinct per bucket so
parallel runs in CI never confuse generations.

## When to regenerate

- If the streaming parser changes its accepted shape, re-read the
  `expectedSnapshotIdentity` and `readResources` paths and update the JSON.
- If the HCL parser changes how it emits `terraform_backends` rows or the
  `attributes` dot-paths for `aws_s3_bucket`, update the `.tf` files to
  carry the new shape.
- Tier-1's locator-hash regeneration recipe in `../tfstate_drift/README.md`
  also applies here. The locator strings used by the s3 backend blocks
  (`s3://eshu-drift-{a,b,d,e}/prod/terraform.tfstate`) are deliberately
  shared with Tier-1 so the resulting `state_snapshot:s3:<hash>` scope IDs
  match.
