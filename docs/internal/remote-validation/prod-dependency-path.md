# prod-dependency-path — production validation

Validation-Slug: prod-dependency-path
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: platform_impact.dependency_path passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: platform_impact.dependency_path -> mcp:explain_dependency_path

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


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
