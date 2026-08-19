# AGENTS.md - internal/collector/awscloud guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - `CollectorKind` and the shared observation contracts
   (`Boundary`, `ResourceObservation`, `RelationshipObservation`,
   `ImageReferenceObservation`, `DNSRecordObservation`, `DNSAliasTarget`,
   `DNSRoutingPolicy`, `DNSGeoLocation`, `WarningObservation`).
3. `constants_<service>.go` (one file per AWS service slice, plus
   `constants_common.go` for cross-service targets like `ResourceTypeAWSAccount`
   and `guardduty_types.go` for the GuardDuty slice) - service, resource type,
   and relationship constants. New service constants MUST land in their own
   `constants_<service>.go` sibling, not back in `types.go`, so the 500-line
   cap stays satisfied.
4. `apicall.go` and `scan_status.go` - bounded API-call accounting and
   durable scan-status contracts.
5. `redaction.go` - AWS launch sensitive-key/provider policy and shared
   redaction payload helper.
6. `envelope.go` - durable fact-envelope construction and validation.
7. Service package docs under `services/` before changing scanner-specific
   behavior.
8. `docs/public/services/collector-aws-cloud.md` - AWS collector
   source-truth, claim, and credential contract.
9. `docs/public/guides/collector-authoring.md` - general collector fact
   contract.

## Why This Package Sits Above The Directory File Cap

The `dirgate` linter caps a package directory at 40 non-test `.go` files. This
directory holds 154 and is held open by a grandfather row in
`tools/golangci-lint-dirgate/grandfather.go`
(`"internal/collector/awscloud": {FileCount: 154, ...}`). That is deliberate, and
the gap grows by one file every time an AWS service is added, because the
`constants_<service>.go` convention above requires exactly that.

**Consolidating the `constants_*.go` files to get under the cap is not the fix,
and has been considered and rejected.** Grouping the 133 files (7,283 lines) into
roughly seventeen ~430-line files would bring the directory to about 38 non-test
files, but it would:

- reverse the deliberate split recorded in `README.md` under "Refactor Evidence
  (types.go Constants Split)", which exists to keep files under the 500-line cap;
- contradict the "Common Changes" rule below, which requires a new service's
  constants to land in their own sibling file; and
- turn additive per-service work into edits on shared files, so parallel service
  additions start colliding where today they cannot.

Moving the constants into the per-service packages under `services/` is also not
available at a sane cost. Every dependent takes a plain, direct import — there
are no dot-imports, blank imports, aliased imports, or package-level re-exports
of these constants — so the dependent set is exactly the set of files importing
the package:

```bash
rg -l --type go '"github.com/eshu-hq/eshu/go/internal/collector/awscloud"' go/
```

**1,312 files**: 1,243 inside this directory's own subpackages (`services/`,
`awsruntime/`, …) and 69 elsewhere, of which 5 are non-test. Go forbids a
duplicate import, so `rg -c` totals 1,312 as well — the file count and the
occurrence count agree, which is the cheapest available check that this number
is not counting something else.

Count imports, not symbol references. `rg 'awscloud\.(ResourceType|Service|Relationship)[A-Z]'`
looks like the more direct measure and is not: without `--type go` it also
matches ~280 package `README.md`/`AGENTS.md` files, and even restricted to Go it
counts files whose only mention is a doc comment — eight of them, seven under
`go/internal/reducer/` and one under `go/internal/storage/postgres/`, which
deliberately mirror a constant's value and name it in prose without importing
the package at all.

A service-aligned move would not be a uniform rewrite of all 1,312: a
`services/ec2` file referencing `awscloud.ResourceTypeEC2Instance` would find it
locally afterwards, while the same file's reference to
`awscloud.ResourceTypeKMSKey` still needs an import, because these are
cross-resource relationship constants and cross-service references are common.
The cost is large and unevenly distributed, which is enough to reject it — the
precise figure is not what carries the argument.

If the file count needs to come down, the candidate is the 21 non-constants
files, or splitting the package along an ownership seam — not the per-service
constants. Re-pin the grandfather row when the count legitimately changes; see
`tools/golangci-lint-dirgate/README.md`.

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
- Redact ECS task-definition environment values through `RedactString` before
  persistence; preserve secret `value_from` references without resolving them.
- Redact Lambda function environment values through `RedactString` before
  persistence; preserve image URI, alias, event-source, execution-role, subnet,
  and security-group evidence without inferring workload truth.
- Keep the AWS redaction payload versioned with `RedactionPolicyVersion`.
  Unknown environment keys fail closed as `unknown_provider_schema`; known
  sensitive key names use `known_sensitive_key`.
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
- Keep GuardDuty finding bodies, filter criteria expressions, threat intel set
  list contents, IP set list contents, and mutation APIs out of facts.
  Aggregate finding counts by severity and finding type are allowed; full
  finding details are not.
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
- Keep SSM parameter values, history values, raw descriptions, raw allowed
  patterns, raw policy JSON, decrypted content, and mutations out of facts.
  Parameter metadata, tags, safe policy type/status metadata, and KMS key
  dependencies are reported metadata only.
- Keep Athena StartQueryExecution, StopQueryExecution, query result rows,
  query execution result location object contents, named-query SQL bodies,
  prepared-statement query bodies, query history strings, and mutation APIs
  out of facts. Workgroup, data catalog, prepared-statement, and named-query
  metadata plus workgroup-to-S3-result-bucket, workgroup-to-KMS-key,
  prepared-statement-to-workgroup, and named-query-to-workgroup relationship
  evidence are reported metadata only.
- Keep Glue job script bodies, job default-argument values, secret-shaped
  argument keys, connection passwords, connection JDBC credential URLs,
  connection property values, table column statistics with sample values,
  classifier custom patterns, workflow graph payloads, workflow run state, and
  mutations out of facts. Database, table, crawler, job, trigger, workflow,
  and connection metadata plus reported table-in-database,
  table-to-S3-location, crawler-to-database, crawler-to-IAM-role,
  job-to-IAM-role, and trigger-to-job relationships are reported metadata
  only. The Glue SDK adapter must call GetConnections with HidePassword=true
  and GetWorkflow with IncludeGraph=false.
- Keep ElastiCache AUTH tokens, user passwords, user access strings, cache
  keys, cache values, snapshot data, and mutations out of facts. Cache cluster,
  replication group, parameter group, subnet group, user, and user group
  metadata, snapshot name/source/status, and reported VPC, subnet, KMS,
  replication-group-cluster, and user-group-user edges are reported metadata
  only. Drop `User.Passwords` and `User.AccessString` at the adapter boundary.
- Keep IAM Access Analyzer external finding bodies, archive-rule filter
  criteria, policy-generation output, and per-action unused-access details out
  of facts. Analyzer metadata, archive-rule names, aggregate finding counts,
  analyzer relationships, and per-resource unused-access last-accessed
  summaries are reported metadata only.
- Keep Organizations policy document bodies, statements, conditions, action
  lists, account lifecycle mutations, policy mutations, delegated-admin
  mutations, and service-access mutations out of facts. Organization roots,
  OUs, accounts, policy summaries, target bindings, and delegated
  administrators are reported metadata only. Redact account email and account
  name values through `RedactString` before persistence.
- Keep ELBv2 target health out of facts; it is live/noisy state, not stable
  topology truth.
- Keep EC2 instance inventory out of the EC2 scanner; ENI attachment target
  ARNs are metadata only.
- Keep EC2 EBS volume facts source-only. They may report encrypted/KMS and
  attachment metadata from `DescribeVolumes`, but reducers own block-device/KMS
  posture truth.
- Keep AWS SDK calls out of this package. Runtime adapters own SDK pagination,
  retries, throttling, and credential loading.

## Common Changes

- Add a new AWS service by creating a new `constants_<service>.go` sibling
  (one file holds the `Service<X>`, `ResourceType<X>...`, and
  `Relationship<X>...` constants for that slice), a service package under
  `services/`, scanner tests, a service `awssdk` adapter, package docs, and a
  branch in `awsruntime.DefaultScannerFactory`. Do not grow `types.go` with
  new service-specific constants; it stays at the shared observation
  contracts only.
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
