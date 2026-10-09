# NornicDB: Grouping Aggregate Over An Empty Match Emits A Phantom `(NULL, 0)` Row

Companion to [NornicDB Pitfalls](nornicdb-pitfalls.md): that page is frozen by
the file cap, so this executor defect gets its own page. Measured live for
[#7816](https://github.com/eshu-hq/eshu/issues/7816) over
`neo4j-go-driver/v5` Bolt on the pinned
`nornicdb-amd64-cpu:fix-500-e022384c@sha256:74a8ed7b...` build, with
`neo4j:2026-community` as the control. One `PackageVersion` node seeded
(`package_id` `zz7816:lib-common`).

## Observed shape

```cypher
MATCH (v:PackageVersion) WHERE v.package_id IN ["zz7816:synthetic-dep"]
RETURN v.package_id AS package_id, count(v) AS version_count
-- expected 0 rows; ACTUAL on the pinned build, 1 row:
-- {package_id: null, version_count: 0}
```

openCypher groups by the non-aggregate `RETURN` key, so an aggregate over an
empty match must return zero rows (only a global `RETURN count(v)` with no
group key returns a single `0` row). The pinned NornicDB build instead emits
one group row with a null key and a zero count. The phantom appears only
when the whole match input is empty: the same statement over a page holding
one present and one absent id groups correctly with no phantom row, which is
why same-shape fixtures with a matching seed pass on both backends.

## Eshu implications

`packageRegistryVersionCountsCypher`
(`go/internal/query/package/registry/cypher.go`) served exactly this shape, so
a version-count page whose ids all lack versions came back on NornicDB with a
`""` key (`StringVal` of null) while Neo4j returned an empty map. The
backend-diff quorum tripped it as a reproduced `1-vs-0` results divergence on
`github.com/acme/synthetic-dep` in both pairings: no writer creates a version
node for that id, Neo4j's 0 rows were correct, and the digest preimage of the
NornicDB row is exactly `{"version_count":0}` with the null key dropped by
digest canonicalization.

## Workaround

Filter the group key after the aggregation, which the pinned build evaluates
correctly (proven live: 0 rows for the absent id, real rows untouched, and
the Neo4j leg byte-identical):

```cypher
MATCH (v:PackageVersion) WHERE v.package_id IN $package_ids
WITH v.package_id AS package_id, count(v) AS version_count
WHERE package_id IS NOT NULL
RETURN package_id, version_count
```

The filter is exact here because a package uid is never legitimately empty.
Any other grouping aggregate that can observe a fully empty match on this
backend needs the same guard; an `OPTIONAL MATCH` rewrite is not a
substitute (see "OPTIONAL MATCH + Aggregate Collapses Every Zero-Match Group
Into One Row" in [NornicDB Pitfalls](nornicdb-pitfalls.md#pitfall-optional-match-aggregate-collapses-every-zero-match-group-into-one-row)).

## Validation

`go test ./internal/query/package/registry -tags live_nornicdb_answer_truth
-run TestLiveVersionCountsEmptyGroupIsAbsent -count=1 -v` against one
container per backend (`ESHU_NEO4J_URI` each; the `nornicdb` / `neo4j`
backend-selection knob is documented in the test header): the
version-less single-id page resolves to an empty map with no `""` key on
both legs, and the seeded id still counts 1. RED before the filter on
NornicDB (`map[:0]`), green after; Neo4j green throughout.

## No-regression evidence

No-Regression Evidence: pure correctness fix measured on the same input
shape before and after. Anchor unchanged: `MATCH (v:PackageVersion) WHERE
v.package_id IN $package_ids` on the `package_version_package_id` index;
the added `WITH ... WHERE package_id IS NOT NULL` filters at most one
group row per page id (pages hold at most 200 keys per
`query-source-coverage.yaml`) after the aggregation, with no `ORDER BY`,
`LIMIT`, or second `MATCH`.

- Backends: `nornicdb-amd64-cpu:fix-500-e022384c@sha256:74a8ed7b...`
  (self-reports 1.3.3) and `neo4j:2026-community`, warm containers on one
  shared host; timings are live-test wall time (driver round trip
  dominated), same fixture, same seeded state both directions.
- Baseline (old statement, base code): NornicDB FAIL in 0.01 s (the
  phantom row trips the assertion; the statement itself executes);
  Neo4j PASS in 0.03 s.
- After (filtered statement): NornicDB PASS in 0.01 s and 0.00 s (two
  runs); Neo4j PASS in 0.05 s and 0.02 s (two runs, run-to-run band
  0.02-0.05 s on the shared host).
- Row counts: version-less page NornicDB 1 row -> 0 rows, Neo4j 0 -> 0;
  seeded page 1 row with count 1 on both legs before and after.

No measurable regression on either leg: the NornicDB leg is identical to
the centisecond, and the Neo4j after-band straddles the baseline. No
full benchmark: the change adds no scan, no hop, and no extra round
trip, and the empty-page fast path (caller skips the query) is
untouched.

No-Observability-Change: row-level correctness fix with no new
telemetry. The phantom `""` key never reached any API/MCP envelope (both
callers zero-fill by package id), so no response shape, metric, span,
log, or status output changes on either backend.
