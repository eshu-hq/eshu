# AWS Cloud Collector

Use this page to understand the AWS collector's runtime boundary and operator
path. Credentials, IAM, and redaction live in
[AWS Collector Security And Config](collector-aws-cloud-security.md). Scanner
coverage lives in [AWS Collector Scanner Coverage](collector-aws-cloud-scanners.md).

`collector-aws-cloud` is a claim-driven worker for AWS control-plane metadata.
It claims coordinator-created `(account_id, region, service_kind)` work, checks
the selected collector instance, obtains claim-scoped credentials, runs one
metadata-only scanner, records scanner status, and commits reported facts
through the ingestion boundary.

It does not schedule AWS work, mutate AWS resources, write graph truth, or
infer service ownership.

| Runtime | Value |
| --- | --- |
| Binary | `/usr/local/bin/eshu-collector-aws-cloud` |
| Kubernetes shape | optional `Deployment` |
| Command package | `go/cmd/collector-aws-cloud/` |
| Runtime package | `go/internal/collector/awscloud/awsruntime/` |
| Service package root | `go/internal/collector/awscloud/services/` |

## Operator Path

1. Run the workflow coordinator in active claim mode.
2. Configure one enabled `aws` collector instance with `claims_enabled=true`.
3. Use exact target scopes: 12-digit account, concrete regions, concrete
   service kinds, per-account concurrency, and one credential mode.
4. Mount `ESHU_AWS_REDACTION_KEY` when any target scope enables ECS or Lambda.
5. Check `/healthz`, `/readyz`, `/metrics`, and `/admin/status?format=json`.
6. Confirm scanner status and commit status before debugging reducer or query
   results.

## Runtime Flow

```text
ESHU_COLLECTOR_INSTANCES_JSON
  -> select aws instance
  -> claim workflow item
  -> parse account/region/service target
  -> authorize target scope
  -> acquire credentials
  -> run scanner
  -> commit aws_* facts
  -> write scanner and commit status
  -> heartbeat/release claim
```

Facts are reported evidence. Reducer domains decide whether AWS observations
become drift, ownership, deployment, or graph truth.

## Status And Failure Boundary

Start with collector status before changing scanner logic:

- `status` says whether the AWS read succeeded.
- `commit_status` says whether the fenced fact transaction reached Postgres.
- `api_call_count`, `throttle_count`, `budget_exhausted`, and
  `credential_failed` identify pressure, budget, and credential failures.
- Resource, relationship, and tag-observation counts show emitted fact volume.

`status=succeeded` with `commit_status=failed` means AWS collection worked and
fact persistence failed. Fix that before debugging downstream reducers.

Do not broaden IAM or raise concurrency first. Prove whether the blocker is
credentials, AWS throttling, pagination, fact commit, or downstream reducer
work.

## Telemetry

Dashboard starting points:

- runtime health: `eshu_runtime_health_state{service_name="collector-aws-cloud"}`
- account concurrency: `eshu_dp_aws_claim_concurrency`
- scanner latency: `eshu_dp_aws_scan_duration_seconds`
- API pressure: `eshu_dp_aws_api_calls_total`,
  `eshu_dp_aws_throttle_total`, `eshu_dp_aws_assumerole_failed_total`
- pagination: `eshu_dp_aws_pagination_checkpoint_events_total`
- emitted facts: `eshu_dp_aws_resources_emitted_total`,
  `eshu_dp_aws_relationships_emitted_total`,
  `eshu_dp_aws_tag_observations_emitted_total`

Metric labels may include account and region. They must not include ARNs, tags,
digests, policy JSON, secret names, parameter names, queue names, object keys,
or raw AWS error payloads.

## Related Docs

- [AWS Collector Security And Config](collector-aws-cloud-security.md)
- [AWS Collector Scanner Coverage](collector-aws-cloud-scanners.md)
- [Collector Runtime Services](../deployment/service-runtimes-collectors.md)
- [Helm Collector And Webhook Values](../deploy/kubernetes/helm-collector-and-webhook-values.md)
- [Collector And Reducer Readiness](../reference/collector-reducer-readiness.md)
