# Terraform State Collector Config

Use this page to configure `collector-terraform-state` instances, discovery
policy, credential routing, redaction, and provider-schema coverage. The
runtime overview lives in [Terraform State Collector](collector-terraform-state.md).

## Required Environment

| Env var | Purpose |
| --- | --- |
| `ESHU_POSTGRES_DSN` or split Postgres DSNs | Shared Postgres runtime loader. |
| `ESHU_COLLECTOR_INSTANCES_JSON` | Must include one enabled `terraform_state` instance with `claims_enabled=true`. |
| `ESHU_TFSTATE_REDACTION_KEY` | Deployment-scoped key for deterministic redaction markers. |
| `ESHU_TFSTATE_REDACTION_RULESET_VERSION` | Non-empty redaction rule-set version; blank values fail startup. |

Optional knobs: `ESHU_TFSTATE_COLLECTOR_INSTANCE_ID`,
`ESHU_TFSTATE_COLLECTOR_OWNER_ID`, `ESHU_TFSTATE_COLLECTOR_POLL_INTERVAL`,
`ESHU_TFSTATE_COLLECTOR_CLAIM_LEASE_TTL`,
`ESHU_TFSTATE_COLLECTOR_HEARTBEAT_INTERVAL`,
`ESHU_TFSTATE_SOURCE_MAX_BYTES`, `ESHU_TFSTATE_REDACTION_SENSITIVE_KEYS`, and
`ESHU_TERRAFORM_SCHEMA_DIR`.

## Instance Shape

The selected instance must be enabled, claimable, and use
`collector_kind="terraform_state"`. Required target-scope fields:

| Field | Rule |
| --- | --- |
| `target_scope_id` | Unique scope ID used by discovery candidates and claims. |
| `provider` | Currently `aws`. |
| `deployment_mode` | `central` requires `central_assume_role`; `account_local` requires `local_workload_identity`. |
| `credential_mode` | `central_assume_role` or `local_workload_identity`. |
| `allowed_regions` | Concrete regions allowed for S3 reads. |
| `allowed_backends` | `s3`, `local`, or both. |

`central_assume_role` requires `role_arn` and `external_id`.
`local_workload_identity` rejects both. The legacy top-level `aws.role_arn`
field still works for one AWS identity, but it cannot be mixed with
`target_scopes`.

## Discovery Rules

- `discovery.seeds` names exact local files or exact S3 bucket/key/region
  tuples.
- `discovery.local_repos` waits for repo-scoped Git generation readiness.
- `discovery.backend_filters` reads indexed Git facts for exact backend
  declarations. It never lists S3 bucket contents.
- `discovery.local_state_candidates` must approve each exact `repo_id` and
  repo-relative path before a Git-observed `.tfstate` file is opened.

Dynamic backend expressions, workspace prefixes, non-S3 cloud backends,
prefix-only S3 keys, and unapproved local paths are not candidates. `graph=true`
without `local_repos` or `backend_filters` must not become a whole-database
scan.

## Redaction And Schemas

Redaction key and rule-set version are mandatory. The parser fails closed:

- sensitive outputs and sensitive leaf keys are redacted
- unknown provider-schema scalars are redacted
- unknown composites are dropped and counted
- schema-known composites are captured by the streaming nested walker
- tag keys and values still pass through classification

`ESHU_TERRAFORM_SCHEMA_DIR` can override the packaged provider-schema bundle.
When the resolver cannot cover a resource type, the conservative
unknown-schema path stays active.

## DynamoDB Lock Metadata

For S3 backends that use Terraform's DynamoDB lock table, set `dynamodb_table`
on the exact S3 seed or let graph discovery read the literal backend block.
Legacy top-level `aws.dynamodb_table` is accepted as a fallback, but
backend-specific values win.

DynamoDB reads are observational. `GetItem` failures emit warning evidence and
do not decide whether the state body is current.

## Related Docs

- [Terraform State Collector](collector-terraform-state.md)
- [Terraform State Collector Operations](collector-terraform-state-operations.md)
- [Collector Environment](../reference/environment-collectors.md)
- [Collector Authoring](../guides/collector-authoring.md)
