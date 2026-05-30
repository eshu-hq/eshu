# AGENTS.md - internal/collector/awscloud/services/firehose guidance

## Read First

1. `README.md` - package purpose, exported surface, redaction policy, and
   invariants.
2. `types.go` - scanner-owned Firehose domain types.
3. `scanner.go` - delivery stream resource emission and destination-kind
   summary.
4. `relationships.go` - destination, source, role, KMS, log-group, and
   transform relationship emission rules.
5. `helpers.go` - identity, ARN, clone, and dedupe helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Firehose API access behind `Client`; do not import the AWS SDK into this
  package.
- Never read delivery records, mutate a delivery stream, toggle encryption, tag
  a stream, or wire any `Create*`, `Update*`, `Delete*`, `PutRecord*`,
  `Start/StopDeliveryStreamEncryption`, or `Tag/UntagDeliveryStream` API.
- Never persist destination access keys, Splunk HEC tokens, Redshift passwords,
  HTTP endpoint URLs/access keys, or processing-configuration Lambda bodies.
  Only the join-relevant target identities and the transform Lambda ARNs survive.
- Emit ARN-keyed edges (S3 bucket, OpenSearch domain, source Kinesis data
  stream, delivery IAM role, SSE KMS key, transform Lambda) only when AWS
  reports an ARN-shaped target identity, and use the reported ARN verbatim. Do
  not synthesize an ARN; a synthesized ARN must derive its partition from the
  boundary region, never hardcode `arn:aws:`.
- Key the Redshift edge by the cluster identifier the Redshift scanner
  publishes as its `Name` (parsed from the JDBC URL host), and the CloudWatch
  log group edge by the reported log group name. Do not set a fabricated
  `target_arn` on a name-keyed edge.
- Emit the KMS SSE edge only for a customer-managed key; AWS-owned keys produce
  no edge.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from delivery stream,
  destination, or role names, or AWS tags.
- Preserve stable delivery stream identities across repeated observations in the
  same AWS generation.
- Keep Firehose ARNs, names, JDBC URLs, endpoint URLs, tags, and AWS error
  payloads out of metric labels.

## Common Changes

- Add a new Firehose metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through the
  `awscloud` envelope builders. If the field can carry credential or
  record-payload material, leave it out of the scanner contract.
- Add new relationship evidence only when the Firehose API reports both sides
  directly and the target identity is not sensitive (an ARN, a catalog-stable
  name, or a cluster identifier that joins an existing scanner's `resource_id`).
- Extend SDK pagination and the describe fan-out in the `awssdk` adapter, not
  here.

## What Not To Change Without An ADR

- Do not put records, mutate streams, toggle encryption, tag streams, or call
  any Firehose mutation API.
- Do not persist Firehose tags into facts beyond the raw AWS tag map already
  carried on the resource; do not infer ownership from them.
- Do not resolve Firehose names, destinations, or tags into workload ownership
  here; correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
