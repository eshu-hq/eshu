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
  (the package-registry family's `UNWIND` count statement, now exported) counts
  versions for that page and Go zero-fills.
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

Performance Evidence: the anchor read is `MATCH (p:Package) WHERE p.uid IS NOT
NULL AND p.ecosystem = $ecosystem [AND p.visibility = 'public'] RETURN ...
ORDER BY p.ecosystem, p.normalized_name, p.uid LIMIT $limit`; the count read is
`UNWIND $package_ids AS candidate_package_id MATCH (p:Package {uid:
candidate_package_id})-[r:HAS_VERSION]->(v:PackageVersion) RETURN p.uid,
count(r)`. Backend: NornicDB 1.3.3, compose pin above, isolated container. Input:
3,000 `Package` nodes in one ecosystem, 1,000 with one `HAS_VERSION` edge. The
probe container had no schema bootstrap, so no `Package.uid` constraint or index
existed; production adds the uid constraint the count statement anchors on. Timings
are client round trips over bolt from the test process, 41 samples each,
verified by that run; the old statement is the exact text the handler sent
before this change.

| `limit` sent | old statement (rows returned, p50) | new pair (rows returned, p50) |
|---:|---|---|
| 51 (the API default 50, plus the probe row) | 3,000 rows, 37.7 ms | 51 rows, 3.9 ms |
| 201 (the API maximum 200, plus the probe row) | 3,000 rows, 12.4 ms | 201 rows, 4.5 ms |

The old statement returned the whole catalog at both limits, which is the bug.
The new pair returns exactly the requested page. Sample variance on the host
was high (p90 25-92 ms for both shapes), so read the p50 column as the
direction of the change, not a precise ratio. The added cost is one extra
round trip bound to at most 200 uids, skipped when the page is empty. The
package-registry browse route pays the same second statement
(`docs/internal/evidence/5167-package-registry-version-count-nornicdb.md`).

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
