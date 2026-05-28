# AGENTS.md - internal/collector/awscloud/services/inspector2/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Inspector v2 SDK pagination and safe metadata mapping.
3. `telemetry.go` - per-call span and counter wiring.
4. `../scanner.go` - scanner-owned Inspector v2 fact selection.
5. `../README.md` - Inspector v2 scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud.md` - AWS collector runtime
   requirements.

## Invariants

- Keep Inspector v2 SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Keep the `apiClient` interface limited to `BatchGetAccountStatus`,
  `ListMembers`, `ListFilters`, and `ListCisScanConfigurations`. The reflection
  gate in `client_test.go` fails on any forbidden method.
- Persist only safe account status, feature status, member, filter-name, and
  CIS scan configuration metadata.
- Do not call ListFindings, GetFindings, ListFindingAggregations,
  BatchGetFindingDetails, BatchGetCodeSnippet, GetSbomExport, or CIS
  scan-result reads; finding details are out of scope.
- Do not call GetFilter; filter criteria expressions are out of scope.
- Do not call any Inspector v2 mutation API (Enable, Disable, Create, Update,
  Delete, Associate, Disassociate, BatchUpdate...).
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Inspector v2 metadata read by extending `Client` and the
  `apiClient` interface, writing a scanner or adapter test first, then mapping
  the SDK response into scanner-owned types. Update the reflection gate's
  forbidden list if the SDK adds a same-prefix mutation.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.

## What Not To Change Without An ADR

- Do not read finding bodies, finding aggregations, code snippets, SBOMs, CIS
  scan results, or filter criteria, and do not call mutation APIs.
- Do not infer workload, environment, deployment, ownership, attacker identity,
  or defender posture truth from Inspector v2 metadata.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
