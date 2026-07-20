# #5346 SQL MIGRATES edge — performance and observability evidence

## Scope

Wires the SQL parser's transient migration metadata into a new `SqlMigration`
content entity, a reducer derivation, a canonical writer/retract path, and a
7th `blastRadiusSqlTableCypher` UNION branch
(`SqlMigration-[:MIGRATES]->SqlTable/View/Function/Trigger/Index`). This note
covers the hot files flagged by `scripts/verify-performance-evidence.sh`:
`go/internal/query/impact_blast_radius.go`,
`go/internal/storage/cypher/edge_writer_sql.go`,
`go/internal/storage/cypher/retractable_edge_types.go`,
`go/internal/storage/cypher/canonical_node_writer_retract_labels.go`,
`go/internal/graph/schema_tables.go`, `go/internal/graph/schema_application.go`,
`go/internal/reducer/sql_relationship_materialization.go` (+ the two files
split out of it, `sql_relationship_metadata.go` and `sql_relationship_names.go`),
`go/internal/collector/git_snapshot_native.go`, and
`go/internal/projector/canonical.go`.

## No-Regression Evidence

The change is additive-only; no existing anchor, index, query shape, or write
template changed for any pre-existing label or edge type.

- **`impact_blast_radius.go`**: the new UNION branch
  (`MATCH (table:SqlTable) WHERE table.name CONTAINS $target_name MATCH
  (repo:Repository)-[:REPO_CONTAINS]->(:File)-[:CONTAINS]->(:SqlMigration)-[:MIGRATES]->(table)`)
  is structurally identical to the six existing branches — same anchor
  predicate, same `Repository-REPO_CONTAINS->File-CONTAINS->Label-REL->table`
  hop shape already proven safe for the sibling `SqlTrigger`/`SqlIndex`/
  `SqlView`/`SqlFunction` branches. `blastRadiusSqlTableBranches` bumped 6->7
  so the over-fetch-before-dedup math in `blastRadiusAffected` stays exact
  (guarded by `TestBlastRadiusSqlTableCypherDropsDeadBranchesKeepsLiveOnes`).
- **`edge_writer_sql.go` / `canonical_node_writer_retract_labels.go`**:
  `SqlMigration` joins the existing per-label retract fan-out
  (`sqlRelationshipRetractSourceLabels`, `canonicalNodeRetractSQLEntityLabels`)
  as one more single-label `MATCH` statement, the same shape already used for
  every sibling SQL label. `SqlMigration` is a new, low-cardinality entity
  label — at most one node per recognized migration file in a repo, the same
  order of magnitude as the sibling SQL labels this file's own top-of-file
  comment already documents as "under ~1k nodes ... already cheap and no worse
  than the old predicate" for the unindexed per-label retract scan.
- **`schema_tables.go` / `schema_application.go`**: adds one `uid` uniqueness
  constraint (`SqlMigration`, mirroring every other SQL entity label) —
  additive DDL, no existing constraint or index changed. The schema
  fingerprint bump is recorded as writer-compatible with its immediate
  predecessor (an older writer creates no `SqlMigration` nodes, so the new
  constraint never applies to it).
- **`sql_relationship_materialization.go`** (`ExtractSQLRelationshipRows`):
  the new `SqlMigration` switch case adds `O(migration_targets)` map lookups
  (`resolveSQLMigrationTarget`) inside the SAME existing `O(n)` single pass
  over `content_entity` envelopes — no new pass, no new fact-load query.
  `migration_targets` is capped at 64 per migration file
  (`sqlMigrationTargetsCap`, parser-side), bounding the added work per
  envelope the same way `source_tables` already bounds the `READS_FROM` case.
- **`git_snapshot_native.go` / `canonical.go`**: `sql_migrations` joins the
  existing bucket-iteration loops (`snapshotEntityBuckets`,
  `entityTypeLabelMap`) as one more O(1) map/slice entry; no loop structure
  changed.

**Live-backend proof**: `bash scripts/verify-golden-corpus-gate.sh` (NornicDB,
20-repo corpus) — PASS, 421 pass / 0 required-fail / 0 advisory-warn, elapsed
32s against a 1800s budget ceiling. `mcp:find_blast_radius` (which drives
`POST /api/v0/impact/blast-radius`, `blastRadiusSqlTableQuery`, and therefore
the new 7-branch `CALL{...UNION...}` form) executed without error, and
`phase_graph_query` stayed at its pre-existing baseline (observed=3.0s,
baseline=3.0s, ceiling=8.0s) — no measurable regression on the query phase
this change touches. The corpus carries no SQL migration fixture files, so the
new branch matched zero rows in this run (`"affected" has 0 results`); the
branch shape itself is exercised end to end by the reducer/writer unit and
offline-tier tests (`go/internal/reducer/sql_relationship_migrates_test.go`,
`go/internal/storage/cypher` per-label retract tests) against realistic
resolved/unresolved/ambiguous target fixtures.

Reproduce:

```bash
cd go && go test ./internal/parser/sql ./internal/parser ./internal/reducer \
  ./internal/storage/cypher ./internal/query ./internal/content/shape \
  ./internal/collector ./internal/graph ./internal/projector -count=1
bash scripts/verify-golden-corpus-gate.sh
```

## Observability Evidence

`sql_relationship_materialization.go`'s `Handle` completion log gains two
structured fields, mirroring the existing `unresolved_read_targets`/
`ambiguous_read_targets` pair added for `READS_FROM` (#5345):
`unresolved_migration_targets` and `ambiguous_migration_targets`
(`slog.InfoContext(ctx, "sql relationship materialization completed", ...)`).
An operator can see silent-empty risk for MIGRATES resolution — including the
same-name ambiguity trap (two same-kind entities across files, e.g.
`schema.sql` and a migration both defining table `users`) — without
re-deriving it from the graph. No new metric series, span, or runtime knob is
added; parse-stage and reducer-stage timing remain owned by the existing
collector/reducer telemetry, which is unchanged by this note's file set.
