# #5410 SQL FK/write relationships - theory, performance, and accuracy evidence

## Scope

This change promotes two parser facts that previously died in the transient
`sql_relationships` bucket:

- `SqlTable-[:REFERENCES_TABLE]->SqlTable` from bounded
  `referenced_tables` metadata.
- `SqlFunction-[:WRITES_TO]->SqlTable` from bounded `write_tables` metadata
  for routine `INSERT`, `UPDATE`, and `DELETE` targets.

It also changes SQL-table blast radius from a direct SqlView read branch to a
bounded `[:READS_FROM*1..2]` reverse traversal, and adds one branch for each new
edge type. The stored graph remains direct; only the read surface follows a
view-on-view chain.

## Prove-the-theory-first result

The bounded traversal was measured before production code changed. The shim
used an isolated Compose project (`eshu-5410-theory`) with
`eshu-nornicdb-pr261:149245885258`, built from NornicDB source commit
`1492458852588c884c32f70d27ea2ee07086769c`. It created uniqueness constraints
for `Repository.id`, `File.uid`, `SqlView.uid`, and `SqlTable.uid`, plus the
production-style `SqlTable.name` index.

The representative graph contained:

- 500 repositories with a direct view reading the target table.
- 500 repositories with a second-level view reading a direct view.
- 100 repositories with a third-level view, outside the intended bound.

Compared shapes:

```cypher
// OLD: direct view only
MATCH (table:SqlTable) WHERE table.name CONTAINS $target_name
MATCH (repo:Repository)-[:REPO_CONTAINS]->(:File)-[:CONTAINS]->
      (:SqlView)-[:READS_FROM]->(table)
RETURN repo, 1 AS hops

// NEW: direct plus one view-on-view hop
MATCH (table:SqlTable)<-[:READS_FROM*1..2]-(:SqlView)<-[:CONTAINS]-
      (:File)<-[:REPO_CONTAINS]-(repo:Repository)
WHERE table.name CONTAINS $target_name
RETURN repo, 1 AS hops
```

Accuracy result: OLD returned the 500 direct repositories. NEW returned exactly
1,000 repositories: the same 500 direct plus all 500 second-level repositories.
The 100 third-level repositories remained excluded. This is the intended
behavior delta and proves the upper bound.

Warm production-Bolt samples, in milliseconds:

| Shape | Samples | Median |
| --- | --- | ---: |
| OLD direct-only | 1.114, 1.246, 2.013, 1.312, 1.203 | 1.246 |
| NEW bounded 1..2 | 2.251, 2.451, 1.974, 1.985, 2.194 | 2.194 |

The new shape returns twice the useful rows and remains below 2.5 ms in every
sample. The pinned NornicDB Bolt transport does not return PROFILE metadata, so
the same literal-parameter queries were also run through the pinned source
executor. PROFILE reported 6.584 microseconds for OLD and 10.666 microseconds
for NEW, with 2,610 estimated database hits for each. Normal source-executor
samples were mostly 14-17 microseconds for OLD and 38-48 microseconds for NEW.

NornicDB's current PROFILE analyzer reports this typed variable-length pattern
as `Expand`, not `VarLengthExpand`; its analyzer recognizes only untyped
`[*1..]` for that display label. Source inspection confirmed that the parser
preserves min=1/max=2 and the executor uses a bounded depth-first traversal
with a per-path visited set. The row-set proof above is the authoritative bound
check; the PROFILE db-hit value is a planner estimate, not a storage counter.

The Compose project, network, volume, and throwaway proof files were removed
after capture.

## Finished-change proof

Failing-first tests covered parser metadata and relationship emission, exact
endpoint labels, unresolved/ambiguous resolution, writer whitelist/retraction,
blast-radius branches, and coverage honesty. The same tests pass after the
implementation:

```bash
cd go
go test ./internal/parser/sql ./internal/reducer \
  ./internal/storage/cypher ./internal/query -count=1
go test ./internal/ifa -count=1
```

The IFA SQL-family baseline and delta fixtures now derive exactly one edge of
all nine writer-registry relationship types. The golden corpus pins at least
one table-to-table `REFERENCES_TABLE` edge and one function-to-table
`WRITES_TO` edge from `tests/fixtures/ecosystems/sql_comprehensive/schema.sql`.

### NornicDB transaction-dispatch proof

The live writer/retract test exposed a pre-existing false assumption in its
production-shaped path. On pinned NornicDB v1.1.11
(`sha256:51b6174ae65e4ce54a158ac2f9eace7d36a1971545824d22add0fe06d94c1090`),
the driver acknowledged the SQL `UNWIND`/`MATCH`/`MERGE` statement inside a
managed transaction but the first existing `QUERIES_TABLE` control read back
zero edges. Reducing the managed group to one statement still returned zero.
Running the identical statement through auto-commit wrote the edge.

Production now selects auto-commit only for SQL relationship writes on
NornicDB. Neo4j retains grouped dispatch. NornicDB already configured a SQL
group size of one, so transaction count, statement count, row batching,
backpressure permits, and worker concurrency are unchanged. Retry and duplicate
delivery remain idempotent through the existing retrying executor and `MERGE`.
A focused concurrency test holds two auto-commit writes inside the executor at
once, proving the dispatch switch does not serialize reducer workers.

The live five-cell Ifá fault-injection matrix also passed after observing four
claimed/running rows at the kill seam. Baseline, kill-after-claim,
expire-lease-mid-handler, fail-write-once, and backend-restart cells converged
with zero dead letters and the identical canonical graph digest
`c072f81d7972c5c27cd529975d80cb7839d6297e41323d12145b097c5409a951`.
The claimed-row observer uses a single server-side Postgres polling loop so the
non-vacuity check can see the real millisecond-scale claim window without
serializing or slowing reducer work.

Backend proof:

```bash
ESHU_REPLAY_TIER_LIVE=1 ESHU_GRAPH_BACKEND=nornicdb \
NEO4J_URI=bolt://127.0.0.1:27611 NEO4J_USERNAME=neo4j \
NEO4J_PASSWORD=change-me ESHU_NEO4J_DATABASE=nornic \
go test ./internal/replay/offlinetier \
  -run '^TestReducerSQLRelationshipRetractGraphTruth$' -count=1 -v
```

Result: PASS in 0.24 seconds after schema setup. Both repository-scope and
delta-file-scope cases wrote all ten fixture edges, including
`REFERENCES_TABLE` and `WRITES_TO`; a duplicate delivery kept every edge count
at one; retraction removed every in-scope edge,
preserved scope/evidence controls and endpoint nodes, and a second retraction
remained idempotent.

### Exact B-7 pipeline proof

The final credential-free gate ran from a fresh Postgres volume and fresh
pinned NornicDB graph after `sql_comprehensive` became an explicit member of
the staged corpus:

```bash
scripts/verify-golden-corpus-gate.sh
```

The local Go cache and temporary directory were redirected to an external
development volume; machine-specific paths are intentionally omitted.

The 25-repository pipeline finished in 35 seconds: bootstrap 3s, cassette
collection 20s, first drain 5s, maintenance drains 7s, and graph/query 2s.
Every first-pass and maintenance drain reported `fact_work_items_residual=0`,
`dead_letter=0`, and `shared_projection_intents_nonterminal=0`.
`REFERENCES_TABLE=1` and `WRITES_TO=1` both passed from the real
`sql_comprehensive/schema.sql` parser-to-reducer-to-graph path. The complete
gate reported 467 pass, 0 required-fail, and 0 advisory-warn.

## Observability evidence

No new metric series, span, queue, or runtime knob is added. The existing
`reducer.sql_relationship_materialization` span and reducer execution metrics
still bound the work. The existing `"sql relationship materialization
completed"` structured log now includes unresolved and ambiguous counters for
both reference targets and write targets, so a missing or ambiguous table
resolution is visible without querying the graph.
For NornicDB, existing shared-edge write logs report `execution_mode=single` with
per-statement duration instead of `dispatch=group`; errors still flow through
the same retry wrapper and reducer failure/status surfaces.
