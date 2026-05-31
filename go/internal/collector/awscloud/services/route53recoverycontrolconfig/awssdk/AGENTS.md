# AGENTS.md - internal/collector/awscloud/services/route53recoverycontrolconfig/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - recovery-control SDK pagination, tag reads, and telemetry.
3. `mappers.go` - safety-rule union mapping and cluster endpoint Region mapping.
4. `exclusion_test.go` - the build-time gate that fails if a mutation or
   routing-control-state method reaches the adapter interface.
5. `../scanner.go` - scanner-owned recovery-control fact selection.
6. `../README.md` - recovery-control scanner contract.
7. `../../../README.md` - AWS cloud envelope contract.
8. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep recovery-control SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Keep the `apiClient` interface limited to `List*` reads. The exclusion test
  fails the build if any method is not a `List` read or matches a mutation/
  routing-control-state name; do not loosen it.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and result.
- Persist only safe cluster, control panel, routing control, and safety rule
  metadata plus resource tags. Never read or persist routing control state.
- Drop cluster endpoint URLs; keep only endpoint Region names.
- Map a `ListSafetyRules` entry by its assertion-or-gating union; skip an entry
  that carries neither shape.
- Do not import the route53recoverycluster data-plane module.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new recovery-control metadata read by extending `Client` and the
  `apiClient` interface with another `List*` read, writing a scanner or adapter
  test first, then mapping the SDK response into scanner-owned types. The
  exclusion test rejects any non-`List` addition.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does not
  reveal routing control state.

## What Not To Change Without An ADR

- Do not read or set routing control state, or call any recovery-control
  configuration mutation API.
- Do not import or wire the route53recoverycluster data-plane module.
- Do not infer workload, environment, deployment, or ownership truth from
  recovery-control names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
