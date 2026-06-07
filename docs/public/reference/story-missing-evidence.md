# Story Missing Evidence Alignment

This note records the no-regression evidence for issues #1420 and #1423.
The change keeps vulnerability and repository story answers aligned with
evidence Eshu has already emitted, without adding new collection, graph write,
queue, or runtime behavior.

## Scope

- `reducer_supply_chain_impact_finding.missing_evidence` now distinguishes
  absent runtime evidence from runtime evidence that exists but is not linked
  all the way to a vulnerable package.
- Supply-chain impact explanation maps those precise missing reasons back to
  the service and environment hops.
- Repository story no longer reports `deployment_surface_unknown` when a
  repository already has workload evidence plus delivery or deployment
  evidence, even when platform labels are absent.

No-Regression Evidence: the baseline focused RED checks were
`go test ./internal/reducer -run TestBuildSupplyChainImpactFindingsAttachesWorkloadIdentityWithoutServiceCatalog -count=1`
and
`go test ./internal/query -run TestBuildRepositoryStoryResponseDoesNotMarkDeploymentUnknownWhenWorkloadHasDeliveryEvidence -count=1`.
The first failed because a finding with workload evidence still reported the
generic `deployment exposure evidence missing` and `service evidence missing`
strings. The second failed because a repository with a workload and delivery
artifact still reported `deployment_surface_unknown`. After the fix, those
same checks passed, plus `go test ./internal/reducer -count=1`,
`go test ./internal/query -count=1`, `go test ./internal/mcp -count=1`, and
`go test ./...`.

The input shape is in-memory reducer and query fixtures: one advisory/package
consumption path with workload identity and no service catalog correlation,
one deployment-and-workload path with no service catalog correlation, one
supply-chain explanation row with precise missing reasons, and one repository
story row with one workload plus a controller delivery artifact. The terminal
row counts are one impact finding or one story response per focused fixture.
No graph backend, Bolt session, Postgres queue row, scanner worker, hosted
collector claim, or external provider call is involved.

No-Observability-Change: this is classification and response shaping over facts
the reducer and query handlers already load. Operators continue to diagnose
the path through the existing `reducer_supply_chain_impact_finding` payload,
`EvidencePath`, `missing_evidence`, `runtime_reachability`,
`query.supply_chain_impact_findings`,
`query.supply_chain_impact_explanation`, repository story query spans,
Postgres query duration instrumentation, reducer execution counters, and
admin/status queue summaries. No metric instrument, metric label, span name,
log key, graph write, queue domain, worker, runtime flag, or pprof behavior was
added.

## Issue #1668 Operational Anchors

Supply-chain impact findings may attach reducer-proven service and environment
anchors after package impact is established, but only through explicit
operational evidence. Repository-scoped CI/CD environment rows attach to a
finding when the finding already has a reducer-owned workload, service, or
deployment anchor for the same repository. Repository-only package findings do
not inherit environments from repository names, image tags, branches, or
free-text paths, and provenance-only deployment rows remain missing evidence.

No-Regression Evidence: `go test ./internal/reducer -run 'TestBuildSupplyChainImpactFindings(AttachesRepositoryScopedOperationalAnchors|DoesNotAttachRepositoryOnlyEnvironment|KeepsProvenanceOnlyDeploymentEnvironmentMissing|ConnectsRuntimeEvidencePath|KeepsRepositoryOnlyRuntimeHopsMissing|AttachesWorkloadIdentityWithoutServiceCatalog|AttachesDeploymentLaneEvidence|AttachesDeploymentAndWorkloadWithoutServiceCatalog|ConsumesRepositoryOnlyServiceCatalogEvidence|ReportsScopedUnresolvedServiceCatalogEvidence|ConsumesDeploymentOnlyExactServiceCatalogEvidence)' -count=1` failed before repository-scoped CI/CD environment evidence could attach to a workload/service-anchored finding, then passed after the reducer matched those rows through the already-proven operational anchor. `go test ./internal/query -run 'TestSupplyChain(ListImpactFindingsExposesOperationalAnchors|ExplainImpactExposesOperationalAnchors)' -count=1` proves the HTTP list and explain payloads expose the same persisted anchors that MCP dispatch reads through the shared handler.

No-Observability-Change: this change adds no route, graph query, table, queue
domain, worker, lease, metric instrument, metric label, span name, or log key.
Operators continue to diagnose the path through existing supply-chain reducer
execution counters, persisted `reducer_supply_chain_impact_finding` payloads,
`EvidencePath`, `missing_evidence`, `runtime_reachability`,
`query.supply_chain_impact_findings`, `query.supply_chain_impact_explanation`,
Postgres query duration instrumentation, and admin/status queue summaries.

No-Whole-Graph-Traversal Evidence: no Cypher or graph read path changed. The
reducer only reorders and filters facts already loaded by the bounded
`ListActiveSupplyChainImpactFacts` repository follow-up allowlist, and the query
tests read the same persisted row fields that the API and MCP routes already
return.
