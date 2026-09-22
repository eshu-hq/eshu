# #6915 upstream half: bare-backend ORDER BY shim

This note records the standalone reproduction for the upstream half of #6915.
The Eshu hardening is in [6915-complexity-resort.md](6915-complexity-resort.md).
This shim uses no Eshu runtime. It seeds the same 12-function graph into two
fresh Bolt endpoints, runs the same statements on each, and diffs the results
row by row.

## Backends

| Backend | Image (by digest) | Source |
| --- | --- | --- |
| NornicDB (Eshu pin) | `ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-490-a427a468@sha256:eb69530fa2951d74d89ed9df947aeea10beb0c5e2f2ede4c0080e09fe78aa555` | orneryd/NornicDB `main` at `a427a46815c607d0801331f4975e26cc941d125a` (2026-09-20) |
| Neo4j | `neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f` | same digest as the #6782 oracle leg |

`git log a427a468..origin/main` on orneryd/NornicDB (at `3f997e04`, 2026-09-22)
lists five commits, all in storage or search. None touches Cypher, so for this
surface the pin is the same as current upstream `main`.

Both images ran natively (arm64) on one host:

```bash
docker run -d --name t6915-nornic -p 127.0.0.1:19687:7687 \
  -e NORNICDB_NO_AUTH=true -e NORNICDB_DATA_DIR=/data \
  -e NORNICDB_ASYNC_WRITES_ENABLED=false -e NORNICDB_HEIMDALL_ENABLED=false \
  -e NORNICDB_QDRANT_GRPC_ENABLED=false -e NORNICDB_EMBEDDING_ENABLED=false \
  -e NORNICDB_SEARCH_BM25_ENABLED=false -e NORNICDB_SEARCH_VECTOR_ENABLED=false \
  ghcr.io/eshu-hq/nornicdb-amd64-cpu@sha256:eb69530fa2951d74d89ed9df947aeea10beb0c5e2f2ede4c0080e09fe78aa555
docker run -d --name t6915-neo4j -p 127.0.0.1:19787:7687 -e NEO4J_AUTH=none \
  neo4j@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f
uv run --with neo4j python docs/internal/evidence/6915-orderby-shim.py out.json
```

## Data

The script is [6915-orderby-shim.py](6915-orderby-shim.py). The full statement
list and both backends' rows from run 2 are in
[6915-orderby-shim-results.json](6915-orderby-shim-results.json).

- 12 `:Function` nodes, each `CONTAINS`-linked from one `:File`, which a
  `:Repository` reaches by `REPO_CONTAINS`. Complexities 9, 7×4, 4×4, 2×2, 1
  give tie groups. Two nodes share the name `Error` (complexities 7 and 4), so
  `e.id` is needed to break one tie. Every `(complexity, name, id)` tuple is
  distinct, so the complete order is fully determined.
- 3 source `:Repository` nodes (`repo-a`, `repo-b`, `repo-c`), each with four
  `DEPLOYS_FROM` edges to `:Target` nodes. One target per source has only
  `uid`, so `coalesce(t.id, t.uid)` matters.
- Nodes are inserted in scrambled order and the sources in reverse order, so
  storage order is not any requested sort order.

## Results

I ran the shim 9 times against the same two containers (runs 2 through 10;
run 1 predates the bisect statements). Neo4j returned identical rows on every
run. NornicDB returned one stable output per statement on every run, with one
exception: B8.

| Query | Sort keys | LIMIT | NornicDB vs Neo4j (9 runs) |
| --- | --- | --- | --- |
| `F1_exact_limit` | `complexity DESC, e.name, e.id` | yes | differ: same rows, other order |
| `F1_exact_nolimit` | `complexity DESC, e.name, e.id` | no | differ: same rows, other order |
| `F2_aliases_limit` | `complexity DESC, name, id` | yes | match |
| `F2_aliases_nolimit` | `complexity DESC, name, id` | no | match |
| `F3_raw_limit` | `coalesce(e.cyclomatic_complexity, 0) DESC, e.name, e.id` | yes | differ: **different rows** |
| `F3_raw_nolimit` | `coalesce(e.cyclomatic_complexity, 0) DESC, e.name, e.id` | no | differ: same rows, other order |
| `F4_nooptional_exact_limit` | `complexity DESC, e.name, e.id` | yes | match |
| `F4_nooptional_exact_nolimit` | `complexity DESC, e.name, e.id` | no | match |
| `F5_nooptional_aliases_limit` | `complexity DESC, name, id` | yes | match |
| `D1_exact_limit` | `s.id, coalesce(t.id, t.uid)` | yes | differ: **different rows** |
| `D1_exact_nolimit` | `s.id, coalesce(t.id, t.uid)` | no | differ: same rows, other order |
| `D2_aliases_limit` | `source_id, target_id` | yes | match |
| `D2_aliases_nolimit` | `source_id, target_id` | no | match |
| `D3_rawid_nolimit` | `s.id, t.id` | no | differ: same rows, other order |
| `B1_node_only_rawkey` | `e.name, e.id` | no | match |
| `B2_rel_pattern_rawkey` | `e.name, e.id` | no | differ: same rows, other order |
| `B3_optional_nowhere_rawkey` | `e.name, e.id` | no | differ: same rows, other order |
| `B4_node_where_rawkey` | `e.name, e.id` | no | match |
| `B5_rel_pattern_projected_same_expr` | `e.name, e.id` | no | match |
| `B6_rel_pattern_single_rawkey` | `e.name` | no | differ: same rows, other order |
| `B7_rel_pattern_single_rawkey_desc` | `e.cyclomatic_complexity DESC, e.id` | no | differ: same rows, other order |
| `B8_rel_pattern_return_node_prop_key` | `e.name, e.id` | no | match 3/9 |
| `B9_with_then_return` | `WITH e ORDER BY e.name, e.id` | no | differ: same rows, other order |

F1 and D1 are the two allowlisted production statements, with the projection
trimmed to the columns the sort touches. F2 and D2 are the same statements
with every sort key rewritten as a projected alias.

### F1_exact_limit (Function top-N, allowlisted shape)

```cypher
MATCH (e:Function) OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository) WHERE coalesce(e.cyclomatic_complexity, 0) > 0 RETURN e.id AS id, e.name AS name, coalesce(e.cyclomatic_complexity, 0) AS complexity ORDER BY complexity DESC, e.name, e.id LIMIT 5
```

| # | NornicDB | Neo4j |
| --- | --- | --- |
| 0 | `fn-02, Min, 9` | `fn-02, Min, 9` |
| 1 | `fn-09, Worker, 7` | `fn-03, Apply, 7` **≠** |
| 2 | `fn-03, Apply, 7` | `fn-01, Divide, 7` **≠** |
| 3 | `fn-01, Divide, 7` | `fn-06, Error, 7` **≠** |
| 4 | `fn-06, Error, 7` | `fn-09, Worker, 7` **≠** |

NornicDB applies `complexity DESC` and then leaves each tie group in insertion
order (Worker, Apply, Divide, Error). That holds at 12 rows. NornicDB sorts with
Go's unstable `sort.Slice`, so a larger tie group can come back in some other
permutation. `e.name` and `e.id` have no effect even
though both expressions are projected (`AS name`, `AS id`).

### F3_raw_limit (same keys, first key written as an expression)

```cypher
MATCH (e:Function) OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository) WHERE coalesce(e.cyclomatic_complexity, 0) > 0 RETURN e.id AS id, e.name AS name, coalesce(e.cyclomatic_complexity, 0) AS complexity ORDER BY coalesce(e.cyclomatic_complexity, 0) DESC, e.name, e.id LIMIT 5
```

| # | NornicDB | Neo4j |
| --- | --- | --- |
| 0 | `fn-09, Worker, 7` | `fn-02, Min, 9` **≠** |
| 1 | `fn-03, Apply, 7` | `fn-03, Apply, 7` |
| 2 | `fn-11, start, 4` | `fn-01, Divide, 7` **≠** |
| 3 | `fn-01, Divide, 7` | `fn-06, Error, 7` **≠** |
| 4 | `fn-07, Error, 4` | `fn-09, Worker, 7` **≠** |

No key has any effect. NornicDB returns the first five rows in storage order,
so the top-5 leaves out the highest-complexity function (`Min`, 9).

### D1_exact_limit (DEPLOYS_FROM, allowlisted shape)

```cypher
MATCH (s:Repository)-[r:DEPLOYS_FROM]->(t) RETURN coalesce(s.id, s.uid, s.name, s.path) AS source_id, coalesce(t.id, t.uid, t.name, t.path) AS target_id ORDER BY s.id, coalesce(t.id, t.uid) LIMIT 6
```

| # | NornicDB | Neo4j |
| --- | --- | --- |
| 0 | `repo-c, tgt-2` | `repo-a, tgt-3` **≠** |
| 1 | `repo-c, tgt-0` | `repo-a, tgt-5` **≠** |
| 2 | `repo-c, tgt-8` | `repo-a, tgt-6` **≠** |
| 3 | `repo-c, tgt-1b` | `repo-a, tgt-9` **≠** |
| 4 | `repo-b, tgt-1` | `repo-b, tgt-0b` **≠** |
| 5 | `repo-b, tgt-7` | `repo-b, tgt-1` **≠** |

Neither key has any effect. NornicDB returns storage order, where `repo-c` was
created first, so the window contains none of `repo-a`'s rows.

### Bisect rows (no LIMIT)

- B1 and B4 (`MATCH (e:Function) RETURN e.id AS id ORDER BY e.name, e.id`,
  with and without `WHERE`) match. A single-node `MATCH` sorts correctly when
  every key is a plain property of the matched node. F4 is the F1 statement
  without the `OPTIONAL MATCH`, and it matches too, but for a different
  reason: its alias key sends it down the post-projection path, where `e.name`
  and `e.id` resolve because they are the exact text of `RETURN` expressions.
- B6 (`MATCH (f:File)-[:CONTAINS]->(e:Function) RETURN e.id AS id ORDER BY
  e.name`): once the `MATCH` contains a relationship pattern, a single
  non-projected key is ignored and rows come back in storage order.
- B2 (same pattern, `ORDER BY e.name, e.id`) comes back ordered by `e.id`
  alone (`fn-01` … `fn-12`). `e.id` is also a projected expression (`e.id AS
  id`), so NornicDB honours it and drops `e.name`, which is not projected. B7
  (`ORDER BY e.cyclomatic_complexity DESC, e.id`) behaves the same way.
- B5 (same pattern, both sort expressions projected) matches.
- B3 (`OPTIONAL MATCH` without `WHERE`, `ORDER BY e.name, e.id`) is in storage
  order. After `OPTIONAL MATCH`, even a projected expression (`e.id`) is
  ignored. Only aliases work there, which F1 against F2 also shows.
- B8 (`RETURN e ORDER BY e.name, e.id`) sorts by `e.name`. The order inside
  the `Error` tie (`fn-06`/`fn-07`) changes from run to run (3 of 9 runs
  match), so the `e.id` tiebreak is not applied.
- B9 (`WITH e ORDER BY e.name, e.id RETURN e.id AS id`) comes back in storage
  order. It is recorded here but left out of the upstream claim, because
  row order carried across a `WITH` into a later `RETURN` is a weaker
  guarantee than a `RETURN ... ORDER BY`.

## Neo4j semantics in source

Read at neo4j/neo4j `eccd584a64d468af3daeab421478fe78567c518f` (2026-07-02),
paths under `community/cypher/`:

- Scope: `front-end/ast/src/main/scala/org/neo4j/cypher/internal/ast/Clause.scala`,
  `ProjectionClause` semantic check. `canSeePreviousScope` is true when the
  `RETURN`/`WITH` has no aggregate and no `DISTINCT`. `ORDER BY` and `WHERE`
  are then checked in a child of the incoming scope, so they "can see both
  variables from before the WITH and variables introduced by the WITH" (source
  comment). With `DISTINCT` or an aggregate, a pre-projection variable raises
  "In a WITH/RETURN with DISTINCT or an aggregation, it is not possible to
  access variables declared before the WITH/RETURN" (`SemanticError.scala`).
  None of the shim statements aggregate or use `DISTINCT`, so `e.name`, `s.id`
  and so on are valid sort keys.
- Key precedence: `interpreted-runtime/.../InterpretedExecutionContextOrdering.scala`,
  `asComparator`, maps each `ColumnOrder` to a comparator and folds them with
  `reduceLeft(... a.thenComparing(b))`. Each later key only breaks ties left by
  the earlier ones, and `Ascending`/`Descending` apply per key.
- LIMIT: `cypher-planner/.../steps/LogicalPlanProducer.scala`, `planTop`, plans
  `ORDER BY ... LIMIT` as `Top` with the full `sortColumns`. The runtime
  `TopNPipe` (`interpreted-runtime/.../pipes/TopPipe.scala`, "used when a query
  does a ORDER BY ... LIMIT query") keeps the top N rows under that same
  multi-key comparator, so the window is the first N rows of the full sort.

The observed Neo4j rows agree with this on every statement in both scripts.

## Reading

Neo4j orders by every sort key, in the order given, whether or not the key's
expression appears in `RETURN`. NornicDB at `a427a468` does that only for a
single-node `MATCH` whose keys are all plain `var.prop` terms, because it sorts
those nodes before projection. Every other statement is sorted after
projection. After a single-node `MATCH` or a relationship pattern, a sort key
then only takes effect when it is an alias or has the same text as a projected
expression. After `OPTIONAL MATCH`, only aliases take effect. Every other key is silently
ignored. With `LIMIT`, the window is then taken from a partly sorted or
unsorted row stream, which is why F3 and D1 return the wrong rows and not only
the wrong order.

For Eshu, rewriting every sort key as a projected alias (F2, D2) gives the
Neo4j result on this pin. The upstream report asks for the general fix.

## Minimal repro filed upstream

The upstream report cuts the graph down to one `CREATE` statement: the same
12 functions in the same order, without the `DEPLOYS_FROM` part. It runs nine
queries (relationship pattern, bare `MATCH` control, `OPTIONAL MATCH`,
alias control, function key). The script is
[6915-orderby-minimal.py](6915-orderby-minimal.py) and the first run's rows
are in [6915-orderby-minimal-results.json](6915-orderby-minimal-results.json).
Three fresh-database runs on both backends
(neo4j Python driver 6.3.1) gave identical rows each time. The NornicDB rows
for the queries shared with the shim (B6, B2, B5, B1, B3, F1, F2, F3) are the
same rows as in the shim runs.

It adds two queries the shim does not have:

- `MATCH (e:Function) RETURN e.id AS id ORDER BY
  coalesce(e.cyclomatic_complexity, 0) DESC, e.id`: a bare `MATCH` with a
  function key. NornicDB drops the `coalesce` key and sorts by `e.id` only.
- `MATCH (f:File)-[:CONTAINS]->(e:Function) RETURN e ORDER BY e.id`: NornicDB
  returned a different order on each of 3 fresh seeds, while Neo4j returned
  `fn-01` … `fn-12` every time. This is the B8 tiebreak behaviour in isolation.

## Where NornicDB drops the keys

Root-Cause Evidence: every observed NornicDB row order in the shim and the
minimal repro follows from the code at `a427a468` below, read at that commit in
a local orneryd/NornicDB checkout. When every key is dropped, rows stay in
storage order. Partly
resolved keys sort by the key that survives. The two queries this reading
predicted before they were run (bare `MATCH` with a `coalesce` key, and
`RETURN e ORDER BY e.id`) came back as predicted.

- `parseOrderBySpecsWithResolver` in `pkg/cypher/match_rows.go` resolves
  post-projection sort keys. It splits each term with `strings.Fields` and
  keeps the first token as the key, so a function call containing a space
  never matches. It then tries an alias, then an optional resolver that only
  accepts the exact text of a `RETURN` expression, then `var.prop` where `var`
  is itself a returned column. Any other key hits a `continue` and is dropped
  without an error.
- For a bare single-node `MATCH`, `parseNodeOrderSpecs` sorts the nodes before
  projection when every key is `var.prop` (B1, B4). It returns nil on any
  other key, such as an alias. The statement then takes the post-projection
  path with the `RETURN`-item resolver. F4 matches on that path because
  `e.name` and `e.id` are the exact text of its `RETURN` expressions.
- The relationship-pattern path in `match.go` always sorts after projection,
  using the `RETURN`-item resolver (B2, B6, B7).
- The `OPTIONAL MATCH` paths (`optional_match_traversal.go`, `clauses.go`) call
  `orderResultRows`, which passes no resolver (B3, F1, F3).
- For a node value, the same file maps a `.id` path segment to the internal
  node ID, not the `id` property (B8).

Upstream #449 and #459 hit the same function, but only on a bare `MATCH`. The
relationship-pattern and `OPTIONAL MATCH` paths, the whitespace split and the
`.id` mapping are not covered by either, so this was filed as a new issue:
[orneryd/NornicDB#500](https://github.com/orneryd/NornicDB/issues/500).
