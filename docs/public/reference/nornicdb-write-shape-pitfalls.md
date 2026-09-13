# NornicDB Write-Shape Pitfalls

Cypher **write-statement-shape** pitfalls — how clause ordering inside a single
write statement (MERGE, CREATE, SET, DELETE) decides whether NornicDB executes
every clause. Split out as its own page rather than added to
[NornicDB Behavior and Pitfalls Reference](nornicdb-pitfalls.md) because that
page is at its grandfathered 500-line-cap ceiling and must not grow; see that
page for storage, schema, constraint, and general transaction behaviors, and
[NornicDB Query-Shape Pitfalls](nornicdb-query-pitfalls.md) for read-side
multi-clause and projection pitfalls.

Use it to avoid rediscovering the same failure shape. Still check the current
NornicDB source before patching.

## Pitfall: A Node `MERGE` Followed By `CREATE` In One Statement Silently Drops The `CREATE`

### Observed shape

On the pinned NornicDB build, a statement that opens with one or more node
`MERGE` clauses and then adds a `CREATE` clause in the SAME statement silently
drops the `CREATE`. No error is returned. The canonical repro:

```cypher
-- BROKEN: reports success. Result is 1 node, 0 relationships.
MERGE (s:Workload {id:$s}) MERGE (t:Workload {id:$t}) CREATE (s)-[:DEPENDS_ON]->(t)
```

Neo4j executes this shape correctly (2 nodes, 1 relationship); this is proven
from the Neo4j source, not just observed behavior. Adding a `SET` or a second
`CREATE` to the broken statement does not change the result — the `CREATE`
clause stays dropped either way.

### Affected versions

The Eshu pin (`v1.2.1`) through NornicDB `main` commit `145ed415` (measured
2026-09-12).

### Root cause

In the affected NornicDB source, a `MERGE`-led statement with a second clause
such as another `MERGE`, `OPTIONAL MATCH`, `WITH`, or `WHERE` routes to
`executeMultipleMerges`. Its splitter (`splitMultipleMerges`,
`pkg/cypher/merge.go`) does not treat `CREATE` as a clause boundary, and the
loop that walks the split clauses has no `CREATE` branch at all — the text
after the last recognized clause boundary is silently never executed.

### Eshu implications

Never write a node `MERGE` followed by `CREATE` in one statement. These
shapes are proven safe instead:

- a relationship `MERGE` in place of the `CREATE`
  (`MERGE (s) MERGE (t) MERGE (s)-[:DEPENDS_ON]->(t)`);
- `MATCH ... MATCH ... CREATE` (no `MERGE` anywhere in the statement);
- a comma-pattern `CREATE` (multiple patterns in one `CREATE` clause, again
  with no `MERGE` in the statement);
- two separate statements (a `MERGE` statement, then a `CREATE` statement).

Filed upstream as
[orneryd/NornicDB#359](https://github.com/orneryd/NornicDB/issues/359). As of
this writing there are zero production hits for the MERGE-then-CREATE shape in
Eshu's own Cypher.

### Validation

`go test ./internal/storage/cypher -run
'TestNoNodeMergeThenCreateCyphersAcrossRepo' -count=1` is the static guard: it
walks every non-test `.go` file under `go/cmd` and `go/internal`, extracts
every string literal, and fails if any of them contain a node `MERGE (`
pattern followed later in the same statement by a real `CREATE (` clause.
`MERGE (n) ON CREATE SET ...` and `ON MATCH SET ...` do not trip it — `CREATE`
there is followed by `SET`, not `(`, so it never matches the clause pattern —
and a `CREATE` in a different statement (past a `;`, or in a separate string
constant) does not either. See `merge_then_create_repo_scan_test.go` for the
unit-level proof of both exclusions and the scan itself.

No-Observability-Change: this entry and its guard are documentation and a
build-time static check only. No runtime metric, span, log field, queue
stage, worker knob, or graph-write route changes.
