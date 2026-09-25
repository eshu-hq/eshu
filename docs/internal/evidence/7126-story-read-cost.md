# Story read cost: measurements and proof (#7126)

Moved from `docs/public/reference/http-api/story-routes.md` (section "Story read
cost (#7126)") to keep that page under the 450-line context-stories limit. The
public page keeps the route-visible behavior; this record keeps the
measurements and proof commands.

Performance Evidence: local PostgreSQL 16, 180,000 mention and claim facts over
600 generations with about 5 KB payloads, 9,000 semantic facts, interleaved runs
alternating the first mover. Target facts, median of 9: 556.8 ms and
2,217,962 shared hits with 126,957 reads before, 27.8 ms and 638 hits with
5,608 reads after; a 60,000-fact rerun gave 638.5 ms and 14.5 ms. Coverage on
one 240,000-entity repository, median of 9: 75.0 ms for the three scans, 25.8 ms
for the single pass. On the shared read-only QA database the split statement ran
in 301 ms warm before migration 124, of which the semantic branch was 225 ms:
no index covered that kind, so it cost 7,370 index searches even with zero rows.
The earlier single statement measured 0.76 s warm and 44.9 s cold on the same
database; the cold figure and the 301 ms warm figure are different cache states
and are not a before/after pair, and the post-change cold cost on QA is
unmeasured. On a disposable postgres:18.6 scale run (600,000 mention and claim
facts over 600 generations with about 5 KB payloads, 30,000 semantic facts,
median of 9): the whole statement took 1,386.6 ms (7.4M shared hits, 421k reads)
in its pre-#7126 single-statement form and 7.0 ms (776 hits, 392 reads) as the
split statement with both indexes; the semantic branch alone dropped from 39.7 ms
(6,912 hits, 11,309 reads, heap scan) to 0.06 ms (18 hits) with migration 124's
index, dropped and rebuilt from the shipped migration text on identical data.
Both branches use their GIN index in a custom and a generic plan
(`TestDocumentationTargetFactsUsesRefsIndexLive` prepares the statement with
`plan_cache_mode=force_generic_plan`). Reproduce with
`ESHU_TEST_DOCUMENTATION_TARGET_FACTS_ROWS` and
`ESHU_TEST_CONTENT_COVERAGE_ROWS` (see `documentation_target_facts_plan_live_test.go`
and `content_reader_coverage_differential_live_test.go`).

Write cost of migrations 122 to 124 (`fact_records` ingest): each index adds
one entry only for the rows its predicate matches. Measured on postgres:18.6 by
inserting 50,000-row batches into two copies of `fact_records` that differ only
by these three indexes (101 versus 104 indexes), ten alternating rounds, WAL
bytes per batch (wall time on the laptop VM moved by up to 15% between identical
runs and is not cited). A batch that is all `content_entity` rows: +0.8% WAL. All
documentation facts with refs: +3.1%. All documentation facts without refs
(migration 122): +9.9%. All support-kind facts (migration 123): +5.5%. All
`semantic.documentation_observation` facts (migration 124, a GIN entry per
row): +15.7%. A mixed batch in which 0.3% of rows match one of the three
predicates (far denser than the QA corpus's unreferenced documentation facts,
about 7,000 of 76 million rows): -1.8%, within noise.
The per-shape figures are upper bounds for a batch made entirely of one matching
kind; semantic observation facts exist only where a semantic provider is
enabled.

No-Regression Evidence: live differentials on real PostgreSQL return identical
ordered rows and payloads for the old and new statements (all three kinds, ties
on `observed_at`, tombstones, ACL join, limits 1, equal, above, and the default
cap) and identical coverage values (populated, tied, empty, and unknown repos):
`go test ./internal/query -run 'TestDocumentationTargetFacts|TestRepositoryCoverageSinglePass|TestGetRepositoryStoryListsRepositoryFilesOnce' -count=1`
with `ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DSN` and
`ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DISPOSABLE=1` set.

No-Regression Evidence (truncation marker): `go test ./internal/query ./internal/query/repository -run 'Test(GetRepositoryStoryDisclosesSemanticReadTruncation|LoadRepositorySemanticOverviewReportsTruncationAtCap|GetRepositoryStoryListsRepositoryFilesOnce)' -count=1`
covers cap-1, cap, and cap+1 for the entity and the file list, the 5,001-row read
limit, clipping back to 5,000, and the response fields; dropping the sentinel
flag fails both tests.

Observability Evidence: the `repository_query.stage_completed` event for the
`semantic_overview` stage gains a `truncated` attribute beside `file_count`, and
the response carries the reason in the existing `limitations` and
`answer_metadata` fields; no metric, span, or runtime knob is added.

No-Observability-Change (other stages): existing `postgres.query` spans and
`repository_query.stage_completed` logs stay; the `content_files` stage is no
longer emitted because the read it timed is shared.

## Source-only and support count proof commands (migrations 122 and 123)

No-Regression Evidence:

```bash
cd go && ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DSN=... ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DISPOSABLE=1 \
  go test ./internal/query -run 'TestDocumentationSourceOnly(CountsFactsWithoutRefKeys|UsesPartialIndex)Live' -count=1
cd go && go test ./internal/query -run 'Test(DocumentationSourceOnlyIndexMatchesQuery|DocumentationNoStructuredRefsPredicateIsTwoValued|BuildDocumentationSourceOnlySQLStaysAggregateOnly)' -count=1
```


No-Regression Evidence:

```bash
cd go && ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DSN=... ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DISPOSABLE=1 \
  go test ./internal/query -run 'TestServiceStoryTargetSupportUsesSupportKindsIndexLive' -count=1
cd go && ESHU_POSTGRES_DSN=... go test ./internal/query -run 'TestServiceStoryTargetSupportSQLSemanticsLive' -count=1
cd go && go test ./internal/query -run 'TestServiceStoryTargetSupport(SQLInlinesKindLiteralsForIndex|IndexMatchesQuery|SQLProbesFactKindIndex)' -count=1
```
