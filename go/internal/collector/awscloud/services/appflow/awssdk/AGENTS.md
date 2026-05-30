# AGENTS.md - internal/collector/awscloud/services/appflow/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - AppFlow SDK pagination, safe metadata mapping, and telemetry.
3. `../scanner.go` - scanner-owned AppFlow fact selection.
4. `../README.md` - AppFlow scanner contract.
5. `../../../README.md` - AWS cloud envelope contract.
6. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep AppFlow SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe flow and connector profile metadata.
- Never read `DescribeFlow` Tasks (field mappings). Copy only connector types,
  connector profile names, S3 bucket references, customer KMS key ARN, trigger
  type, and timestamps.
- Never read flow run records (`DescribeFlowExecutionRecords`).
- Forward only the connector profile's Secrets Manager credentials ARN. Never
  read or forward credential values or OAuth tokens.
- Do not call any AppFlow mutation API (`Create*`, `Update*`, `Delete*`),
  `StartFlow`, or `StopFlow`; the `apiClient` interface excludes them.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new AppFlow metadata read by extending `Client`, writing a scanner or
  adapter test first, then mapping the SDK response into scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does not
  reveal transferred data values, credentials, or tokens.

## What Not To Change Without An ADR

- Do not run, start, stop, or mutate flows or connector profiles, or call any
  AppFlow mutation API.
- Do not read `DescribeFlow` Tasks (field mappings) or flow run records.
- Do not read or forward connector credentials or OAuth tokens.
- Do not infer workload, environment, deployment, or ownership truth from
  AppFlow names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
