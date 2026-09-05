# Agent instructions: internal/reducer/sqlrelationship

Scoped rules for this directory. The root `AGENTS.md` still applies.

## What this package is

The `sql_relationship_materialization` reducer intent handler: derives
canonical SQL relationship edges (`REFERENCES_TABLE`, `READS_FROM`,
`WRITES_TO`, `TRIGGERS`, `EXECUTES`, `HAS_COLUMN`, `MIGRATES`, `INDEXES`,
`QUERIES_TABLE`) from `content_entity` and parsed-file facts and publishes
them as durable shared-projection intents (issue #6061). See `README.md` for
the full ownership boundary and exported surface.

## Read first

- Repository-root `AGENTS.md`
- `go/internal/reducer/AGENTS.md`
- `go/internal/reducer/sqlrelationship/README.md`
- `go/internal/reducer/inheritance/README.md` (the closest sibling: another
  refresh-fenced, file-scoped-partition-key family with the same
  root-side-test-double and partition-convergence-stays-in-root shape)

## Invariants

- **No import of the reducer root, ever.** This package is a leaf below
  `internal/reducer`: the root imports it for `DefaultHandlers` wiring and
  the handler construction, never the reverse.
- **These exported symbols each have a specific, narrower reason than "the
  whole family reuses this package" — do not narrow or reshape any of them
  without checking every consumer below, and do not treat them as a public
  API for unrelated new callers:**
  - `DeltaScope`, `BuildDeltaScope`, `MergeRepositoryIDs`,
    `EmbeddedSQLFunctionIDsByNameLine`, and `EmbeddedSQLFunctionKey` are used
    by the reducer root's `shell_exec` family
    (`shell_exec_materialization.go`, `shell_exec_intents.go`), which has not
    moved out of root yet.
  - `BuildRefreshIntents` and `BuildSharedIntentRows` are used only by the
    sibling-family test (`sibling_edge_intent_delta_gate_test.go`), which
    drives them alongside `inheritance` through one shared table — nothing in
    `shell_exec`'s own production code calls either.
  - `BuildRetractRows` has no caller inside this package. It exists for the
    reducer root's `sql_relationship_partition_convergence_test.go`, which
    asserts the retract and refresh partition keys converge.
  - `EvidenceSource`, `FilePartitionKey`, `WholeScopePartitionKey`, and
    `PartitionKeyVersion` are not consumed by `shell_exec` at all: they
    support the root's generic shared-projection worker test
    (`shared_projection_worker_refresh_redelivery_test.go`) and this
    package's own partition-convergence test
    (`sql_relationship_partition_convergence_test.go`), both of which need
    real evidence-source/partition-key values to construct root-owned
    worker fixtures.
- **The partition-convergence proof stays in the reducer root**
  (`sql_relationship_partition_convergence_test.go`), not in this package.
  It drives `ProcessPartitionOnce` and other generic shared-projection worker
  types that are root-owned infrastructure shared by every domain family and
  have not moved out of root yet — see AGENTS.md precedent in
  `inherits_edge_partition_convergence_test.go`. Do not try to "finish the
  move" by relocating it here; that would require this package to import the
  reducer root, which is forbidden.
- **Two small pure helpers are duplicated from the reducer root, not
  imported:** `codeCallInt` and `codeCallDeltaRelativePathsFromRepository`
  in `sql_relationship_aliases.go`. Their owning family (`code_call`) has not
  moved out of root yet. If `code_call` moves to its own subpackage later,
  re-evaluate whether these local copies should become a shared import
  instead — do not let them silently diverge from the root's originals in
  the meantime (they are pure functions with no reducer-specific state, so
  drift risk is low, but check on future edits to either copy).
- **`ExtractSQLRelationshipRows` is also driven directly by the Ifá
  `sql_relationships` golden-corpus gate**
  (`internal/ifa/materializededges/materialized_edges_sql.go` and its
  cassette test). A behavior change here changes what that gate asserts;
  run `go test ./internal/ifa/materializededges/...` after any edit to
  `ExtractSQLRelationshipRows` or its target-resolution helpers.

## Root-side test doubles this package's move required

`go/internal/reducer/sql_relationship_root_test_helpers_test.go` (root) holds
a SEPARATE, hand-kept-in-sync copy of a subset of this package's own test
fixtures (`recordingSQLRelationshipIntentWriter`,
`sqlRelationshipRepositoryEnvelope`, `sqlRelationshipContentEntity`,
`sqlRelationshipEntityFacts`) — Go test files cannot share unexported
symbols across packages, and the root's fact-kind/fact-payload loader gates,
idempotency cases, and `shell_exec` materialization tests still need to
build these fixtures or drive `SQLRelationshipMaterializationHandler.Handle`
end to end. See `README.md`'s "Root-side test doubles this package's move
required" section. If you change a shared fixture's shape here (a builder's
fields, the recording writer's method set), update the root copy in the same
commit — nothing enforces they stay identical.

## Common changes

Adding a new relationship type: extend the `switch entityType` in
`ExtractSQLRelationshipRows` (`sql_relationship_materialization.go`) with
the new edge derivation, add its unresolved/ambiguous counters to
`SQLRelationshipRowStats` (`sql_relationship_names.go`) if it needs
target resolution, and update the golden-corpus expected-edges fixture at
`internal/ifa/materializededges/testdata/` in the same change.

Adding a field to the delta-scope shape: change `DeltaScope`
(`sql_relationship_delta_scope.go`) and check both this package's own
callers and `shell_exec_materialization.go`/`shell_exec_intents.go` (root)
for the same field.

## Failure modes to avoid

- Exporting a new symbol from this package "just in case" — every exported
  name beyond the handler's own entry point exists for a specific,
  documented cross-family or cross-package caller (see Invariants). Keep it
  that way so a future reader can trust the exported-surface comments.
- Letting the root-side test-double copy (see above) silently diverge from
  this package's own fixtures when either side's shape changes.
- Moving the partition-convergence proof into this package — it needs
  root-owned generic worker infrastructure this package must never import.
