# Evidence: make Variable a semantic-owned label so it materializes on NornicDB (#5156)

## Problem

On NornicDB, `Variable` nodes (Elixir module attributes, TSX component-type
assertions) were never created. The projector's canonical phase E
(`extractEntities` in `go/internal/projector/canonical_builder.go`) deliberately
skips `Variable` — nothing else in the canonical writer creates the base node.
The only other writer, `SemanticEntityWriter`, ran in `canonical-node-owned`
mode on NornicDB
(`NewSemanticEntityWriterWithCanonicalNodeRows(...).WithLabelScopedRetract()`,
`go/cmd/reducer/neo4j_wiring.go`), which rewrites its
`MERGE (n:Variable {uid})` into a `MATCH` on the assumption that some other
writer created the base node first. Because nothing does, the upsert was a
silent no-op: `WriteSemanticEntities` reported `CanonicalWrites: 1` while the
live `Variable` count stayed 0.

Every other canonical-node-owned label (`Annotation`, `Typedef`, `TypeAlias`,
`TypeAnnotation`, `Component`, `ImplBlock`, `Protocol`,
`ProtocolImplementation`, `Function`) is genuinely created by phase E, so the
`MATCH`-only rewrite is correct for them. `Variable` was the lone label both
skipped by phase E AND listed in the canonical-node-owned map — a mechanical
inconsistency, not an intentional design choice.

The canonical writer's retract phase (`canonicalNodeRetractCodeEntityLabels`,
`go/internal/storage/cypher/canonical_node_writer_retract_labels.go`) keeps
`Variable` in its per-domain entity-label retract scan, and this fix leaves it
there. `Variable` used to be the largest canonical entity family (the projector
README records `entities|Variable` at 12,887 chunks) before plain-Variable
source-local canonical projection was disabled, so older graphs still hold
`Variable` nodes with `evidence_source='projector/canonical'`. The canonical
retract is the path that cleans those legacy nodes up on the next full/delta
reindex; dropping `Variable` from that scan would strand them, because the
semantic writer only ever retracts `evidence_source='parser/semantic-entities'`
Variables. The two writers therefore partition `Variable` cleanly by
evidence_source (see the cross-writer note below).

## Fix

1. `go/internal/storage/cypher/semantic_entity_statements.go`: removed the
   `"Variable"` entry from `semanticEntityCanonicalNodeClearProperties` (the
   single source of "canonical-node-owned" truth via
   `semanticEntityCanonicalNodeOwnedLabel()`). This flips `Variable` to the
   exact shape `Module` already uses successfully on NornicDB: merge-first
   `MERGE (n:Variable {uid: row.entity_id})` + File containment MERGE +
   `evidence_source` SET on upsert, and DETACH DELETE by `evidence_source` on
   retract (instead of a canonical-owned property-clear REMOVE).
2. Left `canonicalNodeRetractCodeEntityLabels`,
   `specs/replay-depth-requirements.v1.yaml` `retractable_node_types`, and the
   replay-coverage dashboard unchanged. An earlier draft removed `Variable`
   from the canonical retract-label set as "dead weight"; that was wrong. The
   canonical retract scan is the only path that cleans up the legacy
   `evidence_source='projector/canonical'` `Variable` nodes from before
   plain-Variable canonical projection was disabled, so it must stay. The
   `retractable_node:Variable (delta_tombstone)` entry therefore remains an
   honest advisory-uncovered gap in the machine-readable denominator (87
   retractable node types, not 86), exactly as before this change.
3. Confirmed no snapshot change is needed for the B-7 golden-corpus gate
   (`testdata/golden/e2e-20repo-snapshot.json`): the 20-repo corpus fixture
   list (`scripts/verify-golden-corpus-gate.sh:corpus_fixtures`) does not
   include `elixir_comprehensive` or `tsx_comprehensive`, and `Variable` rows
   only ever come from `isElixirModuleAttributeSemanticEntity` /
   `isTypeScriptJSXComponentTypeAssertionSemanticEntity`
   (`go/internal/reducer/semantic_entity_materialization_helpers.go`). The
   golden corpus therefore produces zero `Variable` rows before and after this
   fix; no node/edge count or required-correlation assertion changes.
4. Confirmed no cross-writer deletion race: the canonical writer's entity
   retract only deletes nodes with `evidence_source = 'projector/canonical'`
   (`canonicalNodeRetractEntityTemplate`,
   `go/internal/storage/cypher/canonical_node_cypher.go`), while the semantic
   writer's template sets `evidence_source = 'parser/semantic-entities'`
   (`semanticEntityEvidenceSource`,
   `go/internal/storage/cypher/semantic_entity_statements.go`) — disjoint
   values, so the two writers' retract phases can never delete each other's
   rows.

## Proof

**Output-preserving (unit, RED before the fix):**
`TestSemanticEntityCanonicalNodeRowsUpsertVariableIsMergeFirstWithContainment`
and `TestSemanticEntityCanonicalNodeRowsRetractsVariableByDetachDelete`
(`go/internal/storage/cypher/semantic_entity_nornicdb_test.go`) fail on `main`
(the upsert cypher was `MATCH`-only, no File containment; the retract cypher
was a `REMOVE` property-clear) and pass after the map edit.
`TestSemanticEntityCanonicalNodeRowsRewritesOnlyCanonicalOwnedLabels/Variable`
(pre-existing, table-driven over every semantic label) automatically flips
from asserting no-containment to asserting containment-retained, since it
branches on map membership.
`TestCanonicalNodeWriterRetractCoversProjectableEntityLabels` and
`TestRetractableNodeTypesLockstep` are unchanged and stay green: `Variable`
remains in both the canonical retract-label set and
`retractable_node_types` (see Fix item 2).
`go test ./internal/storage/cypher ./internal/projector ./internal/reducer
./cmd/reducer ./internal/replaycoverage ./cmd/replay-coverage-gate -count=1`
green.

**Live (LIVE RED before the fix, dedicated NornicDB v1.1.11 container,
`bolt://127.0.0.1:17690`, DB `nornic`):**
`TestSemanticEntityWriterLiveNornicDBMaterializesVariableNodes`
(`go/internal/storage/cypher/semantic_entity_variable_nornicdb_live_test.go`)
constructs the writer exactly as `go/cmd/reducer/neo4j_wiring.go` wires it for
NornicDB, seeds File nodes, writes one Elixir module-attribute `Variable` row
and one TSX component-type-assertion `Variable` row for an in-scope repo plus a
control `Variable` row for a separate out-of-scope repo, then performs a full
write -> retract cycle on the in-scope repo alone.

- On `main` (Variable still canonical-node-owned): in-scope `Variable` count
  after write = 0 (reproduces the reported bug exactly).
- After the fix: in-scope `Variable` count after write = 2, both with
  `evidence_source='parser/semantic-entities'` and a `(File)-[:CONTAINS]->`
  edge present.
- Retract-cycle proof: after retracting the in-scope repo (empty `Rows`, same
  `WriteSemanticEntities` call the reducer uses, dispatched through
  `ExecuteGroup`/`session.ExecuteWrite` since the live executor implements
  `GroupExecutor`), in-scope `Variable` count returns to 0 and the
  out-of-scope control repo's `Variable` node survives (count still 1). Ran 5
  times consecutively with no under-application observed, directly checking
  the NornicDB v1.1.11 grouped-DELETE-under-application class of bug
  (#5152/#5305 precedent) against this specific new retract path. Because it
  did not manifest across 5 runs, `Variable`'s retract was left on the default
  grouped dispatch (matching every other semantic label); it was NOT rerouted
  to a sequential `dispatchRetract`. This is empirical, not exhaustive — the
  prior finding on this same NornicDB pin describes DELETE under-application as
  intermittent (unlike REMOVE, which was deterministic), so a future flake on
  this exact path should be triaged against that precedent before assuming a
  new regression.

No-Regression Evidence: `Variable`'s upsert now uses the identical
`semanticEntityMergeFirstRowsUpsertCypher` shape every other non-canonical-owned
semantic label (`Module`, and previously `Annotation`/`Typedef`/etc. before
canonical ownership) already uses in production on NornicDB — this is not new
query shape, only a routing change for one label onto an already-proven path.
The canonical retract-label set is unchanged, so the legacy
`projector/canonical` `Variable` cleanup scan keeps working exactly as before.

No-Observability-Change: no new metrics, spans, or logs were added or changed.
`Variable` writes/retracts now flow through the same `SemanticEntityWriter`
statement dispatch, `StatementMetadataEntityLabelKey`/`StatementMetadataSummaryKey`
tagging, and `CanonicalWrites` counting every other semantic label already uses;
existing instrumentation covers it without change.
