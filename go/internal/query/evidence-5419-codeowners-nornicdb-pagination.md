# CODEOWNERS NornicDB pagination proof (#5419)

## Correctness theory

The original `GET /api/v0/codeowners/ownership` query represented its
three-column keyset suffix as one mixed `OR` predicate. The repository's pinned
image is `eshu-nornicdb-pr261:149245885258`; its startup log identifies
NornicDB 1.1.11 and its OCI revision is
`1492458852588c884c32f70d27ea2ee07086769c`. Against that backend, the original
no-cursor form silently returned zero rows even when
`after_order_index=-1` made the sentinel branch true.

Removing only the sentinel branch is insufficient. A scaled shim created one
repository with 10,000 `DECLARES_CODEOWNER` edges: 100 order indexes, 10
patterns per index, and 10 owners per pattern. For the cursor after
`(49, "p04", "@perf/o49-p04-team04")`, the exact lexicographic suffix contains
5,055 rows. Both the original mixed-OR query and a mixed-OR query without the
sentinel returned 201 rows, but the first returned row was from order index 50;
both silently omitted the five remaining owners and fifty later patterns at
order index 49. The three disjoint predicates returned branch counts
`[201, 50, 5]`; after global sorting and truncation, their first 201 rows were
exactly equal to the reference suffix:

1. `rel.order_index > $after_order_index`
2. `rel.order_index = $after_order_index AND rel.pattern > $after_pattern`
3. `rel.order_index = $after_order_index AND rel.pattern = $after_pattern AND team.ref > $after_ref`

The predicates are mutually exclusive and their union is exactly the
lexicographic suffix. This is an intentional correctness delta: the old shapes
returned false-empty or under-linked pages, while the new shape returns the
expected page. The retained 25-repository B-7 graph independently reproduced
the no-cursor failure: removing the cursor predicate returned the two expected
`DECLARES_CODEOWNER` edges.

## Performance Evidence

The scaled proof used the same 10,000-edge repository, cursor, 201-row
`limit+1` probe, storage state, and twenty warm Bolt executions for each shape:

| Shape | Calls | Median | Mean | Result |
| --- | ---: | ---: | ---: | --- |
| Old no-cursor mixed OR | 1 | 143.829 ms | 144.009 ms | 0 rows, incorrect |
| New no-cursor query | 1 | 114.707 ms | 115.114 ms | 201 rows, exact |
| Old cursor mixed OR | 1 | 179.884 ms | 180.320 ms | 201 rows, incorrect ordering suffix |
| New disjoint cursor queries | 3 | 359.723 ms | 360.532 ms | 201 rows, exact |

The correct cursor path is slower than the incorrect single query, but remains
at 3.61% of the existing 10-second request timeout on a deliberately large
single-repository declaration set. Each branch keeps the `Repository.id` index
anchor, deterministic order, and `LIMIT`. The API page maximum is 200; its
`limit+1` probe bounds each branch to 201 and the in-process merge to 603 rows
before it sorts and retains only 201. A no-cursor page still executes one query.

The scratch proof was run with an ephemeral, no-auth container and a temporary
Go/Bolt shim, then the container was removed:

```text
docker run -d --name codex-nornic-codeowners-proof-5419 -p 127.0.0.1::7687 eshu-nornicdb-pr261:149245885258 --address 0.0.0.0 --no-auth --headless --embedding-enabled=false
go run <scratch>/codeowners_nornic_proof.go bolt://127.0.0.1:<ephemeral-port>
SKIP_SEED=1 go run <scratch>/codeowners_nornic_proof.go bolt://127.0.0.1:<ephemeral-port>
```

The pinned source's hot-path cookbook recommends splitting cross-property OR
predicates into stable templates. NornicDB upstream `main` was also checked at
`0fbc577dcce1a588cb4ace1a2578605e64acd0ae`; its current cookbook retains that
guidance and identifies `TraversalStartSeedTopK` as the indexed start-node
traversal path. The pinned server does not expose its internal hot-path trace
through Bolt, so this proof does not claim that named flag. It treats the
generic traversal as a deliberate possible fallback: the query remains
repository-anchored and result-bounded, the isolated Neo4j profile proves the
`Repository.id` `NodeIndexSeek`, and the exact pinned-NornicDB runtime is
measured against 10,000 declarations in one repository.

No-Regression Evidence: focused tests prove one no-cursor query, three OR-free
cursor queries, disjoint cursor parameters, global merge order, truncation,
next-cursor selection, and fail-fast branch errors:
`go test ./internal/query -run 'Codeowners|CODEOWNERS' -count=1`. Rebuilt API,
MCP server, and golden-corpus gate binaries then ran the retained query phase;
`GET /api/v0/codeowners/ownership?repository_id=repository:r_8477a002&limit=50`
and `list_codeowners_ownership` each returned two rows, and the phase finished
with 201 pass, 0 required-fail, and 0 advisory-warn. The static/live query-plan
gate (`scripts/verify-query-plan-regression.sh`) also passed: the production
builder fingerprints, graph-call inventory, initial-page shape, all three
cursor branches, required schema, and isolated Neo4j PROFILE assertions are
registered together.

No-Observability-Change: the wire response, capability, truth envelope, route,
timeout, and runtime knobs are unchanged. The existing
`query.codeowners_ownership` handler span covers the whole bounded merge, and
each `GraphQuery.Run` continues through the graph adapter's existing
`neo4j.execute` dependency span. Branch failures remain request failures on the
same route and are not swallowed. Repository IDs, patterns, and owner refs are
not added to metrics, span attributes, or logs.

No retained credential, private repository identity, hostname, or machine path
is recorded in this evidence.
