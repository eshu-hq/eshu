# AGENTS.md - internal/collector/awscloud guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - collector, service, resource, relationship, and observation
   contracts.
3. `apicall.go` and `scan_status.go` - bounded API-call accounting and
   durable scan-status contracts.
4. `envelope.go` - durable fact-envelope construction and validation.
5. Service package docs under `services/` before changing scanner-specific
   behavior.
6. `docs/docs/adrs/2026-04-20-aws-cloud-scanner-collector.md` - AWS collector
   source-truth, claim, and credential contract.
7. `docs/docs/guides/collector-authoring.md` - general collector fact
   contract.

## Invariants

- AWS cloud data is reported source evidence. Do not materialize graph truth in
  this package.
- Keep the claim boundary explicit: account, region, service kind, scope,
  generation, collector instance, and fencing token.
- Preserve generation-specific `FactID` values and source-stable
  `StableFactKey` values.
- Never put secrets, session tokens, presigned URLs, full policies, tags, ARNs,
  or resource names in metric labels.
- Keep `APICallEvent` low-cardinality. It may carry service, account, region,
  operation, result, and throttle state only.
- Redact ECS task-definition environment values before persistence; preserve
  secret `value_from` references without resolving them.
- Redact Lambda function environment values before persistence; preserve image
  URI, alias, event-source, execution-role, subnet, and security-group evidence
  without inferring workload truth.
- Preserve EKS OIDC provider, node group, add-on, IAM role, subnet, and
  security group evidence without inferring Kubernetes workload or ownership
  truth.
- Keep SQS message bodies and queue policy JSON out of facts. Redrive metadata
  is allowed only as reported queue attributes and dead-letter queue
  relationship evidence.
- Keep SNS message payloads, topic policy JSON, delivery-policy JSON,
  data-protection-policy JSON, and raw non-ARN subscription endpoints out of
  facts. ARN subscription endpoints may be reported relationship evidence.
- Keep EventBridge event payloads, mutation APIs, event bus policy JSON, target
  input fields, target transformers, HTTP target parameters, and raw non-ARN
  targets out of facts. ARN target endpoints may be reported relationship
  evidence.
- Keep S3 object inventory, object keys, bucket policy JSON, ACL grants,
  replication rules, lifecycle rules, notification configuration, inventory
  configuration, analytics configuration, and metrics configuration out of
  facts. Server-access-log target buckets may be reported relationship
  evidence.
- Keep RDS database connections, database names, master usernames, passwords,
  snapshots, log contents, Performance Insights samples, schemas, tables, and
  row data out of facts. RDS dependency edges are reported metadata only.
- Keep DynamoDB item values, table scans, table queries, stream records,
  backup/export payloads, resource policies, PartiQL output, and mutations out
  of facts. DynamoDB table metadata and KMS dependency edges are reported
  metadata only.
- Keep CloudWatch Logs log events, log stream payloads, Insights query results,
  export payloads, resource policies, subscription payloads, and mutations out
  of facts. CloudWatch Logs log group metadata and KMS dependency edges are
  reported metadata only.
- Keep CloudFront object contents, origin payloads, distribution config
  payloads, policy documents, certificate bodies, private keys, origin custom
  header values, and mutations out of facts. Distribution metadata, tags, and
  directly reported ACM certificate and WAF web ACL edges are reported metadata
  only.
- Keep API Gateway execution, exports, API keys, authorizer secrets, policy
  JSON, integration credentials, stage variable values, template bodies,
  payloads, and mutations out of facts. API Gateway API, stage, domain, mapping,
  certificate, access-log destination, and ARN-addressable integration edges are
  reported metadata only.
- Keep Secrets Manager secret values, version payloads, resource policy JSON,
  external rotation partner metadata, external rotation role ARNs, and mutations
  out of facts. Secret metadata, tags, KMS key dependencies, and rotation Lambda
  dependencies are reported metadata only.
- Keep ELBv2 target health out of facts; it is live/noisy state, not stable
  topology truth.
- Keep EC2 instance inventory out of the EC2 scanner; ENI attachment target
  ARNs are metadata only.
- Keep AWS SDK calls out of this package. Runtime adapters own SDK pagination,
  retries, throttling, and credential loading.

## Common Changes

- Add a new AWS service by adding service constants here, a service package
  under `services/`, scanner tests, a service `awssdk` adapter, package docs,
  and a branch in `awsruntime.DefaultScannerFactory`.
- For that new service package, include `doc.go`, `README.md`, and `AGENTS.md`
  before merge and run `scripts/verify-package-docs.sh`.
- If the service adds pagination fanout, claim concurrency, batch sizing,
  queue pressure, or downstream graph/materialization pressure, run
  `scripts/verify-performance-evidence.sh` and add tracked
  Performance Evidence plus Observability Evidence markers naming the
  input shape, queue/resource counts, and exact metrics/spans/logs/status
  fields.
- Add a new fact envelope only after `internal/facts` exposes the fact kind and
  schema version.
- Add redaction or credential rules at the runtime boundary unless the value is
  part of the durable envelope contract.

## What Not To Change Without An ADR

- Do not make this package call AWS APIs directly.
- Do not add graph writes, reducer admission, or query behavior here.
- Do not infer environment, workload, ownership, or deployable-unit truth from
  names, tags, folders, or account aliases in this package.
