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

A statement that opens with a node `MERGE`, adds a second `MERGE`, and then
adds a `CREATE` clause in the SAME statement silently drops BOTH the second
`MERGE` and the `CREATE`. No error is returned. The canonical repro:

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
self-reports version `v1.2.1` (the version string baked into that commit,
which is 9 commits past the upstream `v1.2.1` tag itself, a different commit,
`66755bfba882`). Also reproduced on a source build of Eshu's Helm chart pin's
tag commit, `nornicdb-cpu-bge` `v1.2.3` tag commit `d9b76ae82334`
(self-reports `1.2.2`) — the published chart image itself (a separate Linux
build of that same source) has not been run. Neo4j's correct handling of the
shape is proven from its source, not from a live run.

Upstream fixed this in commit `ce8a76a4` on NornicDB `main`, 2026-09-13 — not
yet in any release tag. A live run of that commit on a fresh database wrote 2
nodes and 1 relationship for both statement forms. Neither Eshu pin includes
the fix: not the `docker-compose.yaml` pin (`3722b483c02c`), and not the Helm
chart's `v1.2.3` tag commit (`d9b76ae82334`). NornicDB `main` commit
`0be4aaa5` doesn't include it either. Keep this guard until both Eshu pins
move to or past `ce8a76a4`.

### Root cause

In the affected NornicDB source, a `MERGE`-led statement with a second clause
such as another `MERGE`, `OPTIONAL MATCH`, `WITH`, or `WHERE` routes to
`executeMultipleMerges`. Only the two-`MERGE` form above was reproduced live;
the `OPTIONAL MATCH`/`WITH`/`WHERE` variants take the same
`executeMultipleMerges` path by code read, not separately reproduced. Its
splitter (`splitMultipleMerges`,
`pkg/cypher/merge.go`) does not treat `CREATE` as a clause boundary: the
trailing `CREATE` text is glued onto the second `MERGE`'s segment and parsed
as though it were part of that `MERGE`'s own pattern, and the segment loop has
no `CREATE` branch at all. Neither the second node `MERGE` nor the `CREATE`
executes as a result.

A lone `MERGE (n) CREATE ...` with no second `MERGE`/`OPTIONAL MATCH`/`WITH`/
`WHERE` takes a different path, `executeMerge`, and this is now reproduced
live too (on the compose pin and on NornicDB `main` commit `0be4aaa5`):

```cypher
-- BROKEN: writes ONE node and no relationship. No error.
MERGE (s:Workload {id:'c'}) CREATE (s)-[:DEPENDS_ON]->(:Workload {id:'d'})
```

The written node's `id` property is not `'c'` — it holds the rest of the
statement's literal text, `c'}) CREATE (s)-[:DEPENDS_ON]->(:Workload {id:'d`,
because the `CREATE` text is swallowed into the `MERGE` pattern's own
property parsing rather than starting a new clause. The chart's `v1.2.3` was
not run for this lone-`MERGE` form.

### Eshu implications

Never write a node `MERGE` followed by `CREATE` in one statement. For Eshu
writers, the only acceptable fix is a relationship `MERGE` in place of the
`CREATE`:

```cypher
MERGE (s) MERGE (t) MERGE (s)-[:DEPENDS_ON]->(t)
```

This avoids the NornicDB drop AND keeps the statement idempotent: retrying or
replaying it matches the existing relationship instead of creating a
duplicate, which is this package's own invariant for every canonical writer
(`go/internal/storage/cypher/AGENTS.md`, "no unconditional CREATE").

Three other shapes also avoid the NornicDB drop, but are NOT acceptable on
Eshu write paths, because each still runs an unconditional `CREATE` that
duplicates the relationship on retry or replay:

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
same file, but it cannot see a cross-file or cross-package identifier, `+=`,
or an unresolvable concatenation leaf positioned before the literal
fragments. Three related limits are narrower than they sound: a `fmt.Sprintf`
format string that alone holds the whole shape IS caught by the per-literal
scan -- only a shape assembled across the format string AND its arguments is
invisible; a `const`/`var` whose own declared value is itself a concatenation
IS folded and scanned at its own declaration -- it is missed only when used
as an operand inside ANOTHER `+` chain; and a `;` inside a Cypher comment or a
quoted string property value hides a violation only when it falls between the
LAST `MERGE (` and the `CREATE (` -- a `;` earlier in the statement (for
example inside the first `MERGE`'s own properties) is still caught. See the
doc comments in `merge_then_create_repo_scan_test.go` for the complete,
current list.

No-Observability-Change: this entry and its guard are documentation and a
build-time static check only. No runtime metric, span, log field, queue
stage, worker knob, or graph-write route changes.
