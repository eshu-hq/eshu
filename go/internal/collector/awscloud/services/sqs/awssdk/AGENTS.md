# AGENTS.md - internal/collector/awscloud/services/sqs/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - SQS SDK pagination, safe attribute selection, mapping, and
   telemetry.
3. `../scanner.go` - scanner-owned SQS fact selection.
4. `../README.md` - SQS scanner contract.
5. `../../../README.md` - AWS cloud envelope contract.
6. `docs/public/services/collector-aws-cloud-scanners.md` - scanner coverage and metadata-only data boundaries.

## Invariants

- Keep SQS SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Request only safe queue metadata attributes. Do not request `Policy`.
- Do not call message-content APIs such as `ReceiveMessage`.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new SQS metadata read by extending `sqs.Client`, writing a scanner or
  adapter test first, then mapping the SDK response into scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend queue mapping only for AWS source data that is metadata and does not
  reveal message contents or queue policy JSON.

## What Not To Change Without Architecture-Owner Approval

- Do not read, delete, purge, or mutate SQS messages.
- Do not infer workload, environment, deployment, or ownership truth from queue
  names, tags, or DLQ links.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
