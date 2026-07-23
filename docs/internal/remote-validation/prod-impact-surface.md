# prod-impact-surface — production validation

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
