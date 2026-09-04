# internal/reducer/sqlrelationship

## Purpose

Materializes the `sql_relationship_materialization` reducer domain (issue
#6061): derives canonical SQL relationship edges from `content_entity` facts
describing SQL tables, columns, views, functions, triggers, indexes, and
migrations, plus embedded-SQL evidence carried on parsed file facts, and
publishes them as durable, file-scoped shared-projection intents (#2868).

Edge types produced: `REFERENCES_TABLE` (table -> table, from
`referenced_tables`), `READS_FROM` (view/function -> table or view, from
`source_tables`), `WRITES_TO` (function -> table, from `write_tables`),
`TRIGGERS` (trigger -> table) and `EXECUTES` (trigger -> function, both from
a `SqlTrigger`'s `table_name`/`function_name`), `HAS_COLUMN` (table ->
column), `MIGRATES` (migration -> table/view/etc., from `migration_targets`,
#5346), `INDEXES` (index -> table, #5330), and `QUERIES_TABLE` (function ->
table, from embedded SQL query evidence on parsed file facts).

## Ownership boundary

**Owns:** `SQLRelationshipMaterializationHandler` (the reducer intent
handler), `ExtractSQLRelationshipRows` (the pure extraction seam used both by
the handler and by the Ifá `sql_relationships` golden-corpus gate,
`internal/ifa/materializededges/materialized_edges_sql.go`), the delta-scope
builder and shared-intent promotion, target-resolution rules for every edge
type above, and the embedded-SQL-query scanner.

**Does not own:** the generic shared-projection partitioned worker
(`ProcessPartitionOnce`, `PartitionProcessorConfig`, and their lease/reader/
readiness dependencies) — that is root-owned infrastructure every domain
family's durable intents route through, and has not moved out of the reducer
root yet. This package's partition-convergence proof therefore stays in the
reducer root (`sql_relationship_partition_convergence_test.go`) rather than
here — see "Cross-family reuse" under Gotchas / invariants below, and
AGENTS.md.

## Exported surface

| symbol | what it is |
|---|---|
| `SQLRelationshipMaterializationHandler` | the reducer intent handler; `Handle` is the entry point |
| `SQLRelationshipIntentWriter` | the writer port `Handle` emits durable intents through |
| `ExtractSQLRelationshipRows()` | the pure fact-envelopes -> edge-rows extraction seam; also driven directly by the Ifá `sql_relationships` golden-corpus gate |
| `SQLRelationshipRowStats` | per-relationship-type unresolved/ambiguous target-resolution counts `ExtractSQLRelationshipRows` reports instead of silently dropping (#5345, #5346) |
| `DeltaScope` / `BuildDeltaScope()` / `MergeRepositoryIDs()` | the delta-generation scope this family derives from `repository` facts; exported for cross-family reuse by `shell_exec` |
| `BuildSharedIntentRows()` / `BuildRefreshIntents()` | promote extracted edge rows to durable shared-projection intents (file-scoped per-edge plus one whole-scope per-repo refresh, #2868/#2898) |
| `EvidenceSource` / `FilePartitionKey()` / `WholeScopePartitionKey()` / `PartitionKeyVersion` | this family's evidence-source tag and partition-key builders; exported for cross-family reuse and for the reducer root's generic refresh-fence proofs |
| `EmbeddedSQLFunctionIDsByNameLine()` / `EmbeddedSQLFunctionKey()` | index a parsed file's functions by (name, line) so an embedded-code scanner can resolve the enclosing function's entity ID; exported for cross-family reuse by `shell_exec` |

The reducer root wires `DefaultHandlers.SQLRelationshipIntentWriter`
(`defaults.go`) to this package's writer port and constructs the handler in
`defaults_domain_catalog.go`.

## Dependencies

`internal/reducer/contract` (the `Intent`/`Result`/`Domain` shapes, aliased
`reducercontract`), `internal/reducer/factload` (the scoped fact loader),
`internal/reducer/payloadcore` (deref/trim/convert helpers),
`internal/reducer/schemadecode` (the typed-payload decode seam for
`repository`/`file` facts), `internal/reducer/sharedintent` (the shared
projection intent builder, `ProjectionContext`, and refresh-fence helpers),
`internal/facts`, and the generated `sdk/go/factschema/codegraph/v1`
package. No dependency on the reducer root, and none of the root's other
family subpackages.

Two small pure helpers are duplicated from the reducer root rather than
imported, because their owning family (`code_call`) has not moved out of
root yet: `codeCallInt` (a four-branch numeric type switch) and
`codeCallDeltaRelativePathsFromRepository` (a `codegraphv1.Repository`
field union) — see the comments on `sql_relationship_aliases.go`'s
`codeCallInt` and `codeCallDeltaRelativePathsFromRepository`.

## Telemetry

No metric instruments. `Handle` emits two structured log lines (`sql
relationship materialization started`/`completed`) via `log/slog`, unchanged
by this move. The generic partitioned worker's metrics
(`eshu_dp_reducer_executions_total` and friends) are emitted by the reducer
root's own worker code, not this package.

## Gotchas / invariants

### Cross-family reuse

The `shell_exec` family (`shell_exec_materialization.go`,
`shell_exec_intents.go`), which has not moved out of the reducer root yet,
reuses four pieces of this package's machinery rather than duplicating them:
`BuildDeltaScope`/`DeltaScope` and `MergeRepositoryIDs` (both families derive
the same per-repository `delta_generation`/`delta_relative_paths` shape from
the same `repository` facts), `BuildRefreshIntents` (driven through one
table alongside this family and `inheritance` in
`sibling_edge_intent_delta_gate_test.go`), and
`EmbeddedSQLFunctionIDsByNameLine`/`EmbeddedSQLFunctionKey` (both families
resolve an embedded record's enclosing function by name+line against a
parsed file's `functions` array). This is why those symbols are exported
even though nothing inside this package's own `Handle` path calls them
through the exported name.

### Root-side test doubles this package's move required

Go test files cannot share unexported symbols across packages, and several
reducer-root test suites still construct this family's repository/content-
entity fixtures, or drive `SQLRelationshipMaterializationHandler.Handle` end
to end, the way this package's own tests do. Rather than export production
API for test-only shapes, the root keeps its own hand-kept-in-sync copies in
`sql_relationship_root_test_helpers_test.go`:
`recordingSQLRelationshipIntentWriter`, `sqlRelationshipRepositoryEnvelope`,
`sqlRelationshipContentEntity`, and `sqlRelationshipEntityFacts` — mirror
this package's identically-named builders in
`sql_relationship_test_helpers_test.go` and
`sql_relationship_materialization_test.go`. If you change a shared fixture's
shape here (a builder's fields, the recording writer's method set), update
the root copy in the same commit — nothing enforces they stay identical.

The partition-convergence proof
(`sql_relationship_partition_convergence_test.go`) is a stronger case than a
test double: it drives the actual generic shared-projection worker
(`ProcessPartitionOnce`) against this package's exported
`ExtractSQLRelationshipRows`/`BuildDeltaScope`/`BuildRetractRows`/
`BuildSharedIntentRows`/`EvidenceSource`/`WholeScopePartitionKey`/
`PartitionKeyVersion` surface, so it stays in the reducer root rather than
moving here — mirroring `inherits_edge_partition_convergence_test.go`'s
identical reasoning for the `inheritance` family.

### Evidence

No-Regression Evidence: this move is a pure relocation (issue #6061) --
function and type bodies are unchanged apart from the package clause,
import paths, and the export-casing renames this README documents above;
no algorithm, query, or edge-derivation rule changed. Proven by
`go test ./internal/reducer/... ./internal/ifa/... ./internal/storage/cypher/...`
passing unchanged before and after the move, and by
`internal/ifa/materializededges`'s golden-corpus lockstep/family-count/
stem-trigger tests (`TestMaterializedEdgeCoverageLockstepAgainstRealSpecs`,
`TestMaterializedEdgeFamilyCountClaimsMatchTheCode`,
`TestEveryCoveredFamilyTriggersBothLiveGates`) passing against the moved
package.

No-Observability-Change: see Telemetry above -- no metric, span, or log
field was added, removed, or renamed by this move.

## Related docs

- [Reducer package](../README.md)
- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
- [#5346 SQL MIGRATES edge performance evidence](../../../../docs/internal/evidence/5346-sql-migrates-performance.md)
- [#5410 SQL FK/write relationships evidence](../../../../docs/internal/evidence/5410-sql-relationships-performance.md)
