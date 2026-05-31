# AGENTS.md - internal/collector/awscloud/services/resiliencehub/awssdk guidance

## Read First

1. `README.md` - accepted read surface, versioned reads, telemetry, invariants.
2. `client.go` - SDK interface, constructor, policy/tag reads, telemetry.
3. `client_app.go` - per-app enrichment (describe, assessments, version reads).
4. `client_versioned.go` - published-version input source/component/resource
   pagination and the ARN-only protected-resource filter.
5. `exclusion_test.go` - the build-time gate that fails if a mutation or
   assessment-result/recommendation read reaches the adapter interface.
6. `../README.md` - resiliencehub scanner contract.
7. `../../../README.md` - AWS cloud envelope contract.
8. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Resilience Hub SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Keep the `apiClient` interface limited to the accepted list/describe/tag
  reads. The exclusion test fails the build if any method matches a mutation,
  import, assessment-start, or assessment-result/drift/recommendation name; do
  not loosen it.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, result.
- Persist only safe app/policy/component/input-source/assessment metadata plus
  resource tags. Never read or persist assessment result bodies, drift detail,
  recommendation contents, or resolution payloads.
- Read the published `release` app version for version-scoped reads. Treat
  `ResourceNotFoundException` as a partial-scan warning, not a fatal error.
- Keep only ARN-identified physical resources in `mapProtectedResource`; drop
  Resilience Hub-native identifiers so the scanner never dangles an edge.
- Do not synthesize ARNs. Forward the ARNs AWS reports unchanged.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new metadata read by extending `Client` and the `apiClient` interface
  with another accepted read, writing a scanner or adapter test first, then
  mapping the SDK response into scanner-owned types. The exclusion test rejects
  any mutation/result-reader addition.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.

## What Not To Change Without An ADR

- Do not read assessment results, drift detail, recommendations, or resolution
  payloads, and do not call any mutation, import, or assessment-start API.
- Do not synthesize ARNs or rewrite reported partitions.
- Do not infer workload, environment, deployment, or ownership truth from
  Resilience Hub names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
