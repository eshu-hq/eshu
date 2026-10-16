# #7245 PR-2: infra search read cutover

PR-1 (#8217) keyed every infra node and created the Neo4j keyword FULLTEXT
index `infra_search_key_ft` with no reader. This PR moves
`POST /api/v0/infra/resources/search` onto that index behind a flag. The
default is off, so a deployment that does not set the flag keeps today's
statements byte for byte.

## What changes

- New package `go/internal/query/infra/fulltext`: Lucene escaping, the
  unscoped and Neo4j scoped full-text statements, the readiness cache, the
  per-request selection, and a one-request legacy fallback.
- `searchResources` (`go/internal/query/infra.go`) builds the legacy
  statement as before and hands it with its parts to `fulltext.Reader.Rows`.
- `ESHU_INFRA_SEARCH_KEY_READ` (`off` or `on`, default `off`), read by the
  API and the MCP server. Any other value fails startup.
- `storage/cypher/infra/search.IndexState` reads the index state for the
  probe.

The full-text read is selected only when all of these hold:

1. The graph backend is Neo4j.
2. The query is not empty (structured and Argo CD category-only searches keep
   the legacy statement).
3. The flag is on.
4. The query has at most 256 code points (`fulltext.MaxQueryRunes`).
5. The cached readiness reads the index `ONLINE` and `search.CountUnkeyed`
   sums to 0. The probe runs at start and every 5 minutes, never per request.
   A probe error is not ready.

## Query shape

Legacy (unchanged): `CALL { 27 x MATCH (n:<Label>) WHERE true <extra> RETURN
<19 cols> UNION ... } RETURN <cols> ORDER BY name LIMIT $limit`, where
`<extra>` is the twelve-field `CONTAINS` predicate (or the `::` equality
branch) plus the structured filters.

Full text, unscoped:

```cypher
CALL db.index.fulltext.queryNodes('infra_search_key_ft', $infra_search_lucene) YIELD node AS n
WITH n
WHERE (n:L1 OR ... OR n:L27) <extra>
RETURN DISTINCT <19 cols>
ORDER BY name
LIMIT $limit
```

`$infra_search_lucene` is `infra_search_key:*<escaped query>* OR
infra_search_ovf:T`. The Neo4j scoped form adds `WITH n WHERE <hoisted
list-EXISTS grant predicate>` and drops `DISTINCT`, because the legacy scoped
statement UNIONs nodes, not rows. The index only narrows candidates; the same
`<extra>` decides every row.

## Query length bound

Lucene determinizes the `*<query>*` wildcard with an effort budget of 10,000.
Measured on the pinned Neo4j 2026.08.1 against the 182k-node fixture through
the HTTP transaction endpoint, the longest query that still runs is:

| Query shape | Longest passing length (code points) |
| --- | ---: |
| one repeated character (`a`, or an emoji) | 314 |
| `ab` repeated | 443 |
| `aaab` repeated | 621 |
| random alphanumeric | 980 |
| `aws_iam_role/` repeated | 1,113 |
| ARN-like text | 1,800 |

One code point more fails with `TooComplexToDeterminizeException`. 281 fuzzed
inputs over small alphabets passed at 256 and at 300 code points. The bound is
256. Without it, every longer query would fail the index read and fall back,
which doubles its work and logs a warning on each request.

## Performance Evidence

Performance Evidence: pinned `neo4j:2026-community` 2026.08.1, preserved
container `zz7245pr2-neo4j` with the QA-shaped fixture (181,953 nodes on the
27 infra labels: TerraformVariable 65,257, TerraformResource 35,018,
TerraformLocal 30,414, CloudResource 180, 2,220 on each other label, plus the
adversarial rows), full Neo4j schema, `infra_search_key_ft` ONLINE, every node
keyed (the live readiness probe read ready). Harness:
`TestLiveInfraSearchFulltextTiming` drives the real handler end to end through
the production `Neo4jReader`, `limit` 16, unscoped, at commit db910a815. Cold
is the first call after `db.clearQueryCaches()`; warm is p95 of 20 calls. Host
load average was 2.2. Second run shown; the first run after container start
measured warm p95 within 25 ms of these and a cold `aws_iam_role` of
4,024 ms legacy and 469 ms full text.

| Query | Legacy cold | Full-text cold | Legacy warm p95 | Full-text warm p95 |
| --- | ---: | ---: | ---: | ---: |
| `aws_iam_role` | 2,676 ms | 239 ms | 698 ms | 171 ms |
| `aws_sns_topic` | 2,452 ms | 179 ms | 661 ms | 120 ms |
| `aws_sqs_queue` | 2,357 ms | 178 ms | 660 ms | 119 ms |
| `aws_cloudwatch_event_rule` | 2,337 ms | 175 ms | 660 ms | 119 ms |
| `sweep` (0 rows) | 2,300 ms | 124 ms | 629 ms | 67 ms |
| `svc-1` | 2,403 ms | 299 ms | 737 ms | 243 ms |
| full ARN | 1,453 ms | 117 ms | 193 ms | 73 ms |
| 117-character term | 2,294 ms | 126 ms | 630 ms | 72 ms |
| `e` (broad) | 3,125 ms | 1,540 ms | 1,532 ms | 1,342 ms |

Every hand-off stop threshold holds: issue-term warm p95 at most 171 ms
(threshold 300 ms); cold at most 239 ms on the stable run and 469 ms on the
first run after container start (threshold 500 ms); the broad `e` is 12%
faster, not slower (threshold: no more than 10% slower). The broad term stays
about 1.3 s: most infra nodes contain an `e`, so the index does not narrow it.
This PR does not fix that case.

The proof shim before this PR measured db hits on the same fixture:
`aws_iam_role` 2,421,964 legacy against 358k full text.

Not measured: scoped-token timing, NornicDB (never selected), and deployed
latency.

## No-Regression Evidence

No-Regression Evidence: with the flag off, with the index not ready, with a
nil reader, on NornicDB, for structured searches and for the Argo CD
category-only read, the handler sends the same statement and params as
origin/main e0e7f03f9. `TestInfraSearchLegacyStatementsByteIdentical` pins
SHA-256 digests of 24 shapes (both backends, scoped and unscoped, free text,
`::` equality, structured, category plus filters, Argo CD only), and
`TestInfraSearchFulltextLeavesLegacyShapesByteIdentical` and
`TestInfraSearchFulltextFlagOffOrNotReadyIsLegacy` check the same digests with
a reader configured. Row equality on Neo4j:
`TestLiveInfraSearchFulltextDifferential` on a fresh pinned Neo4j compares
the fixture rows of the legacy and full-text statements the real handler emits
for 67 bodies, unscoped and under a scoped grant (134 shapes, 91 with fixture
rows, 0 mismatches). It covers key overflow (40 KB name, 70 KB config_path),
keys of exactly 8,000 and 8,001 code points, case, Lucene specials,
whitespace and tab, unicode, U+001F inside a value, duplicate projected rows,
a multi-label node, non-string fields, the `data_type` fallback, the `::`
equality branch, the terraform, cloud, k8s and argocd categories, kind
filters, and USES and MATCHES_STATE grant admission. Removing `DISTINCT` from
the unscoped statement makes it fail. `scripts/verify-query-plan-profile.sh`
PROFILEs `QP-INFRA-RESOURCE-SEARCH-FULLTEXT` on an isolated Neo4j with no
AllNodesScan, NodeByLabelScan or CartesianProduct.

## Observability Evidence

Observability Evidence: `eshu_dp_infra_search_path_total{search_path, reason}`
counts every graph search by path (`fulltext`, `legacy`) and reason (`ready`,
`flag_off`, `not_ready`, `index_not_online`, `unkeyed`, `ft_error`,
`backend`, `no_query`, `query_too_long`). The gauges
`eshu_dp_infra_search_key_ready`, `eshu_dp_infra_search_key_index_online` and
`eshu_dp_infra_search_key_unkeyed_nodes` are recorded on every probe, with
the flag off too, so an operator can confirm readiness before turning the flag
on. The request span `query.infra_resource_search` carries
`eshu.infra_search_path` and, on legacy, `eshu.infra_search_fallback_reason`.
`query.infra_search_key.readiness_changed` logs each readiness change and
`query.infra_search_key.fulltext_fallback` each fallback, with no Cypher text
or raw driver errors. Both reads keep `eshu_dp_neo4j_query_duration_seconds`.
Unit tests read the counter and span attributes from an SDK manual reader and
a span recorder.

## Rollout

1. Run the PR-1 image on every graph writer.
2. After the last pre-PR-1 writer is gone, run the bootstrap backfill. It
   also repairs stale keys.
3. Deploy this PR with the flag off and confirm
   `eshu_dp_infra_search_key_ready` reads 1.
4. Set `ESHU_INFRA_SEARCH_KEY_READ=on` on the API and MCP server and measure.

## Known limits

- `CountUnkeyed` finds nodes with no flag, not stale keys (a key whose fields
  a writer without the clause changed later). Step 2 of the rollout covers
  them.
- A node written unkeyed after a probe is invisible to the index until the
  next probe (at most 5 minutes) turns readiness off. Only a pre-PR-1 writer
  writes such a node.
- The first requests after start, before the first probe finishes, use the
  legacy statement (`not_ready`).
- Both statements order by `name` only, as before. Rows that tie on `name`
  at the page edge can differ between calls and between the two paths, as
  they can today between two legacy calls; the full result sets are equal.
  Adding `, id` to `ORDER BY` would change the NornicDB statement bytes, which
  this PR keeps unchanged, so it is left out.

## Not checked

- Deployed latency and the #7245 QA acceptance (warm and cold p95 under 1 s on
  the issue arguments). The QA image does not carry PR-1 yet.
- Scoped-token timing. The scoped row equality is checked.
- Page-cache-cold timing (the container kept its page cache).
- Concurrent writers during reads.
