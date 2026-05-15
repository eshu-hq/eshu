# AWS Cloud Collector

## Role and Purpose

`collector-aws-cloud` is a claim-driven worker that observes AWS control-plane
metadata and commits reported cloud facts through the shared ingestion
boundary. It does not decide what AWS work exists, write graph truth, or infer
service ownership. The workflow control plane creates claimable
`(account_id, region, service_kind)` work items. The collector claims one item,
gets claim-scoped credentials, scans the requested service, records scan
status, and commits facts to Postgres.

**Binary:** `go run ./cmd/collector-aws-cloud` (long-running service)
**Kubernetes shape:** `Deployment`
**Source:** `go/cmd/collector-aws-cloud/`

## Workflow

```text
1. Load ESHU_COLLECTOR_INSTANCES_JSON; select one enabled aws instance whose
   claims_enabled = true.
2. Open Postgres (shared ESHU_POSTGRES_DSN or split DSNs).
3. Build ClaimedService:
     - WorkflowControlStore claim binding
     - awsruntime.ClaimedSource
     - SDKCredentialProvider
     - DefaultScannerFactory
     - AWS scan status writer
4. ClaimedService loop:
     - claim next aws work item
     - parse account_id, region, and service_kind from the acceptance unit
     - authorize the target against configured target_scopes
     - acquire account-scoped credentials
     - expire stale pagination checkpoints for this generation
     - run the service scanner
     - commit aws_resource, aws_relationship, aws_tag_observation, and warning
       facts through the shared facts boundary
     - record scanner status and commit status
     - heartbeat claim; release on success or terminal failure
```

Source entry points:

- Command wiring: `go/cmd/collector-aws-cloud/main.go`
- Configuration: `go/cmd/collector-aws-cloud/config.go`
- Claim runner wiring: `go/cmd/collector-aws-cloud/service.go`
- Commit-side status updates: `go/cmd/collector-aws-cloud/status_committer.go`
- Claim runtime: `go/internal/collector/awscloud/awsruntime/source.go`
- Scanner registry: `go/internal/collector/awscloud/awsruntime/scanner_factory.go`
- Status model: `go/internal/status/aws_cloud.go`
- Status JSON projection: `go/internal/status/json.go`

## Deployment Modes

AWS cloud collection supports two credential modes:

| Mode | Use when | Credential source |
| --- | --- | --- |
| `central_assume_role` | A central Eshu runtime scans target accounts through scoped read roles. | STS AssumeRole with a mandatory external ID. |
| `local_workload_identity` | The collector runs inside the target account or an account-local boundary. | AWS SDK default chain, usually IRSA on EKS. |

Static access-key fields are rejected during configuration parsing. Do not put
AWS keys in Helm values, ConfigMaps, or `ESHU_COLLECTOR_INSTANCES_JSON`.
Central AssumeRole targets must use an IAM role ARN in the configured
`account_id`; local workload identity targets must not set `role_arn` or
`external_id`.

## IAM Security Model

AWS recommends temporary role credentials for workloads, least-privilege
permissions, IAM Access Analyzer policy validation, and external IDs for
cross-account confused-deputy prevention. Eshu's AWS collector follows that
shape:

- `central_assume_role` requires `role_arn` and `external_id`.
- The configured `role_arn` account must match the target `account_id`.
- Wildcard `allowed_regions` and `allowed_services` entries are rejected.
- `allowed_services` must name a shipped scanner family, not arbitrary AWS
  service strings.
- `local_workload_identity` uses the pod/process role directly and rejects
  AssumeRole routing fields.
- ECS and Lambda scans require `ESHU_AWS_REDACTION_KEY` before startup because
  environment values are redacted before persistence.

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

Validate both the trust policy and the permissions policy before rollout:

```bash
aws accessanalyzer validate-policy \
  --policy-type TRUST_POLICY \
  --policy-document file://trust-policy.json

aws accessanalyzer validate-policy \
  --policy-type IDENTITY_POLICY \
  --policy-document file://permissions-policy.json
```

The permissions policy must stay read-only and service-scoped. Start from the
scanner families enabled in `allowed_services`, grant only the `List*`,
`Describe*`, and metadata `Get*` calls required by those scanners, and do not
grant AWS mutation APIs such as `Create*`, `Update*`, `Put*`, `Delete*`,
`Tag*`, or `Untag*`. Do not grant data-plane reads that the collector
intentionally avoids, including secret values, SSM parameter values, SQS
messages, DynamoDB table reads, log events, API execution/export, S3 object
contents, database contents, or Lambda code/package downloads.

## Configuration

### Required

| Env var | Purpose |
| --- | --- |
| `ESHU_POSTGRES_DSN` (or split fact/content DSNs) | Shared Postgres runtime loader. |
| `ESHU_COLLECTOR_INSTANCES_JSON` | One enabled `aws` collector instance with `claims_enabled: true`. |
| `ESHU_AWS_REDACTION_KEY` | Required when any target scope enables `ecs` or `lambda`; used for deterministic HMAC markers for environment values before persistence. |

### Optional

| Env var | Default | Purpose |
| --- | --- | --- |
| `ESHU_AWS_COLLECTOR_INSTANCE_ID` | first enabled AWS instance | Pick a specific instance when more than one AWS collector is configured. |
| `ESHU_AWS_COLLECTOR_OWNER_ID` | `HOSTNAME`, then `collector-aws-cloud` | Operator-readable owner name in claim rows. |
| `ESHU_AWS_COLLECTOR_POLL_INTERVAL` | runtime default | Claim poll cadence while no work is available. |
| `ESHU_AWS_COLLECTOR_CLAIM_LEASE_TTL` | workflow default | Per-claim lease duration. |
| `ESHU_AWS_COLLECTOR_HEARTBEAT_INTERVAL` | derived from lease TTL | Claim heartbeat cadence; keep it below the lease TTL. |

### Instance Configuration (JSON)

The instance entry in `ESHU_COLLECTOR_INSTANCES_JSON` carries the allowed AWS
scope. Minimal central AssumeRole shape:

```json
{
  "instance_id": "aws-primary",
  "collector_kind": "aws",
  "mode": "continuous",
  "enabled": true,
  "claims_enabled": true,
  "configuration": {
    "target_scopes": [
      {
        "account_id": "123456789012",
        "allowed_regions": ["us-east-1", "aws-global"],
        "allowed_services": ["iam", "ecr", "ecs", "ec2", "elbv2", "lambda", "eks", "route53", "sqs", "sns", "eventbridge", "s3", "rds", "dynamodb", "cloudwatchlogs", "cloudfront", "apigateway", "secretsmanager", "ssm"],
        "max_concurrent_claims": 1,
        "credentials": {
          "mode": "central_assume_role",
          "role_arn": "arn:aws:iam::123456789012:role/eshu-readonly",
          "external_id": "external-1"
        }
      }
    ]
  }
}
```

For EKS workload identity:

```yaml
awsCloudCollector:
  enabled: true
  instanceId: aws-primary
  serviceAccount:
    create: true
    annotations:
      eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/eshu-aws-collector
  collectorInstances:
    - instance_id: aws-primary
      collector_kind: aws
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        target_scopes:
          - account_id: "123456789012"
            allowed_regions: [us-east-1]
            allowed_services: [iam, ecr, ecs, ec2, elbv2, lambda, eks, route53]
            credentials:
              mode: local_workload_identity
```

The Helm chart renders `ESHU_COLLECTOR_INSTANCES_JSON`, the instance selector,
owner ID, Postgres env, OTEL env, probes, metrics service, optional
`ServiceMonitor`, `NetworkPolicy`, and `PodDisruptionBudget` for this runtime.
Use `awsCloudCollector.serviceAccount.create=true` for IRSA so AWS collector
permissions do not attach to the API, reducer, ingester, or other pods in the
same release.
The workflow coordinator chart remains dark-only in this branch, so production
deployments need an approved control-plane path that creates AWS workflow work.

## Supported Scanner Families

The production scanner registry currently covers:

- IAM roles, managed policies, instance profiles, and trust relationships.
- ECR repositories, lifecycle policies, and image references.
- ECS clusters, services, tasks, relationships, and redacted task definitions.
- EC2 VPC, subnet, security-group, rule, and ENI topology metadata.
- ELBv2 load balancers, listeners, listener rules, target groups, and routing
  relationships.
- Lambda functions, aliases, event-source mappings, image URIs, execution
  roles, subnets, and security groups with redacted environment values.
- EKS clusters, node groups, add-ons, OIDC providers, IAM roles, and network
  join evidence.
- Route 53 hosted zones and A, AAAA, CNAME, and ALIAS DNS record facts.
- SQS queues and reported dead-letter queue relationships.
- SNS topics and ARN-addressable subscription relationships.
- EventBridge event buses, rules, rule-to-bus relationships, and ARN-addressable
  targets.
- S3 buckets and server-access-log target bucket relationships.
- RDS DB instances, DB clusters, DB subnet groups, and reported relationship
  evidence for security groups, KMS keys, monitoring roles, IAM roles,
  parameter groups, and option groups.
- DynamoDB tables with directly reported KMS key relationships.
- CloudWatch Logs log groups with directly reported KMS key relationships.
- CloudFront distributions with reported ACM certificate and WAF web ACL
  relationships.
- API Gateway REST, HTTP, WebSocket, stage, custom-domain, mapping,
  access-log destination, ACM certificate, and ARN-addressable integration
  metadata.
- Secrets Manager secret metadata with KMS key and rotation Lambda
  relationships.
- SSM Parameter Store metadata with KMS key relationships.

The collector does not read object bodies, queue messages, database contents,
secret values, SSM parameter values, log events, API payloads, CloudFront origin
payloads, or DynamoDB items. It also does not mutate AWS resources.

## Operational Checks

Start with the service-local admin surface:

```bash
curl -fsS http://collector-aws-cloud.example/admin/status?format=json \
  | jq '.aws_cloud_scans[]'
```

Each `aws_cloud_scans` row is keyed by collector instance, account, region, and
service. The row separates scanner status from commit status:

```json
{
  "collector_instance_id": "aws-primary",
  "account_id": "123456789012",
  "region": "us-east-1",
  "service_kind": "lambda",
  "status": "succeeded",
  "commit_status": "succeeded",
  "api_call_count": 12,
  "throttle_count": 0,
  "warning_count": 0,
  "resource_count": 5,
  "relationship_count": 8,
  "tag_observation_count": 14,
  "budget_exhausted": false,
  "credential_failed": false
}
```

Use these rules:

- `status=succeeded` and `commit_status=succeeded`: the scanner observed the
  service and facts reached Postgres.
- `status=succeeded` and `commit_status=failed`: the AWS scan completed, but
  the fenced fact commit failed; inspect Postgres and ingestion-store logs.
- `credential_failed=true`: fix trust policy, external ID, IRSA, or workload
  identity before changing service scanners.
- `budget_exhausted=true`: the scanner yielded a partial result because its API
  budget ended. Lower claim fan-out or raise the service budget only after
  checking throttles and scan duration.
- `throttle_count > 0`: reduce per-account concurrency or schedule fewer
  same-account service claims at once.
- `aws_cloud_scans_truncated=true`: increase the status row cap only for
  operator inspection; do not treat truncation as scanner failure.

## Dashboard Starting Points

Use these panels or queries as the first dashboard for AWS collection:

| Question | Start with |
| --- | --- |
| Is the runtime up? | `eshu_runtime_health_state{service_name="collector-aws-cloud"}` |
| Is claim pressure building? | `eshu_runtime_queue_outstanding` and `eshu_runtime_queue_oldest_outstanding_age_seconds` for the collector runtime |
| Which accounts are active? | `eshu_dp_aws_claim_concurrency` |
| Which service is slow? | `eshu_dp_aws_scan_duration_seconds` split by `service`, `account`, and `region` |
| Are AWS APIs failing or throttling? | `eshu_dp_aws_api_calls_total`, `eshu_dp_aws_throttle_total`, and `eshu_dp_aws_assumerole_failed_total` |
| Are long scans resuming? | `eshu_dp_aws_pagination_checkpoint_events_total` |
| Did facts reach the boundary? | `eshu_dp_aws_resources_emitted_total`, `eshu_dp_aws_relationships_emitted_total`, and `eshu_dp_aws_tag_observations_emitted_total` |

Metric labels include account and region because the operator routing problem
is account-scoped. They still exclude ARNs, tags, digests, policy JSON, secret
names, parameter names, queue names, object keys, and raw AWS error payloads.

## Telemetry

All instruments are registered in `go/internal/telemetry/instruments.go`.
Service adapters record API counters and pagination spans; the claim runtime
records claim, credential, and service-scan spans. The full metric reference is
in [Telemetry Metrics](../reference/telemetry/metrics.md).

| Metric | Type | Labels | Question it answers |
| --- | --- | --- | --- |
| `eshu_dp_aws_api_calls_total` | counter | `service`, `account`, `region`, `operation`, `result` | Which AWS operations are succeeding or failing? |
| `eshu_dp_aws_throttle_total` | counter | `service`, `account`, `region` | Which account/service pairs are throttling? |
| `eshu_dp_aws_assumerole_failed_total` | counter | `account` | Which accounts cannot acquire claim-scoped credentials? |
| `eshu_dp_aws_budget_exhausted_total` | counter | `service`, `account`, `region` | Which scans are yielding partial results due to API budget? |
| `eshu_dp_aws_pagination_checkpoint_events_total` | counter | `service`, `account`, `region`, `operation`, `event_kind`, `result` | Are paginated scans saving, resuming, expiring, and completing checkpoints? |
| `eshu_dp_aws_resources_emitted_total` | counter | `service`, `account`, `region`, `resource_type` | Are resource facts crossing the collector boundary? |
| `eshu_dp_aws_relationships_emitted_total` | counter | `service`, `account`, `region` | Are relationship facts crossing the collector boundary? |
| `eshu_dp_aws_tag_observations_emitted_total` | counter | `service`, `account`, `region` | Are tag evidence facts available for reducer-owned normalization? |
| `eshu_dp_aws_claim_concurrency` | gauge | `account` | How many AWS claims are active per account? |
| `eshu_dp_aws_scan_duration_seconds` | histogram | `service`, `account`, `region`, `result` | How long does each service claim take before durable commit? |

Trace spans:

- `aws.collector.claim.process` - one workflow-claimed account, region, and
  service slice.
- `aws.credentials.assume_role` - claim-scoped credential acquisition.
- `aws.service.scan` - one service scanner run before durable commit.
- `aws.service.pagination.page` - one AWS SDK paginated page or point read.

## Common Failures

| Symptom | Likely cause | First check |
| --- | --- | --- |
| Runtime is healthy but no AWS rows appear | No workflow work exists, wrong instance ID, or `claims_enabled=false`. | `/admin/status?format=json`, collector logs, and `ESHU_COLLECTOR_INSTANCES_JSON`. |
| `credential_failed=true` or `eshu_dp_aws_assumerole_failed_total` rises | Bad role ARN, missing external ID, denied trust policy, or broken IRSA. | Trust policy, service account annotation, STS logs, and `aws.credentials.assume_role` spans. |
| `budget_exhausted=true` | API budget ended before the service scan finished. | `eshu_dp_aws_budget_exhausted_total`, scan duration, and pagination checkpoint events. |
| Throttle counters rise | Too many same-account service claims or service-specific AWS throttling. | `eshu_dp_aws_throttle_total` by service/account/region and `eshu_dp_aws_claim_concurrency`. |
| Scanner succeeds but commit fails | Postgres, schema, or ingestion-store failure after AWS scan. | `commit_status`, Postgres spans, and collector logs around the fenced commit. |
| Missing ECS or Lambda facts at startup | Redaction key not configured. | `ESHU_AWS_REDACTION_KEY` Secret and startup logs. |
| `aws_cloud_scans_truncated=true` | Status output hit the configured row cap. | Inspect the returned rows first, check backing status rows by account/region/service, or temporarily raise the cap. |

## Escalation

Escalate in this order:

1. Confirm claim scope: instance ID, `claims_enabled`, account, region, service,
   and max per-account concurrency.
2. Confirm credentials: IRSA annotation or target-account role ARN, external ID,
   trust policy, and read-only IAM permissions.
3. Confirm AWS API health: throttles, budget exhaustion, scan duration, and
   pagination checkpoint progress.
4. Confirm persistence: scanner status versus commit status, Postgres spans,
   and fact counts.
5. Confirm reducer/query readiness: use reducer status and
   `POST /api/v0/iac/unmanaged-resources` only after AWS facts and relevant
   Terraform-state facts are committed.

Do not broaden IAM permissions or raise concurrency as the first response to a
stalled scan. Prove whether the blocker is credential acquisition, AWS API
throttling, pagination, Postgres commit, or downstream reducer work first.

## Validation

Use focused non-live checks for normal PR validation:

```bash
cd go
go test ./cmd/collector-aws-cloud ./internal/collector/awscloud/... -count=1
go test ./internal/collector/awscloud/awsruntime \
  -run 'TestClaimedSourceRecordsEmissionCounters|TestClaimedSourceRecordsScanStatusWithAPICallStats' \
  -count=1 -v
```

Use the docs build when this page or the docs nav changes:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
```

Live AWS smokes are operator-controlled and must use read-only target roles.
Keep account IDs, role ARNs, external IDs, and any local AWS profiles out of
committed docs and PR comments unless they are non-secret examples.

## Related Docs

- [Service Runtimes](../deployment/service-runtimes.md)
- [Helm Values](../deploy/kubernetes/helm-values.md)
- [Telemetry Overview](../reference/telemetry/index.md)
- [Runtime Admin API](../reference/runtime-admin-api.md)
- [AWS Cloud Scanner ADR](../adrs/2026-04-20-aws-cloud-scanner-collector.md)
