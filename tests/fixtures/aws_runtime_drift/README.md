# AWS Runtime Drift Fixture

Fixture corpus for `scripts/verify_aws_runtime_drift_compose.sh`.

This fixture proves the offline AWS cloud collector (`collector-aws-cloud
-mode fixture`) drives a deterministic `orphaned_cloud_resource` drift finding
(classification `cloud_only`) through the `aws_cloud_runtime_drift` reducer and
into the `POST /api/v0/replatforming/plans` migration plan, with **zero AWS
credentials** and **no network calls**.

## Files

| File | Purpose |
| --- | --- |
| `managed.tfstate` | Terraform state that manages **only** the `eshu-fixture-managed` bucket. The state intentionally omits `eshu-fixture-unmanaged`. |
| `README.md` | This file. |

The replayed AWS estate lives in
`go/cmd/collector-aws-cloud/testdata/fixture-estate.json`. The collector reads
it in fixture mode and commits `aws_resource` / `aws_relationship` facts that
are byte-identical to live-scanner facts.

## Drift intent

The estate declares two S3 buckets in account `111122223333`, region
`us-east-1`:

| ARN | Terraform-managed? | Expected classification |
| --- | --- | --- |
| `arn:aws:s3:::eshu-fixture-managed` | Yes (`managed.tfstate`) | not orphaned |
| `arn:aws:s3:::eshu-fixture-unmanaged` | **No** | `orphaned_cloud_resource` (`cloud_only`) |

`eshu-fixture-unmanaged` is the orphan: it exists in the AWS runtime estate but
no Terraform state (and no Terraform config) declares it. The
`aws_cloud_runtime_drift` reducer classifies an `aws_resource` ARN with cloud
evidence but no state and no config evidence as `cloud_only`, which surfaces as
an `orphaned_cloud_resource` migration-plan item.

Because the compose gate ingests AWS facts via the offline collector and seeds
no Terraform state for either bucket, both buckets are cloud-only at runtime;
`managed.tfstate` documents the intended managed half for reviewers and for a
future tier that seeds the state collector. The gate asserts a **non-empty**
`items_count` from the replatforming plan, which is satisfied by the orphan.

## Determinism

Generation ids are derived only from the scope id
(`awsruntime.FixtureScope.resolvedGenerationID`), never from the clock, so
re-running the collector re-emits the same fact ids and the gate is
reproducible.
