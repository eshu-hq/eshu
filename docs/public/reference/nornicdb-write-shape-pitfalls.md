# NornicDB Write-Shape Pitfalls

Cypher **write-statement-shape** pitfalls — how clause ordering inside a single
write statement (MERGE, CREATE, SET, DELETE) decides whether NornicDB executes
every clause, and how bulk `UNWIND ... CREATE` bounds and batch sizes decide
how many nodes actually get written. Split out as its own page rather than added to
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
every string literal, and fails if any of them contain a `MERGE (` pattern —
plain or named-path (`MERGE p = (`, `MERGE p=(`) — followed later in the same
statement by a real `CREATE (` clause, itself plain or named-path
(`CREATE p = (`, `CREATE p=(`). Both accept a backtick-quoted path name too.
`MERGE (n) ON CREATE SET ...` and `ON MATCH SET ...` do not trip it — `CREATE`
there is followed by `SET`, not `(` and not a `path =` binding, so it never
matches the clause pattern — and a `CREATE` in a different statement (past a
`;`, or in a separate string constant) does not either. See
`merge_then_create_unit_test.go` for the unit-level proof of both exclusions
and the scan itself; `merge_then_create_repo_scan_test.go` holds the guard's
own repo-wide test and the scanning code it exercises.

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

## Pitfall: An Uncorrelated `EXISTS {}` Guard Scans The Whole Label Per Row And Never Matches The Row Value

### Observed shape

On NornicDB v1.3.3 an existence subquery whose pattern does not name an outer
variable is evaluated by loading every node with the label and filtering on
properties in memory, once per outer row. A property taken from an `UNWIND`
row (`row.path`) is not matched in that path, so the guard filters nothing:

```cypher
-- BROKEN: full File scan per row, and existing files still pass the guard
UNWIND $rows AS row
MATCH (r:Repository {id: row.repo_id})
WHERE NOT EXISTS { MATCH (:File {path: row.path}) }
MERGE (f:File {path: row.path}) ...

-- WORKS: correlated lookup through the File.path index
UNWIND $rows AS row
OPTIONAL MATCH (existing:File {path: row.path})
WITH row, existing
WHERE existing IS NULL
MATCH (r:Repository {id: row.repo_id})
MERGE (f:File {path: row.path}) ...
```

The source path is `pkg/cypher/executor_mutations.go` `checkSubqueryMatch`,
which calls `loadNodesWithTemporalViewport(ctx, labels)` for this shape. With
5 rows (3 existing paths), the broken guard took 0.46 s at 5,000 File nodes,
2.03 s at 20,000, and 5.88 s at 100,000, and re-stamped all 5 rows; the
correlated form took 0.003-0.090 s (0.009-0.013 s at 100,000 once warm) and wrote only the 2 missing rows.
On ops-qa the broken form hit the 300 s transaction timeout with 1-22 rows
(#6798). The `WITH ... WHERE existing IS NULL` filter is a bare null test, not
one of the `WITH`-attached `WHERE` shapes that
[NornicDB Query-Shape Pitfalls](nornicdb-query-pitfalls.md) records as ignored
or nulled; `TestCanonicalFileCreateMissingSkipsExistingFilesLive` proves it on
v1.3.3. This was not measured on Neo4j.

### Eshu implications

Never guard a write with `EXISTS {}`/`NOT EXISTS {}` over a pattern that only
references row values. Anchor the check on an indexed property with
`OPTIONAL MATCH` and filter on the bound variable. Evidence:
`docs/internal/evidence/6798-file-create-missing-index.md`.

## Pitfall: A Bare-Label `DETACH DELETE` Scans The Whole Store, Even When Nothing Matches

### Observed shape

On NornicDB v1.3.3, a `DETACH DELETE` whose `MATCH` is a label scan filtered by
property predicates costs time that grows with the total number of nodes in the
store, not with the label or the number of matching rows. A bounded retract that
matches zero rows still pays it:

```cypher
-- SLOW when it matches nothing: ~8.5-9.3 s against a 1M-node store.
MATCH (n:Function)
WHERE n.repo_id = $repo_id AND n.evidence_source = 'projector/canonical' AND n.generation_id <> $generation_id
WITH n ORDER BY elementId(n) LIMIT $batch
DETACH DELETE n
RETURN count(n) AS __drained
```

The same `MATCH`, `WHERE`, and `WITH ... LIMIT` as a read returned in 1-9 ms on
a local 1M-node store and in 0.10-0.17 s on ops-qa. Dropping `ORDER BY` does
not help and is unsafe: the bare-label `WITH n LIMIT k DETACH DELETE n` form
still deletes zero rows with no error on v1.3.3, as it did on v1.1.9. On ops-qa
(1.1M nodes) these retracts took 45-55 s each while deleting nothing (#6822).

### Safe shape

Probe once, and only run the delete when the probe finds a node:

```cypher
MATCH (n:Function)
WHERE n.repo_id = $repo_id AND n.evidence_source = 'projector/canonical' AND n.generation_id <> $generation_id
WITH n ORDER BY elementId(n) LIMIT 1
RETURN elementId(n) AS __id
```

When the probe returns a row, run the single-statement bounded drain loop shown
above until it drains zero rows. When it returns nothing, skip the drain. A
retract with nothing to delete then costs one read bounded by its own label
(1-9 ms for a small label on a local 1M-node store; 0.11 s and up on ops-qa)
instead of a whole-store scan. Probe once per statement, not per batch: the
probe scans its label, which took 0.11 s for `AtlantisProject`, 2.9-4.4 s for
`Directory`, and 31-50 s for `Function` on the 1.1M-node ops-qa store, so
repeating it would add that cost to every drain step. Keep the `WITH ... LIMIT`
before `RETURN`: `MATCH ... WHERE ... RETURN elementId(n) AS id LIMIT $batch`,
without the `WITH`, took 15-18 s on the ops-qa store.

Keep the delete in the single drain statement so its `WHERE` clause is
rechecked atomically. Do not split it into "read the element IDs, then
`DETACH DELETE` by ID". A node that another attempt refreshed to the current
generation between the read and the delete would be deleted, which is how a
stale projection attempt could remove freshly projected nodes. Rechecking the
predicate on an ID-matched node in the same statement (`MATCH (n) WHERE
elementId(n) = __id WITH n WHERE ...`) is correct but costs a whole-store scan:
187-292 s for 20-30 IDs against a 1M-node store. Deleting by an element ID that
no longer exists costs a whole-store scan too.

`BuildBoundedRetractProbeCypher` in `go/internal/storage/cypher` emits the probe,
and the NornicDB phase-group executor probes once before every bare-label
bounded drain. Relationship-anchored retracts, such as
`(r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)`, are bounded by the
anchor (median 0.01 s on ops-qa) and are not probed.

When you probe this, give each run a unique parameter value. NornicDB serves a
repeated identical read from its result cache, so a second run can look fast
without the plan being fast.

## Pitfall: `UNWIND range(0, $count - 1)` Creates One Node, And One Statement Cannot Create 50,000

### Observed shape

A parameter expression as the upper bound of `range()` inside `UNWIND` yields a
single element, so a bulk `CREATE` writes exactly one node and reports success:

```cypher
-- BROKEN: 1 node, no error. $count = 10.
UNWIND range(0, $count - 1) AS i CREATE (n:Probe {id: 'p' + toString(i)})
```

Measured on `nornicdb-cpu-bge:v1.3.3` (`sha256:81cedbf4…`) through the HTTP
transaction endpoint, one throwaway label per shape, 10 requested nodes unless
noted:

| Shape | Nodes created |
| --- | ---: |
| `range(0, 9)` (literal bound) | 10 |
| `range(0, $count - 1)`, `$count = 10` | **1** |
| `range(0, $count - 1)` plus `$label` in the properties | **1** |
| `range(0, $last)`, `$last = 9` (bound computed by the caller) | 10 |
| `UNWIND $rows AS row CREATE (...)`, 3 parameter rows | 3 |

The read-API latency gate (`go/cmd/read-api-latency-gate`, #6797) seeded its
infra graph with the broken shape through the Bolt driver and counted 1
`K8sResource` node instead of 150,000, so every `infra/resources` latency it
had printed was read against an almost empty graph. This was not measured on
Neo4j or on other NornicDB builds.

A second limit shows up as soon as the bound is fixed. A single statement that
creates too many nodes fails at commit, again with a misleading error class:

| Nodes in one `UNWIND range($first, $last) ... CREATE` | Result |
| ---: | --- |
| 5,000 | 0.85 s |
| 20,000 | 3.3 s |
| 50,000 | `Neo.ClientError.Statement.SyntaxError: ... Txn is too big to fit into one request` |
| 150,000 | same error |

A related cost shows on labels that carry a `uid` `UNIQUE` constraint: writing
50,000 parameter-row nodes over 150,000 unconstrained nodes took 6.6 s in
batches of 250, 14.3 s in batches of 1,000 and 91.7 s in batches of 5,000
(fresh container per size), and one 50,000-row statement had not finished after
15 minutes. Per-row cost grows with batch size, so bound the batch.

### Eshu implications

Compute the `range()` bound in the caller and pass it as its own parameter
(`range($first, $last)`), never as an expression on a parameter. Batch bulk
`CREATE` statements (10,000 unconstrained nodes, 250 constrained rows in the
gate), and read the per-label counts back after any bulk seed: a seed that
silently produces one node is indistinguishable from success without a count.
The gate does this in `VerifyGraphNodeCounts`.

## Pitfall: A `CASE` Expression Inside A `CREATE` Property Map Is Stored As Literal Text

### Observed shape

A Cypher expression used as a property value inside `CREATE (n:Label {...})`
is not evaluated. NornicDB substitutes the loop variable into the expression
text and stores the result as a STRING:

```cypher
UNWIND range(0, 2) AS i
CREATE (n:Probe {provider: CASE i % 3 WHEN 0 THEN 'aws' WHEN 1 THEN 'gcp' ELSE 'azure' END})
-- n.provider = "CASE 0 % 3 WHEN 0 THEN 'aws' WHEN 1 THEN 'gcp' ELSE 'azure' END"
-- (and "CASE 1 % 3 ...", "CASE 2 % 3 ...": one distinct string per node)
```

Seen on `nornicdb-cpu-bge:v1.3.3` (`sha256:81cedbf4…`) through the Bolt driver:
the read-API latency gate (`go/cmd/read-api-latency-gate`, #6797) seeded 150,000
nodes per label this way and `count(DISTINCT n.provider)` returned 150,000, so
`/api/v0/infra/resources/count` grouped 150,000 provider buckets instead of 3 and
returned a response body with a key per node. No error is raised, and a count of
the nodes looks right. This was not measured on Neo4j.

### Eshu implications

Compute property values in the caller and send them as parameters
(`UNWIND $rows AS row CREATE (n:Label {provider: row.provider})`). On
unconstrained labels that shape took 0.14s for 2,000 rows, 0.51s for 10,000 and
0.98s for 20,000, with the expected 3 distinct providers. After a bulk seed, read
back a property's distinct count, not only the node count.
## Pitfall: A Missing `UNWIND` Row Key Is Stored As Its Expression Text

### Observed shape

On the pinned `v1.3.3` build, a property read from an `UNWIND` row map whose
key is absent does not evaluate to `null`. The statement stores the literal
expression text instead:

```cypher
-- rows = [{a: 'x', b: 'y'}]   (no source_tool key)
UNWIND $rows AS row
MATCH (a {id: row.a}) MATCH (b {id: row.b})
MERGE (a)-[rel:DEPENDS_ON]->(b)
SET rel.source_tool = row.source_tool
-- NornicDB: rel.source_tool = "row.source_tool", IS NOT NULL = true
-- Neo4j:    no source_tool property,               IS NOT NULL = false
```

The same statement with the key present and an explicit `nil` value behaves
like Neo4j for readers: NornicDB keeps a null-valued key, and
`rel.source_tool IS NOT NULL` is false on both backends. A missing top-level
`$param` evaluates to null as expected. Only the row-map key form is affected.
Measured on 2026-09-18 against
`timothyswt/nornicdb-cpu-bge:v1.3.3@sha256:81cedbf4...` beside
`neo4j:2026-community` (#6782).

### Eshu implications

A writer that builds its row maps conditionally ("add the key only when the
payload has a value") writes junk on NornicDB for every row without that key.
The B-7 golden corpus on Neo4j found this: package-consumption `DEPENDS_ON`
edges carried `source_tool = "row.source_tool"` on NornicDB, which
`get_repo_context` reported as a `source_tool_breakdown` entry, while the edge
was correctly unstamped on Neo4j. The repo-dependency edge writer now sends
every key its statement reads, `nil` when absent (`setOptionalRowString` in
`go/internal/storage/cypher/edge_writer_payload.go`). Apply the same rule to
any `UNWIND $rows` writer: a row map must carry every `row.<key>` its statement
references.

### Validation

`go test ./internal/storage/cypher -run
TestEdgeWriterRepoDependencyRowsCarryEveryReferencedKey -count=1` is the
static check for the repo-dependency routes. The live proof is
`TestLiveRepoDependencyWithoutSourceToolStaysUnstamped` (build tag
`live_nornicdb_answer_truth`, `ESHU_NEO4J_URI` plus
`ESHU_LIVE_GRAPH_BACKEND=nornicdb|neo4j`). It is RED on NornicDB with the old
conditional row map and GREEN on both backends with the fix. Other writers
have not been audited for this shape yet.
