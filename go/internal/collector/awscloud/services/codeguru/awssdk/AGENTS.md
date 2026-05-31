# AGENTS.md - internal/collector/awscloud/services/codeguru/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Reviewer/Profiler client wiring, Snapshot, throttle
   classification, and telemetry.
3. `reviewer.go` - repository-association pagination and describe-only metadata
   mapping.
4. `profiler.go` - profiling-group pagination with inline descriptions and safe
   mapping.
5. `exclusion_test.go` - the build-time gate that fails if a findings/profiling
   read or a mutation method reaches either adapter interface.
6. `../scanner.go` - scanner-owned CodeGuru fact selection.
7. `../README.md` - CodeGuru scanner contract.
8. `../../../README.md` - AWS cloud envelope contract.

## Invariants

- Keep CodeGuru SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Keep the `reviewerAPIClient` and `profilerAPIClient` interfaces limited to
  `List*`/`Describe*` reads. The exclusion test fails the build if any method is
  not a `List`/`Describe` read or matches a findings, profiling-data, code-review,
  or mutation name; do not loosen it.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and result.
- Persist only safe association and profiling-group metadata plus resource tags.
  Never read or persist findings, recommendation content, profiling samples,
  aggregated profiles, flame graphs, or agent telemetry.
- Record only the metadata references from a describe (S3 bucket name,
  customer-managed KMS key id, encryption option), never analyzed source object
  keys or code bodies.
- Call `ListProfilingGroups` with `IncludeDescription=true`; do not add a
  per-group `DescribeProfilingGroup` round trip unless an inline field is
  missing.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new CodeGuru metadata read by extending the relevant adapter interface
  with another `List*`/`Describe*` read, writing a scanner or adapter test first,
  then mapping the SDK response into scanner-owned types. The exclusion test
  rejects any findings/profiling-data/mutation addition.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.

## What Not To Change Without An ADR

- Do not read findings, recommendations, code reviews, profiles, or frame
  metrics, or call any CodeGuru mutation API.
- Do not infer workload, environment, deployment, or ownership truth from
  CodeGuru names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
