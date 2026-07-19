# Package-registry `version_count` OPTIONAL MATCH+count(v) group-collapse: before/after

Fixes `GET /api/v0/package-registry/packages` (also mounted on MCP). On the
pinned NornicDB build, `packageRegistryPackagesCypher`'s single statement

```cypher
MATCH (p:Package {ecosystem: $ecosystem})
OPTIONAL MATCH (p)-[:HAS_VERSION]->(v:PackageVersion)
RETURN p.uid AS package_id, ..., count(v) AS version_count
ORDER BY p.ecosystem, p.normalized_name, p.uid
LIMIT $limit
```

does not group by every non-aggregate `RETURN` key (`p.uid` and the other
`p.*` projections), as openCypher requires. Instead every row whose optional
side is null collapses into a single implicit bucket, so the statement
returns **at most one row total** regardless of how many packages match the
anchor. A zero-version `Package` is silently absent from every list read and
from an exact `package_id` lookup — indistinguishable from "package does not
exist."

Fix (`go/internal/query/package_registry.go`,
`go/internal/query/package_registry_cypher.go`): drop the `OPTIONAL MATCH` +
`count(v)` composition entirely. `packageRegistryPackagesCypher` becomes a
single-clause anchor-only read (no aggregate). A second, separately scoped
statement, `packageRegistryVersionCountsCypher`, resolves `HAS_VERSION` counts
only for the returned page's package uids via `UNWIND` + a concrete
relationship-variable `MATCH` (an inner join, never `OPTIONAL MATCH`):

```cypher
UNWIND $package_ids AS candidate_package_id
MATCH (p:Package {uid: candidate_package_id})-[r:HAS_VERSION]->(v:PackageVersion)
RETURN p.uid AS package_id, count(r) AS version_count
```

`PackageRegistryHandler.attachPackageVersionCounts` merges the two result sets
in Go and zero-fills any package uid absent from the count query's result —
the documented pattern for this backend (see
`docs/public/reference/nornicdb-pitfalls.md`, "run as a SEPARATE single-clause
query merged in Go"). The round trip is skipped entirely when the page is
empty.

Backend: NornicDB `eshu-nornicdb-pr261:149245885258`
(commit `1492458852588c884c32f70d27ea2ee07086769c`), standalone isolated
Compose stack (`docker compose -p nornic-count-repro up -d nornicdb`), no-auth
Bolt/HTTP on `localhost:7687`/`localhost:7474`, database `nornic`. Reproduced
both directly over the HTTP `tx/commit` endpoint and through Eshu's own driver
path (`query.Neo4jReader.Run` → `neo4j-go-driver/v5` `session.Run`,
`AccessModeRead`), i.e. exactly how the handler executes.

## Disproven candidate: pattern-comprehension `size(...)` count

Before landing the two-query fix, the pattern-comprehension count
`size([(p)-[:HAS_VERSION]->() | 1]) AS version_count` was tried as a
single-statement replacement for `OPTIONAL MATCH` + `count(v)`. It initially
appeared to fix the zero-version case (2 zero-version packages both showed
`vc=0`), but adding a **third** package with 2 real `HAS_VERSION` edges
exposed a second, independent NornicDB defect: the pattern comprehension
itself always evaluates to `0`, even when the edges provably exist
(confirmed by a direct `MATCH ... RETURN v.uid` on the same node returning
both edges). This candidate is rejected — do not reuse it for
`packageRegistryPackagesScopedEcosystemCypher` or any other site with the
same shape.

A `WITH p, collect(v) AS versions ... RETURN size(versions)` candidate (an
extra clause between the anchoring `MATCH`/`OPTIONAL MATCH` and the final
`RETURN`) was also tried and also returns `0` for every package regardless of
real edge count — consistent with the existing "Multi-Clause Read Queries
Silently Corrupt The Projection" pitfall. Rejected for the same reason.

The only shape that returned correct counts in every case tried (0-version,
2-version, and a 200-package/100-with-version mixed corpus) is the
UNWIND-scoped inner-join `MATCH` above.

## Accuracy Evidence (behavior fix — corrected delta, not identity)

The OLD output is wrong on this backend, so the proof is the intended delta
(old-wrong → new-correct), measured live in
`TestLivePackageRegistryListPackagesReturnsZeroVersionPackages` (TDD:
confirmed RED against the pre-fix code, then GREEN against the fix) and
directly via `tx/commit`:

| shape | seeded rows | OLD/rejected-candidate result | NEW result |
|---|---|---|---|
| 2 zero-version packages, `OPTIONAL MATCH`+`count(v)` | 2 `Package` | 1 row (`vc=0`); the other package vanishes | 2 rows, both `vc=0` |
| 2 zero-version + 1 two-version package, `OPTIONAL MATCH`+`count(v)` | 3 `Package`, 2 `PackageVersion` | 1 row total: `{id: "no-versions-a", vc: 2}` — wrong id/count pairing, the two-version package's count leaks onto the alphabetically-first zero-version package's id | n/a (not applicable to this shape) |
| same 3-package corpus, size(pattern-comprehension) candidate | 3 `Package`, 2 `PackageVersion` | 3 rows, but **every** `vc=0` including the two-version package (silently wrong) | n/a (candidate rejected) |
| same 3-package corpus, UNWIND-scoped inner-join count | 3 `Package`, 2 `PackageVersion` | n/a | anchor: 3 rows (all packages); count query: 1 row (`{two-versions: 2}`); Go zero-fill: `{a: 0, b: 0, two-versions: 2}` — all correct |
| 200-package corpus (100 with 1 version each), `OPTIONAL MATCH`+`count(v)` | 200 `Package`, 100 `PackageVersion` | **1 row total** | n/a |
| same 200-package corpus, UNWIND-scoped inner-join count | 200 `Package`, 100 `PackageVersion` | n/a | anchor: 200 rows; count query: 100 rows (only the packages with a version, as an inner join); Go zero-fill covers the other 100 |
| exact `package_id` lookup for a zero-version package | 1 `Package` | 0 rows — indistinguishable from "not found" | 1 row, `version_count: 0` |

Deterministic across runs (`TestLivePackageRegistryListPackagesReturnsZeroVersionPackages`
run twice back-to-back with cleanup between runs).

## Performance Evidence (correctness win; no meaningful regression)

No-Regression Evidence: warm-cache timings over the HTTP `tx/commit` endpoint
on the 200-package/100-with-version corpus, back-to-back on the pinned
backend (`curl -w '%{time_total}'`, 5 iterations each after a cold first
call):

| statement | warm timing (2nd-5th call) |
|---|---:|
| OLD single `OPTIONAL MATCH`+`count(v)` statement (wrong: 1 row) | ~2.9–3.7 ms |
| NEW anchor-only statement (200 rows) | ~2.9–3.7 ms |
| NEW UNWIND-scoped count statement (200 candidate uids, 100 matched) | ~1.9–2.5 ms |
| NEW total (anchor + count, 2 round trips) | ~5–6 ms |

At the bounded page size this handler ever returns (`limit ≤ 200`, enforced
by `packageRegistryMaxLimit`), the added round trip costs roughly 2-3ms —
immaterial next to normal HTTP/API latency, and there is no valid prior
baseline to regress against since the old shape's "speed" came from silently
returning 1 row instead of the correct page.

## Observability Evidence

No-Observability-Change: no runtime metric, span, log field, queue stage,
worker knob, or schema phase changes. The handler keeps its existing
`SpanQueryPackageRegistryPackages` span and truth envelope; the added
`attachPackageVersionCounts` round trip runs inside the same span and surfaces
the same `WriteError`/500 path on failure as every other `h.Neo4j.Run` call in
this handler.

## Known second occurrence — reconcile at merge

`go/internal/query/package_registry_cypher.go` on branch
`fix/5167-w5-registry-supply` (F-6/W5b, not touched by this branch) adds
`packageRegistryPackagesScopedEcosystemCypher`, which reuses the identical
`OPTIONAL MATCH (p)-[:HAS_VERSION]->(v) ... count(v)` composition and is
exposed to the same zero-version-package drop. That branch needs the same
two-query fix (anchor-only read + `packageRegistryVersionCountsCypher`-style
scoped count, merged in Go) — not the pattern-comprehension candidate, which
this evidence file disproves. Whichever branch lands first, the other should
rebase and apply the identical pattern to its function.
