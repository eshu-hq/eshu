# AGENTS.md - internal/collector/awscloud/services/s3/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - S3 SDK pagination, safe metadata mapping, and telemetry.
3. `../scanner.go` - scanner-owned S3 fact selection.
4. `../README.md` - S3 scanner contract.
5. `../../../README.md` - AWS cloud envelope contract.
6. `docs/docs/adrs/2026-04-20-aws-cloud-scanner-collector.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep S3 SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Wrap each AWS list page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe bucket control-plane metadata.
- Do not persist object keys, object inventory, bucket policy JSON, ACL grants,
  target grants, replication rules, lifecycle rules, notification
  configuration, inventory configuration, analytics configuration, or metrics
  configuration.
- Do not call object APIs or mutation APIs.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new S3 metadata read by extending `Client`, writing a scanner or adapter
  test first, then mapping the SDK response into scanner-owned types.
- Add a new optional-not-configured error code only after AWS or Smithy evidence
  shows the code represents absent bucket configuration.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.

## What Not To Change Without An ADR

- Do not read objects, object versions, bucket policy JSON, ACL grants, lifecycle
  rules, replication rules, notification configuration, or inventory data.
- Do not infer workload, environment, deployment, or ownership truth from bucket
  names, tags, website configuration, or logging targets.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
