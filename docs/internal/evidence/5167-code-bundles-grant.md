# #5167 — `POST /api/v0/code/bundles` leaves the pending ledger

`POST /api/v0/code/bundles` (MCP `search_registry_bundles`) leaves
`pendingRowFilteringRoutes` and joins the scoped-token allowlist. The same
change fixes a live ordering/`LIMIT` bug in its statement that every caller,
the shared key included, was exposed to.

## What changed

- A scoped caller sees only packages with `visibility = 'public'`, the gate the
  package-registry ecosystem browse route already applies. A package whose fact
  carries no `visibility` stays hidden. An empty grant returns an empty page
  with no graph call. The shared key and all-scope callers read the whole
  catalog, unchanged apart from the fix below.
- The read is now two statements. The anchor read returns the ordered page
  (`MATCH (p:Package) WHERE ... RETURN ... ORDER BY p.ecosystem,
  p.normalized_name, p.uid LIMIT $limit`); `registry.VersionCountsByPackageID`
  (the package-registry family's version-count statement, now exported and
  index-backed, see below) counts versions for that page and Go zero-fills.
- All five ledger promotion steps: `scopedCodeBundlesRoute` in
  `scopedHTTPRouteSupportsTenantFilter`, the `scopedTokenAdvertisedRoutes`
  entry (`scopedRouteGrantBound`), the OpenAPI `x-scoped-token-support` marker,
  the `403` response (already declared, kept), and removal from the ledger.

## The bug

Measured live on the compose pin (`nornicdb-amd64-cpu:fix-500-e022384c@sha256:74a8ed7b...`,
NornicDB 1.3.3, isolated container, bolt 17994): after
`OPTIONAL MATCH (p)-[:HAS_VERSION]->(v) WITH p, count(v) AS version_count`, the
pinned build silently ignores the trailing `ORDER BY` and `LIMIT`. `LIMIT 51`
over a 3,000-package ecosystem returned all 3,000 rows in storage order, so the
handler's `rows[:limit]` served an arbitrary page and `truncated` meant
nothing.

The zero-version row collapse that the pending-ledger comment and
`nornicdb-pitfalls.md` describe does **not** reproduce on this build: two
zero-version packages and one two-version package under the old statement
returned three rows, counts 0, 0, 2, each on its own id. The pitfall now says
so (`docs/public/reference/nornicdb-pitfalls.md`).

## Regression proof (RED then GREEN)

`TestLiveSearchBundlesHonoursOrderByAndLimit`,
`TestLiveSearchBundlesScopedCallerSeesOnlyPublicPackages`, and
`TestLiveSearchBundlesEveryPredicateFiltersRows`
(`go/internal/query/codequery/registry_bundles_live_test.go`,
`ESHU_PKG_REGISTRY_PROVE_LIVE=1`, `ESHU_NEO4J_URI`). The fixture is 30 packages
in a scrambled insertion order, visibility cycling public / private / absent,
version counts cycling 0 / 1 / 2. This build silently ignores an invalid
`WHERE` and returns every row, so each predicate is proven by live row
membership, not statement text.

RED, on the old statement (exit 1):

```text
--- FAIL: TestLiveSearchBundlesHonoursOrderByAndLimit
    anchor statement returned [30] rows, want exactly limit+1 = 6 (the catalog holds 30)
    bundles[1].package_id = "pkg:live5167b:07", want "pkg:live5167b:01" (name order)
    bundles[2].package_id = "pkg:live5167b:14", want "pkg:live5167b:02" (name order)
    bundles[3].package_id = "pkg:live5167b:21", want "pkg:live5167b:03" (name order)
    bundles[4].package_id = "pkg:live5167b:28", want "pkg:live5167b:04" (name order)
--- FAIL: TestLiveSearchBundlesScopedCallerSeesOnlyPublicPackages
    scoped caller saw 30 packages, want exactly the 10 public ones
```

GREEN, on the new statements (exit 0):

```text
--- PASS: TestLiveSearchBundlesHonoursOrderByAndLimit
--- PASS: TestLiveSearchBundlesScopedCallerSeesOnlyPublicPackages
--- PASS: TestLiveSearchBundlesEveryPredicateFiltersRows
```

Row-set results the GREEN run asserts: limit 5 returns exactly 6 anchor rows,
the first five by `(name, uid)`, `truncated = true`, `version_count` exact
including 0; a scoped caller at limit 200 receives exactly the 10 public rows
in order and none of the 20 private or visibility-absent ones; `query` /
`ecosystem` / `unique_only` / scoped-plus-query each return their expected
counts (30, 0, 10, 10, 0, 30, 4, 0).

Handler unit tests on the fake reader
(`registry_bundles_scope_test.go`): a scoped caller (repository-id grant and
scope-only grant) gets the `p.visibility = 'public'` term and no
`allowed_*` parameter; the shared key, an all-scope token, and no auth context
do not; an empty grant makes zero graph calls and returns a well-formed empty
page; the count read is bound to the returned page and not the `limit+1` probe
row; a package missing from the count result is zero-filled. The real auth
middleware test (`TestScopedTokenAdvertisedRoutesReachHandlerThroughRealAuthMiddleware`)
covers `POST /api/v0/code/bundles` through the advertised-routes map.

## Performance Evidence

This is a correctness and bounded-read fix, not a speedup. On the local
3,000-package fixture the new pair is slower than the old single statement,
because the old statement returned the whole catalog without sorting it.

Performance Evidence: backend NornicDB 1.3.3, compose pin above, isolated
container with the production Package/PackageVersion uid constraints and the
`package_ecosystem`, `package_normalized_name`, `package_registry`,
`package_namespace`, `package_visibility`, and `package_version_package_id`
indexes applied. Input: 3,000 `Package` nodes in one ecosystem (all
`visibility = public`), 2,250 with 1-3 `PackageVersion` nodes carrying
`package_id` plus a `HAS_VERSION` edge. This build has a server-side read
result cache keyed on statement text and params, so every measured run carries a
unique unused `$nonce` param (or a unique uid in the id list) and the shapes are
interleaved; timings measured this way are client bolt round trips, n = 15.

End to end with the final code (old single statement vs anchor + count):

| `limit` sent | old single statement | new anchor + count |
|---:|---|---|
| 51 (API default 50, plus the probe row) | 3,000 rows, p50 21.2 ms (min 20.0, max 38.5) | 51 rows, p50 57.3 ms (min 49.9, max 67.8) |
| 201 (API maximum 200, plus the probe row) | 3,000 rows, p50 19.7 ms (min 19.3, max 25.7) | 201 rows, p50 142.7 ms (min 138.3, max 157.4) |

The old statement returned all 3,000 rows at both limits (the bug), so it did no
top-k sort; the new anchor honours ORDER BY/LIMIT and pays the sort. A second
seeding of the same shape measured the old single statement at 42 ms (limit 51)
and 49 ms (limit 201) p50, so treat the old column as 20-50 ms; the new pair is
about 2.7x slower at limit 51 and 7x at limit 201 on this fixture.

Statement level, first seeding, n = 15 interleaved, p50 (the anchor here is the
first version, ordering by ecosystem as well):

| statement | limit 51 / 51 ids | limit 201 / 201 ids |
|---|---:|---:|
| old single (unordered, LIMIT ignored) | 42.3 ms | 48.9 ms |
| anchor, ORDER BY ecosystem, name, uid | 103.2 ms | 356.9 ms |
| count, UNWIND + HAS_VERSION edge (previous helper) | 257.7 ms | 999.9 ms |
| count, `WHERE v.package_id IN $ids` (shipped) | 3.1 ms | 5.7 ms |

The previous count helper, which the package-registry browse route also uses,
was the larger cost, so the shipped count read was rewritten to the
`package_version_package_id`-index-backed form, about 80x faster at 51 ids and
170x at 201 ids. The anchor's cost is the sort: variants measured at n = 9,
p50, limit 51 / 201:

| anchor variant | limit 51 | limit 201 |
|---|---:|---:|
| ORDER BY ecosystem, name, uid (first version) | 114.9 ms | 342.7 ms |
| ORDER BY name, uid (shipped when an ecosystem is pinned) | 69.0 ms | 177.8 ms |
| ORDER BY uid only | 64.9 ms | 187.3 ms |
| no ORDER BY, LIMIT only | 29.9 ms | 24.4 ms |

An ecosystem-pinned read has one ecosystem, so dropping it from the sort key
keeps the (ecosystem, name, uid) contract and roughly halves the sort. A
query-only read keeps ecosystem as the first key. The remaining cost is the
top-k sort itself, which this build makes proportional to the limit; an
index-ordered scan was not available in these measurements.

The browse route's version counts get the same faster helper, so
`GET /api/v0/package-registry/packages` drops its count-read cost accordingly;
its handler tests pass against the new statement (they assert the new text).
The browse route's own anchor statement is unchanged.

Version count semantics. The shipped count reads `PackageVersion` nodes by
`package_id`, not `HAS_VERSION` edges. Both writers set `v.package_id` on the
node (`canonicalPackageRegistryVersionUpsertCypher`), but the `HAS_VERSION`
edge is written in a deferred second write group after the node group commits
(`package_registry_edge_writer.go`). Between the two groups a version node
exists whose edge does not, and the property count is briefly higher than the
edge count. Once both groups have committed they agree: on the 3,000-package
fixture, 2,250 packages have versions and 0 of 3,000 differ between the two
counts, and `TestLiveSearchBundlesVersionCountEqualsEdgeCount` asserts equality
per package plus the no-edge window (a version node with `package_id` and no
edge counts by property, not by edge).

Query-plan pins: `handleSearchBundles` moved from the grandfathered prose
disposition to a typed `label_inventory` entry (label `Package`,
`max_results` 201) and re-derived its `source_sha256`; the version-count read
moved to `package/registry/version_counts.go:VersionCountsByPackageID`
(`keyed_support`, `bounded_key_batch`, 200 keys, 200 results) with a re-derived
`source_sha256`, and the stale `attachPackageVersionCounts` entry was removed.
No `cypher_sha256` changed: neither statement is in `hot-cypher.yaml`.

## Observability Evidence

No-Observability-Change: no metric, span, log field, or status surface is
added or removed. Both graph statements run through the existing
`Neo4jReader.Run`, so the existing `eshu_dp_neo4j_query_duration_seconds`
histogram records the anchor read and now also the count read (one extra sample
per non-empty request); graph failures still map through
`WriteGraphReadError` to the 503/504 bounded-availability contract, and the
API's request duration and status-code signals cover the route. An operator
who suspects a scoped caller is missing rows checks that the caller's grant is
non-empty and that the packages carry `visibility = 'public'`; a package with
no visibility is intentionally hidden from a scoped token.

## Not proven here

- Correlation-granted private packages are not served on this route, unlike
  the ecosystem browse's name/id-anchored branches. The route is public-only
  for scoped callers; that is disclosed in the OpenAPI description and the MCP
  tool description.
- Timings are from a local container, not the remote perf host; they show the
  shape of the change, not a production baseline.
