# NornicDB Order-Limit Pitfalls

Cypher **ordered-page** pitfalls — how `ORDER BY` plus `LIMIT` on an indexed
property is planned on the pinned NornicDB build, and what a keyset page must
carry to be served from the ordered index rather than a label scan. Split out
as its own page rather than added to
[NornicDB Query-Shape Pitfalls](nornicdb-query-pitfalls.md) because that page
is grandfathered at 851 lines above the 500-line cap and must not grow; see that page
for label-disjunction, union, arrow-head, and multi-clause read pitfalls,
[NornicDB Write-Shape Pitfalls](nornicdb-write-shape-pitfalls.md) for write
statements, and [NornicDB Behavior and Pitfalls](nornicdb-pitfalls.md) for
storage, schema, and constraint behavior.

Use it to avoid rediscovering the same failure shape. Still check the current
NornicDB source before patching.

## Pitfall: `ORDER BY` Plus `LIMIT` Without A Range Predicate On The Sort Key

### Observed shape

Measured on the `fix-490-a427a468@sha256:eb69530f…` image (the current
compose default; the same shape was also timed on `v1.3.3@sha256:efcb65ae…`)
with 150,000 `CloudResource` nodes, Eshu's full NornicDB schema applied before
seeding (uid uniqueness constraint plus the separate
`nornicdb_cloud_resource_uid_lookup` index), one shape per fresh container, a
unique parameter per run to defeat the result cache:

```cypher
-- 29.5s to 45.9s for 500 rows: scans the label, then a top-k whose cost grows
-- with LIMIT (k=5: 1.7s, k=50: 3.7s, k=500: 17s, k=5000: >180s on an
-- unindexed store; ORDER BY with no LIMIT at all is 1.8s):
MATCH (n:CloudResource) RETURN <projection> ORDER BY n.uid LIMIT $limit

-- 0.08s to 0.35s for the same 500 rows, same store:
MATCH (n:CloudResource) WHERE n.uid > $after_uid RETURN <projection> ORDER BY n.uid LIMIT $limit
-- with $after_uid = '' for the first page; the next page ($after_uid = last
-- uid) reads in 0.13s to 0.41s.
```

Through the production `Neo4jReader` (10s graph-read deadline) the unfenced
first page fails with `graph query exceeded its deadline`; the fenced one
returns in 1.4s to 1.7s cold and 0.08s to 0.35s with a fresh parameter. The engine's ordered-window paths
(`tryCollectNodesFromPropertyIndexNotNullOrderLimit` and the
`tryCollectNodesFromPropertyIndexOrderLimit` fallback in `pkg/cypher/match.go`)
were read but not traced for the unfenced shape, so the slow plan is inferred
from timing, not from the source.

Two adjacent findings from the same measurement, both source-confirmed in the
pinned build:

- A uniqueness constraint creates no index: after
  `CREATE CONSTRAINT … REQUIRE n.uid IS UNIQUE`, `SHOW INDEXES` is empty, and
  every uid read — point lookup, `IN $uids`, or keyset page — is a label scan
  (1.3s to 1.8s at 150k) until the separate `nornicdb_<label>_uid_lookup`
  index from `graph.nornicDBUIDLookupIndexes` exists. A hand-seeded proof
  store must apply that index, or it measures the wrong plan.
- `CREATE INDEX … IF NOT EXISTS` on an index that already exists re-runs the
  index backfill (`pkg/cypher/schema.go`, `backfillPropertyIndex` runs after
  the no-op `AddPropertyIndex`) and `PropertyIndexInsert` appends node ids
  without deduplication (`pkg/storage/schema.go`). On a populated 150k store a
  second schema application took about five minutes and afterwards every
  index-driven read returned each row twice (500 rows, 250 distinct); a 5k
  store did not reproduce it. Neo4j treats `IF NOT EXISTS` on an existing index
  as a no-op (`IfExistsDoNothing`,
  `community/cypher/.../SchemaCommandConverter.scala`). Eshu applies the graph
  schema from `bootstrap-index`, `bootstrap-data-plane`, and the local
  supervisor, so a re-bootstrap of a populated store is the exposure.

### Eshu implications

The API and MCP startup owner-ledger backfill
(`go/internal/query/cloud_resource_owner_backfill.go`) shipped its first page
as the unfenced shape and its continuation pages as the fenced one; on a large
graph without the `cloud_resource_owner:v1` marker the first page could not
finish inside the deadline and startup failed with `wire api failed` (#6842).
Every page now carries `WHERE n.uid > $after_uid`, with `$after_uid = ''` on
the first page. Do not write a keyset page whose first page omits the range
predicate on the sort key; the predicate is what selects the ordered-index
plan, not an optimisation of the later pages.

### Validation

`TestCloudResourceOwnerBackfillerFirstPageMeetsDeadlineAtScale`
(`go/internal/query/cloud_resource_owner_backfill_scale_live_test.go`, opt-in
via `ESHU_CLOUD_RESOURCE_BACKFILL_NORNICDB_SCALE_LIVE=1` against a disposable
container) seeds 150k nodes behind the production schema and drives the
production `Backfill` entry point through the production reader policy. It
failed on the deadline against the pre-fix code and passes against the fix on
the same seeded store. Its reuse path deliberately never re-applies the schema.
