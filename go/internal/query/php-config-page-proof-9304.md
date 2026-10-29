# PHP configuration page access-path proof (#9304)

Decision: serve exact-name PHP declaration pages from `content_entities`, with
repository grants in the SQL predicate and a four-field keyset cursor. This is
a read-only query addition; it creates no index or write path. The proof began
2026-10-28 18:32:42 UTC, before implementation, with a 20-minute bound and a
threshold of no full-table scan on the representative equality reads. It
completed before the 2026-10-28 18:33:47 UTC status read.

The disposable PostgreSQL 16 fixture held 120,000 `Variable` rows across 60
repositories. Every thousandth row had `entity_name =
'php_server_mode:mod_php'` (120 rows), and every tenth row had
`entity_name = 'php_directive:memory_limit'` (12,000 rows). Other names were
unique. Rows carried repository, path, line, and entity ID; `VACUUM ANALYZE`
ran before the probes. The columns and primary key match the content entity
store in migration 004. The two relevant indexes match migration 062's
`content_entities_name_trgm_idx` and migration 132's
`content_entities_repo_path_start_idx`:

```sql
CREATE INDEX content_entities_name_trgm_idx
  ON content_entities USING gin (entity_name gin_trgm_ops);
CREATE INDEX content_entities_repo_path_start_idx
  ON content_entities (repo_id, relative_path, start_line, entity_id);
```

The read-only probes used `EXPLAIN (ANALYZE, BUFFERS)` on these statements,
with `LIMIT 51` for a requested page of 50:

```sql
SELECT entity_id, repo_id, relative_path, start_line
FROM content_entities
WHERE entity_name = 'php_server_mode:mod_php'
  AND entity_type = 'Variable'
ORDER BY repo_id, relative_path, start_line, entity_id
LIMIT 51;

SELECT entity_id, repo_id, relative_path, start_line
FROM content_entities
WHERE entity_name = 'php_server_mode:mod_php'
  AND entity_type = 'Variable'
  AND (repo_id, relative_path, start_line, entity_id) >
      ('repo-024', 'src/file-0049000.conf', 1, 'entity-0049000')
ORDER BY repo_id, relative_path, start_line, entity_id
LIMIT 51;

SELECT entity_id, repo_id, relative_path, start_line
FROM content_entities
WHERE entity_name = 'php_directive:memory_limit'
  AND entity_type = 'Variable'
ORDER BY repo_id, relative_path, start_line, entity_id
LIMIT 51;
```

The rare first page used a Bitmap Index Scan on
`content_entities_name_trgm_idx`, then top-N sort: 0.791 ms, 175 shared
buffer hits, 120 exact heap rows. The rare keyset page used the same index
and 175 shared hits: 0.360 ms. The dense directive page used an ordered Index
Scan on `content_entities_repo_path_start_idx`: 0.150 ms and 16 shared
buffers. None used a sequential scan. `SearchPHPConfigEntities` was then
exercised against this fixture through the actual Go reader: it returned all
120 rare rows exactly once across 7-row pages and a one-repository grant
returned its 2 rows. The returned `source_cache` was empty.

This synthetic proof establishes the access path for the tested cardinalities
and keyset boundary. It is not a production latency claim for an 8-million-row
corpus or for a dense PHP directive in every file. The query reuses the
existing indexes, caps a page at 200, and reports the next cursor when more
rows exist. The `postgres.query` span records
`db.operation=search_php_config_entities`; it does not attach source values.

## Contract decision

The parser retains the existing `parsed_file_data` and `content_entity` fact
kinds. Guarded PHP settings and mode declarations use the existing `Variable`
entity bucket with names `php_directive:<lowercase name>` and
`php_server_mode:<mode>`. Metadata carries value, line, section or stage, and
source kind where available. This is additive parser output with no factschema
version change. The new HTTP route and MCP tool expose the same bounded page
shape: `results`, `count`, `limit`, `truncated`, and `next_cursor` on a cut page.
The cursor binds the exact directive/mode query and repository filter. The
source body is absent from the response. Both surfaces use the existing
content-index truth basis, so a repository declaring conflicting modes returns
separate rows and no inferred effective runtime mode.

## Managed workspace copy no-regression proof

No-Regression Evidence: the PHP change adds one `.htaccess` selection branch to
`preserveFilesystemHiddenPath`. The decision threshold was at most 20% median
regression for a clean 1,000-file copy; the timed run was bounded to 15
minutes. A disposable Git fixture held 1,000 tracked small
`.go` files (124,000 copied bytes) and no `.htaccess`. The base revision
and the PHP revision ran the unchanged
`BenchmarkFilesystemManagedCopyCommitAttributionLargeRepository/copy-bound-full-managed-copy`
with the same fixture, `GOMAXPROCS=2`, `-benchtime=3x -count=5 -benchmem`,
Go 1.26.6 on Linux arm64, and `/tmp` tmpfs. The ABBA sequence ran from
2026-10-28 19:34:07 to 19:34:57 UTC. For each revision, the command was:

```sh
ESHU_BENCHMARK_REPOSITORY=/tmp/eshu-9304-perf-fixture GOMAXPROCS=2 \
  go test ./internal/collector/repo/git -run '^$' \
  -bench '^BenchmarkFilesystemManagedCopyCommitAttributionLargeRepository/copy-bound-full-managed-copy$' \
  -benchtime=3x -count=5 -benchmem
```

| Revision/run | Five `ns/op` observations | Median |
| --- | --- | ---: |
| Base A | 38,083,259; 38,233,003; 36,053,674; 37,720,193; 37,716,454 | 37,720,193 |
| PHP A | 35,022,756; 33,615,090; 33,643,002; 32,467,198; 33,850,137 | 33,643,002 |
| PHP B | 35,394,138; 35,547,985; 36,581,488; 37,703,658; 36,859,483 | 36,581,488 |
| Base B | 38,235,322; 37,263,487; 37,162,857; 38,269,296; 37,184,502 | 37,263,487 |

All 20 samples reported 1,000 files and 124,000 bytes; each was three
iterations and exited 0. Combined medians were 37,718,324 ns/op before and
35,208,447 ns/op after (6.65% lower). This is a no-regression result for the
unchanged-file path on this synthetic tmpfs fixture, not a production speedup
claim. The `.htaccess` positive path copies one additional bounded source file;
`TestFilesystemCopiedHtaccessReachesFactsWithoutStoredBody` proves that path
and verifies its raw body is absent from content facts. There is no queue in
this copy operation; terminal file and byte counts are above.

Observability Evidence: copy errors still pass through the existing collector
sync error/status path. The PHP parser adds bounded `php_config_dropped`,
`php_config_unparsed`, and safe `php_config_skips` counts to its parsed payload;
its evidence query uses the existing `postgres.query` span with
`db.operation=search_php_config_entities`. No source body or credential value is
attached to these signals.
