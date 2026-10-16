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

## Telemetry

- Span attributes on `query.infra_resource_search`: `eshu.infra_search_path`
  (`fulltext`, `legacy`) and, on legacy, `eshu.infra_search_fallback_reason`.
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
