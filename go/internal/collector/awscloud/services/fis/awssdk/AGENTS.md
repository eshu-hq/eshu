# AGENTS.md - internal/collector/awscloud/services/fis/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - FIS SDK pagination, per-template detail reads, and telemetry.
3. `mapping.go` - safe metadata mapping from SDK types into scanner-owned types.
4. `exclusion_test.go` - the build-time gate that fails if an experiment-run
   read or mutation method reaches the adapter interface.
5. `../scanner.go` - scanner-owned FIS fact selection.
6. `../README.md` - FIS scanner contract.
7. `../../../README.md` - AWS cloud envelope contract.
8. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep FIS SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Keep the `apiClient` interface limited to ListExperimentTemplates,
  GetExperimentTemplate, and ListTagsForResource. The exclusion test fails the
  build if any other method (experiment-run read or mutation) appears; do not
  loosen it.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe template metadata plus resource tags. Never read or persist
  action parameter values, target filter values, target-tag selectors, or
  experiment run output.
- Copy only the S3 log destination bucket name and prefix, never log object
  contents.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new FIS metadata read only if it is a template-metadata read; the
  exclusion test rejects experiment-run reads and mutations. Write a scanner or
  adapter test first, then map the SDK response into scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does not
  reveal parameters, filters, tag selectors, or run output.

## What Not To Change Without An ADR

- Do not start or stop experiments, read experiment runs, or call any FIS
  mutation API.
- Do not persist action parameters, target filters, or target tags.
- Do not infer workload, environment, deployment, or ownership truth from FIS
  names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
