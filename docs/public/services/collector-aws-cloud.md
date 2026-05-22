# AWS Cloud Collector

`collector-aws-cloud` is a claim-driven worker for AWS control-plane metadata.
It claims coordinator-created `(account_id, region, service_kind)` work,
verifies that the target is allowed by the selected collector instance, uses
claim-scoped credentials, runs one metadata-only scanner, records scanner
status, and commits reported facts through the shared ingestion boundary.

It does not schedule AWS work, mutate AWS resources, write graph truth, or infer
service ownership.

| Runtime | Value |
| --- | --- |
| Binary | `/usr/local/bin/eshu-collector-aws-cloud` |
| Kubernetes shape | optional `Deployment` |
| Command package | `go/cmd/collector-aws-cloud/` |
| Runtime package | `go/internal/collector/awscloud/awsruntime/` |
| Service package root | `go/internal/collector/awscloud/services/` |

## Operator Path

1. Enable an active workflow coordinator with claims.
2. Configure one enabled `aws` collector instance with `claims_enabled=true`.
3. Configure exact target scopes: 12-digit account, concrete regions, concrete
   service kinds, per-account concurrency, and one credential mode.
4. Mount `ESHU_AWS_REDACTION_KEY` when any target scope enables `ecs` or
   `lambda`.
5. Check `/healthz`, `/readyz`, `/metrics`, and `/admin/status?format=json` on
   the collector.
6. Confirm scanner status and commit status before debugging reducer or query
   results.

Configuration details live in
[AWS Collector Security And Config](collector-aws-cloud-security.md). Scanner
coverage and forbidden data classes live in
[AWS Collector Scanner Coverage](collector-aws-cloud-scanners.md).

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

Facts are reported evidence. Reducer domains decide whether any AWS observation
becomes drift, ownership, deployment, or graph truth.

## Status

Start with the service-local admin surface:

```bash
curl -fsS http://collector-aws-cloud.example/admin/status?format=json \
  | jq '.aws_cloud_scans[]'
```

Each row is keyed by collector instance, account, region, and service. The key
fields are:

| Field | Meaning |
| --- | --- |
| `status` | Scanner result for the AWS read. |
| `commit_status` | Whether the fenced fact transaction reached Postgres. |
| `api_call_count` / `throttle_count` | AWS API pressure for this scanner run. |
| `warning_count` | Non-fatal scanner warnings. |
| `resource_count`, `relationship_count`, `tag_observation_count` | Emitted fact counts. |
| `budget_exhausted` | Scanner stopped after its API budget. |
| `credential_failed` | Credential acquisition failed before scanning. |

`status=succeeded` with `commit_status=failed` means AWS collection worked but
fact persistence did not. Fix that before changing scanner logic.

## Failure Modes

| Symptom | First check |
| --- | --- |
| Runtime starts but never claims | Workflow coordinator mode, selected instance ID, and `claims_enabled=true`. |
| `credential_failed=true` | IRSA annotation, role ARN account, external ID, trust policy, and STS spans. |
| ECS or Lambda target fails at startup | `ESHU_AWS_REDACTION_KEY` Secret and startup logs. |
| Throttles rise | Same-account claim concurrency and enabled service count. |
| `budget_exhausted=true` | Scanner API budget and pagination checkpoint progress. |
| Facts missing downstream | Scanner status, commit status, Postgres spans, then reducer queues. |

Do not broaden IAM or raise concurrency as the first response. Prove whether the
blocker is credentials, AWS throttling, pagination, fact commit, or downstream
reducer work.

## Metrics

Use these as dashboard starting points:

| Question | Signal |
| --- | --- |
| Is the runtime healthy? | `eshu_runtime_health_state{service_name="collector-aws-cloud"}` |
| Which accounts are active? | `eshu_dp_aws_claim_concurrency` |
| Which service is slow? | `eshu_dp_aws_scan_duration_seconds` |
| Are AWS APIs failing or throttling? | `eshu_dp_aws_api_calls_total`, `eshu_dp_aws_throttle_total`, `eshu_dp_aws_assumerole_failed_total` |
| Are paginated scans resuming? | `eshu_dp_aws_pagination_checkpoint_events_total` |
| Did facts reach the boundary? | `eshu_dp_aws_resources_emitted_total`, `eshu_dp_aws_relationships_emitted_total`, `eshu_dp_aws_tag_observations_emitted_total` |

Metric labels may include account and region. They must not include ARNs, tags,
digests, policy JSON, secret names, parameter names, queue names, object keys,
or raw AWS error payloads.

## Validation

Use focused non-live checks for normal PR validation:

```bash
cd go
go test ./cmd/collector-aws-cloud ./internal/collector/awscloud/... -count=1
go run ./cmd/eshu docs verify ../docs/public/services/collector-aws-cloud.md \
  --limit 1200 --fail-on contradicted,missing_evidence
```

Live AWS smokes are operator-controlled and must use read-only target roles.
Keep real account IDs, role ARNs, external IDs, and local AWS profiles out of
committed docs and PR comments.

## Related Docs

- [AWS Collector Security And Config](collector-aws-cloud-security.md)
- [AWS Collector Scanner Coverage](collector-aws-cloud-scanners.md)
- [Collector Service Runtimes](../deployment/service-runtimes-collectors.md)
- [Helm Collector And Webhook Values](../deploy/kubernetes/helm-collector-and-webhook-values.md)
- [Runtime Admin API](../reference/runtime-admin-api.md)
- [Collector And Reducer Readiness](../reference/collector-reducer-readiness.md)
