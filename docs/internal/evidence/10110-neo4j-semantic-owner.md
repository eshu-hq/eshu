# #10110 Neo4j semantic writer canonical ownership

## Defect

On Neo4j the reducer wired the legacy semantic writer
(`entity.NewSemanticEntityWriter`). For canonical-owned labels it ran
`MATCH (f:File) MERGE (n:Function {uid}) SET ..., n.evidence_source =
'parser/semantic-entities' MERGE (f)-[:CONTAINS]->(n)`. After a delta moved a
Function to a new uid, the stale gen-1 node kept the semantic stamp, so the
canonical retract (`n.evidence_source = 'projector/canonical'`) skipped it
and the phantom survived with its CALLS edge. NornicDB already ran
canonical-node-rows mode and did not have the defect.

## Fix

Neo4j now runs `NewSemanticEntityWriterWithCanonicalNodeRowsLegacyOrder`:

- Canonical-owned labels (Annotation, Typedef, TypeAlias, TypeAnnotation,
  Component, ImplBlock, Protocol, ProtocolImplementation, Function) are
  enriched with `UNWIND $rows AS row MATCH (n:Label {uid: row.entity_id})
  SET ...` — no `evidence_source` re-stamp, no File match, no CONTAINS
  re-MERGE. The emitted bytes are identical to canonical-node mode
  (asserted in `canonical_ownership_test.go`).
- Semantic-owned labels (Variable, Module) keep the legacy
  MATCH-File-first templates, so a row whose File is absent still creates
  no node. NornicDB keeps merge-first rows, which would create fileless
  nodes here.
- Retracts keep the broad Neo4j shape (stamped edge retract, node-scoped
  edge retract, edgeless reap) and add one `REMOVE` clear per
  canonical-owned label (9), full (`n.repo_id IN $repo_ids`) and delta
  (`n.path IN $file_paths`). No statement deletes a canonical-owned node.
- Attempted-vs-matched reconciliation (`UnmatchedEntityIDs`) now also runs
  on Neo4j, matching the NornicDB contract: a non-derived Function uid is
  reported instead of silently MERGE-creating a duplicate node.

Anchors and cardinality: upsert anchor is the `<label>_uid_unique`
constraint seek on `uid` (batch in: wiring batch size, 100 in the
measurement; out: at most one node per row). Clears anchor on the
`repo_id` / `path` label indexes where they exist (Function:
`function_repo_id`, `function_path`; Protocol carries both indexes too);
the other 7 cleared labels ride the same label-scan class the existing
broad retract already pays, bounded per label population. Largest fan-out
is the clear's per-label repo (or touched-file) population.

## No-Regression Evidence:

Correctness fix: this trades a full benchmark for a same-shape
no-regression check on the pinned backend, per the cypher-performance
ladder. Backend: Neo4j `2026-community` (container
`eshu-lane3-10110-neo4j`, disposable, auth disabled), production schema
applied by the live test (`EnsureSchemaWithBackend`, 269 statements).
Shape: 2,000 seeded canonical Function nodes + 1 File in one repo,
100-row Function upsert batch, PROFILE plus 15 wall runs each. Wall
numbers are a same-machine relative comparison on a local container,
not a production wall-time claim; db hits are the load-bearing metric.

- Legacy `MERGE (n:Function {uid})` + File match + CONTAINS merge:
  2,200 db hits, avg 24.2 ms/batch. Plan includes
  `MergeUniqueNode`, `LockingMerge`, `Expand(Into)`.
- New `MATCH (n:Function {uid: row.entity_id})` enrichment: 1,600 db hits
  (-27%), avg 15.6 ms/batch (-36%). Plan is a single
  `NodeUniqueIndexSeek(Locking)` + `SetProperties`; no scan operators.
- Added cost: 9 `REMOVE` clears per semantic write (retract statements
  3 -> 12). The Function clear PROFILEs as `NodeIndexSeek` on `repo_id`
  at ~16 db hits per node in the repo population (32,001 hits at 2,000
  nodes), the same per-node clear NornicDB already pays; the delta
  variant is scoped to touched file paths. The broad retract already
  scanned the same population three times per write.

Live RED -> GREEN: `TestLiveNeo4jSemanticWriterKeepsCanonicalFunctionOwnership`
(delta move + full-regeneration re-key) failed before with two Function
nodes (stale gen-1 `evidence_source:parser/semantic-entities`) and passes
after, with the surviving node keeping `projector/canonical`, the
semantic `docstring` enrichment, and the canonical CONTAINS edge.

Residuals: graphs already polluted before this fix keep their semantic
stamp; the semantic edgeless reap deletes such a phantom only after its
foreign edges (for example CALLS) are retracted by their owning writers,
so healing can lag one projection cycle. A derived uid whose canonical
node is genuinely absent still MATCH-misses silently, as on NornicDB
today; only non-derived uids are reported.

## Observability Evidence:

No new instruments. Existing signals cover the change: Bolt write-count
summaries per statement (`ReportWriteCounts` with statement summaries
such as `semantic label=Function rows=100`), and the semantic handler's
attempted-vs-matched WARN plus non-retryable failure on
`UnmatchedEntityIDs`, which now reaches Neo4j instead of the previous
silent legacy MERGE. An operator sees a divergent uid as a WARN naming
the count, not as a phantom CALLS edge weeks later.
