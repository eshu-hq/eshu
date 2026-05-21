# AWS Collector Security And Config

Use this page to configure AWS collector credentials, target scopes, and
redaction. The collector accepts temporary role credentials through either
central AssumeRole or local workload identity. It rejects static access-key
fields during configuration parsing.

## Credential Modes

| Mode | Use when | Required fields | Rejected fields |
| --- | --- | --- | --- |
| `central_assume_role` | A central Eshu runtime scans target accounts through scoped read roles. | `role_arn`, `external_id` | static access keys |
| `local_workload_identity` | The collector runs inside the target account or an account-local boundary. | none beyond the local AWS SDK chain | `role_arn`, `external_id`, static access keys |

For `central_assume_role`, the configured IAM role ARN account must match the
target `account_id`. For `local_workload_identity`, the pod or process role is
the credential source.

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
| `ESHU_AWS_COLLECTOR_OWNER_ID` | `HOSTNAME`, then `collector-aws-cloud` | Owner name written into claim rows. |
| `ESHU_AWS_COLLECTOR_POLL_INTERVAL` | `1s` | Claim poll cadence while no work is available. |
| `ESHU_AWS_COLLECTOR_CLAIM_LEASE_TTL` | workflow default | Per-claim lease duration. |
| `ESHU_AWS_COLLECTOR_HEARTBEAT_INTERVAL` | workflow default | Claim heartbeat cadence; must be below the lease TTL. |

## Target Scope Rules

Each target scope must name:

- a 12-digit `account_id`
- at least one concrete `allowed_regions` entry
- at least one concrete `allowed_services` entry
- one credential mode

The parser rejects wildcard regions and wildcard services. It also rejects an
`allowed_services` value unless `awsruntime.SupportsServiceKind` names a shipped
scanner adapter for that service.

```json
{
  "target_scopes": [
    {
      "account_id": "123456789012",
      "allowed_regions": ["us-east-1", "aws-global"],
      "allowed_services": ["iam", "ecr", "ecs", "lambda"],
      "max_concurrent_claims": 1,
      "credentials": {
        "mode": "central_assume_role",
        "role_arn": "arn:aws:iam::123456789012:role/eshu-readonly",
        "external_id": "external-1"
      }
    }
  ]
}
```

`max_concurrent_claims` is optional. `0` or unset means one active claim per
account. Positive values raise the collector-side per-account limit through the
runtime account limiter.

## IAM Policy Shape

Target-account trust policy shape for central AssumeRole:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "AWS": "arn:aws:iam::111122223333:role/eshu-collector-control-plane"
      },
      "Action": "sts:AssumeRole",
      "Condition": {
        "StringEquals": {
          "sts:ExternalId": "customer-or-target-scope-id"
        }
      }
    }
  ]
}
```

Validate trust and permissions policies before rollout:

```bash
aws accessanalyzer validate-policy \
  --policy-type TRUST_POLICY \
  --policy-document file://trust-policy.json

aws accessanalyzer validate-policy \
  --policy-type IDENTITY_POLICY \
  --policy-document file://permissions-policy.json
```

Permissions should stay read-only and service-scoped. Start from the scanner
families enabled in `allowed_services`, grant only the `List*`, `Describe*`,
and metadata `Get*` calls required by those scanners, and do not grant AWS
mutation APIs such as `Create*`, `Update*`, `Put*`, `Delete*`, `Tag*`, or
`Untag*`.

Do not grant data-plane reads that the collector intentionally avoids,
including secret values, SSM parameter values, SQS messages, DynamoDB table
items, log events, API execution or export, S3 object contents, database
contents, or Lambda code/package downloads.

## Redaction

ECS and Lambda scans require `ESHU_AWS_REDACTION_KEY` before startup because
environment values are redacted before persistence. The key produces
deterministic HMAC markers for sensitive values; it is not stored in facts.

The collector must not persist:

- credential material, bearer tokens, session tokens, or presigned query
  parameters
- secret values or SSM parameter values
- policy JSON, payload bodies, queue messages, log events, database rows, S3
  object contents, or Lambda package contents
- raw AWS error payloads in metric labels

## Helm Notes

The Helm chart renders `ESHU_COLLECTOR_INSTANCES_JSON`, the instance selector,
owner ID, Postgres env, OpenTelemetry env, probes, metrics Service, optional
`ServiceMonitor`, `NetworkPolicy`, and `PodDisruptionBudget`.

Use `awsCloudCollector.serviceAccount.create=true` for IRSA so AWS collector
permissions do not attach to API, reducer, ingester, or other pods in the same
release.

```yaml
awsCloudCollector:
  enabled: true
  instanceId: aws-primary
  serviceAccount:
    create: true
    annotations:
      eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/eshu-aws-collector
```

## Failure Triage

| Symptom | Likely cause | First check |
| --- | --- | --- |
| `credential_failed=true` | Bad role ARN, missing external ID, denied trust policy, or broken IRSA. | Trust policy, ServiceAccount annotation, STS logs, and `aws.credentials.assume_role` spans. |
| Runtime starts but never claims work | Missing active workflow coordinator, wrong instance ID, or `claims_enabled=false`. | `/admin/status?format=json`, collector logs, and `ESHU_COLLECTOR_INSTANCES_JSON`. |
| Missing ECS or Lambda facts at startup | Redaction key is not configured. | `ESHU_AWS_REDACTION_KEY` Secret and startup logs. |
| Throttles rise after enabling more services | Too many same-account claims or service-specific AWS throttling. | `eshu_dp_aws_throttle_total` and `eshu_dp_aws_claim_concurrency`. |

## Related Docs

- [AWS Cloud Collector](collector-aws-cloud.md)
- [AWS Collector Scanner Coverage](collector-aws-cloud-scanners.md)
- [Helm Collector And Webhook Values](../deploy/kubernetes/helm-collector-and-webhook-values.md)
- [Collector Environment](../reference/environment-collectors.md)
