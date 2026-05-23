# AGENTS.md - internal/collector/awscloud/services/s3 guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned S3 domain types.
3. `scanner.go` - bucket and logging-target relationship emission.
4. `../../README.md` - shared AWS cloud observation and envelope contract.
5. `docs/docs/adrs/2026-04-20-aws-cloud-scanner-collector.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep S3 API access behind `Client`; do not import the AWS SDK into this
  package.
- Never read objects, list object keys, or mutate S3 buckets.
- Never persist bucket policy JSON, ACL grants, replication rules, lifecycle
  rules, notification configuration, inventory configuration, analytics
  configuration, or metrics configuration.
- Never persist website index or error document object keys.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, or deployable-unit truth from bucket names or tags.
- Preserve stable bucket identities across repeated observations in the same
  AWS generation.
- Keep bucket ARNs, bucket names, tags, prefixes, and KMS key IDs out of metric
  labels.

## Common Changes

- Add a new S3 metadata field by extending the scanner-owned type, writing a
  focused scanner or adapter test first, then mapping it through `awscloud`
  envelope builders.
- Add new relationship evidence only when S3 reports both sides directly and
  the target identity is not sensitive.
- Extend SDK pagination and optional-not-configured handling in the `awssdk`
  adapter, not here.

## What Not To Change Without An ADR

- Do not read objects, list object versions, list multipart uploads, mutate
  buckets, or mutate bucket configuration.
- Do not resolve bucket names, tags, website status, or logging targets into
  workload ownership here; correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
