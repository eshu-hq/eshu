# AGENTS.md - internal/collector/awscloud/services/proton/awssdk guidance

## Read First

1. `README.md` - package purpose, read surface, telemetry, and invariants.
2. `client.go` - the `apiClient` interface, Snapshot orchestration, tag reads,
   and the `recordAPICall` telemetry wrapper.
3. `mappers.go` - per-type list pagination and safe metadata mapping.
4. `exclusion_test.go` - the build-time gate that fails if a mutation or
   body/output reader reaches the adapter interface.
5. `../scanner.go` - scanner-owned Proton fact selection.
6. `../README.md` - Proton scanner contract.
7. `../../../README.md` - AWS cloud envelope contract.
8. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Proton SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Keep the `apiClient` interface limited to the accepted reads (`List*` reads
  plus the single `GetService` detail read). The exclusion test fails the build
  if any method is a mutation, a sync/config/output/provisioned-resource reader,
  or anything other than a `List` read or `GetService`; do not loosen it.
- Never map the `GetService` `Service.Spec` or `Pipeline.Spec` body, a template
  version schema body, or any deployment input parameter value. Map only
  reference fields (template name, status, repository id/branch/connection ARN,
  role ARN, provisioning mode, versions, timestamps, tags).
- From `ListServiceInstances`, keep only the service-name/environment-name join
  keys.
- Wrap each AWS paginator page and the per-service detail read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and result.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Proton metadata read by extending `Client` and the `apiClient`
  interface with another accepted read, writing a scanner or adapter test first,
  then mapping the SDK response into scanner-owned types. The exclusion test
  rejects any forbidden addition.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does not
  reveal a spec, schema, or input parameter body.

## What Not To Change Without An ADR

- Do not read spec/schema bodies, deployment outputs, provisioned resources, or
  service-instance input parameters, and do not call any Proton mutation API.
- Do not infer workload, environment, deployment, or ownership truth from Proton
  names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
