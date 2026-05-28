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
| `ec2` | VPC, subnet, security group, security-group rule, ENI topology. The EC2 scanner owns the ENI surface and may carry instance target evidence on ENI attachments, but does not emit `aws_ec2_instance` resources; VPC network-fabric resources live in the `vpc` scanner. |
| `vpc` | Route tables, internet gateways, NAT gateways, network ACLs, VPC peering connections, VPC endpoints (gateway and interface), Elastic IPs, DHCP option sets, customer gateways, virtual private gateways, and site-to-site VPN connections plus relationships back to EC2-owned VPCs, subnets, ENIs, and instances. No VPN tunnel pre-shared keys or `CustomerGatewayConfiguration` XML bodies. |
| `transitgateway` | Transit gateways, transit gateway route tables, attachments (VPC, VPN, Direct Connect gateway, peering, Connect), peering attachments, multicast domains, and policy tables. Relationships: attachment-to-VPC/VPN-connection/Direct-Connect-gateway/peer, route-table-to-attachment, attachment-to-transit-gateway, route-table/multicast-domain/policy-table-to-transit-gateway, and peering-to-remote-transit-gateway. Cross-account peer transit gateway IDs, owner accounts, and Regions are surfaced as AWS reports them and flagged `cross_account` for downstream org-context joins; the scanner never resolves the remote account identity. No transit gateway routes, multicast group memberships, or policy table rule bodies. |
| `elbv2` | Load balancers, listeners, listener rules, target groups, routing relationships. |
| `lambda` | Functions, aliases, event-source mappings, image URIs, execution roles, network joins, redacted environment values. |
| `eks` | Clusters, node groups, add-ons, OIDC providers, IAM roles, network join evidence. |
| `route53` | Hosted zones and A, AAAA, CNAME, ALIAS DNS record facts. |
| `sqs`, `sns`, `eventbridge` | Queue/topic/bus metadata and ARN-addressable relationships. |
| `guardduty` | Detectors, member accounts, filter names, publishing destinations, threat intel/IP set metadata, and aggregate finding counts. |
| `s3` | Bucket metadata and server-access-log target bucket relationships. |
| `rds` | DB instances, clusters, subnet groups, and reported security/KMS/role/group relationships. |
| `docdb` | DocumentDB DB clusters, cluster instances, cluster parameter groups (name + family + parameter count only - NOT parameter values), cluster snapshot metadata, subnet groups, global clusters, and event subscription metadata with cluster-to-VPC, cluster-to-subnet-group, cluster-to-KMS-key, instance-to-cluster, and global-cluster-to-cluster relationships. No master user passwords, master user secrets, database document contents, collections, indexes, cluster parameter values, or snapshot contents. |
| `redshift` | Provisioned clusters, cluster parameter groups, cluster subnet groups, cluster snapshot metadata, scheduled action metadata, Serverless namespaces, Serverless workgroups, and reported VPC/subnet/security-group/KMS/IAM/snapshot/scheduled-action/namespace-workgroup relationships. Provisioned and Serverless share `service_kind=redshift`; resource types distinguish the two surfaces. |
| `dynamodb`, `cloudwatchlogs` | Table or log-group metadata and KMS relationships. |
| `efs` | File system metadata (performance mode, throughput mode, encryption status, lifecycle policy transition summary), access points, mount targets, and replication configurations with mount-target-to-subnet, mount-target-to-security-group, file-system-to-KMS-key, access-point-to-file-system, and replication-to-target-file-system relationships. No NFS file system policy bodies and no file contents. |
| `cloudwatch` | Metric alarms, composite alarms, dashboards (name + last modified only), Contributor Insights rules (name + state only), and metric streams with alarm-to-SNS-topic, composite-alarm-to-child-alarm, metric-stream-to-Firehose, and alarm-to-metric (dimension summary) relationships. No dashboard body JSON, no Contributor Insights rule definitions, no metric data points. Customer-tag-named alarm dimensions are routed through the shared redact library. |
| `cloudfront` | Distribution metadata plus ACM certificate and WAF web ACL relationships. |
| `acm` | Public ACM certificate metadata (ARN, domain name, SANs, status, type, issuer, validity, key and signature algorithms) and certificate-to-using-resource relationships derived from ACM-reported in-use-by ARNs (ELB v2, CloudFront, API Gateway, AppSync, App Runner, and other ARN-shaped targets). No certificate body PEM, no private key material, no `GetCertificate` calls, no `ExportCertificate` calls; ACM Private CA is out of scope. |
| `cloudtrail` | Trail (multi-region and per-region), Lake event data store, channel, and Lake dashboard configuration metadata with trail-to-S3-bucket, trail-to-CloudWatch-Logs, trail-to-KMS-key, trail-to-SNS-topic, and event-data-store-to-KMS-key relationships. Event selectors are summarized as counts only; CloudTrail event payloads, Lake query strings, Lake query results, and dashboard widget query SQL are never read or persisted. |
| `cloudformation` | Stack (active and recently deleted), stack set, change set (metadata only), drift detection result (status + per-status counts), stack-set instance, and registered extension type metadata with stack-to-resource-type (from ListStackResources), stack-set-to-instance, stack-to-IAM-role, and stack-to-S3-template-URL relationships. Highest template-body redaction surface in the collector: template bodies (`GetTemplate`/`GetTemplateSummary`), parameter values (including NoEcho and SSM-resolved values, only keys are kept), change-set bodies (`DescribeChangeSet`), stack policies (`GetStackPolicy`), and drift property documents are never read or persisted. Secret-like stack output values are routed through the shared redact library by output key; the stack-set `TemplateBody` is never carried. Requires `ESHU_AWS_REDACTION_KEY`. |
| `apigateway` | REST, HTTP, WebSocket, stage, custom-domain, mapping, access-log, ACM, and integration metadata. |
| `secretsmanager`, `ssm` | Secret or parameter metadata with KMS relationships; no secret/parameter values. |
| `athena` | Workgroup, data catalog, prepared-statement, and named-query metadata plus workgroup-to-S3-result-bucket, workgroup-to-KMS-key, prepared-statement-to-workgroup, and named-query-to-workgroup relationships. No SQL bodies, query results, query result location object contents, or query history strings. |
| `securityhub` | Hub configuration, enabled standards, controls, member accounts, action targets, insight summaries, and aggregate finding counts; no finding bodies or insight filters. |
| `glue` | Data Catalog database, table, crawler, job, trigger, workflow, and connection metadata plus table-in-database, table-to-S3-location, crawler-to-database, crawler-to-IAM-role, job-to-IAM-role, and trigger-to-job relationships. No script bodies, default-argument values, connection passwords, JDBC credential URLs, workflow graphs, table column sample statistics, or classifier custom patterns. |
| `elasticache` | Cache clusters, replication groups, parameter and subnet groups, users, user groups, and snapshot metadata (name/source/status only); cluster-to-VPC, cluster-to-subnet, cluster-to-KMS, replication-group-to-cluster, and user-group-to-user relationships. No AUTH tokens, user passwords, user access strings, cache contents, or snapshot data. |
| `msk` | MSK cluster, broker configuration, and replicator metadata with subnet, security-group, KMS-key, IAM-role, and configuration relationships; no broker `server.properties` bodies, broker logs, bootstrap broker endpoints, SCRAM secrets, or Kafka topic data. |
| `mq` | Amazon MQ broker and broker-configuration metadata for ActiveMQ and RabbitMQ engines (name, engine type/version, deployment mode, instance type, status, security-group refs, encryption options, broker usernames) with broker-to-subnet, broker-to-security-group, broker-to-KMS-key (customer-managed only), broker-to-configuration, and broker-to-CloudWatch-log-group relationships; no broker user passwords, configuration XML bodies, or queue/topic message contents. |
| `stepfunctions` | State machine and activity metadata, execution-role relationships, and ARN-only Task-target relationships; no execution payloads, history events, task tokens, or definition literals. |
| `backup` | Backup vault, backup plan, backup selection, recovery point (metadata only - id, source resource ARN, vault, status, creation/expiration), report plan, restore testing plan, and framework metadata with plan-to-selection, selection-to-resource, selection-to-IAM-role, vault-to-KMS-key, recovery-point-to-vault, recovery-point-to-source-resource, and framework-to-control relationships. No recovery point contents, vault access policy bodies, or framework control input parameter values. |
| `accessanalyzer` | Analyzer metadata, archive-rule names, aggregate finding counts, relationships, and unused-access summaries. |
| `kms` | Customer master keys, aliases, and grants with alias-to-key, grant-to-key, and grant-to-grantee-principal relationships. The scanner never calls cryptographic operations (Encrypt, Decrypt, GenerateDataKey, Sign, Verify, ReEncrypt, GenerateMac, VerifyMac, GenerateDataKeyPair, GenerateDataKeyWithoutPlaintext, DeriveSharedSecret, GetPublicKey) or key lifecycle mutations (CreateKey, ScheduleKeyDeletion, EnableKey, DisableKey, EnableKeyRotation, DisableKeyRotation, PutKeyPolicy, CreateGrant, RevokeGrant, RetireGrant, ReplicateKey, ImportKeyMaterial, DeleteImportedKeyMaterial). Key policy Statement bodies, grant encryption contexts, and key material stay outside the scan slice. |
| `organizations` | Organization root, OUs, accounts, policy summaries, policy target bindings, and delegated administrators. |
| `ssoadmin` | IAM Identity Center instances, permission sets (name, description, session duration, relay state), account assignments (principal type/id, permission set, account), application instances, trusted token issuers, and group/user principals resolved from the connected identity store. Relationships cover assignment-to-permission-set, assignment-to-account, assignment-to-principal, permission-set-to-managed-policy (ARN ref), and permission-set-to-customer-managed-policy (name ref). No permission set inline policy bodies, permissions boundary bodies, customer-managed policy bodies, or application access-scope group filters. Principal display names are redacted; only the identity store display name is read. |
| `sagemaker` | Notebook instances, models, endpoints, endpoint configs, training jobs, processing jobs, transform jobs, hyperparameter tuning jobs, projects, pipelines, feature groups (metadata only), Studio domains, user profiles, apps, and inference components, with model-to-S3-artifact, model-to-container-image-URI, model-to-IAM-role, endpoint-to-endpoint-config, endpoint-config-to-model, training-job-to-IAM-role, notebook-to-subnet, domain-to-VPC, and user-profile-to-domain relationships. Metadata only: the scanner never invokes endpoints (InvokeEndpoint / InvokeEndpointAsync) and never persists hyperparameter values, training/processing/transform input or output data references, notebook lifecycle-config script bodies, container environment maps, or pipeline definition bodies. |

IAM, Route 53, and CloudFront are global-style families. Use a concrete global
region label such as `aws-global` so claims keep the
`(account_id, region, service_kind)` shape. Organizations and Identity Center
(`ssoadmin`) use the `us-east-1` control-plane endpoint and require
management-account or delegated-administrator credentials.

## Data Boundaries

The collector does not read S3 object contents, SQS messages, DynamoDB table
data, EFS file contents or NFS file system policy bodies, RDS database
contents, DocumentDB database document contents, DocumentDB collections,
DocumentDB indexes, DocumentDB cluster parameter values, DocumentDB master
user passwords or secrets, DocumentDB snapshot
contents, Redshift warehouse queries, Redshift table data,
Redshift snapshot contents, Redshift master user passwords or admin passwords,
ElastiCache cache keys, cache values, AUTH tokens, user passwords, user access
strings, or snapshot data, CloudWatch log events, CloudWatch metric data
points, CloudWatch dashboard body JSON, CloudWatch Contributor Insights rule
definitions, Secrets Manager secret
values, SSM parameter values, API Gateway execution payloads, Lambda code
packages, CloudFront origin payloads, private keys, raw SNS endpoints, raw
EventBridge target inputs, Athena query result rows, Athena named-query SQL
bodies, Athena prepared-statement query bodies, Athena query history strings,
Glue job script bodies, Glue default-argument values, Glue connection
passwords or JDBC credential URLs, Glue workflow graph payloads, Glue table
column statistics with sample values, Glue classifier custom patterns, MSK
Kafka topic or message data, MSK broker logs, MSK broker `server.properties`
bodies, MSK configuration revision bodies, MSK bootstrap broker endpoints, MSK
SCRAM secret material, Amazon MQ broker user passwords, Amazon MQ
configuration XML bodies, Amazon MQ queue or topic message contents, Step
Functions execution input or output, Step
Functions execution history events, Step Functions activity task tokens, AWS
Backup recovery point contents, AWS Backup vault access policy bodies, AWS
Backup framework control input parameter values, AWS Backup recovery-point
restore metadata values, or IAM/resource policy JSON unless a service
package explicitly documents a sanitized metadata-only exception. Step Functions state machine definitions
are persisted only as state names, state types, structural transitions, and
Task Resource ARNs; Parameters, ResultPath, ResultSelector, InputPath,
OutputPath, and Result literal contents are excluded.
GuardDuty finding bodies, GuardDuty filter criteria, GuardDuty threat intel set
list contents, and GuardDuty IP set list contents are also out of scope.
CloudTrail audit event payloads, Lake query strings, Lake query result rows,
event selector bodies, and dashboard widget query SQL stay outside the
collector contract; the CloudTrail scanner emits trail and Lake configuration
only, summarizing selectors as bounded counts.

Transit gateway routes, transit gateway multicast group memberships, and
transit gateway policy table rule bodies are out of scope. The Transit Gateway
scanner reads transit gateway, route table, attachment, peering attachment,
multicast domain, and policy table identity, ownership, state, and bounded
option metadata only; it never calls `SearchTransitGatewayRoutes` or
`GetTransitGatewayPolicyTableEntries`, and never mutates transit gateway
resources or changes route-table association or propagation. Cross-account peer
transit gateway identities are surfaced exactly as AWS reports them and flagged
for org-context resolution; the scanner does not resolve the remote account.

CloudFormation is the highest template-body redaction surface in the collector.
Stack and stack-set template bodies, parameter values (including NoEcho and
SSM-resolved values), change-set bodies, stack policies, and drift property
documents are out of scope. The scanner never calls `GetTemplate`,
`GetTemplateSummary`, `DescribeChangeSet`, `GetStackPolicy`, or any
`Detect*Drift` or mutation API; the accepted SDK surface excludes them by
construction, proven by reflective guard tests on both the scanner-owned
`Client` interface and the SDK adapter `apiClient` interface. Parameter mapping
keeps keys only, the stack-set `TemplateBody` is never carried, drift results
are reduced to per-status counts, and secret-like stack output values are
redacted by output key through the shared redact library. Stack name, ARN,
status, capabilities, role ARN, tags, and timestamps are persisted as
metadata.

ACM certificate body PEM and ACM-issued private key material are out of scope.
The ACM scanner never calls `GetCertificate` or `ExportCertificate`, and ACM
Private CA (acm-pca) APIs are not exercised.

KMS key policy Statement bodies, KMS grant encryption contexts, KMS key
material, and the output of any KMS cryptographic operation are out of scope.
The KMS scanner reaches the control plane only through List- and
Describe-class APIs; it never calls Encrypt, Decrypt, GenerateDataKey,
GenerateDataKeyPair, GenerateDataKeyPairWithoutPlaintext,
GenerateDataKeyWithoutPlaintext, Sign, Verify, ReEncrypt, GenerateMac,
VerifyMac, DeriveSharedSecret, GetPublicKey, GenerateRandom, or any key
lifecycle mutation. Only the bounded list of policy revision names from
ListKeyPolicies is persisted; the scanner does not call GetKeyPolicy.

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

IAM Identity Center (`ssoadmin`) permission set metadata is in scope: name,
description, session duration, relay state, and managed-policy ARN or
customer-managed-policy name references. Permission set inline policy bodies
(`GetInlinePolicyForPermissionSet`) and permissions boundary bodies
(`GetPermissionsBoundaryForPermissionSet`) are out of scope by default; they
encode the org least-privilege model and live in IAM. Customer-managed policy
bodies are never read; only the attachment name and path are persisted.
Application instance access-scope attributes (`GetApplicationAccessScope`,
`ListApplicationAccessScopes`) are out of scope because they can carry
sensitive group filters. Principal display names resolved from the connected
identity store pass through the shared AWS redaction path; only the identity
store display name is read, never addresses, emails, phone numbers, structured
name, or group memberships.

It also does not call AWS mutation APIs. If a scanner needs a new API family,
update the owning service package README with source APIs, forbidden data
classes, emitted evidence, and verification.

Access Analyzer has an extra security boundary: external finding bodies,
archive-rule filter criteria, policy-generation results, and per-action
unused-access details are not persisted. The scanner keeps aggregate finding
counts by status and resource type, plus per-resource unused-access
last-accessed timestamps.

SageMaker is a metadata-only control-plane scan. The scanner never invokes
endpoints (it never calls InvokeEndpoint or InvokeEndpointAsync, which live in
the separate `sagemakerruntime` API the collector does not link) and never
calls a mutation API. It never persists hyperparameter values (training or
tuning), training/processing/transform input or output data references,
notebook lifecycle-config script bodies, container environment maps, or
pipeline definition bodies. These exclusions are enforced by omission from the
scanner-owned types and by a reflection gate over the SDK adapter that fails
the build if a forbidden method ever becomes reachable.

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
