# AWS Collector Scanner Coverage

This page lists the AWS service families backed by production scanner adapters
in `go/internal/collector/awscloud/awsruntime/registry.go`.

The collector is metadata-only. It emits reported facts for reducer admission.
It does not mutate AWS resources, read protected payloads, or write graph
truth.

## Supported Service Kinds

`allowed_services` may include these values:

| Service kind | Scanner coverage |
| --- | --- |
| `iam` | Roles, managed policies, instance profiles, and trust relationships. |
| `ecr` | Repositories, lifecycle policies, image references, and pagination checkpoints. |
| `ecs` | Clusters, services, tasks, relationships, and redacted task definitions. |
| `ec2` | VPC, subnet, security group, security-group rule, and ENI topology metadata. |
| `elbv2` | Load balancers, listeners, listener rules, target groups, and routing relationships. |
| `lambda` | Functions, aliases, event-source mappings, image URIs, execution roles, subnets, and security groups with redacted environment values. |
| `eks` | Clusters, node groups, add-ons, OIDC providers, IAM roles, and network join evidence. |
| `route53` | Hosted zones and A, AAAA, CNAME, and ALIAS DNS record facts. |
| `sqs` | Queue metadata and reported dead-letter queue relationships. |
| `sns` | Topic metadata and ARN-addressable subscription relationships. |
| `eventbridge` | Event buses, rules, rule-to-bus relationships, and ARN-addressable targets. |
| `s3` | Bucket metadata and server-access-log target bucket relationships. |
| `rds` | DB instances, DB clusters, subnet groups, and reported security group, KMS key, monitoring role, IAM role, parameter group, and option group relationships. |
| `dynamodb` | Table metadata and directly reported KMS key relationships. |
| `cloudwatchlogs` | Log group metadata and directly reported KMS key relationships. |
| `cloudfront` | Distribution metadata plus reported ACM certificate and WAF web ACL relationships. |
| `apigateway` | REST, HTTP, WebSocket, stage, custom-domain, mapping, access-log destination, ACM certificate, and ARN-addressable integration metadata. |
| `secretsmanager` | Secret metadata with KMS key and rotation Lambda relationships. |
| `ssm` | Parameter Store metadata with KMS key relationships. |

IAM, Route 53, and CloudFront are global-style scanner families. Use a concrete
global region label such as `aws-global` in target scopes and workflow claims
so the claim shape remains `(account_id, region, service_kind)`.

## Data Boundaries

The collector intentionally does not read S3 object contents, SQS messages,
DynamoDB table data, RDS database contents, CloudWatch log events, Secrets
Manager secret values, SSM parameter values, API Gateway execution payloads,
Lambda code packages, CloudFront origin payloads, private keys, raw SNS
endpoints, raw EventBridge target inputs, or IAM/resource policy JSON unless a
service package explicitly documents a sanitized metadata-only exception.

The collector also does not call AWS mutation APIs. If a scanner needs a new
API family, update the owning service package README with source APIs,
forbidden data classes, emitted evidence, and verification.

## Evidence Emitted

AWS scanner packages emit reported-confidence facts:

| Fact kind | Meaning |
| --- | --- |
| `aws_resource` | Source-reported AWS resource metadata. |
| `aws_relationship` | Source-reported relationship between AWS resources or external references. |
| `aws_image_reference` | ECR image digest and tag reference evidence. |
| `aws_dns_record` | Route 53 DNS record evidence. |
| `aws_warning` | Non-fatal scan condition, such as partial throttling or credential failure. |

Reducers must corroborate AWS facts before promoting workload, deployment,
ownership, drift, or unmanaged-resource truth.

## Status And Telemetry

Scanner runs record API call and throttle counts, budget exhaustion, pagination
checkpoint events, emitted fact counts, scan duration, scanner status, and
commit status. Runtime spans include:

- `aws.collector.claim.process`
- `aws.credentials.assume_role`
- `aws.service.scan`
- `aws.service.pagination.page`

For the full metric catalog, use
[Ingestion And Collector Metrics](../reference/telemetry/metrics-ingestion-collectors.md).

## Change Rules

When adding or widening a scanner, keep it metadata-only unless an active design
record says otherwise. Add scanner tests, SDK-adapter tests, command-side target
validation tests, and registry support through `awsruntime.SupportedServiceKinds`.
Run the performance evidence gate when the scanner adds pagination fanout,
claim concurrency, batch sizing, queue pressure, or downstream reducer work.

## Related Docs

- [AWS Cloud Collector](collector-aws-cloud.md)
- [AWS Collector Security And Config](collector-aws-cloud-security.md)
- [Collector Service Runtimes](../deployment/service-runtimes-collectors.md)
- [Collector Authoring](../guides/collector-authoring.md)
