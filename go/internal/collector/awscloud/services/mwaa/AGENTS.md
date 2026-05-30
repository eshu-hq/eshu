# AGENTS.md - internal/collector/awscloud/services/mwaa guidance

## Read First

1. `README.md` - package purpose, exported surface, metadata-only policy, and
   invariants.
2. `types.go` - scanner-owned MWAA domain types.
3. `scanner.go` - environment resource and relationship emission.
4. `relationships.go` - relationship emission rules and edge source/target
   shapes.
5. `helpers.go` - partition derivation, S3 bucket ARN synthesis, log-group
   wildcard trim, and clone helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep MWAA API access behind `Client`; do not import the AWS SDK into this
  package.
- Never create, update, or delete an environment, request an Apache Airflow
  CLI token or web-login token, invoke the Airflow REST API, publish metrics,
  or wire any `Create*`, `Update*`, `Delete*`, token, or tagging API.
- Never persist Apache Airflow configuration option values, connection
  strings, the Celery executor queue ARN, the database VPC endpoint service,
  the webserver URL, or any value that smells like a credential. The
  scanner-owned `Environment` type must never grow a field that can hold one.
- Source every outgoing edge on the same identifier the environment resource
  publishes as its `resource_id` (the environment ARN, falling back to the
  bare name).
- Target each edge at the `resource_id` shape the target scanner publishes:
  the S3 bucket ARN (`aws_s3_bucket`), the bare subnet id (`aws_ec2_subnet`),
  the bare security group id (`aws_ec2_security_group`), the IAM role ARN
  (`aws_iam_role`), the KMS key reference (`aws_kms_key`), and the non-wildcard
  CloudWatch Logs log group ARN (`aws_cloudwatch_logs_log_group`).
- Derive the partition with `partition(boundary)` only when synthesizing an S3
  bucket ARN from a bare bucket name; when AWS reports a full ARN, use it
  verbatim so it inherits its own partition. Never hardcode `arn:aws:`.
- Trim the trailing `:*` wildcard suffix MWAA appends to a CloudWatch Logs log
  group ARN before emitting the edge, or it dangles against the
  `cloudwatchlogs` scanner's non-wildcard `resource_id`.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from environment names or
  AWS tags.
- Keep MWAA ARNs, names, tags, and AWS error payloads out of metric labels.

## Common Changes

- Add a new MWAA metadata field by extending the scanner-owned `Environment`
  type, writing a focused scanner or adapter test first, then mapping it
  through `awscloud` envelope builders. If the field can carry configuration,
  connection, or credential material, leave it out of the scanner contract
  until an ADR documents a sanitized exception.
- Add new relationship evidence only when the MWAA API reports both sides
  directly and the target identity matches an existing scanner's published
  `resource_id` shape.
- Extend SDK pagination and point reads in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not create, update, delete, or otherwise mutate an MWAA environment, and
  do not call any token, REST-API, metric-publishing, or tagging API.
- Do not read or persist Apache Airflow configuration option values,
  connection strings, executor queue ARNs, webserver URLs, or login tokens.
- Do not resolve MWAA names or tags into workload ownership here; correlation
  belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
