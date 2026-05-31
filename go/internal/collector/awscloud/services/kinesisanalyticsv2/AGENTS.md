# AGENTS.md - internal/collector/awscloud/services/kinesisanalyticsv2 guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned Managed Flink domain types.
3. `scanner.go` - application resource and relationship emission.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - resource_id derivation and scanner-side cloning helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Managed Flink API access behind `Client`; do not import the AWS SDK into
  this package.
- Never read or persist application code bodies, SQL text, environment property
  values, run-configuration content, or record payloads. Never call any
  `Create*`, `Update*`, `Delete*`, `Start*`, `Stop*`, `Add*`, or
  `Rollback*` mutation API.
- The application node publishes its resource_id as the application ARN
  (fallback to name). Source every application edge on that exact value.
- Key the Kinesis data stream and Firehose delivery stream edges on the reported
  stream ARN (the resource_id the kinesis/firehose scanners publish); set
  `target_arn` only for ARN-shaped identifiers.
- Key the S3 code-bucket edge on the reported bucket ARN; record only the bucket
  identity and object key, never the code body.
- Key subnet and security group edges on the bare `subnet-…`/`sg-…` id, the
  resource_id the EC2 scanner publishes.
- Key the IAM role edge on the reported service execution role ARN.
- Key the CloudWatch log group edge on the log GROUP ARN; the adapter derives it
  from the reported log STREAM ARN, so this package consumes a clean log group
  ARN.
- Do not add an MSK edge from environment property values: those values are
  never persisted, and the control-plane describe output exposes no structured
  MSK reference.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from application names or AWS
  tags.
- Keep Managed Flink resource ARNs, names, tags, and AWS error payloads out of
  metric labels.

## Common Changes

- Add a new Managed Flink metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. If the field can carry code, SQL, environment
  property values, or record content, leave it out of the scanner contract.
- Add new relationship evidence only when the Managed Flink API reports both
  sides directly and the target identity matches an existing scanner's published
  resource_id shape (ARN-equality for streams, buckets, and roles; bare id for
  subnets and security groups; log group ARN for CloudWatch).
- Extend SDK pagination and the log-stream-to-log-group ARN derivation in the
  `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read or persist application code, SQL, environment property values,
  run-configuration content, or records, and do not call any mutation API.
- Do not resolve Managed Flink names or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
