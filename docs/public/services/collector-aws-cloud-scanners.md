# AWS Collector Scanner Coverage

Use this page for the AWS `service_kind` values backed by production scanner
adapters. Each scanner self-registers from
`go/internal/collector/awscloud/services/<svc>/runtimebind/init()`. The
collector-aws-cloud command pulls every binding through
`go/internal/collector/awscloud/awsruntime/bindings/bindings.go`, and the
runtime entry point `awsruntime.DefaultScannerFactory.Scanner` dispatches
through the resulting registry.

The collector is metadata-only. It emits reported facts for reducer admission.
It does not mutate AWS resources, read protected payloads, or write graph truth.

## Supported Service Kinds

`allowed_services` may include:

| Service kind | Coverage |
| --- | --- |
| `iam` | Roles, managed policies, instance profiles, trust relationships. |
| `ecr` | Repositories, lifecycle policies, image references, pagination checkpoints. |
| `ecs` | Clusters, services, tasks, relationships, redacted task definitions. |
| `ec2` | VPC, subnet, security group, security-group rule, ENI topology. |
| `elbv2` | Load balancers, listeners, listener rules, target groups, routing relationships. |
| `lambda` | Functions, aliases, event-source mappings, image URIs, execution roles, network joins, redacted environment values. |
| `eks` | Clusters, node groups, add-ons, OIDC providers, IAM roles, network join evidence. |
| `route53` | Hosted zones and A, AAAA, CNAME, ALIAS DNS record facts. |
| `sqs`, `sns`, `eventbridge` | Queue/topic/bus metadata and ARN-addressable relationships. |
| `guardduty` | Detectors, member accounts, filter names, publishing destinations, threat intel/IP set metadata, and aggregate finding counts. |
| `s3` | Bucket metadata and server-access-log target bucket relationships. |
| `rds` | DB instances, clusters, subnet groups, and reported security/KMS/role/group relationships. |
| `redshift` | Provisioned clusters, cluster parameter groups, cluster subnet groups, cluster snapshot metadata, scheduled action metadata, Serverless namespaces, Serverless workgroups, and reported VPC/subnet/security-group/KMS/IAM/snapshot/scheduled-action/namespace-workgroup relationships. Provisioned and Serverless share `service_kind=redshift`; resource types distinguish the two surfaces. |
| `dynamodb`, `cloudwatchlogs` | Table or log-group metadata and KMS relationships. |
| `cloudfront` | Distribution metadata plus ACM certificate and WAF web ACL relationships. |
| `acm` | Public ACM certificate metadata (ARN, domain name, SANs, status, type, issuer, validity, key and signature algorithms) and certificate-to-using-resource relationships derived from ACM-reported in-use-by ARNs (ELB v2, CloudFront, API Gateway, AppSync, App Runner, and other ARN-shaped targets). No certificate body PEM, no private key material, no `GetCertificate` calls, no `ExportCertificate` calls; ACM Private CA is out of scope. |
| `cloudtrail` | Trail (multi-region and per-region), Lake event data store, channel, and Lake dashboard configuration metadata with trail-to-S3-bucket, trail-to-CloudWatch-Logs, trail-to-KMS-key, trail-to-SNS-topic, and event-data-store-to-KMS-key relationships. Event selectors are summarized as counts only; CloudTrail event payloads, Lake query strings, Lake query results, and dashboard widget query SQL are never read or persisted. |
| `apigateway` | REST, HTTP, WebSocket, stage, custom-domain, mapping, access-log, ACM, and integration metadata. |
| `secretsmanager`, `ssm` | Secret or parameter metadata with KMS relationships; no secret/parameter values. |
| `athena` | Workgroup, data catalog, prepared-statement, and named-query metadata plus workgroup-to-S3-result-bucket, workgroup-to-KMS-key, prepared-statement-to-workgroup, and named-query-to-workgroup relationships. No SQL bodies, query results, query result location object contents, or query history strings. |
| `securityhub` | Hub configuration, enabled standards, controls, member accounts, action targets, insight summaries, and aggregate finding counts; no finding bodies or insight filters. |
| `glue` | Data Catalog database, table, crawler, job, trigger, workflow, and connection metadata plus table-in-database, table-to-S3-location, crawler-to-database, crawler-to-IAM-role, job-to-IAM-role, and trigger-to-job relationships. No script bodies, default-argument values, connection passwords, JDBC credential URLs, workflow graphs, table column sample statistics, or classifier custom patterns. |
| `elasticache` | Cache clusters, replication groups, parameter and subnet groups, users, user groups, and snapshot metadata (name/source/status only); cluster-to-VPC, cluster-to-subnet, cluster-to-KMS, replication-group-to-cluster, and user-group-to-user relationships. No AUTH tokens, user passwords, user access strings, cache contents, or snapshot data. |
| `msk` | MSK cluster, broker configuration, and replicator metadata with subnet, security-group, KMS-key, IAM-role, and configuration relationships; no broker `server.properties` bodies, broker logs, bootstrap broker endpoints, SCRAM secrets, or Kafka topic data. |
| `stepfunctions` | State machine and activity metadata, execution-role relationships, and ARN-only Task-target relationships; no execution payloads, history events, task tokens, or definition literals. |
| `accessanalyzer` | Analyzer metadata, archive-rule names, aggregate finding counts, relationships, and unused-access summaries. |
| `organizations` | Organization root, OUs, accounts, policy summaries, policy target bindings, and delegated administrators. |

IAM, Route 53, and CloudFront are global-style families. Use a concrete global
region label such as `aws-global` so claims keep the
`(account_id, region, service_kind)` shape. Organizations uses the `us-east-1`
control-plane endpoint and requires management-account or
delegated-administrator credentials.

## Data Boundaries

The collector does not read S3 object contents, SQS messages, DynamoDB table
data, RDS database contents, Redshift warehouse queries, Redshift table data,
Redshift snapshot contents, Redshift master user passwords or admin passwords,
ElastiCache cache keys, cache values, AUTH tokens, user passwords, user access
strings, or snapshot data, CloudWatch log events, Secrets Manager secret
values, SSM parameter values, API Gateway execution payloads, Lambda code
packages, CloudFront origin payloads, private keys, raw SNS endpoints, raw
EventBridge target inputs, Athena query result rows, Athena named-query SQL
bodies, Athena prepared-statement query bodies, Athena query history strings,
Glue job script bodies, Glue default-argument values, Glue connection
passwords or JDBC credential URLs, Glue workflow graph payloads, Glue table
column statistics with sample values, Glue classifier custom patterns, MSK
Kafka topic or message data, MSK broker logs, MSK broker `server.properties`
bodies, MSK configuration revision bodies, MSK bootstrap broker endpoints, MSK
SCRAM secret material, Step Functions execution input or output, Step
Functions execution history events, Step Functions activity task tokens, or
IAM/resource policy JSON unless a service package explicitly documents a
sanitized metadata-only exception. Step Functions state machine definitions
are persisted only as state names, state types, structural transitions, and
Task Resource ARNs; Parameters, ResultPath, ResultSelector, InputPath,
OutputPath, and Result literal contents are excluded.
GuardDuty finding bodies, GuardDuty filter criteria, GuardDuty threat intel set
list contents, and GuardDuty IP set list contents are also out of scope.
CloudTrail audit event payloads, Lake query strings, Lake query result rows,
event selector bodies, and dashboard widget query SQL stay outside the
collector contract; the CloudTrail scanner emits trail and Lake configuration
only, summarizing selectors as bounded counts.

ACM certificate body PEM and ACM-issued private key material are out of scope.
The ACM scanner never calls `GetCertificate` or `ExportCertificate`, and ACM
Private CA (acm-pca) APIs are not exercised.

Security Hub finding aggregate counts are metadata-only when grouped by bounded
posture fields such as severity, standard, control, compliance status, and
workflow status. Security Hub finding bodies, resource IDs from findings,
resource details, remediation text, product fields, user-defined fields, note
text, network/process details, and insight filter expressions remain outside
the collector contract.

Organizations policy attachment metadata is in scope: policy ID, policy name,
policy type, and target binding. Policy document bodies, statements,
conditions, action lists, and guardrail text are out of scope by default.
Account email and account name values must pass through the shared AWS
redaction path before persistence.

It also does not call AWS mutation APIs. If a scanner needs a new API family,
update the owning service package README with source APIs, forbidden data
classes, emitted evidence, and verification.

Access Analyzer has an extra security boundary: external finding bodies,
archive-rule filter criteria, policy-generation results, and per-action
unused-access details are not persisted. The scanner keeps aggregate finding
counts by status and resource type, plus per-resource unused-access
last-accessed timestamps.

## Evidence And Telemetry

Scanner packages emit reported `aws_resource`, `aws_relationship`,
`aws_image_reference`, `aws_dns_record`, and `aws_warning` facts. Reducers must
corroborate them before promoting workload, deployment, ownership, drift, or
unmanaged-resource truth.

Runtime spans include `aws.collector.claim.process`,
`aws.credentials.assume_role`, `aws.service.scan`, and
`aws.service.pagination.page`. The metric catalog lives in
[Ingestion And Collector Metrics](../reference/telemetry/metrics-ingestion-collectors.md).

## Change Rules

When adding or widening a scanner, keep it metadata-only unless an active design
record says otherwise. Add scanner tests, SDK-adapter tests, command-side target
validation tests, and registry support through `awsruntime.SupportedServiceKinds`.
Run the performance evidence gate when the scanner adds pagination fanout, claim
concurrency, batch sizing, queue pressure, or downstream reducer work.

## Related Docs

- [AWS Cloud Collector](collector-aws-cloud.md)
- [AWS Collector Security And Config](collector-aws-cloud-security.md)
- [Collector Runtime Services](../deployment/service-runtimes-collectors.md)
