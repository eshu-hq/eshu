# AWS Collector Scanner Coverage

Use this page for the AWS `service_kind` values backed by production scanner
adapters in `go/internal/collector/awscloud/awsruntime/registry.go`.

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
| `s3` | Bucket metadata and server-access-log target bucket relationships. |
| `rds` | DB instances, clusters, subnet groups, and reported security/KMS/role/group relationships. |
| `dynamodb`, `cloudwatchlogs` | Table or log-group metadata and KMS relationships. |
| `cloudfront` | Distribution metadata plus ACM certificate and WAF web ACL relationships. |
| `apigateway` | REST, HTTP, WebSocket, stage, custom-domain, mapping, access-log, ACM, and integration metadata. |
| `secretsmanager`, `ssm` | Secret or parameter metadata with KMS relationships; no secret/parameter values. |
| `athena` | Workgroup, data catalog, prepared-statement, and named-query metadata plus workgroup-to-S3-result-bucket, workgroup-to-KMS-key, prepared-statement-to-workgroup, and named-query-to-workgroup relationships. No SQL bodies, query results, query result location object contents, or query history strings. |
| `securityhub` | Hub configuration, enabled standards, controls, member accounts, action targets, insight summaries, and aggregate finding counts; no finding bodies or insight filters. |

IAM, Route 53, and CloudFront are global-style families. Use a concrete global
region label such as `aws-global` so claims keep the
`(account_id, region, service_kind)` shape.

## Data Boundaries

The collector does not read S3 object contents, SQS messages, DynamoDB table
data, RDS database contents, CloudWatch log events, Secrets Manager secret
values, SSM parameter values, API Gateway execution payloads, Lambda code
packages, CloudFront origin payloads, private keys, raw SNS endpoints, raw
EventBridge target inputs, Athena query result rows, Athena named-query SQL
bodies, Athena prepared-statement query bodies, Athena query history strings,
or IAM/resource policy JSON unless a service package explicitly documents a
sanitized metadata-only exception.

Security Hub finding aggregate counts are metadata-only when grouped by bounded
posture fields such as severity, standard, control, compliance status, and
workflow status. Security Hub finding bodies, resource IDs from findings,
resource details, remediation text, product fields, user-defined fields, note
text, network/process details, and insight filter expressions remain outside
the collector contract.

It also does not call AWS mutation APIs. If a scanner needs a new API family,
update the owning service package README with source APIs, forbidden data
classes, emitted evidence, and verification.

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
