# AGENTS.md - internal/collector/awscloud/services/lakeformation/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Lake Formation SDK pagination, safe metadata mapping, and
   telemetry.
3. `client_test.go` - the reflection guard forbidding mutation and
   credential-vending operations.
4. `../scanner.go` - scanner-owned Lake Formation fact selection.
5. `../README.md` - Lake Formation scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Lake Formation SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Keep the `apiClient` interface limited to `GetDataLakeSettings`,
  `ListResources`, and `ListPermissions`. The reflection guard test fails if a
  mutation or credential-vending method appears.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe settings, registered-resource, and permission metadata.
- Drop every permission condition (LF-Tag) expression, LF-Tag value, and
  `AdditionalDetails` payload. Forward only the principal identifier, the
  governed resource reference, and the bounded privilege enum names.
- Sort privilege enum names so the fact payload is deterministic across
  rescans.
- Do not call any Lake Formation mutation API, LF-Tag mutation, settings-put,
  or credential-vending / governed-data read.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Lake Formation metadata read by extending `Client`, writing a
  scanner or adapter test first, then mapping the SDK response into
  scanner-owned types. Add the corresponding `apiClient` method only for a
  read that exposes no policy body, condition expression, LF-Tag value, or
  credential.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy
  evidence shows the code is retry/throttle-shaped.

## What Not To Change Without An ADR

- Do not grant, revoke, register, deregister, put settings, mutate LF-Tags, or
  call any Lake Formation mutation API.
- Do not call any credential-vending or governed-data read
  (`GetTemporaryGlueTableCredentials`, `GetTemporaryGluePartitionCredentials`,
  `GetTemporaryDataLocationCredentials`, `GetTableObjects`, `GetWorkUnits`,
  `GetWorkUnitResults`, `StartQueryPlanning`).
- Do not propagate permission condition expressions, LF-Tag values, LF-Tag
  policy expressions, or `AdditionalDetails` RAM-share payloads.
- Do not infer workload, environment, deployment, or ownership truth from Lake
  Formation principal or resource names.
- Do not write facts, graph rows, or reducer-owned state here.
