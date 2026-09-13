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

## Pitfall: A Node `MERGE` Followed By `CREATE` In One Statement Silently Drops The Second `MERGE` And The `CREATE`

### Observed shape

A statement that opens with a node `MERGE`, adds a second `MERGE` (or an
`OPTIONAL MATCH`/`WITH`/`WHERE`), and then adds a `CREATE` clause in the SAME
statement silently drops BOTH the second `MERGE` and the `CREATE`. No error is
returned. The canonical repro:

```cypher
-- BROKEN: reports success. Result is 1 node, 0 relationships.
MERGE (s:Workload {id:$s}) MERGE (t:Workload {id:$t}) CREATE (s)-[:DEPENDS_ON]->(t)
```

Only `s` is written; `t` (the second `MERGE`'s node) and the `DEPENDS_ON`
relationship are both lost. Neo4j executes this shape correctly (2 nodes, 1
relationship) — see Affected versions below for how each claim was proven.
Adding a `SET` or a second `CREATE` to the broken statement does not change
the result — the loss is the same either way.

### Affected versions

Reproduced live on a headless build of NornicDB commit `3722b483c02c` —
Eshu's `docker-compose.yaml` pin (`eshu-nornicdb-pr290:3722b483c02c`), which
self-reports version `v1.2.1` (the version string baked into that commit; the
upstream `v1.2.1` tag itself is a different commit, `66755bfba882`). By code
read, the same `executeMultipleMerges`/`splitMultipleMerges` executor code —
no `CREATE` clause boundary, no `CREATE` branch in the segment loop — is
present at Eshu's Helm chart pin (`nornicdb-cpu-bge:v1.2.3@sha256:4dfa887d…`,
tag commit `d9b76ae82334`, self-reports `1.2.2`) and at NornicDB `main` commit
`145ed415` (2026-09-12); neither has been run against this statement yet.
Neo4j's correct handling of the shape is proven from its source, not from a
live run.

### Root cause

In the affected NornicDB source, a `MERGE`-led statement with a second clause
such as another `MERGE`, `OPTIONAL MATCH`, `WITH`, or `WHERE` routes to
`executeMultipleMerges`. Its splitter (`splitMultipleMerges`,
`pkg/cypher/merge.go`) does not treat `CREATE` as a clause boundary: the
trailing `CREATE` text is glued onto the second `MERGE`'s segment and parsed
as though it were part of that `MERGE`'s own pattern, and the segment loop has
no `CREATE` branch at all. Neither the second node `MERGE` nor the `CREATE`
executes as a result.

A lone `MERGE (n) CREATE ...` with no second `MERGE`/`OPTIONAL MATCH`/`WITH`/
`WHERE` takes a different path, `executeMerge`, where a similar swallowing of
the `CREATE` text into the `MERGE` pattern looks likely by code read but was
not separately reproduced — this page and the guard below both treat it as
unsafe on the same conservative basis Eshu applies to the proven two-clause
case.

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

This is a textual scan, not a Cypher parser, and its coverage is narrower than
"anywhere in the tree": it resolves a `+` chain of string literals and
package-level (not function-local) `const`/`var` identifiers defined in the
same file, but it cannot see `fmt.Sprintf` or other runtime template assembly,
a cross-file or cross-package identifier, `+=`, a `const`/`var` whose own value
is itself a concatenation, an unresolvable concatenation leaf positioned
before the literal fragments, or a `;` inside a Cypher comment or a quoted
string property value (the statement-boundary split has no comment/string
awareness). See the doc comments in `merge_then_create_repo_scan_test.go` for
the complete, current list.

No-Observability-Change: this entry and its guard are documentation and a
build-time static check only. No runtime metric, span, log field, queue
stage, worker knob, or graph-write route changes.
