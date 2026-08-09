# prod-impact-surface — production validation

Validation-Slug: prod-impact-surface
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: platform_impact.blast_radius passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: platform_impact.blast_radius -> mcp:find_blast_radius

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `platform_impact.blast_radius` (tool `find_blast_radius`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: multi_repo_platform`, `p95_latency_ms: 7000`,
`max_truth_level: exact`.

## Claim validated

Bounded blast-radius traversal over hybrid graph-plus-infra truth: for a
repository, Terraform resource, or SQL-table target, the handler returns
directly-affected and tiered-transitive repositories/resources with dead
branches dropped and coverage reported, correct on the pinned NornicDB
backend.

## Committed reproducible evidence

**Handler correctness across target types** —
`go/internal/query/impact_blast_radius_test.go`:
`TestFindBlastRadiusRepositoryMergesAffectedAndTiers`,
`TestFindBlastRadiusTerraformAnchorsDependentsByID`,
`TestFindBlastRadiusSqlTableOverFetchesBeforeDedup`,
`TestBlastRadiusQueriesAreNornicDBSafe`,
`TestMergeBlastRadiusRowsMinHopsDedup`. Reproduce:

```bash
cd go && go test ./internal/query -run TestFindBlastRadius -count=1
cd go && go test ./internal/query -run TestBlastRadiusQueriesAreNornicDBSafe -count=1
```

**Coverage reporting and dead-branch pruning** —
`go/internal/query/impact_blast_radius_coverage_test.go`:
`TestBlastRadiusSqlTableCypherDropsDeadBranchesKeepsLiveOnes`,
`TestFindBlastRadiusSqlTableReportsUnmaterializedCoverage`,
`TestFindBlastRadiusCrossplaneXrdReportsMaterializedCoverage`,
`TestFindBlastRadiusRepositoryCompleteWithEmptyCoverage`. Reproduce:

```bash
cd go && go test ./internal/query -run TestFindBlastRadius -count=1
```

**Live NornicDB before/after correctness proof** —
`docs/internal/evidence/5279-blast-radius-nornicdb.md` records the #5279 fix
that rewrote all four `target_type` branches to a NornicDB-safe shape, with a
19-repo before/after read against an isolated Compose NornicDB instance
(literal-alias-text corruption before, correct affected-repo/tier output
after).

## Notes

No private data: this artifact cites only committed tests and a committed
before/after evidence note, no deployment-specific values.

Related: #5552 (burn-down), #5407 (artifact-existence gate).
