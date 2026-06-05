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
