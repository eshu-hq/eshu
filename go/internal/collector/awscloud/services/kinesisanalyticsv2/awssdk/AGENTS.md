# AGENTS.md - internal/collector/awscloud/services/kinesisanalyticsv2/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Managed Flink SDK pagination, describe/snapshot/tag reads, and
   telemetry.
3. `mapping.go` - safe ApplicationDetail-to-Application metadata mapping and the
   log-stream-to-log-group ARN derivation.
4. `exclusion_test.go` - the build-time gate that fails if a mutation method
   reaches the adapter interface.
5. `../scanner.go` - scanner-owned Managed Flink fact selection.
6. `../README.md` - Managed Flink scanner contract.
7. `../../../README.md` - AWS cloud envelope contract.
8. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Managed Flink SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Keep the `apiClient` interface limited to the `List*`/`Describe*` reads the
  scanner needs. The exclusion test fails the build if any method matches a
  mutation prefix or a code/record-read name; do not loosen it.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe application metadata, snapshot names/status, and resource
  tags. Never copy code bodies, SQL text, environment property values,
  run-configuration content, or the verbose job plan.
- Call `DescribeApplication` without `IncludeAdditionalDetails`.
- Derive the log group ARN from the reported log stream ARN with
  `logGroupARNFromLogStreamARN`; do not emit a raw log stream ARN as an edge
  target.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Managed Flink metadata read by extending `Client` and the
  `apiClient` interface with another `List*`/`Describe*` read, writing a scanner
  or adapter test first, then mapping the SDK response into scanner-owned types.
  The exclusion test rejects any mutation addition.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does not
  reveal code, SQL, environment property, or record content.

## What Not To Change Without An ADR

- Do not read or persist application code, SQL, environment property values,
  run-configuration content, or records, and do not call any mutation API.
- Do not request the verbose job plan.
- Do not infer workload, environment, deployment, or ownership truth from
  Managed Flink names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
