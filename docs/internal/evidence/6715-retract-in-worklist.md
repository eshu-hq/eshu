# Projector delta retracts: positive IN worklist instead of UNWIND seed (#6715)

Classification: **Wall-clock win** (cold projector retract phase on large
stores). Output-preserving (deletes the identical node/edge set — proven by
count read-back on live data for every rewritten shape). Not a correctness
change to graph truth.

## Root cause

#6715 reports the projector canonical retract phase-group timing out at the
300s `ESHU_CANONICAL_WRITE_TIMEOUT` on `MATCH (f:File) WHERE f.path IN
$file_paths`, identically for 271-fact and 52k-fact repositories, on NornicDB
v1.3.2. On current `main` that exact statement is gone, but four delta
retract statements in the same phase used the sibling shape the issue asks
for — an `UNWIND $paths AS p` seed plus an inline-anchored `MATCH` plus a
compound `WHERE`:

- `canonicalNodeRetractDeltaDeletedFilesCypher`
- `canonicalNodeRetractDeltaDeletedDirectoryEdgesCypher`
- `canonicalNodeRetractDeltaDeletedDirectoriesCypher`
- `canonicalNodeRetractDeltaEmptyDirectoriesCypher`

Timed against the pinned backend (`timothyswt/nornicdb-cpu-bge:v1.3.3`,
digest `sha256:81cedbf4…`, isolated instance, fresh volume, 200k `:File` +
400k `:Function` + 10k `:Parameter` + 100k unindexed `:Gadget` + 410
`:Directory` nodes, path indexes ONLINE, 25-path worklist) via the HTTP
`tx/commit` endpoint, each UNWIND-seeded compound statement costs 87–216s
per execution while the equivalent positive-`IN` shape costs 0.1–9s with an
identical deleted set. EXPLAIN/PROFILE return no plan over this endpoint, so
the proof is timing plus row-equivalence. Host idle; slow results reproduce
across fresh statement texts. The NEW-shape verification ran on the same
instance after ~200 additional scratch nodes were added (a strict superset
of the store the OLD shapes were timed on), so the before/after comparison
is conservative.

## Performance Evidence:

| shape (per execution, 600k-node store) | time | deleted set |
|---|---|---|
| OLD `UNWIND $file_paths AS file_path MATCH (f:File {path: file_path}) WHERE f.repo_id=$repo_id AND f.evidence_source=… DETACH DELETE f` (production-verbatim) | 188.6s | 25 files, 0 remain |
| NEW `MATCH (f:File) WHERE f.path IN $file_paths AND f.repo_id=$repo_id AND f.evidence_source=… DETACH DELETE f` (this change, production-verbatim) | 9.0s | 25 files, 0 remain |
| OLD UNWIND delta directory-edges / deleted-directories / empty-directories DELETE (production-verbatim) | 87.5s / 87.6s / 87.2s | correct, 0 remain |
| NEW IN directory-edges / deleted-directories / empty-directories DELETE (this change, production-verbatim) | 8.5s / 8.8s / 8.8s | correct, 0 remain |
| OLD UNWIND+WHERE Function DETACH DELETE vs NEW IN+WHERE | 216.4s vs 9.0s | 50 nodes each, 0 remain |
| OLD `MATCH (n:Function) WHERE … n.path IN $file_paths …` RETURN (delta-entity template, unchanged) | 4.7s cold, 1ms warm | 2=2 |
| UNWIND + inline anchor, no extra WHERE (import / directory-file edge refreshes, unchanged) | 2ms cold | 25=25 |
| Edge retracts with relationship patterns, UNWIND vs IN (CALLS DELETE) | 35ms vs 17ms cold | both fully delete, 0 survivors |

Warm repeats are ms for the pure-seek shapes (single-predicate IN and
UNWIND without extra WHERE); the compound UNWIND shapes measured slow on
nearly every execution (one 4ms warm repeat out of five runs of the same
Function delta text, otherwise 114–138s, and 218–248s across three runs of
the parent-edges text), so no reliability claim rests on plan-cache warmth.
Inlining scalar predicates into the node map does not help the UNWIND shape
(113.7s). A Bolt
managed-transaction probe (Go driver, explicit tx, like
`RetryingExecutor.ExecuteGroup`) deletes correctly through both shapes on
v1.3.3, so the IN shape keeps the grouped-transaction safety the pitfalls
doc requires. The phase-group chunker (`ChunkPositiveStringSliceRetractStatement`)
splits `IN $param` and `UNWIND $param AS` worklists identically, so batching
is unchanged, as are the statement parameters.

Out of scope with data (follow-up): directory parent-edge refresh
(`UNWIND $rows … p.path <> row.parent_path`, 221–248s per run; scalar form
9s per directory) and entity-containment refresh (240s per 25-row chunk) show
a distinct per-row-scan pathology that needs a two-trip redesign, not a shape
swap — scalarizing would cost ~9s per row. Edge retracts with relationship
patterns and pure UNWIND seeks show no cold penalty and stay as they are
(the #4708 UNWIND evidence stands for those shapes).

## Observability Evidence:

No new metrics. The changed statements keep their
`OperationCanonicalRetract` operation, phase-group `first_statement` logging,
and per-statement duration logging, so the existing `phase-group retract
statement %d/%d part %d/%d (duration=…, first_statement=…)` error shape that
surfaced #6715 keeps reporting the same fields. No-observability-change
beyond the expected duration drop on the four rewritten statements.
Unit coverage: `TestDeltaFileAndDirectoryRetractsUsePositiveInWorklist`
(fails on the old UNWIND texts, passes on the new IN texts) plus updated
pinned wants in `TestCanonicalNodeRefreshStructuralEdgesSeedsFromFilePath`.
