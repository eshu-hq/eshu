# infra/fulltext

The #7245 read cutover of `POST /api/v0/infra/resources/search` onto the
Neo4j keyword FULLTEXT index `infra_search_key_ft`.

## Why

The legacy search runs one `MATCH (n:<Label>)` branch per infra label (27 for
an uncategorized search) inside a `CALL { ... UNION ... }` and filters every
node with a twelve-field `CONTAINS` predicate. Its cost is flat in the label
size: about 2.4 M db hits on a 182k-node graph whatever the query. The 27
branches also replan for each new query-length bucket, so a cold call takes
about 2.4 s.

The writers in `go/internal/storage/cypher/infra/search` store the twelve
fields joined with U+001F as `infra_search_key`, or set
`infra_search_ovf = 'T'` when the joined key passes 8,000 code points and is
not stored. The full-text form asks the index for
`infra_search_key:*<escaped query>* OR infra_search_ovf:T`, keeps the label
set, re-applies the unchanged predicate and filters, and projects the same
columns. The index only narrows candidates; the legacy predicate still decides
every row.

## Statements

| Shape | Statement |
| --- | --- |
| Unscoped | `CALL db.index.fulltext.queryNodes(...) YIELD node AS n WITH n WHERE (labels) <extra> RETURN DISTINCT <cols> ORDER BY name LIMIT $limit` |
| Neo4j scoped | the same candidates, then `WITH n WHERE <hoisted grant predicate>`, then `RETURN <cols>` (no `DISTINCT`) |

The unscoped legacy statement UNIONs projected rows, so it de-duplicates
identical rows; the scoped one UNIONs nodes. Each full-text form keeps the
matching semantics. `DISTINCT` must run before `ORDER BY ... LIMIT`: an inner
top-k returned 13 rows where 17 are correct.

## Selection

`Select` picks the full-text read only when all of these hold:

1. The backend is Neo4j.
2. The query is not empty.
3. `ESHU_INFRA_SEARCH_KEY_READ=on`.
4. The query has at most `MaxQueryRunes` (256) code points. Lucene refuses to
   determinize a longer `*<query>*` wildcard for some inputs (a repeated
   character fails at 315), so a longer query keeps the legacy statement
   (`query_too_long`).
5. `Readiness` is ready: `search.IndexState` reads the index `ONLINE` and
   `search.CountUnkeyedSearchable` sums to zero.

An unkeyed node is invisible to the index, so a node the search could match
must not be unkeyed. `CountUnkeyedSearchable` counts only flagless nodes that
hold at least one of the twelve searched fields: a node a posture writer
created with none of them can never match a search, so it does not hold
readiness off. `Readiness.Watch` probes at start and every five minutes;
requests never probe. A probe error is not ready.

`Reader.Rows` falls back to the legacy statement for one request when the
full-text read fails while the request context is live.

## Broad class (#7245)

A query is *broad* when `Broad` says it can match corpus-wide: the trimmed
query is one or two runes (`BroadQueryMaxRunes = 2`). Empty queries
(structured search) and the `"::"` equality branch are never broad, and the
bound applies only on the full-text path (`RowsBounded` checks `path ==
PathFulltext && Broad(req.Query)`). A short wildcard would otherwise run the
twelve-field `CONTAINS` recheck, `DISTINCT`, and the `ORDER BY` sort over
the whole candidate set before the late `LIMIT`.

`BroadCandidateCap` (K = 1000) bounds the candidates the broad class
evaluates. It sits above the largest servable page (`infraSearchMaxLimit`
200, over-fetched by one), so a page is complete whenever the probe fits in
K. `rowsBroad` first runs one combined probe, `ProbeStatement`: the same
full-text index call with the combined `(n:A OR n:B ...)` label filter, then
`WITH n LIMIT 1001` (`BroadProbeLimit = BroadCandidateCap + 1`) and
`RETURN count(n) AS candidate_count`. The probe runs no recheck, no
`DISTINCT`, and no sort. A count past the cap proves the cap would cut rows;
a malformed probe count is treated as over-cap (conservative capped), never
as uncapped.

When the probe fits in K, the read runs the unchanged full-text statement,
so rows equal the uncapped read (rows-equal). Past the cap, it runs the
capped form (`CappedUnscopedStatement` / `CappedScopedStatement`): same
labels and `whereExtra`, with `WITH n LIMIT 1000` between the label filter
and the recheck, before `DISTINCT` / `ORDER BY` / the grant predicate, so at
most K rows ever reach them. A capped page always carries its marker:
`truncated=true` in the response, plus the
`eshu.infra_search_broad_capped=true` span attribute. Worst case per broad page is about 1001 probe rows plus at most
1000 candidates through the recheck. Probe and read failures keep the
`Rows` contract: a canceled context returns the error with no fallback,
otherwise the legacy statement answers once.

Covered by `TestBroadClassifier`,
`TestRowsBoundedBroadOverCapReturnsCappedRowsWithMarker`,
`TestRowsBoundedBroadAtCapIsUncappedWithoutMarker`,
`TestRowsBoundedSelectiveIsSingleUncappedRead`,
`TestCappedStatementsBoundEvaluationBeforeRecheck`, and the handler tests
`TestInfraSearchBroadOverCapMarksTruncated`,
`TestInfraSearchBroadAtCapIsUncapped`,
`TestInfraSearchSelectiveHasNoCapEffect`.

No-Regression Evidence (#7245): selective reads emit byte-identical
statements and rows before and after (the selective and at-cap tests
above), and the full `go/internal/query/...` suite is green. Corpus-scale
before/after timing on the broad class stays with the owner lane and is
NOT_CHECKED here.

Observability Evidence (#7245): a broad page sets the
`eshu.infra_search_broad_capped=true` span attribute on
`query.infra_resource_search` and reuses the existing
`eshu_dp_infra_search_path_total{search_path="fulltext"}` counter. No new
metric or log is added.

Known limits of the bound:

- Scope-blind probe. `ProbeStatement` counts label matches without the grant
  predicate, so a scoped broad query is served capped with `truncated=true`
  whenever label matches exceed K, even when the grant-visible rows would fit
  in K. The direction is safe (the marker is always set), but scoped callers
  see truncation noise. A scope-aware probe is a follow-up, not this change.
- Unstable capped subset. The cap has no `ORDER BY`, so repeated identical
  broad queries can return different rows. `truncated=true` discloses that the
  page is incomplete, not that it is stable. Deterministic pre-cap ordering
  is a follow-up.

## Telemetry

- Span attributes on `query.infra_resource_search`: `eshu.infra_search_path`
  (`fulltext`, `legacy`) and, on legacy, `eshu.infra_search_fallback_reason`.
  A broad page also sets `eshu.infra_search_broad_capped=true`.
- `eshu_dp_infra_search_path_total{search_path, reason}`.
- `eshu_dp_infra_search_key_ready`, `eshu_dp_infra_search_key_index_online`,
  `eshu_dp_infra_search_key_unkeyed_nodes` gauges, recorded on every probe.
- `query.infra_search_key.readiness_changed` log on each readiness change and
  `query.infra_search_key.fulltext_fallback` on each fallback.

## Known limits

`CountUnkeyedSearchable` finds nodes with no flag. It cannot find a stale key:
a key whose node fields a writer without the clause changed later. The rollout
runs the backfill after the last pre-#7245 writer is gone, which repairs stale
keys, before the flag goes on. A searchable node written unkeyed after a probe
stays invisible to the index until the next probe turns readiness off (at most
five minutes).
