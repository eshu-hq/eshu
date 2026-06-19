# AGENTS.md - internal/collector/awscloud/awsruntime guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - target, credential, scanner, and config contracts.
3. `credentials.go` - AWS SDK config, STS AssumeRole, and lease release.
4. `registry.go` - production service scanner registry.
5. `source.go` - claim validation, target authorization, checkpoint expiry, and generation
   construction.
6. `fixture_source.go` - offline fixture/replay `collector.Source`
   (`FixtureSource`) plus its declarative config (`FixtureConfig`,
   `FixtureScope`, `FixtureResource`, `FixtureRelationship`).
7. `scan_status.go` - scanner-side durable status projection.
7. `../checkpoint/README.md` - durable pagination checkpoint contract.
8. `../README.md` - shared AWS fact-envelope contract.
9. `docs/public/services/collector-aws-cloud.md` - runtime and
   credential requirements.

## Invariants

- Authorize `(account_id, region, service_kind)` before acquiring credentials.
- Keep static AWS credentials out of this package and out of tests.
- Keep `fixture_source.go` fully offline: no AWS SDK imports, no network, no
  credentials. It must reuse `awscloud.NewResourceEnvelope` /
  `awscloud.NewRelationshipEnvelope` so fixture facts stay byte-identical to
  live facts, and must derive generation ids from the scope id only (never the
  clock) so replay is idempotent.
- Preserve `aws.RetryModeAdaptive` on every loaded AWS SDK config.
- Pass the command-validated STS external ID for central AssumeRole targets.
- Keep the command-side credential guard intact: central scopes require a
  same-account role ARN and external ID, while local workload identity scopes
  reject AssumeRole routing fields.
- Preserve claim fencing by copying `CurrentFencingToken` into every AWS
  boundary and warning fact.
- Expire pagination checkpoints for prior generations before building service
  scanners.
- Record AWS scan status after claim start and after scanner completion when a
  scan-status store is configured. Scanner status is not the same as durable
  fact commit status.
- Release credential leases even when scanner construction or service scanning
  fails.
- Keep resource ARNs, policy JSON, tags, account names, and raw error payloads
  out of metric labels.
- Keep ECR lifecycle policy JSON and image digests out of metric labels.
- Keep ECS task-definition environment values out of persisted payloads unless
  they are replaced by `internal/redact` markers.
- Keep ELBv2 target health out of service scans; it is live/noisy status, not
  stable routing topology.
- Keep Route 53 DNS names, hosted-zone IDs, and record values out of metric
  labels.
- Keep EC2 instance inventory out of EC2 service scans; collect ENI attachment
  metadata only.
- Keep SQS queue scans metadata-only. Do not wire ReceiveMessage, queue
  mutations, or queue policy persistence through the runtime registry.
- Keep SNS topic scans metadata-only. Do not wire Publish, subscription
  mutations, topic policy persistence, data-protection-policy persistence, or
  raw non-ARN endpoint persistence through the runtime registry.
- Keep EventBridge scans metadata-only. Do not wire PutEvents, rule or target
  mutations, event bus policy persistence, target input payload persistence,
  input-transformer persistence, HTTP-parameter persistence, or raw non-ARN
  target persistence through the runtime registry.
- Keep GuardDuty scans metadata-only. Do not wire finding-body reads, filter
  criteria reads, threat intel/IP list content fetches, invitation/member
  mutations, publishing destination mutations, set mutations, or finding
  feedback mutations through the runtime registry.
- Keep S3 scans metadata-only. Do not wire object inventory, object keys, bucket
  policy JSON, ACL grants, replication, lifecycle, notification, inventory,
  analytics, metrics, or mutation APIs through the runtime registry.
- Keep RDS scans metadata-only. Do not wire database connections, database
  names, master usernames, secrets, snapshots, log contents, Performance
  Insights samples, schemas, tables, row data, or mutation APIs through the
  runtime registry.
- Keep DynamoDB scans metadata-only. Do not wire item reads, table scans, table
  queries, stream record reads, backup/export payload reads, resource-policy
  persistence, PartiQL calls, or mutation APIs through the runtime registry.
- Keep CloudWatch Logs scans metadata-only. Do not wire log event reads, log
  stream payload reads, Insights query calls, export payload reads,
  resource-policy persistence, subscription payload reads, or mutation APIs
  through the runtime registry.
- Keep CloudFront scans metadata-only. Do not wire object reads, origin payload
  reads, distribution config payload persistence, policy-document persistence,
  certificate body reads, private-key handling, origin custom header value
  persistence, or mutation APIs through the runtime registry.
- Keep Secrets Manager scans metadata-only. Do not wire secret value reads,
  version payload reads, resource-policy persistence, external rotation partner
  metadata persistence, external rotation role ARN persistence, or mutation APIs
  through the runtime registry.
- Keep SSM scans metadata-only. Do not wire parameter value reads, history value
  reads, raw description persistence, raw allowed-pattern persistence, raw
  policy JSON persistence, decryption, or mutation APIs through the runtime
  registry.
- Keep Athena scans metadata-only. Do not wire StartQueryExecution,
  StopQueryExecution, GetQueryResults, GetQueryExecution, ListQueryExecutions,
  named-query SQL body reads, prepared-statement query body reads, query
  history persistence, or mutation APIs through the runtime registry.
- Keep Glue scans metadata-only. Do not wire StartCrawler, StartJobRun,
  BatchStopJobRun, Create*/Update*/Delete* APIs, job script body reads, job
  default-argument value persistence, secret-shaped argument key
  persistence, connection password reads, JDBC credential URL persistence,
  connection property value persistence, table column statistics with sample
  values, classifier custom-pattern reads, workflow graph payload reads,
  workflow run state reads, or any other mutation or sensitive-payload API
  through the runtime registry. The SDK adapter must call GetConnections with
  HidePassword=true and GetWorkflow with IncludeGraph=false.
- Keep Step Functions scans metadata-only. Do not wire StartExecution,
  StopExecution, CreateStateMachine, UpdateStateMachine, DeleteStateMachine,
  SendTaskSuccess, SendTaskFailure, execution input/output persistence,
  execution history event persistence, activity task token persistence, or
  literal Parameters/ResultPath/ResultSelector/InputPath/OutputPath/Result
  persistence through the runtime registry. The state machine definition
  surface is restricted to state names, state types, transitions, and Task
  Resource ARNs.
- Keep Access Analyzer scans metadata-only. Do not wire external finding-body
  persistence, archive-rule filter persistence, policy-generation output,
  per-action unused-access detail persistence, GetFinding, or mutation APIs
  through the runtime registry.
- Keep Organizations scans metadata-only. Do not wire policy body reads,
  account lifecycle mutations, policy mutations, delegated-admin mutations, or
  service-access mutations through the runtime registry. Organizations API
  calls must use the `us-east-1` control-plane endpoint and report org-aware
  credential skips through bounded warnings/status/metrics.

## Common Changes

- Add a new credential mode by extending `CredentialMode`, writing focused
  claim tests, and implementing the provider here.
- Add a new service scanner. Production registration is now init-time and the
  awsruntime package has zero compile-time dependency on individual service
  packages. The new-scanner workflow is:
  1. Add the service constant in `awscloud` (e.g. `ServiceFoo = "foo"`).
  2. Build the scanner package under `services/<svc>/` (scanner.go, tests,
     `awssdk/` adapter, doc.go, README.md, AGENTS.md).
  3. Add `services/<svc>/runtimebind/` containing `bind.go`, `doc.go`,
     `README.md`, `AGENTS.md`, and `bind_test.go`. The `bind.go` calls
     `awsruntime.Register` from `init()`; the test asserts the binding
     resolves via `awsruntime.LookupBuilder`.
  4. Append one underscore-import line to
     `awsruntime/bindings/bindings.go`. That file is marked `merge=union` in
     `.gitattributes` so parallel scanner PRs do not conflict.
  5. Do NOT edit any want-list — there is none. The supported-service guard is
     DERIVED: the guard tests in
     `awsruntime/registry_supported_services_test.go` and
     `awsruntime/bindings/bindings_test.go` enumerate the
     `services/<svc>/runtimebind/` directories on disk and the runtimebind
     blank imports parsed from `bindings.go`, then assert the two sets and the
     registry count agree (see `awsruntime/internal/guardset`). A new
     `services/<svc>/runtimebind/` directory without a matching `bindings.go`
     import fails the guard automatically, so adding a scanner touches zero
     want-lists.

  Command-side target-scope validation continues to use `SupportsServiceKind`,
  which delegates to the registry.
- Change claim shape only with coordinator, workflow, and ADR updates in the
  same PR.

## What Not To Change Without An ADR

- Do not bypass workflow claims or claim fencing.
- Do not cache cross-account credentials beyond a claim lease.
- Do not infer environment, workload, or ownership truth in the runtime.
