# Semantic grouped-write split for oversized materializations (#6738)

## Baseline

`SemanticEntityWriter.WriteSemanticEntities` dispatched every retract and
upsert statement of a materialization in one `ExecuteGroup` call — one atomic
backend transaction. On ops-qa that single transaction exceeds the backend
request ceiling for the largest scopes: three reducer dead letters with
`Neo4jError ... Txn is too big to fit into one request`
(`semantic_entity_materialization`, scopes r_087b3438, r_b0ff4ca0,
r_c28df058), plus two `graph_write_timeout` dead letters on the same domain
that burned a fourth attempt on retry. Per-statement row batching (500 rows,
`DefaultBatchSize`) does not help because the group is still one transaction.

## Change

`go/internal/storage/cypher/semantic_entity.go` packs the statement list into
sequential groups capped at `DefaultMaxGroupRows` (2000) UNWIND parameter
rows each, preserving statement order (retracts lead upserts), and dispatches
one `ExecuteGroup` per group. A group failure aborts the write; the work item
retry replays all groups. Retract statements carry repo ID lists, not rows,
and never force a split on their own; singleton-parameterized upserts carry
one row each without a `rows` key and count 1 apiece. `MaxGroupRows`/
`WithMaxGroupRows` override the cap — keep it at or above the largest
per-statement batch size. Writes under the cap dispatch exactly one group,
identical to the old path.

No-Regression Evidence: writes at or under the cap produce a single group —
byte-identical dispatch to before. Splitting only engages past 2000 rows,
i.e. only writes that previously failed outright. Retry convergence holds
because upserts are idempotent MERGEs and retracts are scoped to the same
repo IDs the upserts write; a partial prefix replays to the same end state.
Splitting trades away single-transaction atomicity: concurrent readers can
observe a prefix state (retract committed, upserts partial) on oversized
writes until the retry converges. Only writes that previously failed
outright ever split, so this is transient by construction.
Splitting adds commit boundaries, which strictly increases cross-statement
read-your-writes visibility on NornicDB (same-transaction multi-label MATCH
invisibility is the documented failure direction, not the reverse).

Performance Evidence: unit proof in
`go/internal/storage/cypher/semantic_entity_group_split_test.go` — a
size-capped fake executor rejects any group over the row budget; the writer
delivers all rows across sequential bounded groups with the retract leading,
keeps small writes in one group, and surfaces the backend error with the
failing group index. `go test ./internal/storage/cypher/ ./internal/reducer/code/semantic/`
green, full `go build ./...` clean. Live proof pending rollout: republish,
pin, then replay the five `semantic_entity_materialization` dead letters on
ops-qa and watch them succeed; expected group counts in the completion log.

Observability Evidence: `EntityWriteResult.Groups` counts grouped
transactions dispatched, surfaced on the "semantic entity materialization
completed" structured log as `group_count` next to the existing `row_count`,
so an operator can verify oversized writes actually split live. No new
metric; covered by the existing reducer execution instruments.
