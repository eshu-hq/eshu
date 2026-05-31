# AGENTS.md - internal/collector/awscloud/services/servicequotas/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Service Quotas SDK pagination, applied-vs-default join, safe
   metadata mapping, and telemetry.
3. `exclusion_test.go` - the build-time gate that fails if a mutation, request,
   or change-history method reaches the adapter interface.
4. `../scanner.go` - scanner-owned Service Quotas fact selection.
5. `../README.md` - Service Quotas scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Service Quotas SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Keep the `apiClient` interface limited to `List*` reads. The exclusion test
  fails the build if any method is not a `List` read or matches a mutation,
  request, or change-history name; do not loosen it.
- Wrap each AWS paginator page in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe quota metadata. Never read a metric sample value, never read
  quota-change request history, and never request, modify, or delete a quota.
- Compute the override flag by joining the applied quota value against the
  AWS-published default by quota code; never invent a default.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Service Quotas metadata read by extending `Client` and the
  `apiClient` interface with another `List*` read, writing a scanner or adapter
  test first, then mapping the SDK response into scanner-owned types. The
  exclusion test rejects any non-`List` addition.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend quota mapping only for AWS source data that is metadata and does not
  reveal usage-sample or quota-change request content.

## What Not To Change Without An ADR

- Do not request, modify, or delete quotas, or touch quota-increase templates or
  request history.
- Do not read a CloudWatch metric sample value through this adapter.
- Do not infer workload, environment, deployment, or ownership truth from quota
  names or values.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
