# AWS Cloud Collector

`collector-aws-cloud` is a claim-driven worker that observes AWS control-plane
metadata and commits reported cloud facts through the shared ingestion boundary.
It does not schedule AWS work, write graph truth, or infer service ownership.

The workflow coordinator creates claimable `(account_id, region, service_kind)`
work items. The collector claims one item, validates it against the configured
target scopes, obtains claim-scoped credentials, scans the requested service,
records scanner status, and commits facts to Postgres.

| Runtime | Value |
| --- | --- |
| Binary | `go run ./cmd/collector-aws-cloud` |
| Kubernetes shape | `Deployment` |
| Command package | `go/cmd/collector-aws-cloud/` |
| Fact package | `go/internal/collector/awscloud/` |
| Claim runtime | `go/internal/collector/awscloud/awsruntime/` |

## Read Next

| Need | Read |
| --- | --- |
| Credential modes, target scopes, IAM guardrails, and redaction | [AWS Collector Security And Config](collector-aws-cloud-security.md) |
| Supported AWS services and what each scanner does not read | [AWS Collector Scanner Coverage](collector-aws-cloud-scanners.md) |
| Helm values for the collector deployment | [Helm Collector And Webhook Values](../deploy/kubernetes/helm-collector-and-webhook-values.md) |
| Collector metrics | [Ingestion And Collector Metrics](../reference/telemetry/metrics-ingestion-collectors.md) |

## Workflow

```text
1. Load ESHU_COLLECTOR_INSTANCES_JSON.
2. Select one enabled aws instance with claims_enabled=true.
3. Open Postgres and workflow control stores.
4. Claim the next AWS work item.
5. Parse account_id, region, and service_kind from the claim target.
6. Verify the target is allowed by configured target_scopes.
7. Acquire credentials for the target.
8. Expire stale pagination checkpoints for this generation.
9. Run the service scanner.
10. Commit aws_resource, aws_relationship, aws_tag_observation, image, DNS,
    or warning facts through the shared ingestion boundary.
11. Record scanner status and commit status.
12. Heartbeat and release the claim on success or terminal failure.
```

Source entry points:

- `go/cmd/collector-aws-cloud/main.go`
- `go/cmd/collector-aws-cloud/config.go`
- `go/cmd/collector-aws-cloud/service.go`
- `go/cmd/collector-aws-cloud/status_committer.go`
- `go/internal/collector/awscloud/awsruntime/source.go`
- `go/internal/collector/awscloud/awsruntime/registry.go`

## Configuration Shape

The collector requires one enabled `aws` collector instance with
`claims_enabled=true`.

```json
{
  "instance_id": "aws-primary",
  "collector_kind": "aws",
  "mode": "continuous",
  "enabled": true,
  "claims_enabled": true,
  "configuration": {
    "scheduled_scan_enabled": true,
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
}
```

For EKS workload identity, use the Helm chart to render the ServiceAccount and
`ESHU_COLLECTOR_INSTANCES_JSON`:

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
        scheduled_scan_enabled: true
        target_scopes:
          - account_id: "123456789012"
            allowed_regions: [us-east-1]
            allowed_services: [iam, ecr, ecs, lambda]
            credentials:
              mode: local_workload_identity
```

`scheduled_scan_enabled=true` lets the active workflow coordinator plan one
bounded AWS work item per configured `(account_id, region, service_kind)` tuple.
Leave it unset when a deployment should react only to AWS freshness triggers.

## Freshness Triggers

AWS freshness is a targeted wake-up path for AWS Config and EventBridge
signals. The webhook listener normalizes provider events into a concrete
`(account_id, region, service_kind)` target, stores a row in
`aws_freshness_triggers`, and lets the workflow coordinator enqueue ordinary
AWS collector work.

Freshness triggers do not write graph truth and do not replace scheduled scans.
Even when an event names one resource, the collector rescans the affected
service tuple so relationships, tags, and dependent metadata match the normal
snapshot path.

The default webhook path is `/webhooks/aws/eventbridge`. Configure the webhook
listener with `webhookListener.awsFreshness.*` Helm values.

## Operational Checks

Start with the service-local admin surface:

```bash
curl -fsS http://collector-aws-cloud.example/admin/status?format=json \
  | jq '.aws_cloud_scans[]'
```

Each `aws_cloud_scans` row is keyed by collector instance, account, region, and
service. It separates scanner status from durable commit status:

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

- `status=succeeded` and `commit_status=succeeded`: facts reached Postgres.
- `status=succeeded` and `commit_status=failed`: the AWS scan completed, but
  the fenced fact commit failed.
- `credential_failed=true`: fix trust policy, external ID, IRSA, or workload
  identity before changing scanners.
- `budget_exhausted=true`: the scanner yielded a partial result because its API
  budget ended.
- `throttle_count > 0`: reduce same-account concurrency or schedule fewer
  same-account service claims.
- `aws_cloud_scans_truncated=true`: status output hit the row cap; inspect the
  returned rows before treating it as scanner failure.

## Dashboard Starting Points

| Question | Start with |
| --- | --- |
| Is the runtime up? | `eshu_runtime_health_state{service_name="collector-aws-cloud"}` |
| Is claim pressure building? | Runtime queue outstanding and oldest-age metrics. |
| Which accounts are active? | `eshu_dp_aws_claim_concurrency` |
| Which service is slow? | `eshu_dp_aws_scan_duration_seconds` |
| Are AWS APIs failing or throttling? | `eshu_dp_aws_api_calls_total`, `eshu_dp_aws_throttle_total`, `eshu_dp_aws_assumerole_failed_total` |
| Are paginated scans resuming? | `eshu_dp_aws_pagination_checkpoint_events_total` |
| Did facts reach the boundary? | `eshu_dp_aws_resources_emitted_total`, `eshu_dp_aws_relationships_emitted_total`, `eshu_dp_aws_tag_observations_emitted_total` |

Metric labels include account and region because AWS operations are routed by
account. They do not include ARNs, tags, digests, policy JSON, secret names,
parameter names, queue names, object keys, or raw AWS error payloads.

## Escalation

1. Confirm claim scope: instance ID, `claims_enabled`, account, region,
   service, and max per-account concurrency.
2. Confirm credentials: IRSA annotation or target-account role ARN, external
   ID, trust policy, and read-only IAM permissions.
3. Confirm AWS API health: throttles, budget exhaustion, scan duration, and
   pagination checkpoint progress.
4. Confirm persistence: scanner status versus commit status, Postgres spans,
   and fact counts.
5. Confirm reducer/query readiness only after AWS facts and relevant
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

Live AWS smokes are operator-controlled and must use read-only target roles.
Keep account IDs, role ARNs, external IDs, and local AWS profiles out of
committed docs and PR comments unless they are non-secret examples.

## Related Docs

- [Service Runtimes](../deployment/service-runtimes.md)
- [Collector Service Runtimes](../deployment/service-runtimes-collectors.md)
- [Helm Values](../deploy/kubernetes/helm-values.md)
- [Runtime Admin API](../reference/runtime-admin-api.md)
- [Collector And Reducer Readiness](../reference/collector-reducer-readiness.md)
