# AGENTS.md - internal/collector/awscloud/services/timestream/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Timestream SDK pagination, safe metadata mapping, and
   telemetry.
3. `exclusion_test.go` - the build-time gate that fails if a record-read or
   mutation method reaches the adapter interface.
4. `../scanner.go` - scanner-owned Timestream fact selection.
5. `../README.md` - Timestream scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Timestream SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Keep the `apiClient` interface limited to `List*` reads. The exclusion test
  fails the build if any method is not a `List` read or matches a record/
  mutation name; do not loosen it.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe database and table metadata plus resource tags. Never read
  or persist record, measure, or query-result content.
- Copy only the magnetic-store rejected-data report S3 location configuration,
  never the rejected records.
- Do not import the timestream-query module.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Timestream metadata read by extending `Client` and the `apiClient`
  interface with another `List*` read, writing a scanner or adapter test first,
  then mapping the SDK response into scanner-owned types. The exclusion test
  rejects any non-`List` addition.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy
  evidence shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does
  not reveal record, measure, or query-result content.

## What Not To Change Without An ADR

- Do not read records or measures, run queries, write records, run batch loads,
  mutate databases, mutate tables, or call any Timestream mutation API.
- Do not import or wire the timestream-query module.
- Do not disable endpoint discovery on the Timestream-write client.
- Do not infer workload, environment, deployment, or ownership truth from
  Timestream names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
