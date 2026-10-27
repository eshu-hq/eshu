# Terraform State Collector Config

Use this page to configure `collector-terraform-state` instances, discovery
policy, credential routing, redaction, and provider-schema coverage. The
runtime overview lives in [Terraform State Collector](collector-terraform-state.md).

## Required Configuration (Satellite Activation)

The extracted satellite (`eshu-hq/eshu-collector-terraform-state`) resolves
state sources, redaction, and claim behavior through its activation config
(see the satellite repo's `config.example.yaml`), not through in-tree process
environment. It reads S3 with ambient workload credentials only (IRSA web
identity, then the process environment triple); no credential key exists in
config.

| Setting | Purpose |
| --- | --- |
| Satellite redaction key | Deployment-scoped key for deterministic redaction markers. |
| Redaction ruleset version | Non-empty redaction rule-set version; blank values fail startup. |

Claim polling, lease TTL, heartbeat, source size caps, sensitive-key
overrides, and the provider-schema bundle directory are satellite activation
settings. (`ESHU_TERRAFORM_SCHEMA_DIR` remains a live in-tree input for the
core schema loader used in local schema development.)

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
  declarations. It never lists S3 bucket contents. S3 filters may include
  `key` to select one exact object; this is recommended for release and
  all-collector proofs so stale same-bucket backend declarations do not become
  configured targets.
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
- schema-known sensitive composites, including Lambda environment blocks, are
  intentionally skipped with `reason=known_sensitive_key`
- provider shapes without packaged schema proof, including
  `cloudinit_config.part`, remain unsupported with `reason=schema_unknown`
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
