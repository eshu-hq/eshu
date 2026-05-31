# AGENTS.md - internal/collector/awscloud/services/cleanrooms/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Clean Rooms SDK pagination, safe metadata mapping, and
   telemetry.
3. `exclusion_test.go` - the build-time gate that fails if a query/job/result or
   mutation method reaches the adapter interface.
4. `../scanner.go` - scanner-owned Clean Rooms fact selection.
5. `../README.md` - Clean Rooms scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Clean Rooms SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Keep the `apiClient` interface limited to `List*` and `Get*` reads. The
  exclusion test fails the build if any method is not a List/Get read or matches
  a query/job/result/mutation name; do not loosen it.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe collaboration, configured-table, and membership metadata
  plus resource tags. Record the allowed-column count only, never the column
  names. Never read or persist analysis-rule SQL, protected-query bodies or
  results, the Snowflake `SecretArn`, or any query/connection identifier.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Clean Rooms metadata read by extending `Client` and the `apiClient`
  interface with another `List*` or `Get*` read, writing a scanner or adapter
  test first, then mapping the SDK response into scanner-owned types. The
  exclusion test rejects any non-List/Get addition and any query/job/result
  surface.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy
  evidence shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does
  not reveal SQL, query results, allowed-column names, or secrets.

## What Not To Change Without An ADR

- Do not run protected queries or jobs, read protected-query results, read
  analysis-rule or analysis-template bodies, or call any Clean Rooms mutation
  API.
- Do not map allowed-column names, the Snowflake `SecretArn`, the Athena
  `OutputLocation`, or any query/connection identifier.
- Do not infer workload, environment, deployment, or ownership truth from Clean
  Rooms names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
