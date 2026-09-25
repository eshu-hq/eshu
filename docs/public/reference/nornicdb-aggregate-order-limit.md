# NornicDB: `ORDER BY`/`LIMIT` After An Aggregate Is Silently Ignored

This page re-measures the "`OPTIONAL MATCH` + Aggregate" pitfall in
[NornicDB Pitfalls](nornicdb-pitfalls.md#pitfall-optional-match-aggregate-collapses-every-zero-match-group-into-one-row)
on the current compose pin and records a different defect the same statement
shape hits.

## Measured on the current pin

The zero-match row collapse was measured on the older `eshu-nornicdb-pr261` pin.
It does **not** reproduce on the current compose pin
(`nornicdb-amd64-cpu:fix-500-e022384c@sha256:74a8ed7b...`, NornicDB 1.3.3),
measured live for #5167 (code bundles): two zero-version packages and one
two-version package under `OPTIONAL MATCH ... WITH p, count(v) AS version_count`
returned 3 rows with counts 0, 0, 2, each on its own id. Do not cite the
collapse as a reason on this pin.

What the same shape does still get wrong is the tail of the statement. With
`WITH p, count(v) AS version_count RETURN ... ORDER BY p.ecosystem,
p.normalized_name, p.uid LIMIT $limit`, the pinned build ignores both the
`ORDER BY` and the `LIMIT`: `LIMIT 51` over a 3,000-package ecosystem returned
all 3,000 rows in storage order. A handler that then cuts `rows[:limit]`
serves an arbitrary page, and its `truncated` flag is meaningless.
`POST /api/v0/code/bundles` shipped that statement until #5167 and is now the
same anchor-only read plus a page-bound `UNWIND` count as the package-registry
browse route (`registry.VersionCountsByPackageID`). Keep `ORDER BY` and
`LIMIT` directly on the anchor `RETURN`, where this build honours them, and
resolve aggregates for the returned page in a second statement. Proof:
`TestLiveSearchBundlesHonoursOrderByAndLimit` in
`go/internal/query/codequery/registry_bundles_live_test.go`
(`ESHU_PKG_REGISTRY_PROVE_LIVE=1`), and
`docs/internal/evidence/5167-code-bundles-grant.md`.

A second trap surfaced by the same work: this build **silently ignores a
syntactically invalid `WHERE` predicate and returns every row**. A guard test
that only checks the statement text proves nothing; assert row membership
against a live seed.
