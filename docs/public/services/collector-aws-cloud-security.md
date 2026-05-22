# AWS Collector Security And Config

Use this page for AWS collector credentials, target scopes, IAM guardrails, and
redaction. The collector accepts temporary role credentials through central
AssumeRole or local workload identity. Static access-key fields are rejected
during configuration parsing.

## Required Environment

| Env var | Purpose |
| --- | --- |
| `ESHU_POSTGRES_DSN` or split Postgres DSNs | Shared Postgres runtime loader. |
| `ESHU_COLLECTOR_INSTANCES_JSON` | Desired collector instances. Must include one enabled `aws` instance with `claims_enabled=true`. |
| `ESHU_AWS_REDACTION_KEY` | Required when any target scope enables `ecs` or `lambda`. |

Optional runtime knobs:

| Env var | Default | Purpose |
| --- | --- | --- |
| `ESHU_AWS_COLLECTOR_INSTANCE_ID` | first enabled AWS instance | Selects one AWS instance when multiple are configured. |
| `ESHU_AWS_COLLECTOR_OWNER_ID` | `HOSTNAME`, then `collector-aws-cloud` | Owner label written into claim rows. |
| `ESHU_AWS_COLLECTOR_POLL_INTERVAL` | `1s` | Empty-claim poll cadence. |
| `ESHU_AWS_COLLECTOR_CLAIM_LEASE_TTL` | workflow default | Per-claim lease duration. |
| `ESHU_AWS_COLLECTOR_HEARTBEAT_INTERVAL` | workflow default | Claim heartbeat cadence; must be below the lease TTL. |

## Target Scope Rules

Each target scope must name a 12-digit `account_id`, at least one concrete
`allowed_regions` entry, at least one concrete `allowed_services` entry, and
one credential mode. The parser rejects wildcard regions and wildcard services.
`allowed_services` must name a shipped scanner adapter listed in
[AWS Collector Scanner Coverage](collector-aws-cloud-scanners.md).

`max_concurrent_claims` is optional. `0` or unset means one active claim per
account. Positive values raise the collector-side per-account limit through the
runtime account limiter.

## Credential Modes

| Mode | Required fields | Rejected fields |
| --- | --- | --- |
| `central_assume_role` | `role_arn`, `external_id`; role ARN account must match `account_id` | static access keys |
| `local_workload_identity` | none beyond the local AWS SDK chain | `role_arn`, `external_id`, static access keys |

## IAM Guardrails

Permissions must stay read-only and service-scoped. Grant only the metadata
`List*`, `Describe*`, and safe `Get*` calls required by enabled scanners. Do
not grant mutation APIs such as `Create*`, `Update*`, `Put*`, `Delete*`,
`Tag*`, or `Untag*`.

Do not grant data-plane reads the collector intentionally avoids: secret values,
SSM parameter values, SQS messages, DynamoDB items, log events, API execution
payloads, S3 object contents, database contents, or Lambda code packages.

## Redaction

ECS and Lambda scans require `ESHU_AWS_REDACTION_KEY` before startup because
environment values are redacted before persistence. The key produces
deterministic HMAC markers for sensitive values; it is not stored in facts.

The collector must not persist credential material, bearer tokens, session
tokens, presigned query parameters, secret values, policy JSON payload bodies,
queue messages, log events, database rows, S3 object contents, Lambda package
contents, or raw AWS error payloads in metric labels.

## Helm Notes

The Helm chart renders `ESHU_COLLECTOR_INSTANCES_JSON`, the instance selector,
owner ID, Postgres env, OTEL env, probes, metrics Service, optional
`ServiceMonitor`, `NetworkPolicy`, and `PodDisruptionBudget`.

Use `awsCloudCollector.serviceAccount.create=true` for IRSA so AWS collector
permissions do not attach to API, reducer, ingester, or other pods in the same
release.

## Related Docs

- [AWS Cloud Collector](collector-aws-cloud.md)
- [AWS Collector Scanner Coverage](collector-aws-cloud-scanners.md)
- [Helm Collector And Webhook Values](../deploy/kubernetes/helm-collector-and-webhook-values.md)
- [Collector Environment](../reference/environment-collectors.md)
