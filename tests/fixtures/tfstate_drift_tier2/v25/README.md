# tfstate Drift Tier-2 v2.5 Fixture (buckets C and F)

Fixture corpus for the Tier-2 v2.5 follow-up to issue #187 (tracked as issue
#209). Tier-2 v1 (`../`) covers drift buckets A, B, D, and E from a single
collector pass. v2.5 covers the two drift kinds a single-pass run cannot
produce:

- **Bucket C — `removed_from_state`**: a state file rotates from serial=1
  (resource present) to serial=2 (resource absent) on the same lineage.
- **Bucket F — `removed_from_config`**: a `.tf` file rotates from gen-1
  (resource declared) to gen-2 (resource omitted) on the same repo scope_id.

## Why two collector passes are required

The drift loader's prior-side queries depend on Postgres rows that exist only
after a previous generation has already been collected and persisted:

- `priorStateSnapshotMetadataQuery`
  (`go/internal/storage/postgres/tfstate_drift_evidence_sql.go:135`) selects
  the prior state by `serial = currentSerial - 1`. Two
  `terraform_state_snapshot` rows must exist on the same
  `state_snapshot:s3:<hash>` scope_id, one per serial.
- `listPriorConfigAddressesQuery`
  (`go/internal/storage/postgres/tfstate_drift_evidence_sql.go:170`) walks
  prior `scope_generations` rows by `ingested_at DESC`, excluding the current
  `generation_id`. The current generation must be `status='active'` and the
  prior must be `status='superseded'` on the same repo scope_id.

A single collector pass cannot produce both rows because the planner is
idempotent per `terraformStateRunID` (`coordinator/tfstate_scheduler.go:129`).
The v2.5 verifier runs two distinct collector instances back-to-back against
gen-1 and gen-2 fixtures so each pass gets a fresh RunID and Phase 3.5 can
walk the prior-generation history on the second pass.

## Layout

| Path | Purpose |
| --- | --- |
| `repos_gen1/drift-c-tf-config/main.tf` | Bucket C gen-1 config: declares `aws_s3_bucket.cached` and the s3 backend. |
| `repos_gen2/drift-c-tf-config/main.tf` | Bucket C gen-2 config: identical to gen-1 (bucket C drifts on state, not config). |
| `repos_gen1/drift-f-tf-config/main.tf` | Bucket F gen-1 config: declares `aws_s3_bucket.legacy` and the s3 backend. |
| `repos_gen2/drift-f-tf-config/main.tf` | Bucket F gen-2 config: same backend, resource removed. |
| `state_gen1/drift-c.tfstate` | Bucket C gen-1 state: serial=1, lineage `…c3`, carries `aws_s3_bucket.cached`. |
| `state_gen2/drift-c.tfstate` | Bucket C gen-2 state: serial=2, lineage `…c3`, no resources. |
| `state_gen1/drift-f.tfstate` | Bucket F gen-1 state: serial=1, lineage `…f6`, no resources. |
| `state_gen2/drift-f.tfstate` | Bucket F gen-2 state: serial=2, lineage `…f6`, no resources. |
| `minio-init-gen1.sh` | Bucket creation + gen-1 object upload (compose `up`). |
| `minio-init-gen2.sh` | Gen-2 object overwrite for the same `prod/terraform.tfstate` keys (between collector passes). |

## How the corpus drives the drift handler

The v2.5 verifier orchestration (Option B — dual collector instance):

1. Compose `up` with the v2.5 overlay. `minio-init-gen1` uploads gen-1 state
   objects to MinIO. Two terraform_state collector instances are declared
   in `ESHU_COLLECTOR_INSTANCES_JSON`: `tier2-v25-terraform-state-gen1` and
   `tier2-v25-terraform-state-gen2`. Only gen1 is enabled at start.
2. Bootstrap-index Pass 1 collects `repos_gen1/`. Git collector emits
   `terraform_backends` facts for bucket C and bucket F. Coordinator plans
   `terraform_state` work items keyed against the gen1 instance.
3. Collector-instance-1 claims the work, reads gen-1 state from MinIO, emits
   `terraform_state_snapshot` (serial=1) and `terraform_state_resource` facts
   on the same `state_snapshot:s3:<hash>` scope_id. The repo's gen-1
   `scope_generations` row is created at `status='active'`.
4. Phase 3.5 runs. Bucket C has only one snapshot (no prior), bucket F has no
   prior superseded generation, so no drift fires yet.
5. The verifier overwrites the MinIO objects via `minio-init-gen2.sh` and
   rebinds the fixture repos volume to `repos_gen2/`. Collector-instance-2
   takes over with a fresh RunID. Bootstrap-index Pass 2 ingests
   `repos_gen2/`; the gen-1 `scope_generations` row is marked `superseded`
   and a new `status='active'` row is published with `aws_s3_bucket.legacy`
   absent. Collector-instance-2 emits the gen-2 `terraform_state_snapshot`
   (serial=2) for bucket C.
6. Phase 3.5 runs again. The prior-state query finds gen-1's serial=1
   snapshot for bucket C and emits `drift_kind="removed_from_state"`. The
   prior-config query finds gen-1's superseded `scope_generations` row for
   bucket F's repo and emits `drift_kind="removed_from_config"`.

## Locator and scope notes

- Bucket C locator: `s3://eshu-drift-c/prod/terraform.tfstate`. The
  derived `state_snapshot:s3:<hash>` scope_id must be identical between
  gen-1 and gen-2 facts — Tier-1's locator-hash recipe in
  `../README.md` applies unchanged.
- Bucket F locator: `s3://eshu-drift-f/prod/terraform.tfstate`. Bucket F
  state is always empty; the locator only exists so the planner emits a
  work item the collector can drain (otherwise the repo never enters the
  state pipeline).
- Lineages are deliberately distinct from Tier-2 v1 (`…a1`, `…b2`, `…d4`,
  `…e5`) to avoid cross-bucket confusion in CI logs.

## Why Option B (and not Option A)

Option A (compose `down -v` between passes) was attempted and rejected:
wiping Postgres between passes erases the gen-1 `scope_generations` row that
the prior-config walk requires. Option B keeps Postgres alive between passes
and uses two distinct collector instances so each pass gets its own RunID,
bypassing the per-RunID planner idempotency at
`coordinator/tfstate_scheduler.go:129`.

## When to regenerate

The same rules as the Tier-2 v1 README apply. Additionally:

- If `priorStateSnapshotMetadataQuery` changes how it picks the prior
  serial, re-confirm `state_gen{1,2}/drift-c.tfstate` still satisfies the
  `serial = currentSerial - 1` predicate.
- If `listPriorConfigAddressesQuery` changes how it walks generations,
  re-confirm `repos_gen{1,2}/drift-f-tf-config/main.tf` still produces an
  `active`/`superseded` generation pair on the same repo scope_id.
