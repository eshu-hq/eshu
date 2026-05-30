# AGENTS.md - internal/collector/awscloud/services/detective/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Detective SDK pagination and safe metadata mapping.
3. `telemetry.go` - per-call span and counter wiring.
4. `../scanner.go` - scanner-owned Detective fact selection.
5. `../README.md` - Detective scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud.md` - AWS collector runtime
   requirements.

## Invariants

- Keep Detective SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Keep the `apiClient` interface limited to `ListGraphs`, `ListMembers`, and
  `ListTagsForResource`. The reflection gate in `client_test.go` fails on any
  method outside that allow-set; the forbidden-substring scan runs only against
  non-allow-set methods so the safe tag read is not mis-flagged.
- Never read a member's contact email. Drop the deprecated usage-volume,
  graph-utilization, and master-id fields.
- Persist only safe graph identity, creation time, member identity, membership
  status, and data-source package names (keys only).
- Pass behavior graph ARNs through unchanged so synthesized identities inherit
  the graph's partition. Never hardcode a partition.
- Skip a graph with a blank ARN; it has no stable identity.

## Common Changes

- Add a new Detective metadata read by extending `Client` and the `apiClient`
  interface, writing a scanner or adapter test first, then mapping the SDK
  response into scanner-owned types. Add the new method to the allow-set in the
  reflection gate only after proving it is identity/status/count metadata.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.

## What Not To Change Without An ADR

- Do not call GetInvestigation, ListInvestigations, StartInvestigation,
  UpdateInvestigationState, ListIndicators, BatchGetGraphMemberDatasources,
  BatchGetMembershipDatasources, GetMembers, ListDatasourcePackages, or any
  mutation API.
- Do not infer workload, environment, deployment, ownership, or defender posture
  truth from Detective metadata.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
