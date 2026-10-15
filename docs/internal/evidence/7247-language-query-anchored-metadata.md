# #7247 language-query id-anchored content metadata

Refs #7247 (child of #7098). This note covers the language-query function
branch only. The inventory branches and the name-query scan budget are out of
scope.

## Cause

`POST /api/v0/code/language-query` reaches Postgres on its graph-served
branches only through `enrichLanguageResultsWithContentMetadata`
(`go/internal/query/language/metadata.go`). Before this change the
enrichment issued a second, independent content page read,
`search_entities_by_language_and_type`. That statement uses
`EXISTS(filter) AND filter ORDER BY relative_path, start_line, entity_name
LIMIT n` (`go/internal/query/content_reader_entity_search.go`). It then merged
that page into the graph page by repository, path, label, name and start line.

The two pages are different sets. The graph page orders by
`f.relative_path, e.name` and matches names with case-sensitive `CONTAINS`
(`go/internal/query/language/cypher.go`). The content page orders by
`relative_path, start_line, entity_name` and matches with `ILIKE`. On the
coordinator's PostgreSQL 18 fixture (843,495 `content_entities` rows, one
repository with 45,495 Functions), the two pages shared 2 of 50 and 28 of 200
rows. So 172 of 200 graph rows got no content metadata. The read also scaled
with the candidate rows rather than with the page. One generic-plan case took
46,817 buffers (issue comment of 2026-10-15).

## Change

For a graph label whose graph `entity_id` is the content primary key, the
enrichment reads the graph rows' own content rows by id. The new
`ContentReader.ListEntityMetadataByIDsForAccess` method
(`go/internal/query/content_reader_entities_by_ids.go`) issues one statement:

```sql
SELECT entity_id, repo_id, relative_path, entity_type, entity_name,
       start_line, end_line, coalesce(language, ''), metadata
FROM content_entities
WHERE entity_id = ANY($1) AND repo_id = ANY($2)   -- or repo_id = $2, or no scope for unscoped callers
```

It has no `source_cache`, `ORDER BY`, or `LIMIT`. `entity_id` is the primary
key, and the id set is capped at 200 (`taxonomy.MaxLanguageEntityIDLookup`).
A content row merges only into the graph row with the same id and the same
repository.

The label set (`contentIDAnchoredLabels` in
`go/internal/query/language/metadata_anchored.go`) is Function, Class, Struct,
Enum, Union, Macro, TypeAnnotation, Annotation and TerragruntDependency. It is
derived from code, not copied:

- `projector/canonical/entity_identity.go` derives the graph id with
  `content.CanonicalEntityID(repo, path, type, name, line)` for labels in
  `canonicalNamePathLineEntityLabels`, whatever id the producer supplied.
  `storage/cypher/canonical_node_writer_entities.go` writes that id as both
  `uid` and `id`, and the route's Cypher returns `e.id as entity_id`.
- Every content mint site (`content/shape/materialize.go`,
  `projector/runtime/projection.go`) uses
  `content.CanonicalEntityIDWithMetadata`. That function differs from
  `CanonicalEntityID` only for dependency-identity Variables
  (`content/dependency_identity.go`).
- The set is the route-reachable members of the derived-id set, minus
  Variable. `metadata_anchored_identity_test.go` runs every member through
  `canonical.ExtractEntityRows` with dependency-shaped metadata and three
  incoming ids (minted, stale, none), and asserts graph id equals content id.
  A mutation that added Module, SqlTable and Variable to the set failed that
  test with "projector wrote 0 graph rows" for Module and Variable, and
  "graph id producer-supplied-stale-id != content id" for SqlTable.

Labels outside the set keep the page read in this PR.

## Not a rows-equal change

The row set, the order, and every non-metadata field are unchanged
(`TestLanguageMetadataAnchoredLookupKeepsRowSetAndOrder`). Metadata,
`semantic_summary` and `semantic_profile`, the truth basis and
`source_backend` do change on rows the page read missed. An answer can move
from `authoritative_graph`/`graph` to `hybrid`/`hybrid_graph_and_content`.
This fixes silently incomplete answers. It does not regress them.

Test fixtures that changed, and why: each one drove a real `ContentReader`
through a fake driver that queued the old page-read answer, with a content id
(`content-1`, `js-fn-1`, `annotation-1`) different from the graph id
(`graph-1`). For an anchored label that pair cannot exist in production. Each
fixture now queues the id-lookup answer (nine columns, no `source_cache`)
under the graph row's id. The assertions are unchanged.

- `language_query_metadata_test.go`: `TestEnrichLanguageResultsWithContentMetadata`,
  `TestEnrichLanguageResultsWithContentMetadataSkipsUnmatchedRows` (keeps a
  non-matching id, so it still proves an unmatched row is not merged),
  `TestEnrichLanguageResultsWithContentMetadataAnnotation`.
- `javascript_semantics_test.go`:
  `TestEnrichLanguageResultsWithContentMetadataJavaScriptMethod`,
  `TestEnrichLanguageResultsWithContentMetadataPreservesGraphJavaScriptFields`.
- `language_query_metadata_merge_test.go`:
  `TestEnrichLanguageResultsWithContentMetadataReportsMergedEvenWhenGraphShadowsAllContentValues`.
- `language_query_metadata_promotion_test.go`:
  `TestEnrichLanguageResultsWithContentMetadataPreservesPythonGraphMetadata`,
  `TestEnrichLanguageResultsWithContentMetadataPromotesExistingPythonSemanticsWithoutContent`
  (zero-row fixture; it passed either way and was updated so it names the
  read it exercises).
- `language_query_metadata_handler_test.go`:
  `TestHandleLanguageQuery_AnnotationPrefersGraphPathAndEnrichesMetadata`.

No golden snapshot moved. `testdata/golden/e2e-20repo-snapshot.json` holds
graph shape, not route responses. The MCP catalog sweep classifies
`execute_language_query/default` as `allowlisted`, which this change does not
touch.

## Performance Evidence

Performance Evidence: measured on a disposable PostgreSQL 18.6 container
(`postgres:18-alpine`, `shared_buffers=256MB`). Every bootstrap migration
through 177 was applied, so `content_entities_pkey` and migrations 164 and 174
exist. The fixture has 160,000 `content_entities` rows: one 40,000-row PHP
repository over 2,500 files, and three 40,000-row PHP repositories whose paths
sort first. The ids are the first 200 rows of the graph-page order. The page
read is the replaced statement for the same repository and language with the
two-character pattern `qz`, which matches 100 rows. That is the
selective-pattern shape the issue named as the worst generic-plan case.
Prepared with typed parameters, then `EXPLAIN (ANALYZE, BUFFERS)`.
Proof: `TestEntityMetadataByIDsLive`
(`go/internal/query/content_reader_entity_metadata_by_ids_live_test.go`),
three runs with identical buffers.

| Read | plan_cache_mode | Rows | Shared buffers | Plan |
| --- | --- | ---: | ---: | --- |
| id lookup (new) | auto (custom) | 200 | 458 | `content_entities_pkey` index scan, no Seq Scan |
| id lookup (new) | force_generic_plan | 200 | 458 | `content_entities_pkey` index scan, no Seq Scan |
| page read (old) | auto (custom) | 100 | 2,259 | |
| page read (old) | force_generic_plan | 100 | 157,469 | |

Execution time, non-gating, shared host: the id lookup took 0.45-0.65 ms in
both modes. The page read took 16.2-16.5 ms (custom) and 64.6-112.0 ms
(generic).

The test gates on the new read's shape: exact rows, a primary-key index scan,
no Seq Scan, and at most 10 buffers per id. The old read's buffers are logged
and not asserted. On the coordinator's 843,495-row fixture the same lookup
read 563 buffers in about 0.8 ms under both plan modes. The old page read
took 46,817 buffers (604 ms) under a generic plan.

Statement count: an anchored request issues one content statement, as before.
The id lookup replaces the page read, and a request whose graph page has no
ids issues none. Non-anchored labels still issue one page read.
`TestLanguageMetadataAnchoredLookupEnrichesEveryGraphRow` asserts zero page
reads and one id lookup. `TestLanguageMetadataNonAnchoredLabelsKeepThePageRead`
asserts the reverse for Module, Variable and SqlTable.

Reproduce:

```bash
cd go
ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN='postgres://postgres:<pw>@127.0.0.1:<port>/postgres?sslmode=disable' \
ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=1 \
go test ./internal/query/ -run TestEntityMetadataByIDsLive -count=1 -v
```

## Observability Evidence

Observability Evidence: the request span `query.language_query` carries
`eshu.language_query.metadata_path` (`anchored` or `page_read`),
`eshu.language_query.metadata_rows_enriched` and
`eshu.language_query.metadata_rows_missing`. The new counter
`eshu_dp_language_query_metadata_rows_total{metadata_path, outcome}` has
closed labels: `anchored` or `page_read`, and `enriched` or `missing`. It is
registered in `go/internal/telemetry/instruments_language_query_metadata.go`
and wired through `Handler.Instruments` in `cmd/api` and `cmd/mcp-server`.
The id lookup's `postgres.query` span reports
`db.operation=list_entity_metadata_by_ids` and the id count.
`metadata_anchored_telemetry_test.go` asserts the span attributes and counter
values for both paths. An operator at 3 AM reads `page_read/missing` against
`page_read/enriched` to size the remaining page-read gap. A non-zero
`anchored/missing` means graph rows with no content row: projection lag or
drift, not a query fault.

## NOT_CHECKED

- Deployed cold and warm p95 on the original #7247 arguments (the 7,097-file
  PHP repository). No deployed image was measured from this machine.
- Whether migration 174 is applied where the issue's p95 figures were taken.
  It does not affect this read, which uses the primary key.
