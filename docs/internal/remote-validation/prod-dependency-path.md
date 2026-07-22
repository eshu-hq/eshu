# prod-dependency-path — production validation

Capability: `platform_impact.dependency_path` (tool `explain_dependency_path`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: multi_repo_platform`, `p95_latency_ms: 7000`,
`max_truth_level: exact`; exact only when dependency graph relations
converge.

## Claim validated

`explain_dependency_path` resolves the source and target identifiers
(id-or-name) and runs a per-label, single-anchor `shortestPath`, returning
correct hop provenance (`from_id`/`to_id`, edge type, and aggregate
confidence) rather than the multi-label disjunction shape that silently
mangled hops on the pinned NornicDB backend.

## Committed reproducible evidence

**Handler contract and resolver guards** —
`go/internal/query/impact_anchor_label_test.go`:
`TestExplainDependencyPathAnchorsResolvedEndpoints` and
`TestExplainDependencyPathNullPathRecordOmitsPath`. Reproduce:

```bash
cd go && go test ./internal/query -run 'ExplainDependencyPath|ImpactAnchor' -count=1
```

**Live NornicDB before/after proof** — `docs/internal/evidence/5286-by-id-impact-anchors-nornicdb.md`
documents a live-backend regression (the label-disjunction anchor returned
zero rows / mangled hops; the per-label resolved-id anchor returns a correct
shortest path) measured through `TestLiveByIdImpactAnchorReads`, plus a
golden-corpus run of the shipped handler against the bootstrapped 20-repo
corpus returning an HTTP 200 real shortest path for
`explain_dependency_path {source: "orders-api", target: "lib-common"}`.
Reproduce (requires a live NornicDB backend):

```bash
ESHU_OCI_PROVE_LIVE=1 ESHU_NEO4J_URI=bolt://localhost:17687 \
  go test ./internal/query -run TestLiveByIdImpactAnchorReads -count=1 -v
```

## Notes

No private data: the live-corpus example above cites only the bootstrapped
golden-corpus fixture's own repository name (`orders-api`), not a real
deployment or credential.

Related: #5407 (artifact-existence gate), #5552 (burn-down).
