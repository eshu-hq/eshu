# #8833 part 3: cleanup runbook, purge proofs, and the retention check

Part of #8807. The design is in the #8813 records: the cleanup runbook and the
purge SQL page. The operator page this part ships is
[Credential File Cleanup](../../public/deployment/credential-file-cleanup.md).
Every figure below was produced on this branch, against PostgreSQL 18.6 in a
scratch container on a shared 16 vCPU arm64 host.

## Retention check

The #8813 ruling accepted the superseded-payload window, with no scrub, on the
condition that none of the five copy kinds is exempt from the default retention
policy. `TestGenerationRetentionPrunesCredentialCopiesByDefaultLive` seeds one row of each kind,
carrying a credential, into three superseded generations and the active
generation. It then runs `PruneSupersededGenerations` with
`DefaultGenerationRetentionPolicy()`.

| Kind | Superseded 100 days ago | Oldest of 25 superseded, more than 7 days old | Recent superseded | Active |
| --- | --- | --- | --- | --- |
| `content` fact | pruned | pruned | kept | kept |
| `content_entity` fact | pruned | pruned | kept | kept |
| `reducer_eshu_search_document` fact | pruned | pruned | kept | kept |
| `eshu_search_index_documents` | pruned | pruned | kept | kept |
| `eshu_search_index_terms` | pruned | pruned | kept | kept |

None is exempt, so the ruling's condition holds. Superseded copies live at most
until the generation leaves the newest-24 / 7-day window, and never past the
90-day ceiling. Reads serve only the active generation.

## Purge proofs

The proofs run the operator page's SQL fences verbatim; nothing is retyped. The
psql variables are bound the way `psql -v` binds them.

`TestCredentialCleanupPurgeFromThePageLive` seeds:

- five policy-path shapes and five non-matching names;
- a row whose generation is NULL;
- an entity with no file row;
- an orphan index document and an orphan fact;
- a superseded generation with its own document;
- a repository rebuilt after the watermark.

| Check | Result |
| --- | --- |
| Census before | every exit column is above 0, and so is `eshu_dp_content_policy_raw_rows` |
| Statement L | lists both repositories |
| First pass | each of refs, entities, files, A, and B touches rows |
| Census after | every exit column is 0, and so is the gauge |
| Second pass | 0 rows everywhere |
| Survivors | non-matching paths keep their bodies; the post-watermark repository and the superseded generation keep their documents |
| Active-document read | `ListActiveDocuments` serves no seeded context text for a matched path |
| Seeded violation | with the watermark guard removed from the files statement, the rebuilt row is blanked and the test fails |

`TestCredentialCleanupPurgeRacesTheContentWriterLive` runs the files,
entities, and references statements while the production `ContentWriter`
rewrites the same paths under a post-watermark generation. It runs 40
iterations with random starts. On odd iterations the purge holds its batch
locks for 150 ms before it rewrites, so the writer waits on them.

- Both orders occur in every run; the test fails if one order never runs. Two
  runs split 34/6 and 33/7, purge first against writer first.
- In every iteration each row ended with the post-watermark generation, the
  redacted body, the rebuilt entity cache, and its reference row.
- No raw body and no blank row was left, and no deadlock occurred.
- With the watermark guard removed from the page's files statement, the test
  fails (`iteration 24: 0 of 5 files hold the rebuilt body`).

The narrow window between the lock statement's snapshot and its row lock is
not forced here without editing the page's SQL. The #8813 purge page's measured
`pg_sleep` race covers that window.

`TestCredentialCleanupSearchFactKindOwnsNoKeySidecar` pins the premise of
statement B: `reducer_eshu_search_document` owns no key sidecar rows.

## In-process search cache

The SQL purge does not reach one copy: the API and the MCP server both wire
`NewCachedPersistedLocalSemanticSearchHybrid`, in
`cmd/api/semantic_search_vector_wiring.go` and
`cmd/mcp-server/semantic_search_vector_wiring.go`. That cache holds the scope's
documents, `ContextText` included, in an LRU with no TTL. Its key comes from
`loadSemanticSearchSnapshotQuery`:

- the active generation;
- the document and vector projection revisions;
- the document count;
- the vector build fence.

The purge changes none of these. `TestPersistedSemanticSearchCachesStableReadySnapshotAcrossQueries`
already proves that an unchanged snapshot reloads no documents. The runbook
therefore restarts the API and MCP server replicas after each run, and the
exit criterion includes that restart.

The purge also leaves the deleted documents' rows in `eshu_search_vector_values`
and `eshu_search_vector_metadata`. `readyVectors` loads vectors only for the
document IDs that remain, so semantic search stays ready and no read returns
them. The runbook lists them under what the cleanup does not reach.

## Performance Evidence

Performance Evidence: `EXPLAIN (ANALYZE, BUFFERS)` of the five statements, one
batch each, run with a throwaway test that is not committed.

The fixture is a bootstrapped schema holding 145,000 files across 20
repositories, with 145,000 entities, references, index documents, and
search-document facts, and 580,000 index terms. Every 150th path is a tfvars
file, so the measured repository holds 483 policy paths.

| Statement | Plan | Time |
| --- | --- | --- |
| refs, lock | bitmap scan of `content_file_references_repo_path_idx` | 14.2 ms |
| refs, rewrite | primary-key lookups from the batch | 6.3 ms |
| entities, lock | bitmap scan of `content_entities_repo_language_type_idx` | 13.2 ms |
| entities, rewrite | `content_entities_pkey` | 20.3 ms |
| files, lock | bitmap scan of `content_files_language_repo_idx` | 16.1 ms |
| files, rewrite | `content_files_repo_path_pattern_idx` | 49.0 ms |
| A | bitmap scan of `eshu_search_index_documents_repo_idx`, then term deletes pruned to the hash partitions' primary keys | 55.5 ms |
| B | sequential scan of `fact_records` (145k rows), then `fact_records_pkey` | 31.5 ms |

At this scale every batch takes under 60 ms.

Statement B has no `repo_id` index. A deployment's `fact_records` holds every
fact kind, not only search documents. The active-generation join leaves the
planner a path through `fact_records (scope_id, generation_id, fact_kind,
observed_at)`, which would make B's cost follow the active search documents
rather than all facts. That is a theory: a measurement against a
deployment-sized `fact_records` is NOT_CHECKED.

## Observability Evidence

No-Observability-Change: part 3 adds no runtime code. The cleanup's operator
signals are part 2's gauge `eshu_dp_content_policy_raw_rows`, which the purge
test reads, and the census on the operator page.
