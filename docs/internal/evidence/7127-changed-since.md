# #7127 changed-since: single-statement diff (PR-1)

Change: `StatusStore.ComputeChangedSinceDelta` in
`go/internal/storage/postgres/changed_since*.go` evaluates the classification
diff once per request. The counts statement and the per-bucket samples
statements are replaced by `changedSinceDeltaQuery`, which materializes
`classified` once and returns each non-empty (category, classification)
bucket's exact count with its first `sample_limit+1` keys ordered by
`stable_fact_key` (lateral join per bucket). The classification CTE is
unchanged, so rows are identical.

Performance Evidence: local PostgreSQL 18.6, one database, 2,219,152
`fact_records` rows (target scope about 0.4x of a 12,403-file repository plus
30 noise scopes inserted in random order; `pg_stats` correlation of
`generation_id` 0.063), a synthetic fixture built for this measurement. Its
generator scripts were scratch files and are not committed, so the figures below
are single-host measurements to be re-derived, not a replayable benchmark. Each
request is measured as the sum of `EXPLAIN (ANALYZE, BUFFERS)` execution times
of its statements, three interleaved rounds with the first mover alternating,
run on an otherwise idle machine:

| flow | statements | runs (s) | median (s) | row-set SHA-256 (first 16 hex) |
| --- | --- | --- | --- | --- |
| before (origin/main constants) | 8 | 65.12, 52.52, 80.93 | 65.12 | `84c7ba3f71f4f03c` |
| after (`changedSinceDeltaQuery`) | 1 | 9.43, 10.88, 16.13 | 10.88 | `84c7ba3f71f4f03c` |

About 6x, with identical rows on all six runs (189 rows: bucket counts plus
26 samples per bucket). The before flow's 8 statements are one counts statement
and one samples statement for each of 7 non-empty buckets on this fixture. This
is a single-host local proof: it is not a production wall-time claim. One diff
is still O(generation size); ops-qa measured 20-45 s for one diff on a
12,403-file repository, so the request path stays over budget for very large
repositories until the precomputed-delta work in the #7127 ruling lands.

Row-equality proof: `TestChangedSinceSingleStatementMatchesLegacyFlowLive`
(`changed_since_single_statement_live_test.go`) seeds every category and
classification with duplicate-payload sets, a key present in all three
categories, tombstones, older and later generations, and a second scope with the
same generation ids. It builds the old counts and samples statements from the
shipped `changedSinceClassificationCTEs` plus a frozen tail (never a hand copy of
the CTE), asserts both are prefixed by the shipped CTE, and compares the
assembled categories for sample limits 1, 2, 25, 26, 28, and 200, including
truncated and exactly-full buckets. Reversing the lateral `ORDER BY` makes it
fail. `TestComputeChangedSinceDeltaClassifiesAllVerdicts` pins three round trips
per request (scope, prior generation, one diff).

Observability Evidence: no runtime signal changes. The route keeps the
`query.freshness_changed_since` span and the shared Postgres query metrics; a
single statement makes their duration the whole diff cost.

## PR-2: indexed_at and reducer-derived facts (intended delta)

Change: the digest input drops `indexed_at` for `content_entity` rows with an
object payload (`changedSincePayloadDigestInput`; `jsonb - key` raises on a
scalar payload, so a scalar digests as itself) and every `fact_records` scan in the CTE
excludes `fact_kind LIKE 'reducer\_%'` (`changedSinceExcludeReducerDerivedKinds`).
The escaped pattern is shared with `collector_evidence_summary.go`, whose
unescaped `reducer_%` also matched a kind such as `reducerX`. Rows are NOT
equal to PR-1 by design: today's rows report every content entity as `updated`
and every reducer-written fact as `added`.

Accuracy proof, RED first: on the unmodified statement the new live tests failed
(`content_entities` counts `{Updated:3}` against `{Updated:1 Unchanged:2}`;
`facts` counts `{Added:3 Updated:1 Unchanged:1 Retired:1}` against
`{Added:1 Unchanged:1}`); with the constants wired in they pass. They also pin
that a real content-entity change still reports `updated`, that only
`content_entity` is normalized (`indexed_at` on a `file` still counts), that a
duplicate-row multiset that differs only in `indexed_at` is `unchanged`, and that
a `reducerX` kind is not excluded, and that a scalar `content_entity` payload
does not raise. The `repository` fact's `source_run_id`
change is asserted as `updated` (documented, not normalized).

Fixture result (same 2.2M-row scope, sample limit 26): content_entities
`updated 970 / unchanged 96,030`; facts `added 800 / retired 320 / superseded 800
/ unchanged 201,980 / updated 1,101`; files `unchanged 5,000`. These equal the
planted truth from the #7127 ruling: 1% real entity change, reducer rows
excluded, indexed_at churn ignored.

Performance Evidence: normalization adds a `payload - 'indexed_at'` on
content_entity rows and drops reducer rows before the scan. Interleaved,
alternating first mover, four rounds on the same fixture, `EXPLAIN ANALYZE`
execution time: PR-1 statement median 9.02 s (11.69, 7.68, 9.25, 8.79), PR-2
statement median 9.57 s (8.05, 7.42, 14.39, 11.08). The difference is inside
run-to-run noise on a shared laptop (a separate un-interleaved run took 22.6 s
under machine load, which is why only the interleaved medians count); this is
no-regression evidence, not a speedup claim.

No-Observability-Change: no runtime signal changes; the route keeps its span and
the shared Postgres query metrics.

Golden-corpus oracle: `scripts/lib/golden-corpus-changed-since.sh` computes the
expected facts counts the live `get_changed_since` call must equal, so it must
classify with the API's fragments. It sources
`scripts/lib/golden-corpus-changed-since-sql-fragments.sh`, generated from
`changedSincePayloadDigestInput` and `changedSinceExcludeReducerDerivedKinds`;
`TestChangedSinceOracleFragmentsMatchGoConstants` fails when the file drifts and
evaluates it in a shell. Proof on one PostgreSQL 18 fixture (facts category: 1
added, 1 updated, 1 unchanged, 1 retired, 1 superseded once reducer rows are
excluded, plus reducer, scalar and `indexed_at` rows): the API statement returns
`1|1|1|1|1`; the pre-change oracle returned `2|2|1|2|2`; the updated oracle
returns `1|1|1|1|1`. The live golden-corpus gate itself was not run for this
change (a live gate was already in flight on the host).
