# AGENTS.md - internal/collector/awscloud/services/databrew/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - DataBrew SDK pagination and telemetry.
3. `mapping.go` - safe SDK-type to scanner-type mapping.
4. `exclusion_test.go` - the build-time gate that fails if a Describe/detail or
   mutation method reaches the adapter interface.
5. `../scanner.go` - scanner-owned DataBrew fact selection.
6. `../README.md` - DataBrew scanner contract.
7. `../../../README.md` - AWS cloud envelope contract.
8. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep DataBrew SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Keep the `apiClient` interface limited to the four `List*` reads. The
  exclusion test fails the build if any method is not a `List` read or matches a
  Describe/detail or mutation name; do not loosen it.
- Wrap each AWS paginator page in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- The recipe mapper records only the step COUNT. Never copy recipe step actions,
  operations, or parameters into scanner-owned types.
- The dataset mapper never copies the custom SQL `QueryString` a database input
  carries, dataset-parameter values, or path-option expressions.
- The job mapper copies only output bucket NAMES, the role ARN, the encryption
  mode, and dataset/recipe references; never output object data or sample rows.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new DataBrew metadata read by extending `Client` and the `apiClient`
  interface with another `List*` read, writing a scanner or adapter test first,
  then mapping the SDK response into scanner-owned types. The exclusion test
  rejects any non-`List` addition.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does not
  reveal a recipe step expression, custom SQL, parameter value, or sample data.

## What Not To Change Without An ADR

- Do not call any DataBrew Describe or mutation API, read recipe steps, read
  custom SQL, or read sample data.
- Do not infer workload, environment, deployment, or ownership truth from
  DataBrew names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
