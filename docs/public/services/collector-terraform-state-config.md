# Terraform State Collector Config

Use this page to configure `collector-terraform-state` instances, discovery
policy, credential routing, and redaction. The collector only opens exact local
files or exact S3 objects. It does not scan buckets, read unapproved local
state, or write Terraform state.

## Required Environment

| Env var | Purpose |
| --- | --- |
| `ESHU_POSTGRES_DSN` or split Postgres DSNs | Shared Postgres runtime loader. |
| `ESHU_COLLECTOR_INSTANCES_JSON` | Desired collector instances. Must include one enabled `terraform_state` instance with `claims_enabled=true`. |
| `ESHU_TFSTATE_REDACTION_KEY` | Deployment-scoped key for deterministic redaction markers. |
| `ESHU_TFSTATE_REDACTION_RULESET_VERSION` | Non-empty redaction rule-set version. Blank values fail startup. |

Optional runtime knobs:

| Env var | Default | Purpose |
| --- | --- | --- |
| `ESHU_TFSTATE_COLLECTOR_INSTANCE_ID` | required when more than one enabled instance exists | Selects one claim-capable `terraform_state` instance. |
| `ESHU_TFSTATE_COLLECTOR_OWNER_ID` | host/process-derived | Owner label written into workflow claim rows. |
| `ESHU_TFSTATE_COLLECTOR_POLL_INTERVAL` | `1s` | Empty-claim poll cadence. |
| `ESHU_TFSTATE_COLLECTOR_CLAIM_LEASE_TTL` | workflow default | Per-claim lease duration. |
| `ESHU_TFSTATE_COLLECTOR_HEARTBEAT_INTERVAL` | workflow default | Claim heartbeat cadence; must be below the lease TTL. |
| `ESHU_TFSTATE_COLLECTOR_HEARTBEAT` | workflow default | Backward-compatible alias for `ESHU_TFSTATE_COLLECTOR_HEARTBEAT_INTERVAL`. |
| `ESHU_TFSTATE_SOURCE_MAX_BYTES` | reader default | Max bytes read from one local or S3 state source. |
| `ESHU_TFSTATE_REDACTION_SENSITIVE_KEYS` | `password,secret,token,access_key,private_key,certificate,key_pair` | Comma-separated leaf keys treated as secrets. |
| `ESHU_TERRAFORM_SCHEMA_DIR` | packaged schema default | Overrides the Terraform provider-schema bundle path. |

## Instance Shape

The selected `ESHU_COLLECTOR_INSTANCES_JSON` entry must be enabled, claimable,
and use `collector_kind="terraform_state"`. Source-specific configuration
lives under `configuration`:

```json
{
  "instance_id": "terraform-state-prod",
  "collector_kind": "terraform_state",
  "mode": "continuous",
  "enabled": true,
  "claims_enabled": true,
  "configuration": {
    "target_scopes": [
      {
        "target_scope_id": "aws-prod",
        "provider": "aws",
        "deployment_mode": "central",
        "credential_mode": "central_assume_role",
        "role_arn": "arn:aws:iam::123456789012:role/eshu-tfstate-read",
        "external_id": "external-123",
        "allowed_regions": ["us-east-1"],
        "allowed_backends": ["s3", "local"]
      }
    ],
    "discovery": {
      "graph": true,
      "local_repos": ["platform-infra"],
      "backend_filters": [
        {
          "target_scope_id": "aws-prod",
          "backend_kind": "s3",
          "bucket": "company-terraform-state",
          "region": "us-east-1"
        }
      ],
      "local_state_candidates": {
        "mode": "approved_candidates",
        "approved": [
          {
            "repo_id": "platform-infra",
            "path": "env/prod/terraform.tfstate",
            "target_scope_id": "aws-prod"
          }
        ]
      },
      "seeds": [
        {
          "kind": "s3",
          "target_scope_id": "aws-prod",
          "bucket": "company-terraform-state",
          "key": "prod/app/terraform.tfstate",
          "region": "us-east-1",
          "dynamodb_table": "company-terraform-locks"
        }
      ]
    }
  }
}
```

## Discovery Modes

| Mode | Rule |
| --- | --- |
| `discovery.seeds` | Each seed names one exact local file or one exact S3 bucket/key/region tuple. |
| `discovery.local_repos` | Repo-scoped graph discovery waits for Git generation readiness. |
| `discovery.backend_filters` | Filters read indexed Git facts for exact backend declarations. They never list S3 bucket contents. |
| `discovery.local_state_candidates` | The config must approve each exact `repo_id` and repo-relative path before a Git-observed `.tfstate` file is opened. |

`graph=true` without `local_repos` or `backend_filters` is not useful and must
not become a whole-database scan. Dynamic backend expressions, workspace
prefixes, non-S3 cloud backends, prefix-only S3 keys, and unapproved local paths
are not candidates.

## Credential Routing

Use target scopes for new deployments.

| Credential mode | Behavior |
| --- | --- |
| `central_assume_role` | Assumes the configured account-scoped read role before opening matching S3 state. |
| `local_workload_identity` | Uses the local AWS SDK credential chain in the target account or account-local boundary. |

Explicit seeds may name `target_scope_id`. Graph-discovered S3 candidates route
through backend and region allowlists. If more than one target scope matches,
the collector fails before opening the object.

The legacy top-level `aws.role_arn` field still works for one AWS identity, but
it cannot be mixed with `target_scopes`.

## Redaction And Schemas

`ESHU_TFSTATE_REDACTION_KEY` and
`ESHU_TFSTATE_REDACTION_RULESET_VERSION` are mandatory. The rule-set version
proves which redaction policy produced each audit decision.

The parser fails closed:

- sensitive outputs and sensitive leaf keys are redacted
- unknown provider-schema scalars are redacted
- unknown composites are dropped and counted
- schema-known composites are captured with the streaming nested walker
- tag keys and values still pass through classification

`ESHU_TERRAFORM_SCHEMA_DIR` can override the packaged provider-schema bundle.
When the resolver cannot cover a resource type, the conservative unknown-schema
path stays active.

## DynamoDB Lock Metadata

For S3 backends that use Terraform's DynamoDB lock table, set
`dynamodb_table` on the exact S3 seed or let graph discovery read the literal
`dynamodb_table` from the committed backend block. A legacy top-level
`aws.dynamodb_table` value is accepted as a fallback, but backend-specific
values win.

DynamoDB reads are observational. `GetItem` failures emit warning evidence and
do not decide whether the state body is current.

## Related Docs

- [Terraform State Collector](collector-terraform-state.md)
- [Terraform State Collector Operations](collector-terraform-state-operations.md)
- [Collector Environment](../reference/environment-collectors.md)
- [Collector Authoring](../guides/collector-authoring.md)
