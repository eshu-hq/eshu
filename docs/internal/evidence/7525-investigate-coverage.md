# #7525 dead-code investigation coverage skips the entity aggregate

Scope: `POST /api/v0/code/dead-code/investigate` and the MCP tool
`investigate_dead_code` for a repository-scoped request. Parent: #7249.

## Decision and change

The coverage block called the full `ContentReader.RepositoryCoverage`, whose
entity statement is
`SELECT entity_type, count(*), max(indexed_at) FROM content_entities WHERE repo_id = $1 GROUP BY entity_type`.
The repository owner chose to drop `entity_count` from the investigation
response rather than maintain a rollup (a per-repository entity rollup cannot be
kept exact across the incremental writes, `repo_id` moves, and retention prunes
the content writer performs).

The investigation now reads:

- `ContentReader.RepositoryContextCoverage` (the narrow file-language summary
  repository context already uses) for `file_count` and `languages`, unchanged
  in meaning and ordering (`count(*) ... GROUP BY language ORDER BY file_count DESC`);
- the new `ContentReader.RepositoryFilesLastIndexedAt`, one
  `SELECT max(indexed_at) FROM content_files WHERE repo_id = $1` statement, for
  `content_last_indexed_at`. No index is added: `content_files` has the
  `(repo_id, relative_path)` primary key, and this is the same statement full
  coverage already issued for its file timestamp.

Contract changes in the response `coverage` block:

- `entity_count` is removed (the key is absent, not zero). Nothing in the
  repository read it: the investigation test asserted only
  `content_last_indexed_at`, the golden snapshot asserts only
  `coverage.query_shape`, OpenAPI declares `coverage` as a free-form object, and
  no cassette, console, SDK, or doc consumed the field. External HTTP clients
  were not checked.
- `content_last_indexed_at` is the files maximum only. It was the maximum of the
  files and entities timestamps; the two were equal on the three repositories
  sampled but nothing guarantees it, so the value can now be older when
  entities were indexed after the last file.
- One edge changes `freshness_state`. A repository with content entity rows but
  no content file rows used to take its timestamp from the entities and report
  `content_index_available`; it now reports `not_reported`, because the
  timestamp comes from the files. The schema allows that state (entities have no
  foreign key to files), but the content writer stamps files and entities of one
  write with the same `indexed_at`, so it needs a write that carries entities
  and no file rows, and `file_count` is 0 for such a repository anyway.
- `file_count`, `languages`, and `content_coverage_available` keep their
  meaning, and so does `freshness_state` outside that edge. The repository
  stats, coverage, and
  branches routes still use full `RepositoryCoverage` and still report
  `entity_count`.

## Proof

Regression (RED before, GREEN after), in
`go/internal/query/codequery/deadcode/investigation_coverage_test.go`: a content
store double records its coverage reads. The investigation made one full
`RepositoryCoverage` call and returned `entity_count` before the change; it now
makes none, issues one narrow context read and one files `max(indexed_at)`
read, omits `entity_count`, and returns `file_count`, `languages`, and
`content_last_indexed_at` from the narrow reads. A zero files timestamp reports
`freshness_state: not_reported` with no timestamp, and a failed narrow read
fails the request instead of falling back to the full read.
`content_reader_files_indexed_at_test.go` pins the reader SQL (one
`content_files` statement, bound to the repository, no `content_entities`
statement).

Performance Evidence: measured on the QA replica with read-only
`EXPLAIN (ANALYZE, BUFFERS)` on the largest repository sampled (measurements
reported on #7525, not repeated here): the removed entity aggregate read about
134.7k heap blocks (about 1 GiB) and took 294-369 ms warm, with a 2.9 s first
call. The endpoint after-numbers are NOT_CHECKED: this change was proven
locally only, and the owner's deploy plus the replay sweep measure the endpoint
latency. The bound is the repository's `content_files` row set, which is not
larger than the file summary repository context already reads.

No-Observability-Change: the existing `postgres.query` spans cover the reads
(`db.operation=repository_context_coverage` for the file summary and the new
`repository_files_last_indexed_at` for the timestamp, both on
`db.sql.table=content_files`), and the `query.dead_code_investigation` handler
span is unchanged. The removed `repository_coverage` read is simply no longer
issued from this route.
