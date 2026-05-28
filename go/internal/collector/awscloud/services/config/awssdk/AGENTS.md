# AGENTS.md - internal/collector/awscloud/services/config/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Config SDK pagination and safe metadata mapping.
3. `telemetry.go` - per-call span and counter wiring.
4. `../scanner.go` - scanner-owned Config fact selection.
5. `../README.md` - Config scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud.md` - AWS collector runtime
   requirements.

## Invariants

- Keep AWS Config SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Keep the `apiClient` interface limited to the eight Describe operations the
  scanner needs. The reflection gate in `client_test.go` fails on any forbidden
  method, including substring matches for Put/Delete/Start/Stop and config-item
  reads.
- Persist only safe Config control-plane metadata. Never read recorded
  configuration item bodies via GetResourceConfigHistory,
  BatchGetResourceConfig, GetDiscoveredResourceCounts, or discovered-resource
  listings.
- Never read per-resource compliance detail
  (GetComplianceDetailsByConfigRule, GetComplianceDetailsByResource) or
  custom-rule Lambda code (GetCustomRulePolicy,
  GetOrganizationCustomRulePolicy).
- Use DescribeConformancePackCompliance only to enumerate member-rule names.
  Read the rule name; do not propagate per-resource compliance results.
- Do not call any Config mutation API (Put, Delete, Start, Stop, Tag, Untag,
  Select, BatchPut...).
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Config metadata read by extending `Client` and the `apiClient`
  interface, writing a scanner or adapter test first, then mapping the SDK
  response into scanner-owned types. Update the reflection gate's forbidden list
  if the SDK adds a same-prefix mutation or config-item read.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.

## What Not To Change Without An ADR

- Do not read recorded configuration item bodies, per-resource compliance
  detail, custom-rule Lambda code, or stored query bodies, and do not call
  mutation APIs.
- Do not infer workload, environment, deployment, ownership, or compliance
  posture truth from Config metadata.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
