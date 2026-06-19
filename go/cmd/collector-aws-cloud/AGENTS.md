# AGENTS.md - cmd/collector-aws-cloud guidance

## Read First

1. `README.md` - command purpose, configuration, and invariants.
2. `main.go` - `-mode fixture|claimed-live` flag parsing and runner selection.
3. `config.go` - collector instance selection and target-scope parsing.
4. `fixture_config.go` - declarative fixture-mode config loading
   (`loadFixtureConfig`) into `awsruntime.FixtureConfig`.
5. `service.go` - `buildCollectorService` (fixture) and `buildClaimedService`
   (live) runtime wiring.
6. `status_committer.go` - commit-side AWS scan status updates after fenced
   fact persistence.
7. `go/internal/collector/awscloud/awsruntime/README.md` - claim runtime
   contract.
8. Service `awssdk` README files under
   `go/internal/collector/awscloud/services/` - SDK adapter contracts.
9. `docs/public/services/collector-aws-cloud.md` - security and
   runtime requirements.

## Invariants

- Keep `-mode` defaulting to `claimed-live`. Fixture mode is opt-in; flipping the
  default would silently change live deployments. `-config` is required in
  fixture mode and rejected in claimed-live mode.
- Fixture mode requires no redaction key (AWS resource/relationship envelopes
  carry no fingerprinted material). Do not add one.
- Do not accept static AWS credential fields.
- Require central AssumeRole targets to provide an external ID and an IAM role
  ARN in the configured `account_id`.
- Keep local workload identity targets local: do not accept `role_arn` or
  `external_id` in that mode.
- Reject wildcard AWS regions or service lists. `allowed_services` must name a
  scanner family wired into the runtime registry.
- Require `ESHU_AWS_REDACTION_KEY` when any allowed service declared
  `RequiresRedactionKey: true` in its `runtimebind` registration, so
  sensitive-derived fields cannot cross persistence boundaries in plaintext.
  `awsConfigNeedsRedactionKey` and the missing-key error string derive this set
  from `awsruntime.ServiceKindsRequiringRedactionKey()`; do not reintroduce a
  hardcoded service switch or literal list in `config.go`. A new
  redaction-requiring scanner declares the requirement only in its own
  `runtimebind/bind.go`.
- Keep this command process-only. AWS credentials belong in `awsruntime`; AWS
  service pagination belongs in service `awssdk` adapters.
- Keep ELBv2 target health out of stable AWS collector facts; target health is
  live status, not routing topology.
- Keep Route 53 DNS names, hosted-zone IDs, and record values out of metric
  labels.
- Keep EC2 instance inventory out of the EC2 scanner; ENI attachment target
  evidence is metadata only.
- Keep Lambda function code and presigned package download URLs out of facts.
  Lambda image URIs, aliases, event-source mappings, execution roles, subnets,
  and security groups are reported join evidence only.
- Keep SQS message bodies and queue policy JSON out of facts. The command may
  enable `sqs`, but the SDK adapter owns the safe metadata allowlist.
- Keep SNS message payloads, topic policy JSON, delivery-policy JSON,
  data-protection-policy JSON, and raw non-ARN endpoints out of facts. The
  command may enable `sns`, but the SDK adapter owns safe topic and subscription
  mapping.
- Keep EventBridge event payloads, event bus policy JSON, target input payloads,
  input transformers, HTTP target parameters, and raw non-ARN targets out of
  facts. The command may enable `eventbridge`, but the SDK adapter owns safe bus,
  rule, and target mapping.
- Keep GuardDuty finding bodies, filter criteria expressions, threat intel set
  list contents, IP set list contents, and mutation APIs out of facts. The
  command may enable `guardduty`, but the SDK adapter owns safe detector,
  member, filter-name, publishing destination, set metadata, and aggregate
  finding-count reads.
- Keep S3 object inventory, object keys, bucket policy JSON, ACL grants,
  replication rules, lifecycle rules, notification configuration, inventory
  configuration, analytics configuration, and metrics configuration out of
  facts. The command may enable `s3`, but the SDK adapter owns safe bucket
  metadata mapping.
- Keep DynamoDB item values, table scans, table queries, stream records,
  backup/export payloads, resource policies, PartiQL output, and mutations out
  of facts. The command may enable `dynamodb`, but the SDK adapter owns safe
  table metadata mapping.
- Keep CloudWatch Logs log events, log stream payloads, Insights query results,
  export payloads, resource policies, subscription payloads, and mutations out
  of facts. The command may enable `cloudwatchlogs`, but the SDK adapter owns
  safe log group metadata mapping.
- Keep CloudFront object contents, origin payloads, distribution config
  payloads, policy documents, certificate bodies, private keys, origin custom
  header values, and mutations out of facts. The command may enable
  `cloudfront`, but the SDK adapter owns safe metadata mapping.
- Keep API Gateway execution, exports, API keys, authorizer secrets, policy
  JSON, integration credentials, stage variable values, template bodies,
  payloads, and mutations out of facts. The command may enable `apigateway`,
  but the SDK adapter owns safe REST and v2 metadata mapping.
- Keep Secrets Manager secret values, version payloads, resource policy JSON,
  external rotation partner metadata, external rotation role ARNs, and mutations
  out of facts. The command may enable `secretsmanager`, but the SDK adapter
  owns safe metadata mapping.
- Keep SSM parameter values, history values, raw descriptions, raw allowed
  patterns, raw policy JSON, decrypted content, and mutations out of facts. The
  command may enable `ssm`, but the SDK adapter owns safe metadata mapping.
- Keep Athena StartQueryExecution, StopQueryExecution, query result rows,
  query execution result location object contents, named-query SQL bodies,
  prepared-statement query bodies, query history strings, and mutations out of
  facts. The command may enable `athena`, but the SDK adapter owns safe
  workgroup, data catalog, prepared-statement, and named-query metadata
  mapping with the SQL-body fields explicitly discarded.
- Keep Security Hub finding bodies, resource details, remediation text, notes,
  product fields, user-defined fields, network/process details, insight
  filters, and mutations out of facts. The command may enable `securityhub`,
  but the SDK adapter owns safe metadata and aggregate-count mapping.
- Keep Glue job script bodies, job default-argument values, secret-shaped
  argument keys, connection passwords, connection JDBC credential URLs,
  connection property values, table column statistics with sample values,
  classifier custom patterns, workflow graph payloads, and mutations out of
  facts. The command may enable `glue`, but the SDK adapter owns safe
  database, table, crawler, job, trigger, workflow, and connection mapping
  and must call GetConnections with HidePassword=true and GetWorkflow with
  IncludeGraph=false.
- Keep ElastiCache AUTH tokens, user passwords, user access strings, cache
  keys, cache values, snapshot data, and mutations out of facts. The command
  may enable `elasticache`, but the SDK adapter owns safe cache cluster,
  replication group, parameter group, subnet group, user, user group, and
  snapshot (name/source/status only) mapping.
- Keep MSK broker `server.properties` bodies, configuration revision bodies,
  broker log contents, bootstrap broker endpoints, SCRAM secret material,
  cluster resource policy JSON, Kafka topic data, Kafka message contents, and
  mutations out of facts. The command may enable `msk`, but the SDK adapter
  owns safe cluster, configuration, and replicator mapping.
- Keep Step Functions execution input, execution output, execution history
  events, activity task tokens, and literal
  Parameters/ResultPath/ResultSelector/InputPath/OutputPath/Result contents from
  the state machine definition out of facts. The command may enable
  `stepfunctions`, but the SDK adapter owns the safe state-graph and ARN-only
  reference projection.
- Keep IAM Access Analyzer external finding bodies, archive-rule filter
  criteria, policy-generation output, and per-action unused-access details out
  of facts. The command may enable `accessanalyzer`, but the SDK adapter owns
  safe analyzer, archive-rule, aggregate-count, and unused-access summary
  metadata mapping.
- Keep Organizations policy document bodies, account lifecycle mutations, policy
  mutations, delegated-admin mutations, and service-access mutations out of
  facts. The command may enable `organizations`, but the SDK adapter owns safe
  `us-east-1` metadata mapping and org-aware skip classification.
- Do not log credential values, trust policy JSON, resource ARNs, tags, or raw
  source payloads as metric labels.
- Preserve the split between scanner-side status in `awsruntime` and
  commit-side status in `status_committer.go`.

## Common Changes

- Add a new AWS service by extending target validation, adding scanner package
  tests, adding a service `awssdk` adapter, package docs, and branching in
  `awsruntime.DefaultScannerFactory.Scanner`.
- Run `scripts/verify-package-docs.sh` whenever the change adds or edits a Go
  package under this command or `go/internal/collector/awscloud`.
- Run `scripts/verify-performance-evidence.sh` whenever the change touches
  claim concurrency, leases, worker fanout, batching, pagination pressure, or
  downstream graph/materialization cost. The PR must include tracked
  Performance Evidence and Observability Evidence markers.
- Add new command configuration with config tests first.
- Add SDK pagination in the service adapter so spans and AWS API counters stay
  complete.

## What Not To Change Without An ADR

- Do not bypass workflow claims or claim-aware commits.
- Do not cache cross-account credentials beyond the claim lease.
- Do not make this command write graph truth or reducer-owned rows directly.
