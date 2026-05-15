# ADR: AWS Cloud Scanner Collector

**Date:** 2026-04-20
**Status:** Accepted with follow-up
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering
**Related:**

- `2026-04-19-multi-source-correlation-dsl-and-collector-readiness.md`
- `2026-04-20-workflow-coordinator-and-multi-collector-runtime-contract.md`
- `2026-04-20-workflow-coordinator-claiming-fencing-and-convergence.md`
- `2026-04-20-terraform-state-collector.md`
- `2026-04-20-multi-source-reducer-and-consumer-contract.md` — fact-field
  back-propagation source for the reducer/consumer contract this ADR now
  incorporates directly.

---

## Status Review (2026-05-13)

**Current disposition:** Architecture gate closed; IAM-first scanner runtime
slice merged; ECR scanner slice merged; ECS scanner slice merged; ELBv2 scanner
slice merged; Route 53 scanner slice merged; EC2 network-topology scanner slice
merged; Lambda scanner slice merged; EKS scanner slice merged; durable
pagination checkpoint slice merged; coordinator completeness and admin-status
slice implemented in this PR pending merge.

Gate issue #48 is the start point for AWS collector work. The architecture
workflow plan now maps to the current Eshu issue set (#51 epic, #42 runtime,
#41 container vertical slice, #30-#36 service scanners, #40 admin/completeness,
#39 correlation, #38 expansion, #37 freshness) and to the code that already
exists: `scope.CollectorAWS`, the workflow AWS reducer contract, and
`go/internal/redact`.

Issue #48 recorded the principal engineer, principal SRE, and security
sign-offs. The first runtime slice added `collector-aws-cloud`, AWS fact
envelope contracts, the IAM scanner, STS/local workload identity credential
wiring, and AWS collector telemetry. The container-vertical-slice work added
ELBv2 load balancers, listeners, target groups, rules, and stable routing
relationships so ECS target group bindings can resolve toward load balancer
hostnames. The DNS slice added Route 53 hosted zones and A/AAAA/CNAME/ALIAS
`aws_dns_record` facts so later reducers can join public and private names to
cloud routing targets without inferring ownership in the collector. The EC2
slice added VPC, subnet, security-group, security-group-rule, and ENI
network-topology facts without emitting EC2 instance inventory. It also
preserved ECS task ENI attachment IDs from `DescribeTasks` so reducers can join
task evidence to EC2 subnet and VPC topology later. The current Lambda slice
adds function, alias, and event-source mapping facts; redacts function
environment values; preserves image URI evidence for ECR joins; and emits
reported relationships to execution roles, subnets, and security groups for
later reducer-owned correlation.

## Status Review (2026-05-14)

**Current disposition:** Phase 2 service expansion now includes SQS, SNS,
EventBridge, S3, RDS, DynamoDB, CloudWatch Logs, CloudFront, API Gateway, and
Secrets Manager and SSM metadata-only slices for issue #38. The `sqs` service
scans queue metadata, queue tags, safe queue attributes, and reported
dead-letter queue relationships from redrive policy fields. The `sns` service
scans topic metadata, topic tags, safe topic attributes, and reported delivery
relationships only when the subscription endpoint is ARN-addressable. The
`eventbridge` service scans event bus
metadata, rule metadata, tags, rule-to-bus relationships, and reported target
relationships only when the target is ARN-addressable. The `s3` service scans
bucket tags, versioning, default encryption, public-access-block status, policy
public/not-public status, ownership controls, website status, and
server-access-log target metadata. The `rds` service scans DB instance, DB
cluster, and DB subnet group metadata plus directly reported cluster
membership, subnet group, security group, KMS key, monitoring role, IAM role,
parameter group, and option group relationships. The `dynamodb` service scans
table metadata, tags, key schema, attribute definitions, capacity settings,
table class, index metadata, TTL status, continuous backup status, stream
settings, replicas, and directly reported KMS key relationships. The
`cloudwatchlogs` service scans log group metadata, tags, retention, stored byte
count, metric filter count, log group class, data protection status, inherited
properties, deletion protection, bearer-token authentication state, and directly
reported KMS key relationships. The `cloudfront` service scans distribution
metadata, tags, aliases, origins, cache behavior selectors, viewer certificate
selectors, status/enabled flags, and directly reported ACM certificate and WAF
web ACL relationships. The `apigateway` service scans REST, HTTP, and
WebSocket API identities, stages, custom domains, base path/API mappings, tags,
access-log destinations, ACM certificate dependencies, and ARN-addressable
integration targets. The `secretsmanager` service scans secret identity, tags,
description presence, KMS key identifiers, rotation state, timestamps, primary
region, owning service, type, safe rotation schedule fields, and directly
reported KMS key and rotation Lambda relationships. The `ssm` service scans
Parameter Store identity, tags, type, tier, data type, KMS key identifiers,
last-modified timestamp, safe presence flags for descriptions and allowed
patterns, safe policy type/status metadata, and directly reported KMS key
relationships. These slices
deliberately do not call message, event payload, object, database, snapshot,
log-content, Performance Insights sample, schema, table, Insights query, log
stream payload, export payload, secret-value, secret-version, parameter-value,
parameter-history, decryption, or mutation APIs;
do not persist
SQS/SNS/EventBridge/S3/CloudWatch Logs policy JSON; do not persist raw non-ARN
SNS endpoints, EventBridge targets, S3 object inventory, ACL grants,
replication rules, lifecycle rules, notification configuration, inventory
configuration, analytics configuration, metrics configuration, RDS database
names, master usernames, secrets, snapshots, log payloads, schemas, tables, or
row data, DynamoDB item values, table scan results, query results, stream
records, backup/export payloads, resource policies, PartiQL output, or
CloudWatch Logs subscription payloads; and do not persist API Gateway API keys,
authorizer secrets, policy JSON, integration credentials, stage variable
values, request templates, response templates, or API request/response
payloads; and do not persist Secrets Manager secret values, version payloads,
resource policy JSON, external rotation partner metadata, or external rotation
role ARNs; and do not persist SSM parameter values, history values, raw
descriptions, raw allowed patterns, or raw policy JSON.

The AWS redaction close-out adds a versioned launch policy,
`aws-launch-2026-05-14`, in `go/internal/collector/awscloud`. ECS
task-definition and Lambda function environment values now share the same
`go/internal/redact` path and persist marker, reason, source, and
ruleset-version metadata. Known sensitive key names such as `DATABASE_URL`,
`PASSWORD`, `TOKEN`, access keys, client secrets, private keys, and connection
strings receive `known_sensitive_key`; other environment values fail closed as
`unknown_provider_schema` until a narrower provider schema exists.

No-Regression Evidence: `go test ./internal/collector/awscloud ./internal/collector/awscloud/services/ecs ./internal/collector/awscloud/services/lambda ./internal/storage/postgres`
covers the AWS redaction policy helper, ECS and Lambda scanner payloads, and a
scanner-to-Postgres fake transaction proof that raw Lambda environment values
do not appear in persisted fact arguments.

No-Observability-Change: the redaction close-out changes fact payload
classification metadata only. The existing AWS collector diagnostics still
cover the runtime path through `aws.service.scan`,
`eshu_dp_aws_resources_emitted_total`, `eshu_dp_aws_relationships_emitted_total`,
and `aws_scan_status`; no new metric or span name is needed for redacted scalar
construction.

## Security Review (2026-05-15)

**Scope:** Closes the AWS slice of issue #26. Covers the claim-driven
`collector-aws-cloud` runtime, target-scope credential configuration, Helm IRSA
deployment shape, AWS service scanner API posture, and redaction boundary after
the AWS scanner families landed.

### External Contract Evidence

AWS IAM guidance reviewed for this sign-off:

- AWS IAM best practices require temporary credentials for workloads, least
  privilege, condition-based restriction, and IAM Access Analyzer validation:
  <https://docs.aws.amazon.com/IAM/latest/UserGuide/best-practices.html>
- AWS confused-deputy guidance requires the third-party deputy to pass a
  customer-specific `sts:ExternalId` and the target role trust policy to check
  it:
  <https://docs.aws.amazon.com/IAM/latest/UserGuide/confused-deputy.html>
- AWS STS `AssumeRole` supports external IDs and short-lived session
  credentials:
  <https://docs.aws.amazon.com/STS/latest/APIReference/API_AssumeRole.html>
- EKS IRSA binds an IAM role to a Kubernetes service account:
  <https://docs.aws.amazon.com/eks/latest/userguide/associate-service-account-role.html>
- IAM Access Analyzer policy validation is the operator gate for trust-policy
  and identity-policy checks:
  <https://docs.aws.amazon.com/IAM/latest/UserGuide/access-analyzer-policy-validation.html>

### Controls Verified

- **Temporary credentials only.** `go/cmd/collector-aws-cloud/config.go`
  rejects `access_key_id`, `secret_access_key`, and `session_token` in
  collector instance config. The runtime uses SDK workload identity or
  claim-scoped STS AssumeRole credentials and releases in-process credential
  material after scanner construction and scan completion in
  `go/internal/collector/awscloud/awsruntime/credentials.go`.
- **External ID / tenant guard.** Central AssumeRole scopes now require
  `external_id`; the role ARN must be an IAM role ARN in the configured
  `account_id`. This keeps target labels, trust-policy scope, and STS
  acquisition tied to the same AWS account.
- **Ambiguous credential config rejected.** `local_workload_identity` scopes now
  reject `role_arn` and `external_id`, so account-local mode cannot silently
  behave like central mode.
- **Broad target scope rejected.** Target scopes now reject wildcard regions and
  services, reject unsupported service names, reject non-12-digit AWS account
  IDs, and reject negative per-account concurrency.
- **IRSA blast radius narrowed.** The Helm chart now supports
  `awsCloudCollector.serviceAccount.create` and uses the dedicated service
  account for the AWS collector deployment. Operators can bind the AWS collector
  role only to `collector-aws-cloud` instead of annotating the shared release
  service account used by API, reducer, ingester, and other pods.
- **Read-only scanner posture.** Production AWS service adapters under
  `go/internal/collector/awscloud/services/*/awssdk` were reviewed. The
  scanner families use metadata-shaped `List*`, `Describe*`, and safe `Get*`
  calls such as `GetBucketPolicyStatus`, `GetLifecyclePolicy`,
  `GetFunction`, `GetTopicAttributes`, and
  `GetOpenIDConnectProvider`. They do not call mutation APIs (`Create*`,
  `Update*`, `Put*`, `Delete*`, `Tag*`, `Untag*`) and do not call value or
  payload APIs such as `GetSecretValue`, SSM `GetParameter*`,
  `ReceiveMessage`, `PutEvents`, DynamoDB `Query` or `Scan`, CloudWatch Logs
  event reads, API execution/export, S3 object reads, database reads, or Lambda
  package downloads.
- **Redaction boundary.** ECS task-definition and Lambda environment values
  require `ESHU_AWS_REDACTION_KEY` and cross the fact boundary only as
  `go/internal/redact` markers with reason, source, and
  `RedactionPolicyVersion`.

No-Regression Evidence: `go test ./cmd/collector-aws-cloud
./internal/runtime ./internal/collector/awscloud/... -count=1` proves the new
credential and target-scope guards, the AWS collector dedicated service-account
render, and the existing scanner/redaction/runtime contracts. `helm lint
deploy/helm/eshu` proves the chart change remains renderable under Helm's chart
gate. `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions
mkdocs build --strict --clean --config-file docs/mkdocs.yml` proves the
operator docs and ADR references build.

No-Observability-Change: this security review tightens startup configuration
validation and Helm identity isolation. Runtime scan telemetry remains the
existing `aws.credentials.assume_role`, `aws.collector.claim.process`,
`eshu_dp_aws_assumerole_failed_total`, `eshu_dp_aws_claim_concurrency`, and
`aws_scan_status` status rows. Invalid unsafe config now fails before the
collector claims work, so no new runtime metric is needed.

## Freshness Layer Start (2026-05-15)

**Scope:** Starts issue #37 by adding the internal AWS freshness trigger
contract, durable Postgres coalescing store, and coordinator planner that turns
claimed AWS Config/EventBridge triggers into ordinary AWS collector work items.

### External Contract Evidence

AWS EventBridge service events can be best-effort or durable-at-least-once
depending on the source, so Eshu must not assume every event arrives exactly
once:
<https://docs.aws.amazon.com/eventbridge/latest/ref/event-delivery-level.html>.
EventBridge target delivery retries default to 24 hours and up to 185 attempts,
and undelivered events are dropped without a dead-letter queue:
<https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-rule-retry-policy.html>.
AWS Config creates configuration items when it detects supported recorded
resource changes, and Config rules evaluate after change notifications:
<https://docs.aws.amazon.com/config/latest/developerguide/config-item-table.html>
and
<https://docs.aws.amazon.com/config/latest/developerguide/evaluate-config_components.html>.

### Design Decision

Freshness events are wake-up signals only. The durable key coalesces by
`(account_id, region, service_kind)` because the AWS collector's claim boundary
is service-scoped. The planner rejects provider events that are outside the
collector instance `target_scopes`, so EventBridge or Config input cannot widen
AWS access. Scheduled scans remain authoritative and continue to repair missed,
late, duplicate, or dropped provider events.

No-Regression Evidence: `go test ./internal/collector/awscloud/freshness
./internal/storage/postgres ./internal/coordinator ./internal/telemetry
-count=1` covers trigger validation, durable coalescing SQL, targeted workflow
item planning, and telemetry registration without adding AWS API calls or graph
writes.

Observability Evidence: `eshu_dp_aws_freshness_events_total{kind,action}`
records bounded AWS Config/EventBridge intake and handoff actions. The
resulting AWS scans continue through existing `aws.collector.claim.process`,
`aws.service.scan`, `eshu_dp_aws_api_calls_total`,
`eshu_dp_aws_resources_emitted_total`, and `aws_scan_status` signals.

Collector Performance Evidence: `go test ./internal/collector/awscloud/services/cloudfront/...`
covers the bounded CloudFront call shape: paginated ListDistributions with
MaxItems=100 and one ListTagsForResource read per ARN-addressable
distribution; no object reads, origin payload reads, GetDistributionConfig
calls, policy-document reads, certificate body reads, private-key handling,
mutations, or downstream graph writes in the collector. This slice did not run
against a live AWS account; the performance contract is the bounded
O(distribution count) SDK call shape and existing workflow claim/account
concurrency limits.

No-Regression Evidence: `go test ./cmd/collector-aws-cloud ./internal/collector/awscloud/...`
covers CloudFront distribution metadata fact emission, reported ACM certificate
and WAF web ACL relationship emission, omission of secret-bearing origin header
values and data-plane fields, SDK pagination, tag reads, runtime scanner
registration, and the command config path that confirms CloudFront does not
require an environment-value redaction key.

Collector Observability Evidence: CloudFront uses the existing AWS collector
`aws.service.pagination.page` span plus `eshu_dp_aws_api_calls_total`,
`eshu_dp_aws_throttle_total`, `eshu_dp_aws_resources_emitted_total`,
`eshu_dp_aws_relationships_emitted_total`, and `aws_scan_status` rows. Metric
labels stay bounded to service, account, region, operation, result, and status;
distribution IDs, ARNs, aliases, tags, certificate identifiers, WAF IDs, and
raw AWS error payloads stay out of metric labels.

No-Observability-Change: the existing AWS collector telemetry contract already
diagnoses the CloudFront path through `aws.service.scan`,
`aws.service.pagination.page`, `eshu_dp_aws_api_calls_total`,
`eshu_dp_aws_throttle_total`, `eshu_dp_aws_resources_emitted_total`,
`eshu_dp_aws_relationships_emitted_total`, and `aws_scan_status`; no new metric
or span name is needed for this metadata-only scanner.

Collector Deployment Evidence: CloudFront runs inside the existing hosted
`collector-aws-cloud` runtime through `awsruntime.DefaultScannerFactory`, so
the already documented `/healthz`, `/readyz`, `/metrics`, and `/admin/status`
surfaces apply without a separate deployment.

Collector Performance Evidence: `go test ./internal/collector/awscloud/services/apigateway/...`
covers the bounded API Gateway call shape: paginated GetRestApis,
GetResources with embed=methods, GetDomainNames, GetBasePathMappings, GetApis,
GetStages, GetIntegrations, and GetApiMappings with page sizes capped at 100
where the AWS API exposes a page-size knob. The collector does not call API
execution, export, API key, authorizer secret, policy body, payload, template
body, credential, or mutation APIs, and it does not write graph data directly.
This slice did not run against a live AWS account; the performance contract is
the bounded O(api count + stage count + domain mapping count + integration
count) SDK call shape and existing workflow claim/account concurrency limits.

No-Regression Evidence: `go test ./cmd/collector-aws-cloud ./internal/collector/awscloud/...`
covers API Gateway REST and v2 metadata fact emission, stage facts, custom
domain facts, domain-to-API mapping relationships, ACM certificate
relationships, access-log destination relationships, ARN-addressable
integration relationships, SDK pagination, runtime scanner registration, and
the command config path that confirms API Gateway does not require an
environment-value redaction key.

Collector Observability Evidence: API Gateway uses the existing AWS collector
`aws.service.pagination.page` span plus `eshu_dp_aws_api_calls_total`,
`eshu_dp_aws_throttle_total`, `eshu_dp_aws_resources_emitted_total`,
`eshu_dp_aws_relationships_emitted_total`, and `aws_scan_status` rows. Metric
labels stay bounded to service, account, region, operation, result, and status;
API IDs, stage names, domain names, mapping keys, integration IDs, ARNs, tags,
and raw AWS error payloads stay out of metric labels.

No-Observability-Change: the existing AWS collector telemetry contract already
diagnoses the API Gateway path through `aws.service.scan`,
`aws.service.pagination.page`, `eshu_dp_aws_api_calls_total`,
`eshu_dp_aws_throttle_total`, `eshu_dp_aws_resources_emitted_total`,
`eshu_dp_aws_relationships_emitted_total`, and `aws_scan_status`; no new metric
or span name is needed for this metadata-only scanner.

Collector Deployment Evidence: API Gateway runs inside the existing hosted
`collector-aws-cloud` runtime through `awsruntime.DefaultScannerFactory`, so
the already documented `/healthz`, `/readyz`, `/metrics`, and `/admin/status`
surfaces apply without a separate deployment.

Collector Performance Evidence: `go test ./internal/collector/awscloud/services/secretsmanager/...`
covers the bounded Secrets Manager call shape: paginated ListSecrets with
MaxResults=100 and IncludePlannedDeletion=true. The collector does not call
GetSecretValue, BatchGetSecretValue, ListSecretVersionIds, GetResourcePolicy,
or mutation APIs, and it does not write graph data directly. This slice did not
run against a live AWS account; the performance contract is the bounded
O(secret count) SDK call shape and existing workflow claim/account concurrency
limits.

No-Regression Evidence: `go test ./cmd/collector-aws-cloud ./internal/collector/awscloud/...`
covers Secrets Manager metadata fact emission, direct KMS and rotation Lambda
relationship emission, omission of value/version/policy fields, SDK pagination,
runtime scanner registration, and the command config path that confirms Secrets
Manager does not require an environment-value redaction key.

Collector Observability Evidence: Secrets Manager uses the existing AWS
collector `aws.service.pagination.page` span plus
`eshu_dp_aws_api_calls_total`, `eshu_dp_aws_throttle_total`,
`eshu_dp_aws_resources_emitted_total`,
`eshu_dp_aws_relationships_emitted_total`, and `aws_scan_status` rows. Metric
labels stay bounded to service, account, region, operation, result, and status;
secret names, secret ARNs, tags, KMS identifiers, Lambda ARNs, and raw AWS
error payloads stay out of metric labels.

No-Observability-Change: the existing AWS collector telemetry contract already
diagnoses the Secrets Manager path through `aws.service.scan`,
`aws.service.pagination.page`, `eshu_dp_aws_api_calls_total`,
`eshu_dp_aws_throttle_total`, `eshu_dp_aws_resources_emitted_total`,
`eshu_dp_aws_relationships_emitted_total`, and `aws_scan_status`; no new metric
or span name is needed for this metadata-only scanner.

Collector Deployment Evidence: Secrets Manager runs inside the existing hosted
`collector-aws-cloud` runtime through `awsruntime.DefaultScannerFactory`, so
the already documented `/healthz`, `/readyz`, `/metrics`, and `/admin/status`
surfaces apply without a separate deployment.

Collector Performance Evidence: `go test ./internal/collector/awscloud/services/ssm/...`
covers the bounded SSM Parameter Store call shape: paginated
DescribeParameters with MaxResults=50 and one ListTagsForResource read per
parameter name. The collector does not call GetParameter, GetParameters,
GetParametersByPath, GetParameterHistory, decryption, mutation APIs, or graph
writes. This slice did not run against a live AWS account; the performance
contract is the bounded O(parameter count) SDK call shape and existing workflow
claim/account concurrency limits.

No-Regression Evidence: `go test ./cmd/collector-aws-cloud ./internal/collector/awscloud/...`
covers SSM parameter metadata fact emission, direct KMS relationship emission,
omission of values/history/descriptions/allowed-patterns/policy JSON, SDK
pagination, tag reads, runtime scanner registration, and the command config
path that confirms SSM does not require an environment-value redaction key.

Collector Observability Evidence: SSM uses the existing AWS collector
`aws.service.pagination.page` span plus `eshu_dp_aws_api_calls_total`,
`eshu_dp_aws_throttle_total`, `eshu_dp_aws_resources_emitted_total`,
`eshu_dp_aws_relationships_emitted_total`, and `aws_scan_status` rows. Metric
labels stay bounded to service, account, region, operation, result, and status;
parameter names, paths, ARNs, tags, KMS identifiers, and raw AWS error payloads
stay out of metric labels.

No-Observability-Change: the existing AWS collector telemetry contract already
diagnoses the SSM path through `aws.service.scan`,
`aws.service.pagination.page`, `eshu_dp_aws_api_calls_total`,
`eshu_dp_aws_throttle_total`, `eshu_dp_aws_resources_emitted_total`,
`eshu_dp_aws_relationships_emitted_total`, and `aws_scan_status`; no new metric
or span name is needed for this metadata-only scanner.

Collector Deployment Evidence: SSM runs inside the existing hosted
`collector-aws-cloud` runtime through `awsruntime.DefaultScannerFactory`, so
the already documented `/healthz`, `/readyz`, `/metrics`, and `/admin/status`
surfaces apply without a separate deployment.

Collector Performance Evidence: `go test ./internal/collector/awscloud/services/sqs/...`
covers the bounded SQS call shape: one paginated ListQueues stream, one
GetQueueAttributes metadata read per discovered queue, one ListQueueTags read
per discovered queue, no message reads, no queue mutations, and no downstream
graph writes in the collector. This slice did not run against a live AWS
account; the performance contract is the bounded O(queue count) SDK call shape
and existing workflow claim/account concurrency limits.

No-Regression Evidence: `go test ./cmd/collector-aws-cloud ./internal/collector/awscloud/...`
covers SQS queue metadata fact emission, dead-letter queue relationship
emission, omission of queue policy/message payload fields, SDK pagination, tag
reads, runtime scanner registration, and the command config path that confirms
SQS does not require an environment-value redaction key.

Collector Observability Evidence: SQS uses the existing AWS collector
`aws.service.pagination.page` span plus `eshu_dp_aws_api_calls_total`,
`eshu_dp_aws_throttle_total`, `eshu_dp_aws_resources_emitted_total`,
`eshu_dp_aws_relationships_emitted_total`, and `aws_scan_status` rows. Metric
labels stay bounded to service, account, region, operation, result, and status;
queue URLs, ARNs, tags, redrive values, and raw AWS error payloads stay out of
metric labels.

No-Observability-Change: the existing AWS collector telemetry contract already
diagnoses the SQS path through `aws.service.scan`,
`aws.service.pagination.page`, `eshu_dp_aws_api_calls_total`,
`eshu_dp_aws_throttle_total`, `eshu_dp_aws_resources_emitted_total`,
`eshu_dp_aws_relationships_emitted_total`, and `aws_scan_status`; no new metric
or span name is needed for this metadata-only scanner.

Collector Deployment Evidence: SQS runs inside the existing hosted
`collector-aws-cloud` runtime through `awsruntime.DefaultScannerFactory`, so
the already documented `/healthz`, `/readyz`, `/metrics`, and `/admin/status`
surfaces apply without a separate deployment.

Collector Performance Evidence: `go test ./internal/collector/awscloud/services/sns/...`
covers the bounded SNS call shape: one paginated ListTopics stream, one
GetTopicAttributes metadata read per discovered topic, one ListTagsForResource
read per discovered topic, one paginated ListSubscriptionsByTopic stream per
discovered topic, no Publish calls, no subscription mutations, and no downstream
graph writes in the collector. This slice did not run against a live AWS
account; the performance contract is the bounded O(topic count + subscription
count) SDK call shape and existing workflow claim/account concurrency limits.

No-Regression Evidence: `go test ./cmd/collector-aws-cloud ./internal/collector/awscloud/...`
covers SNS topic metadata fact emission, ARN-only subscription relationship
emission, omission of topic policy/data-protection/message payload fields, SDK
pagination, tag reads, runtime scanner registration, and the command config path
that confirms SNS does not require an environment-value redaction key.

Collector Observability Evidence: SNS uses the existing AWS collector
`aws.service.pagination.page` span plus `eshu_dp_aws_api_calls_total`,
`eshu_dp_aws_throttle_total`, `eshu_dp_aws_resources_emitted_total`,
`eshu_dp_aws_relationships_emitted_total`, and `aws_scan_status` rows. Metric
labels stay bounded to service, account, region, operation, result, and status;
topic ARNs, tags, subscriptions, endpoint values, policy values, and raw AWS
error payloads stay out of metric labels.

No-Observability-Change: the existing AWS collector telemetry contract already
diagnoses the SNS path through `aws.service.scan`,
`aws.service.pagination.page`, `eshu_dp_aws_api_calls_total`,
`eshu_dp_aws_throttle_total`, `eshu_dp_aws_resources_emitted_total`,
`eshu_dp_aws_relationships_emitted_total`, and `aws_scan_status`; no new metric
or span name is needed for this metadata-only scanner.

Collector Deployment Evidence: SNS runs inside the existing hosted
`collector-aws-cloud` runtime through `awsruntime.DefaultScannerFactory`, so
the already documented `/healthz`, `/readyz`, `/metrics`, and `/admin/status`
surfaces apply without a separate deployment.

Collector Performance Evidence: `go test ./internal/collector/awscloud/services/eventbridge/...`
covers the bounded EventBridge call shape: one paginated ListEventBuses stream,
one ListTagsForResource read per event bus, one paginated ListRules stream per
event bus, one DescribeRule metadata read per rule, one ListTagsForResource read
per rule, one paginated ListTargetsByRule stream per rule, no PutEvents calls,
no rule or target mutations, and no downstream graph writes in the collector.
This slice did not run against a live AWS account; the performance contract is
the bounded O(bus count + rule count + target count) SDK call shape and existing
workflow claim/account concurrency limits.

No-Regression Evidence: `go test ./cmd/collector-aws-cloud ./internal/collector/awscloud/...`
covers EventBridge event bus metadata fact emission, rule metadata fact
emission, rule-to-bus relationship emission, ARN-only target relationship
emission, omission of event bus policy and target payload fields, SDK
pagination, tag reads, runtime scanner registration, and the command config path
that confirms EventBridge does not require an environment-value redaction key.

Collector Observability Evidence: EventBridge uses the existing AWS collector
`aws.service.pagination.page` span plus `eshu_dp_aws_api_calls_total`,
`eshu_dp_aws_throttle_total`, `eshu_dp_aws_resources_emitted_total`,
`eshu_dp_aws_relationships_emitted_total`, and `aws_scan_status` rows. Metric
labels stay bounded to service, account, region, operation, result, and status;
event bus ARNs, rule ARNs, target ARNs, tags, event patterns, target payload
fields, policy values, and raw AWS error payloads stay out of metric labels.

No-Observability-Change: the existing AWS collector telemetry contract already
diagnoses the EventBridge path through `aws.service.scan`,
`aws.service.pagination.page`, `eshu_dp_aws_api_calls_total`,
`eshu_dp_aws_throttle_total`, `eshu_dp_aws_resources_emitted_total`,
`eshu_dp_aws_relationships_emitted_total`, and `aws_scan_status`; no new metric
or span name is needed for this metadata-only scanner.

Collector Deployment Evidence: EventBridge runs inside the existing hosted
`collector-aws-cloud` runtime through `awsruntime.DefaultScannerFactory`, so
the already documented `/healthz`, `/readyz`, `/metrics`, and `/admin/status`
surfaces apply without a separate deployment.

Collector Performance Evidence: `go test ./internal/collector/awscloud/services/s3/...`
covers the bounded S3 call shape: regional paginated ListBuckets with
MaxBuckets set plus HeadBucket,
GetBucketTagging, GetBucketVersioning, GetBucketEncryption,
GetPublicAccessBlock, GetBucketPolicyStatus, GetBucketOwnershipControls,
GetBucketWebsite, and GetBucketLogging per discovered bucket; no object
inventory calls, no bucket policy JSON reads, no ACL grant reads, no mutations,
and no downstream graph writes in the collector. This slice did not run against
a live AWS account; the performance contract is the bounded O(bucket count) SDK
call shape and existing workflow claim/account concurrency limits.

No-Regression Evidence: `go test ./cmd/collector-aws-cloud ./internal/collector/awscloud/...`
covers S3 bucket metadata fact emission, server-access-log target relationship
emission, omission of object inventory, object key, policy JSON, ACL grant,
replication, lifecycle, notification, inventory, analytics, and metrics fields,
SDK point reads, runtime scanner registration, and the command config path that
confirms S3 does not require an environment-value redaction key.

Collector Observability Evidence: S3 uses the existing AWS collector
`aws.service.pagination.page` span plus `eshu_dp_aws_api_calls_total`,
`eshu_dp_aws_throttle_total`, `eshu_dp_aws_resources_emitted_total`,
`eshu_dp_aws_relationships_emitted_total`, and `aws_scan_status` rows. Metric
labels stay bounded to service, account, region, operation, result, and status;
bucket names, bucket ARNs, tags, prefixes, KMS key IDs, and raw AWS error
payloads stay out of metric labels.

No-Observability-Change: the existing AWS collector telemetry contract already
diagnoses the S3 path through `aws.service.scan`,
`aws.service.pagination.page`, `eshu_dp_aws_api_calls_total`,
`eshu_dp_aws_throttle_total`, `eshu_dp_aws_resources_emitted_total`,
`eshu_dp_aws_relationships_emitted_total`, and `aws_scan_status`; no new metric
or span name is needed for this metadata-only scanner.

Collector Deployment Evidence: S3 runs inside the existing hosted
`collector-aws-cloud` runtime through `awsruntime.DefaultScannerFactory`, so
the already documented `/healthz`, `/readyz`, `/metrics`, and `/admin/status`
surfaces apply without a separate deployment.

Collector Performance Evidence: `go test ./internal/collector/awscloud/services/rds/...`
covers the bounded RDS call shape: paginated DescribeDBInstances,
DescribeDBClusters, DescribeDBSubnetGroups, and ListTagsForResource for
ARN-addressable resources; no database connections, snapshot reads, log-content
reads, Performance Insights sample reads, schema/table reads, mutations, or
downstream graph writes in the collector. This slice did not run against a live
AWS account; the performance contract is the bounded O(instance count + cluster
count + subnet group count) SDK call shape and existing workflow claim/account
concurrency limits.

No-Regression Evidence: `go test ./cmd/collector-aws-cloud ./internal/collector/awscloud/...`
covers RDS DB instance, DB cluster, and DB subnet group metadata fact emission,
direct dependency relationship emission, omission of database name, master
username, secret, snapshot, log, Performance Insights sample, schema, table,
and row-data fields, SDK pagination, tag reads, runtime scanner registration,
and the command config path that confirms RDS does not require an
environment-value redaction key.

Collector Observability Evidence: RDS uses the existing AWS collector
`aws.service.pagination.page` span plus `eshu_dp_aws_api_calls_total`,
`eshu_dp_aws_throttle_total`, `eshu_dp_aws_resources_emitted_total`,
`eshu_dp_aws_relationships_emitted_total`, and `aws_scan_status` rows. Metric
labels stay bounded to service, account, region, operation, result, and status;
RDS endpoints, ARNs, tags, KMS key IDs, subnet group names, parameter group
names, option group names, and raw AWS error payloads stay out of metric labels.

No-Observability-Change: the existing AWS collector telemetry contract already
diagnoses the RDS path through `aws.service.scan`,
`aws.service.pagination.page`, `eshu_dp_aws_api_calls_total`,
`eshu_dp_aws_throttle_total`, `eshu_dp_aws_resources_emitted_total`,
`eshu_dp_aws_relationships_emitted_total`, and `aws_scan_status`; no new metric
or span name is needed for this metadata-only scanner.

Collector Deployment Evidence: RDS runs inside the existing hosted
`collector-aws-cloud` runtime through `awsruntime.DefaultScannerFactory`, so
the already documented `/healthz`, `/readyz`, `/metrics`, and `/admin/status`
surfaces apply without a separate deployment.

Collector Performance Evidence: `go test ./internal/collector/awscloud/services/dynamodb/...`
covers the bounded DynamoDB call shape: paginated ListTables with Limit=100,
one DescribeTable metadata read per discovered table, one paginated
ListTagsOfResource read per ARN-addressable table, one DescribeTimeToLive read
per discovered table, and one DescribeContinuousBackups read per discovered
table; no item reads, table scans, table queries, stream record reads,
backup/export payload reads, resource-policy reads, PartiQL calls, mutations,
or downstream graph writes in the collector. This slice did not run against a
live AWS account; the performance contract is the bounded O(table count) SDK
call shape and existing workflow claim/account concurrency limits.

No-Regression Evidence: `go test ./cmd/collector-aws-cloud ./internal/collector/awscloud/...`
covers DynamoDB table metadata fact emission, reported KMS relationship
emission, omission of item/data-plane fields, SDK pagination, tag reads,
runtime scanner registration, and the command config path that confirms
DynamoDB does not require an environment-value redaction key.

Collector Observability Evidence: DynamoDB uses the existing AWS collector
`aws.service.pagination.page` span plus `eshu_dp_aws_api_calls_total`,
`eshu_dp_aws_throttle_total`, `eshu_dp_aws_resources_emitted_total`,
`eshu_dp_aws_relationships_emitted_total`, and `aws_scan_status` rows. Metric
labels stay bounded to service, account, region, operation, result, and status;
table names, ARNs, tags, index names, TTL attribute names, KMS key IDs, and raw
AWS error payloads stay out of metric labels.

No-Observability-Change: the existing AWS collector telemetry contract already
diagnoses the DynamoDB path through `aws.service.scan`,
`aws.service.pagination.page`, `eshu_dp_aws_api_calls_total`,
`eshu_dp_aws_throttle_total`, `eshu_dp_aws_resources_emitted_total`,
`eshu_dp_aws_relationships_emitted_total`, and `aws_scan_status`; no new metric
or span name is needed for this metadata-only scanner.

Collector Deployment Evidence: DynamoDB runs inside the existing hosted
`collector-aws-cloud` runtime through `awsruntime.DefaultScannerFactory`, so
the already documented `/healthz`, `/readyz`, `/metrics`, and `/admin/status`
surfaces apply without a separate deployment.

Collector Performance Evidence: `go test ./internal/collector/awscloud/services/cloudwatchlogs/...`
covers the bounded CloudWatch Logs call shape: paginated DescribeLogGroups with
Limit=50 and one ListTagsForResource read per ARN-addressable log group; no
DescribeLogStreams, GetLogEvents, FilterLogEvents, Insights query calls,
resource-policy reads, export payload reads, subscription payload reads,
mutations, or downstream graph writes in the collector. This slice did not run
against a live AWS account; the performance contract is the bounded O(log group
count) SDK call shape and existing workflow claim/account concurrency limits.

No-Regression Evidence: `go test ./cmd/collector-aws-cloud ./internal/collector/awscloud/...`
covers CloudWatch Logs log group metadata fact emission, reported KMS
relationship emission, omission of log event/data-plane fields, SDK
pagination, tag reads, runtime scanner registration, and the command config
path that confirms CloudWatch Logs does not require an environment-value
redaction key.

Collector Observability Evidence: CloudWatch Logs uses the existing AWS
collector `aws.service.pagination.page` span plus
`eshu_dp_aws_api_calls_total`, `eshu_dp_aws_throttle_total`,
`eshu_dp_aws_resources_emitted_total`,
`eshu_dp_aws_relationships_emitted_total`, and `aws_scan_status` rows. Metric
labels stay bounded to service, account, region, operation, result, and status;
log group names, ARNs, tags, KMS key IDs, and raw AWS error payloads stay out
of metric labels.

No-Observability-Change: the existing AWS collector telemetry contract already
diagnoses the CloudWatch Logs path through `aws.service.scan`,
`aws.service.pagination.page`, `eshu_dp_aws_api_calls_total`,
`eshu_dp_aws_throttle_total`, `eshu_dp_aws_resources_emitted_total`,
`eshu_dp_aws_relationships_emitted_total`, and `aws_scan_status`; no new metric
or span name is needed for this metadata-only scanner.

Collector Deployment Evidence: CloudWatch Logs runs inside the existing hosted
`collector-aws-cloud` runtime through `awsruntime.DefaultScannerFactory`, so
the already documented `/healthz`, `/readyz`, `/metrics`, and `/admin/status`
surfaces apply without a separate deployment.

## Status Review (2026-05-10)

**Current disposition:** Design accepted; deployment model amended.

The original ADR chose a single central `collector-aws-cloud` deployment with
STS `AssumeRole` per claim. That remains the default for teams that can grant a
central Eshu control plane scoped cross-account read roles.

The design now also supports account-local collector deployments. Some
customers will prefer, or require, one collector per AWS account, organization
unit, or regulated boundary. The scanner must therefore share the same
provider-neutral target-scope and credential model as the Terraform-state
collector: central assume-role and local workload identity are both valid
deployment modes, and the coordinator/fact contracts must not depend on which
one was chosen.

## Status Review (2026-05-03)

**Current disposition:** Design accepted; runtime not implemented.

The shared identity and workflow substrate exists, including the AWS collector
kind and reducer phase contract. The actual collector runtime does not exist in
this repo yet: there is no `go/cmd/collector-aws-cloud` or
`go/internal/collector/awscloud` implementation.

**Remaining work:** implement the collector runtime, IAM-first scanner, service
packages, claim loop, AWS telemetry, fact emission, correlation consumers,
tests, and operator docs.

## Context

Eshu already correlates Terraform *configuration* from Git,
and the companion Terraform state collector ADR adds what Terraform *believes*
it built. Neither of those is sufficient to describe what AWS actually runs.

Many organizations, including parts of the platforms the Eshu targets, do not
provision every resource through Terraform. Pulumi, CloudFormation, CDK, SAM,
Serverless Framework, and direct console or CLI actions are all in scope.
Without direct cloud observation, Eshu cannot:

- see resources provisioned outside Terraform
- detect orphaned resources
- anchor canonical identity on real ARNs observed by the cloud API
- confirm hostnames, target groups, and load balancer bindings
- correlate runtime platform placement (ECS service, EKS workload, Lambda
  alias) with source repositories and IaC
- report on tags as they actually exist on cloud resources rather than as
  they were declared

The multi-source correlation DSL ADR already named `aws` as a first-class
collector family. The workflow coordinator ADR already defined the runtime
shape for `collector-aws-cloud` and reserved a place in the runtime contract.
This ADR is the collector-specific design ADR those two ADRs deferred.

### What Is Already Decided

This ADR inherits and does not revisit:

1. Collectors emit typed facts. They do not write canonical graph rows.
2. The reducer owns cross-source correlation through the correlation DSL.
3. The workflow coordinator owns scheduling, claim issuance, fencing, and
   completeness. The AWS scanner claims bounded work; it does not self-
   schedule.
4. `CollectorKind.aws` is part of the shared identity enum.
5. Collector instances are declarative. Each AWS account is one instance.

### What This ADR Decides

1. The scanner framework choice: roll own against AWS SDK for Go v2.
2. Deployment topology: central deployment by default, account-local
   deployment when customer boundaries require it.
3. Claim granularity and its relationship to AWS throttling boundaries.
4. Launch resource coverage and deferred coverage.
5. Rate discipline, concurrency bounds, and retry posture.
6. Credential model: workload identity plus scoped target credentials. No
   static keys.
7. Scope and generation identity for cloud observations.
8. Fact shapes and the tag-raw-emission contract.
9. Phased rollout, including which correlation DSL work depends on the
   scanner's output.

---

## Problem Statement

The platform must decide how AWS scanner runtimes can observe cloud state
across many accounts and many regions:

- safely under throttling limits
- without turning into a cloud security incident through over-privileged
  credentials
- without regressing the coordinator's invariants around fencing and
  completeness
- at a cadence that is useful without being abusive
- with telemetry strong enough to debug and tune from dashboards
- with tag and ARN emission that the correlation DSL can turn into canonical
  joins

The same runtime must scale from "one small account" to "dozens of accounts,
multiple regions each, many resource families" without demanding a separate
deployment per account or per region.

---

## Decision

### Framework: Roll Own Against AWS SDK For Go v2

The scanner should be built in-house against the AWS SDK for Go v2. The
platform should not adopt Cartography, Steampipe, AWS Config as a primary
source, or a third-party cloud CMDB as the scanner implementation.

Rationale:

- **Cartography** writes directly to a graph. It violates the Eshu rule that
  collectors emit facts and the reducer owns canonical writes. Adopting it
  would require gutting its write path or running a second graph. Neither is
  acceptable.
- **Steampipe** is SQL-over-API: an operator query surface rather than a
  fact emitter. It is not designed to be the upstream of a durable fact
  queue with scope and generation identity.
- **AWS Config** gives delta events but requires org-wide Config enablement,
  which the platform cannot assume across all customers. Config is useful
  as a configured freshness source; it is not the baseline authority.
- **Third-party CMDBs** add licensing, vendor dependency, and opaque
  correctness models. The platform's accuracy-first priority demands we
  own the observation path.

Rolling our own has real costs: schema maintenance per service, throttling
discipline, pagination logic, and cross-service dependency ordering. The
alternative is worse: misaligned ownership, loss of invariants, or both.

### Topology: Central Default, Account-Local Supported

The AWS scanner supports two deployment modes.

The default mode is a central `collector-aws-cloud` deployment. It runs in the
Eshu control-plane account or cluster and assumes target-account read roles per
claim.

Inside that deployment:

- a bounded worker pool drains coordinator claims
- each claim carries `(collector_instance, account, region, service_kind)`
- each worker performs `sts:AssumeRole` against the claim's account role ARN
  at claim-start time, caches the resulting credentials until the claim's
  lease expiry, and discards them on release
- no static access keys are loaded into the pod at any time

The second supported mode is account-local deployment. A customer may run a
collector in each account or organization boundary and let that workload use a
local role. The coordinator still assigns claims and the collector still emits
the same AWS facts. Only credential acquisition changes.

This is explicitly not:

- long-lived cached cross-account credentials (blast radius)
- one fact schema per deployment mode
- a reason to bypass coordinator claims, fencing, or rate limits

Central deployment is operationally simpler. Account-local deployment narrows
blast radius and may fit customer trust boundaries better. Eshu must support
both without forking the scanner architecture.

### Claim Granularity Matches Throttling Boundaries

AWS throttles API calls on a scope of roughly `(account, region, service)`
with some sharing across a control plane. Claim granularity must match:

```
claim_key = (collector_instance_id, account_id, region, service_kind)
```

The coordinator's existing claim-uniqueness and fencing contract then
enforces that at most one worker is actively calling `ecs.describe_*` in
`acct123/us-east-1` at any moment. This alone prevents self-inflicted
throttle wars.

Cross-account scans run fully in parallel. Cross-region within the same
account run in parallel. Cross-service within the same account+region run
concurrently up to a per-account concurrency cap (see below) to avoid
hammering the shared control plane.

### Launch Resource Coverage

Phase 1 covers the services that produce the highest-value correlation anchors
for container and service delivery, matching the DSL ADR's container vertical
slice:

| Service | Resources | Why |
| --- | --- | --- |
| **IAM** | Roles, Policies, InstanceProfiles, trust relationships | Trust chain from code to runtime identity |
| **ECR** | Repositories, Images (tags, digests), lifecycle policies | Image identity, the strongest code→runtime join |
| **ECS** | Clusters, Services, TaskDefinitions (redacted), Tasks | Runtime placement for non-EKS workloads |
| **EKS** | Clusters, Nodegroups, add-ons, OIDC provider | Runtime placement for Kubernetes workloads |
| **ELBv2** | LoadBalancers, Listeners, TargetGroups, Rules | Hostname + routing evidence |
| **Lambda** | Functions, Aliases, EventSourceMappings | Function-as-service runtime identity |
| **Route53** | HostedZones, Records (A, AAAA, CNAME, ALIAS) | Public hostname evidence for ALB/CloudFront targets |
| **EC2** | VPCs, Subnets, SecurityGroups, ENIs (metadata only) | Network topology referenced by ECS/EKS/Lambda |

Phase 2 (explicit non-goal for launch but planned):

- RDS metadata (implemented 2026-05-14; database connections, database names,
  master usernames, secrets, snapshots, log contents, Performance Insights
  samples, schemas, tables, row data, and mutations remain out of scope)
- DynamoDB metadata (implemented 2026-05-14; item values, table scans, table
  queries, stream records, backup/export payloads, resource policies, PartiQL
  output, and mutations remain out of scope)
- S3 bucket metadata (implemented 2026-05-14; object inventory, object keys,
  bucket policy JSON, ACL grants, replication rules, lifecycle rules,
  notification configuration, inventory configuration, analytics configuration,
  and metrics configuration remain out of scope)
- SNS topic metadata (implemented 2026-05-14; message payloads, topic policy
  JSON, data-protection-policy JSON, and raw non-ARN subscription endpoints
  remain out of scope)
- EventBridge metadata (implemented 2026-05-14; PutEvents, rule/target
  mutations, event bus policy JSON, target input payload fields, input
  transformers, HTTP target parameters, and raw non-ARN targets remain out of
  scope)
- SQS queue metadata (implemented 2026-05-14; message bodies and queue policy
  JSON remain out of scope)
- CloudFront metadata (implemented 2026-05-14; object contents, origin
  payloads, distribution config payloads, policy documents, certificate bodies,
  private keys, origin custom header values, and mutations remain out of scope)
- API Gateway metadata (implemented 2026-05-14; API execution, exports, API
  keys, authorizer secrets, policy JSON, integration credentials, stage
  variable values, request templates, response templates, payloads, and
  mutations remain out of scope)
- Secrets Manager metadata (implemented 2026-05-14; secret values, version
  payloads, resource policy JSON, external rotation partner metadata, external
  rotation role ARNs, and mutations remain out of scope)
- SSM Parameter Store metadata (implemented 2026-05-14; parameter values,
  history values, raw descriptions, raw allowed patterns, raw policy JSON,
  decryption, and mutations remain out of scope)
- CloudWatch Logs (implemented 2026-05-14; log events, log stream payloads,
  Insights query results, export payloads, resource policies, subscription
  payloads, and mutations remain out of scope)

Phase 3+ covers additional services on operator demand.

### Scan Model: Full Scheduled Scans, With AWS Config As Later Freshness Layer

Launch scan behavior:

- full `List*` + `Describe*` sweep per `(account, region, service)` on a
  schedule
- default refresh interval per service family configurable per instance
- conditional refresh uses the scanner's own checkpoints
  (`last_seen_etag`-equivalent for each resource type) to skip re-emission
  when the cloud API did not actually change observed state, but the scanner
  still *queries* the API each cycle; cache is for fact emission, not for
  call avoidance
- EventBridge / AWS Config integration is a **phase 3** freshness
  layer. When enabled, it converts cloud change events into coordinator run
  requests targeted at the affected `(account, region, service)` tuple.
  Baseline full scans remain.

This model keeps the scanner simple and correct first, and opens a path to
lower-latency freshness once the baseline is trusted.

### Rate Discipline

Required behavior:

1. **Per-service token bucket** inside each worker, sized per-service and
   configurable per collector instance. Defaults are conservative.
2. **SDK v2 retry mode** enabled on every client. No retries disabled
   anywhere. Adaptive mode is allowed only when clients are isolated by the
   service throttle boundary for the claim; otherwise the scanner uses
   standard retry plus Eshu's token bucket.
3. **Pagination pacing** for high-cardinality services (EC2, Lambda,
   CloudWatch Logs). A small sleep between pages when a run exceeds a
   configurable page count. Backoff on throttle.
4. **Per-account concurrency cap** across the worker pool: a bounded
   in-flight claim count per `account_id` regardless of service. Default
   `4`. Prevents surge when many services in the same account become
   claimable simultaneously.
5. **Hard API budget per scan** per `(account, region, service)`. When
   exceeded, the scan is marked `budget_exhausted`, emits an `aws_warning`
   fact, and yields the claim. The next coordinator run resumes from the
   last page checkpoint.
6. **Throttle observability.** `aws_throttle_total{service, account,
   region}` is a first-class metric. Operators must be able to see throttle
   events as they happen.

### Credential Model: IRSA + Cross-Account AssumeRole + External ID

The scanner runtime runs under an IRSA-bound service account in the hosting
cluster.

For each collector instance (= AWS account):

- operator configures `role_arn` in the target account
- operator configures `external_id` supplied through the platform's secret
  source, not committed to config
- the target role grants only read permissions required for the declared
  service set
- the trust policy restricts principals to the scanner's IRSA principal and
  requires the external ID

At claim time:

1. worker calls `sts:AssumeRole` with the configured `role_arn` and external
   ID
2. worker receives short-lived credentials with `DurationSeconds=900` requested
   to match the proposed claim lease
3. credentials are used only for the duration of this claim
4. on claim completion or expiry, scanner-owned references to the credentials,
   provider, cache, and claim-scoped clients are cleared

No long-lived keys, no env-var-injected static credentials, no role chaining
beyond the cross-account hop. The runtime must reject static access key
credential sources; central and account-local modes use workload identity or
an equivalent role provider.

### Scope And Generation Identity

Scope: `ScopeKind.account` with attributes `(account_id, region)`. No new
scope kind is introduced; the multi-source correlation ADR already committed
to `account`, and adding `cloud_region` now would splinter scope identity.
Region is carried as a scope attribute.

### Required Reducer/Consumer Contract Fields

The accepted multi-source reducer/consumer ADR freezes the AWS scanner fields
that downstream joins and drift queries require. These are first-class fact
fields, not query-time reconstructions.

Add to `aws_resource_fact`:

- `arn` — required for every resource type that has an ARN
- `resource_type` — frozen enum matching the reducer-owned canonical label
- `correlation_anchors` — `[]string` containing deterministic join anchors
  such as ARN, normalized hostname, image digest, or normalized tags

Add to the shared AWS fact envelope:

- `scope_id`
- `collector_kind=aws`
- `generation_id`
- `fence_token`
- `source_confidence`

Normalization contract:

- the scanner emits raw `tags`
- `tags_normalized` is populated later by `reducer/tags/normalizer`
- downstream queries must treat `tags_normalized` as a canonical-node field,
  not a raw collector field

Resources without ARNs must document a deterministic fallback key family in
the collector implementation plan. Examples include ECS task-definition
family+revision or IAM policy version identity. Those fallback keys must also
be surfaced through `correlation_anchors`; they must never remain implicit in
opaque payload blobs.

Generation: monotonic per `(account_id, region, service_kind)`, assigned by
the scanner at claim start from a coordinator-provided monotonic counter.
This gives a stable "which scan produced this fact" anchor without requiring
the cloud API to provide one.

Each resource fact also carries, where available:

- the AWS-reported `last_modified` or equivalent timestamp
- a resource-level digest computed by the scanner over the normalized
  observed attributes, used for unchanged detection on the next scan

### Fact Shapes

The scanner emits a bounded set of fact kinds. Per-service schemas live in
versioned packages; the envelope is shared.

| Fact Kind | Purpose |
| --- | --- |
| `aws_resource` | one per observed resource; carries `arn`, `resource_type`, `resource_id`, `region`, `account_id`, `tags`, `state`, `created_at`, `modified_at`, typed attributes |
| `aws_relationship` | one per observed relationship (ECS service→task def, ALB listener→target group, Lambda alias→function, IAM role→policy, ENI→subnet) |
| `aws_tag_observation` | tag key/value pairs emitted separately for correlation indexing, preserving account + region + resource ARN |
| `aws_dns_record` | Route53 record emitted as its own shape because of its join importance |
| `aws_image_reference` | ECR image digest + tag + repository + pushed_at; separate shape because images are the single most important code→cloud join |
| `aws_warning` | anti-pattern signals (`plaintext_env_var`, `public_s3_metadata_flag`, `budget_exhausted`, `unknown_resource_schema`, `throttle_sustained`, `assumerole_failed`) |

Every fact carries:

- `scope_id`, `generation_id`, `collector_kind = aws`
- `account_id`, `region`, `service_kind`
- `collector_instance_id`

### Raw Tag Emission

Tags are emitted unchanged. Keys preserve original case and exact spelling.
Values preserve original case and spelling.

Tag normalization is **not** the collector's concern. Aliasing (`env`,
`environment`, `Env`, `ENV`), value normalization (`prod` vs `production`),
precedence against TF state tags, exclusion rules (`DoNotIndex`,
`Name:^(test|scratch|tmp)-`), and confidence scoring all live in the
correlation DSL.

The collector also emits:

- `aws_tag_distribution` summary facts per `(account, region, resource_type,
  tag_key)` with distinct-value count and total occurrences. These feed a
  later operator-facing "suggest alias" workflow in the DSL admin surface.

### Sensitive Value Discipline

Some AWS resources leak secrets through attributes:

- Lambda function environment variables
- ECS task definition container environment variables
- ECS task definition secret `value_from` references, which are preserved as
  references and never resolved to secret values
- RDS database names and master usernames are identity-adjacent database fields
  and stay out of scanner facts

The scanner applies redaction analogous to the state collector:

- env var values replaced with deterministic `go/internal/redact` markers
- each redacted value carries marker, reason, source, and the
  `aws-launch-2026-05-14` ruleset version
- known sensitive env var keys use `known_sensitive_key`
- other env var keys fail closed as `unknown_provider_schema`
- secret references (ARN or provider reference) preserved as references, not
  secret values
- telemetry never carries plaintext values

Cloud scanner redaction and state collector redaction share the same library
(`go/internal/redact`) so the marker implementation stays unified while AWS
owns its provider-specific key policy.

### Durable Pagination Checkpoints

Long AWS service scans persist retry-safe page markers in Postgres table
`aws_scan_pagination_checkpoints`. Each row is scoped by collector instance,
account, region, service kind, resource parent, operation, generation, and the
workflow fencing token. The primary key omits generation so a new workflow
generation can replace or expire stale operation state for the same logical
service scan.

The current commit boundary is still whole-generation ingestion: service
scanners return facts, and the shared collector committer persists those facts
after scanning completes. Because of that, checkpoint values represent the page
token that is safe to retry next, not the token after uncommitted facts. A crash
may re-read the last API page, but deterministic fact identity prevents durable
duplicates and avoids skipping pages whose facts may not have committed.

At claim start, `collector-aws-cloud` deletes prior-generation checkpoint rows
for the same `(collector_instance_id, account_id, region, service_kind)` scope.
Writes use the workflow fencing token so an expired worker cannot overwrite or
delete checkpoint state written by a newer claim. ECR image pagination is the
first service consumer; ECS, ELBv2, Lambda, Route 53, EC2, and EKS should reuse
the same `checkpoint.Store` seam as their higher-cardinality operations move
from in-memory pagination to resumable pagination.

Checkpoint observability uses
`eshu_dp_aws_pagination_checkpoint_events_total{service,account,region,operation,event_kind,result}`.
The event-kind taxonomy is `load`, `save`, `resume`, `expiry`, `complete`, and
`failure`. Resource parents, ARNs, page tokens, repository names, and
hosted-zone IDs are not metric labels.

---

## Architecture

### Runtime Shape

- binary: `/usr/local/bin/eshu-collector-aws-cloud`
- package: `go/cmd/collector-aws-cloud/`
- internal: `go/internal/collector/awscloud/`
- Kubernetes: `Deployment`, horizontally scalable under the coordinator
  claim model
- deployment count follows the target-scope credential model: one central
  deployment by default, or one account-local deployment per boundary when
  that is the safer customer choice

### Package Layout

```text
go/
  cmd/
    collector-aws-cloud/
      main.go
  internal/
    collector/
      awscloud/
        service.go              # coordinator claim loop
        worker.go               # per-claim worker
        credentials.go          # STS AssumeRole and claim-scoped lifecycle
        throttle.go             # per-service token buckets
        checkpoint/             # durable pagination checkpoint contract
        pagination.go           # shared pagination + pacing
        facts.go                # fact envelope builders
        redact.go               # thin wrapper over go/internal/redact
        telemetry.go            # span/metric helpers
        services/
          ec2/
          ecr/
          ecs/
          eks/
          elbv2/
          iam/
          lambda/
          route53/
```

Each service package exposes a small interface:

```go
type ServiceScanner interface {
    Kind() ServiceKind
    Scan(ctx context.Context, claim Claim, emit FactEmitter) (ScanResult, error)
}
```

This keeps per-service code isolated and allows independent testing and
evolution.

### Collector Instance Configuration

```yaml
collectors:
  - id: aws-prod-main
    kind: aws
    mode: scheduled
    enabled: true
    bootstrap: true
    config:
      account_id: "123456789012"
      account_alias: prod-main
      role_arn: arn:aws:iam::123456789012:role/eshu-collector
      external_id: ${secret:aws.prod-main.external_id}

      regions:
        - us-east-1
        - us-west-2

      services:
        - iam
        - ecr
        - ecs
        - eks
        - elbv2
        - lambda
        - route53
        - ec2

      max_concurrent_claims: 4
      per_service_rps:
        ec2: 5
        lambda: 5
        elbv2: 10
        ecs: 10
        ecr: 10
        route53: 5
        iam: 5
      scan_interval_default: 30m
      scan_interval:
        iam: 6h
        route53: 1h
      page_pacing_threshold: 50
      claim_lease: 15m
```

One instance row per AWS account. Many instances supported; coordinator
handles them uniformly.

### Control Flow Per Claim

1. Worker receives claim `(instance, account, region, service)`.
2. Credentials: `sts:AssumeRole` using `role_arn` + `external_id`.
3. Scanner for `service` executes its `Scan` method, which:
   - issues `List*` and `Describe*` calls with throttling budget
   - resolves intra-service relationships (ECS service→task def, ALB
     listener→target group, Lambda alias→function)
   - streams `aws_resource`, `aws_relationship`, `aws_tag_observation`,
     `aws_image_reference`, `aws_dns_record` facts
   - applies redaction in the emission loop
4. On completion, worker reports resource counts, API call counts, throttle
   counts, and budget consumption to the coordinator.
5. Scanner-owned credential references and claim-scoped clients are cleared.
   Claim is released.

Cross-service dependencies (e.g., ECS service references a task def that
references an ECR image) are **not** resolved inside the scanner. Those
joins land in the correlation DSL, which already operates across fact
boundaries. The scanner emits flat, typed facts; the DSL joins them.

### Coordinator Completeness

The coordinator considers an AWS run complete when, for every
`(account, region, service_kind)` in the instance's configured set:

- the claim succeeded without `budget_exhausted`
- all declared pages were read or a checkpoint was committed
- no `assumerole_failed` warning is outstanding

Partial completion is a first-class state. A run where `ec2` succeeded but
`lambda` was budget-exhausted is explicitly `partial`, not `failed`, and
the next run resumes `lambda` from checkpoint.

`collector-aws-cloud` persists this operator view in `aws_scan_status`, keyed by
`(collector_instance_id, account_id, region, service_kind)`. Scanner-side state
records API call counts, throttle counts, warning counts, and budget or
credential flags. Commit-side state records whether the fenced fact transaction
committed after the scanner returned. `/admin/status` reads that row so a 3 AM
operator can separate throttling, credentials, budget ceilings, and commit
failures without scanning logs.

---

## Invariants

After this collector lands, the following must hold:

1. The scanner holds no static AWS credentials.
2. Cross-account credentials live only for the duration of a single claim
   and are never persisted.
3. No plaintext environment variable value, secret field, or password
   attribute is emitted. Redaction is enforced in the emission path.
4. No S3 object contents, CloudWatch log contents, Secrets Manager secret
   values, or SSM parameter values are read. Metadata only, ever.
5. Every emitted fact carries `scope_id`, `generation_id`, `account_id`,
   `region`, `service_kind`, and `collector_instance_id`.
6. At most one worker is actively scanning a given `(account, region,
   service_kind)` tuple at a time. Throttle contention is coordinator-
   enforced, not prayer-driven.
7. Tags are emitted raw. The collector does not rename, alias, or normalize
   tag keys or values.
8. Claim ownership, fencing, and completeness flow through the workflow
   coordinator's shared contract.
9. The scanner does not write canonical graph truth. It does not correlate
   across accounts, across services outside its own scan, or with
   non-cloud sources.

---

## Observability Requirements

Metrics (prefix `eshu_dp_aws_`):

- `api_calls_total{service, account, region, operation}`
- `api_errors_total{service, account, region, error_class}`
- `throttle_total{service, account, region}`
- `scan_duration_seconds_bucket{service, account, region}`
- `claim_concurrency{account}` (gauge)
- `resources_emitted_total{service, account, region, resource_type}`
- `relationships_emitted_total{service, account, region}`
- `tag_observations_emitted_total{service, account, region}`
- `budget_exhausted_total{service, account, region}`
- `assumerole_failed_total{account}`
- `unchanged_resources_total{service, account, region}` (skipped emission)
- `pagination_pacing_events_total{service}`

Spans:

- `aws.collector.claim.process`
- `aws.credentials.assume_role`
- `aws.service.scan` (per service kind)
- `aws.service.pagination.page`
- `aws.fact.emit_batch`

Structured logs carry `scope_id`, `generation_id`, `account_id`, `region`,
`service_kind`, `collector_instance_id`. They must not carry tag values,
environment variable values, or secret-adjacent attribute values.

Admin status exposes, per instance:

- last successful scan per `(account, region, service_kind)`
- last throttle event and its counts
- outstanding `budget_exhausted` or `assumerole_failed` warnings
- per-account concurrency in-flight
- recent API error classes summarized

At 3 AM an operator should be able to answer: is the scanner stuck on
throttling, stuck on credentials, stuck on a budget ceiling, or simply
behind schedule.

---

## Explicit Non-Goals

1. Non-AWS clouds (GCP, Azure, Oracle, OCI) — separate ADRs per provider.
2. AWS data-plane content (S3 objects, log contents, Secrets Manager
   values).
3. Tag normalization, aliasing, or value canonicalization.
4. Drift detection between AWS and Terraform state — lives in the DSL.
5. Orphan detection, unmanaged-resource detection — lives in the DSL.
6. A second graph writer. The scanner emits facts; the reducer writes.
7. Replacing the coordinator's claim contract.
8. Long-lived or org-wide super credentials. Every account is a separate
   assume-role hop.
9. Cartography, Steampipe, or third-party CMDB adoption.

---

## Rollout Plan

### Phase 1: Runtime + IAM + Credentials + One Service (IAM)

- publish this ADR
- implement the runtime skeleton, claim loop, throttling, credentials
- ship IAM scanner first because it produces the trust-chain evidence that
  every other service needs, and because it has the most stable schema
- wire coordinator claim flow end to end, including completeness and
  partial-run semantics
- land core telemetry

### Phase 2: Container Vertical Slice (ECR + ECS + EKS + ELBv2 + Lambda + Route53 + EC2)

- add the remaining launch services
- validate the correlation DSL's container vertical slice against a real
  account with known state
- validate that throttling behavior at target concurrency is acceptable
  against a medium-sized org account

### Phase 3: Correlation DSL Joins

- image-to-code joins (ECR → build workflow → repo)
- ECS/EKS/Lambda → IAM role → code trust chain
- ALB listener + TargetGroup → ECS service → workload
- Route53 record → ALB/CloudFront → service
- drift joins against Terraform state facts (depends on state collector)

### Phase 4: Phase 2 Service Expansion

- RDS metadata (implemented 2026-05-14)
- DynamoDB metadata (implemented 2026-05-14)
- S3 metadata (implemented 2026-05-14)
- SQS queue metadata (implemented 2026-05-14)
- SNS topic metadata (implemented 2026-05-14)
- EventBridge metadata (implemented 2026-05-14)
- CloudFront metadata (implemented 2026-05-14)
- API Gateway metadata (implemented 2026-05-14)
- Secrets Manager metadata (implemented 2026-05-14)
- SSM Parameter Store metadata (implemented 2026-05-14)
- CloudWatch Logs group metadata (implemented 2026-05-14)

### Phase 5: Freshness Layer

- Internal trigger contract, durable coalescing store, and workflow planner
  started 2026-05-15
- EventBridge / AWS Config ingress service and admin status projection remain
  follow-up work
- Baseline scheduled scans remain authoritative and repair missed, duplicate,
  late, or dropped events

---

## Consequences

### Positive

- Platform gains cloud ground truth that no Git-only or state-only source
  can provide.
- Correlation DSL gains ARN-anchored joins across code, state, and cloud.
- Orphan, drift, and unmanaged detection become possible without inventing
  a second writer.
- Operators get one pane of glass for AWS observability under the existing
  coordinator status surface.

### Negative

- Introduces the first Eshu runtime that holds cross-account cloud
  credentials. Security review is mandatory at every phase.
- Adds per-service schema maintenance. AWS resource shapes evolve; the
  scanner must track SDK v2 updates.
- Introduces AWS API cost as a platform operational concern. Scan tuning
  moves from "nice to have" to "must have."

### Risks

- **Throttle-driven outages** of cloud control planes. Mitigated by
  coordinator-enforced claim uniqueness, per-service token buckets, SDK retry,
  and per-account concurrency caps.
- **Credential compromise.** Mitigated by IRSA-only principals, required
  external IDs, claim-scoped credential lifetime, redaction of secret-
  adjacent attributes, and telemetry on `assumerole_failed`.
- **Schema drift** between SDK v2 and the scanner's typed fact shape.
  Mitigated by `unknown_resource_schema` warning facts and a versioned
  per-service schema package that is required to compile.
- **Silent under-scanning** when a high-cardinality service pages forever.
  Mitigated by hard API budgets per scan, resumable pagination checkpoints,
  and `budget_exhausted` warnings.

---

## Appendix: Implementation Workstreams

### Chunk A: Runtime Skeleton + IAM Scanner

**Scope:** runtime, claim loop, credentials, throttling, redaction, IAM
service scanner.

**Likely files:**

- `go/cmd/collector-aws-cloud/main.go`
- `go/internal/collector/awscloud/service.go`
- `go/internal/collector/awscloud/worker.go`
- `go/internal/collector/awscloud/credentials.go`
- `go/internal/collector/awscloud/throttle.go`
- `go/internal/collector/awscloud/services/iam/`

### Chunk B: Container Vertical Slice Services

**Scope:** ECR, ECS, EKS, ELBv2, Lambda, Route53, EC2.

**Likely files:**

- `go/internal/collector/awscloud/services/ecr/`
- `go/internal/collector/awscloud/services/ecs/`
- `go/internal/collector/awscloud/services/eks/`
- `go/internal/collector/awscloud/services/elbv2/`
- `go/internal/collector/awscloud/services/lambda/`
- `go/internal/collector/awscloud/services/route53/`
- `go/internal/collector/awscloud/services/ec2/`

### Chunk C: Coordinator Completeness + Admin Status

**Scope:** partial-run semantics, pagination checkpoints, admin status
surface parity with other collectors.

**Likely files:**

- `go/internal/workflow/...` (new in coordinator substrate)
- `go/internal/runtime/admin/...`
- `go/internal/collector/awscloud/service.go` checkpoints

### Chunk D: DSL Integration

**Scope:** correlation DSL rule packs consuming AWS facts. Depends on state
collector for drift rules, on Git collector for code joins.

**Likely files:**

- `go/internal/correlation/rules/aws_ecr/`
- `go/internal/correlation/rules/aws_ecs/`
- `go/internal/correlation/rules/aws_eks/`
- `go/internal/correlation/rules/aws_elbv2/`
- `go/internal/correlation/rules/aws_lambda/`
- `go/internal/correlation/rules/aws_route53/`
- `go/internal/correlation/rules/state_to_cloud_arn/`
- `go/internal/correlation/rules/cloud_drift/`

### Chunk E: Phase 2 Services

**Scope:** SQS queue metadata, SNS topic metadata, EventBridge metadata, S3
bucket metadata, RDS metadata, DynamoDB metadata, CloudWatch Logs group
metadata, CloudFront metadata, API Gateway metadata, Secrets Manager metadata,
and SSM metadata are implemented.

### Chunk F: Freshness Layer

**Scope:** EventBridge / AWS Config integration. The first slice adds the
internal normalized trigger contract, `aws_freshness_triggers` coalescing
store, and AWS workflow planner. Follow-up slices must add the provider ingress
runtime, admin status projection, and deployment wiring.

---

## Recommendation

The platform should build a first-party AWS scanner against AWS SDK for Go
v2, with a worker pool claiming `(account, region, service_kind)` tuples
through the workflow coordinator. It should default to central STS
`AssumeRole` per claim with mandatory external IDs, while also supporting
account-local workload identity for stricter customer boundaries. It should
ship the container vertical slice at launch and emit raw tags for the
correlation DSL to normalize.

This runtime is the prerequisite for turning Terraform state and code intent
into anchored ground truth. Every cross-source canonical join the platform
wants to make — drift, orphan, unmanaged, workload placement, service
hostname evidence — becomes expressible the moment this collector emits
clean ARN-anchored facts.
