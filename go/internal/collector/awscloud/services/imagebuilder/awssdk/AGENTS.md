# AGENTS.md - internal/collector/awscloud/services/imagebuilder/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - the `apiClient` read interface and `Snapshot` orchestration.
3. `list.go` - pagination and per-resource get reads.
4. `map.go` - safe metadata mapping into scanner-owned types.
5. `telemetry.go` - the `recordAPICall` wrapper and throttle classifier.
6. `exclusion_test.go` - the build-time gate that fails if a body-read, run, or
   mutation method reaches the adapter interface.
7. `../scanner.go` - scanner-owned Image Builder fact selection.
8. `../README.md` - Image Builder scanner contract.
9. `../../../README.md` - AWS cloud envelope contract.
10. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
    service coverage and runtime requirements.

## Invariants

- Keep Image Builder SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Keep the `apiClient` interface limited to the `List*` enumerations and the
  matching `Get*` control-plane reads. The exclusion test fails the build if any
  method is not a `List`/`Get` read or matches a body-read, run, or mutation
  name; do not loosen it.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and result.
- Persist only safe pipeline, recipe, container recipe, infrastructure
  configuration, and distribution configuration metadata. Never read or persist
  component build-document bodies, Dockerfile bodies, instance user data, EC2
  key pair names, scan findings, or build artifacts.
- List recipes and container recipes with `Owner = Self` so the scan stays
  scoped to account-owned resources.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Image Builder metadata read by extending `Client` and the
  `apiClient` interface with another `List*` or `Get*` read, writing a scanner
  or adapter test first, then mapping the SDK response into scanner-owned types.
  The exclusion test rejects any body-read, run, or mutation addition.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does not
  reveal a component body, Dockerfile body, user data, scan finding, or build
  artifact.

## What Not To Change Without An ADR

- Do not read component bodies, Dockerfile bodies, user data, scan findings, or
  build artifacts; do not call any Image Builder mutation or run-control API.
- Do not relax the `Owner = Self` recipe scoping without a coverage decision.
- Do not infer workload, environment, deployment, or ownership truth from Image
  Builder names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
