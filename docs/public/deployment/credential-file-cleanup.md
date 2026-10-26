# Credential File Cleanup

Eshu stores `.yarnrc.yml` marker-redacted and `*.tfvars` and `*.tfvars.json`
as an allowlist skeleton (#8833; see
[Admitted files](../languages/admitted-files.md#content-store-policy)). Rows
written before that release can still hold the verbatim file. This page is the
one-shot cleanup for them: the run order, the read-only census, the purge SQL,
the exit signal, and what the cleanup cannot reach.

The design and the measurements behind it are in the #8813 records
(`docs/internal/design/8813-credential-file-exposure*.md`).

## When the re-collection is enough

The release moves the parser registry fingerprint, so a forced full snapshot of
each repository stores a new generation. The collector writes the redacted body
over the old row, and the search documents are rebuilt from it. The fingerprint
alone schedules nothing: the reconciliation sweep or a reindex request has to
reach the repository. The SQL below covers what the re-collection does not:

- repositories the sweep has not reached yet;
- rows an ingester still running the old binary wrote after the reindex
  request;
- repositories no ingester collects any more.

## Run order

1. Deploy the release everywhere, and wait until every ingester runs it. A
   repository an old binary re-parses after step 2 counts as fresh and is not
   parsed again, so its raw copy would be written after the watermark.
   Generations a new-binary ingester collects between this step and step 2 are
   ingested before `W`, so the census and the purge guards treat their copies
   as raw; run step 2 immediately after every ingester runs the release to
   keep that window small.
2. Send `POST /api/v0/admin/reindex`. Record the response `requested_at` as the
   watermark `W`. It is also stored as
   `runtime_ingester_control.reindex_request_requested_at` for the `repository`
   ingester.
   - Raise `ESHU_REPO_RECONCILE_MAX_PER_CYCLE` for the drain (for example to
     50). Do not set it to `0`: that removes the cap, and every due repository
     is forced in one cycle.
3. Run the purge statements below for every repository statement L returns,
   with `w = W`. The files, entities, and references statements lock the batch
   first and re-test the "ingested before `W`" guard in the same transaction,
   so those three can run while the reindex rebuilds. Statements A and B carry
   the guard only in the batch select, with no lock and no re-check, so until
   #9166 lands this page claims no concurrent-rebuild safety for them.
4. Watch `eshu_dp_collector_reconciliation_due_unvisited`. When it reads zero
   for every reason, run the statements again. The second run catches rows
   that an old binary wrote during the rollout, and repositories no sweep
   visits.
5. After each run, restart the API and MCP server replicas (a rolling restart
   is enough). Both keep an in-process semantic and hybrid search index. The
   cache key is the scope's active generation and projection revision, and
   the purge changes neither. A replica that indexed a scope before the purge
   keeps returning the purged `context_text` until it restarts or the scope's
   rebuilt generation becomes active.
6. Restore `ESHU_REPO_RECONCILE_MAX_PER_CYCLE`.

The drain takes `ceil(repositories / cap)` selection cycles.

## Exit criterion

The cleanup is done when all of these hold, the two readings on two
consecutive scrapes:

- The API and MCP server replicas restarted after the last run.
- `eshu_dp_content_policy_raw_rows` reads zero, or every remaining row sits on
  a metadata-policy path. The purge never touches `.npmrc` or
  `gradle-wrapper.properties` — the metadata policy stores an empty body going
  forward — so a legacy row on those paths keeps its raw body and its finding
  lines until generation retention prunes the generation, and the gauge keeps
  counting them. The census below attributes the remainder: `raw_files` reads
  zero for the `yarnrc_yml` and `tfvars` kinds.
- The census below reads zero in `raw_files` and `files_with_raw_finding` for
  the `yarnrc_yml` and `tfvars` kinds (the `npmrc` and `gradle_wrapper` kinds
  may keep legacy rows, as above), and zero in `raw_entity_rows`,
  `raw_reference_rows`, `raw_search_documents`, and `raw_search_fact_payloads`.

`files`, `repos`, `reference_rows`, `search_documents`, and
`search_fact_payloads` size the work and never reach zero: a redacted row keeps
a non-empty body.

## Census

Read-only, and it returns counts, never values. Findings mean something only
while `content_file_secret_lines_state.state` is `ready`. Census 2 counts all
four governed kinds on the lowercased path — `(^|/)\.yarnrc\.yml$`,
`\.tfvars(\.json)?$`, `(^|/)\.npmrc$`, and
`(^|/)gradle-wrapper\.properties$` — so a remaining gauge row is attributable
to a metadata-policy legacy row when `raw_files` reads zero for `yarnrc_yml`
and `tfvars`. The purge statements keep the narrower predicate
`(^|/)\.yarnrc\.yml$|\.tfvars(\.json)?$`: they never touch `.npmrc` or
`gradle-wrapper.properties`. Keep single backslashes in `psql`: a doubled
backslash matches no path and reads a false zero.

```sql
-- census 1: is the side table current? Zero findings only mean something when ready.
SELECT state FROM content_file_secret_lines_state;

-- census 2: files per file type. Exit columns: raw_files, files_with_raw_finding.
-- The npmrc and gradle_wrapper rows are exit-scoped, not exit-zero: the purge
-- never touches metadata-policy paths, so a legacy row there keeps its raw
-- body (see Exit criterion).
-- With no reindex requested yet, the watermark is now(), so every copy counts raw.
-- The watermark is never cleared: after an earlier reindex this census undercounts
-- until the reindex request of step 2 moves it, so read it after step 2.
WITH w AS (SELECT COALESCE((SELECT reindex_request_requested_at
                            FROM runtime_ingester_control
                            WHERE ingester = 'repository'), now()) AS at)
SELECT
  CASE WHEN lower(f.relative_path) ~ '(^|/)\.yarnrc\.yml$' THEN 'yarnrc_yml'
       WHEN lower(f.relative_path) ~ '(^|/)\.npmrc$' THEN 'npmrc'
       WHEN lower(f.relative_path) ~ '(^|/)gradle-wrapper\.properties$' THEN 'gradle_wrapper'
       ELSE 'tfvars' END                                   AS file_kind,
  max(w.at)                                                AS watermark,
  count(*)                                                 AS files,  -- sizing, not an exit column
  count(DISTINCT f.repo_id)                                AS repos,  -- sizing, not an exit column
  count(*) FILTER (WHERE f.content <> ''
    AND COALESCE(g.ingested_at, '-infinity') < w.at)       AS raw_files,
  count(*) FILTER (WHERE EXISTS (
    SELECT 1 FROM content_file_secret_lines s
    WHERE s.repo_id = f.repo_id AND s.relative_path = f.relative_path
      AND NOT s.suppressed
      AND s.line_text !~ 'REDACTED_(aws_access_key|private_key|slack_token|api_token|password_literal|secret_literal)'))
                                                           AS files_with_raw_finding
FROM w, content_files f
LEFT JOIN scope_generations g ON g.generation_id = f.generation_id
WHERE lower(f.relative_path) ~ '(^|/)\.yarnrc\.yml$|\.tfvars(\.json)?$|(^|/)\.npmrc$|(^|/)gradle-wrapper\.properties$'
GROUP BY 1;

-- census 3: derived copies. Exit columns: the raw_* columns. Counts only.
WITH w AS (SELECT COALESCE((SELECT reindex_request_requested_at
                            FROM runtime_ingester_control
                            WHERE ingester = 'repository'), now()) AS at)
SELECT
  (SELECT count(*) FROM content_entities e                  -- an entity has no generation:
    LEFT JOIN content_files f ON f.repo_id = e.repo_id     -- it takes its file's
                             AND f.relative_path = e.relative_path
    LEFT JOIN scope_generations g ON g.generation_id = f.generation_id
    WHERE lower(e.relative_path) ~ '(^|/)\.yarnrc\.yml$|\.tfvars(\.json)?$'
      AND e.source_cache <> ''
      AND COALESCE(g.ingested_at, '-infinity') < (SELECT at FROM w))
                                                           AS raw_entity_rows,
  (SELECT count(*) FROM content_file_references r
    LEFT JOIN content_files f ON f.repo_id = r.repo_id
                             AND f.relative_path = r.relative_path
    LEFT JOIN scope_generations g ON g.generation_id = f.generation_id
    WHERE lower(r.relative_path) ~ '(^|/)\.yarnrc\.yml$|\.tfvars(\.json)?$'
      AND COALESCE(g.ingested_at, '-infinity') < (SELECT at FROM w))
                                                           AS raw_reference_rows,
  (SELECT count(*) FROM content_file_references
    WHERE lower(relative_path) ~ '(^|/)\.yarnrc\.yml$|\.tfvars(\.json)?$')
                                                           AS reference_rows,  -- sizing
  -- Search index copy: active-generation documents with non-empty context text.
  -- Counted from the documents alone, so a document with no fact row still counts.
  (SELECT count(*) FROM eshu_search_index_documents d
    JOIN ingestion_scopes sc ON sc.scope_id = d.scope_id
                            AND sc.active_generation_id = d.generation_id
    JOIN scope_generations g ON g.generation_id = d.generation_id
    WHERE d.document->>'ContextText' <> ''
      AND lower(d.document->>'Path') ~ '(^|/)\.yarnrc\.yml$|\.tfvars(\.json)?$'
      AND g.ingested_at < (SELECT at FROM w))              AS raw_search_documents,
  (SELECT count(*) FROM eshu_search_index_documents d
    JOIN ingestion_scopes sc ON sc.scope_id = d.scope_id
                            AND sc.active_generation_id = d.generation_id
    WHERE d.document->>'ContextText' <> ''
      AND lower(d.document->>'Path') ~ '(^|/)\.yarnrc\.yml$|\.tfvars(\.json)?$')
                                                           AS search_documents,  -- sizing
  -- Search fact payload copy: active-generation reducer_eshu_search_document
  -- facts with non-empty context text, served by the semantic and hybrid read
  -- when embeddings are enabled. The document sits under the lowercase
  -- `document` key; its fields are Go field names (no JSON tags).
  (SELECT count(*) FROM fact_records f
    JOIN ingestion_scopes sc ON sc.scope_id = f.scope_id
                            AND sc.active_generation_id = f.generation_id
    JOIN scope_generations g ON g.generation_id = f.generation_id
    WHERE f.fact_kind = 'reducer_eshu_search_document'
      AND NOT f.is_tombstone
      AND f.payload->'document'->>'ContextText' <> ''
      AND lower(f.payload->'document'->>'Path') ~ '(^|/)\.yarnrc\.yml$|\.tfvars(\.json)?$'
      AND g.ingested_at < (SELECT at FROM w))              AS raw_search_fact_payloads,
  (SELECT count(*) FROM fact_records f
    JOIN ingestion_scopes sc ON sc.scope_id = f.scope_id
                            AND sc.active_generation_id = f.generation_id
    WHERE f.fact_kind = 'reducer_eshu_search_document'
      AND NOT f.is_tombstone
      AND f.payload->'document'->>'ContextText' <> ''
      AND lower(f.payload->'document'->>'Path') ~ '(^|/)\.yarnrc\.yml$|\.tfvars(\.json)?$')
                                                           AS search_fact_payloads;  -- sizing
```

## Purge

Statement L lists the repositories. Every other statement runs per repository
and touches at most 500 rows. Repeat it until it touches zero. They are `psql`
scripts with three variables: `repo` (the repository id), `pred` (the path
predicate above), and `w` (the watermark). For example:

```text
psql -v repo=<id> -v pred='(^|/)\.yarnrc\.yml$|\.tfvars(\.json)?$' -v w=<W> -f statement.sql
```

The statements do the following:

- blank `content_files.content`; `content_lower` follows, because it is a
  generated column;
- blank `content_entities.source_cache`;
- delete `content_file_references`;
- delete the active generation's search index documents and terms;
- delete the matching `reducer_eshu_search_document` fact rows by `fact_id`.

The reindex rebuilds all of them from the redacted rows. Every statement keeps
the guard "the copy's generation was ingested before `w` and the copy is
non-empty". The references, entities, and files statements lock the batch first
and test the guard again in a second statement of the same transaction, so a
row the writer rebuilt after `W` is never blanked.

```sql
-- Statement L: repositories to visit (any copy of the five kinds on a policy path).
SELECT repo_id FROM content_files WHERE lower(relative_path) ~ :'pred'
UNION SELECT repo_id FROM content_entities WHERE lower(relative_path) ~ :'pred'
UNION SELECT repo_id FROM content_file_references WHERE lower(relative_path) ~ :'pred'
UNION SELECT repo_id FROM eshu_search_index_documents WHERE lower(document->>'Path') ~ :'pred'
UNION SELECT payload->>'repo_id' FROM fact_records
  WHERE fact_kind = 'reducer_eshu_search_document'
    AND lower(payload->'document'->>'Path') ~ :'pred';

-- Statement refs: reference rows. A reference row has no body; the row is the copy.
BEGIN;
CREATE TEMP TABLE batch ON COMMIT DROP AS
  SELECT r.repo_id, r.relative_path, r.reference_kind, r.reference_value
  FROM content_file_references r
  LEFT JOIN content_files f ON f.repo_id = r.repo_id
                           AND f.relative_path = r.relative_path
  LEFT JOIN scope_generations g ON g.generation_id = f.generation_id
  WHERE r.repo_id = :'repo'
    AND lower(r.relative_path) ~ :'pred'
    AND COALESCE(g.ingested_at, '-infinity') < :'w'
  ORDER BY r.relative_path, r.reference_kind, r.reference_value
  LIMIT 500
  FOR UPDATE OF r SKIP LOCKED;
DELETE FROM content_file_references r USING batch b
WHERE r.repo_id = b.repo_id AND r.relative_path = b.relative_path
  AND r.reference_kind = b.reference_kind
  AND r.reference_value = b.reference_value
  AND COALESCE((SELECT g.ingested_at FROM content_files f
                JOIN scope_generations g ON g.generation_id = f.generation_id
                WHERE f.repo_id = r.repo_id AND f.relative_path = r.relative_path),
               '-infinity') < :'w';
COMMIT;

-- Statement entities: an entity takes its file row's generation.
BEGIN;
CREATE TEMP TABLE batch ON COMMIT DROP AS
  SELECT e.entity_id
  FROM content_entities e
  LEFT JOIN content_files f ON f.repo_id = e.repo_id
                           AND f.relative_path = e.relative_path
  LEFT JOIN scope_generations g ON g.generation_id = f.generation_id
  WHERE e.repo_id = :'repo'
    AND lower(e.relative_path) ~ :'pred'
    AND e.source_cache <> ''
    AND COALESCE(g.ingested_at, '-infinity') < :'w'
  ORDER BY e.entity_id
  LIMIT 500
  FOR UPDATE OF e SKIP LOCKED;
UPDATE content_entities e SET source_cache = ''
FROM batch b
WHERE e.entity_id = b.entity_id
  AND e.source_cache <> ''
  AND COALESCE((SELECT g.ingested_at FROM content_files f
                JOIN scope_generations g ON g.generation_id = f.generation_id
                WHERE f.repo_id = e.repo_id AND f.relative_path = e.relative_path),
               '-infinity') < :'w';
COMMIT;

-- Statement files: blank the body. content_lower follows (generated column).
BEGIN;
CREATE TEMP TABLE batch ON COMMIT DROP AS
  SELECT f.repo_id, f.relative_path
  FROM content_files f
  LEFT JOIN scope_generations g ON g.generation_id = f.generation_id
  WHERE f.repo_id = :'repo'
    AND lower(f.relative_path) ~ :'pred'
    AND f.content <> ''
    AND COALESCE(g.ingested_at, '-infinity') < :'w'
  ORDER BY f.relative_path
  LIMIT 500
  FOR UPDATE OF f SKIP LOCKED;
UPDATE content_files f SET content = ''
FROM batch b
WHERE f.repo_id = b.repo_id AND f.relative_path = b.relative_path
  AND f.content <> ''
  AND COALESCE((SELECT g.ingested_at FROM scope_generations g
                WHERE g.generation_id = f.generation_id),
               '-infinity') < :'w';
COMMIT;

-- Statement A: index documents and terms, keyed on the documents alone.
WITH batch AS (
  SELECT d.scope_id, d.generation_id, d.document_id
  FROM eshu_search_index_documents d
  JOIN ingestion_scopes sc ON sc.scope_id = d.scope_id
                          AND sc.active_generation_id = d.generation_id
  JOIN scope_generations g ON g.generation_id = d.generation_id
  WHERE d.repo_id = :'repo'
    AND lower(d.document->>'Path') ~ :'pred'
    AND d.document->>'ContextText' <> ''
    AND g.ingested_at < :'w'
  ORDER BY d.scope_id, d.generation_id, d.document_id
  LIMIT 500
  FOR UPDATE OF d SKIP LOCKED
), gone_terms AS (
  DELETE FROM eshu_search_index_terms t USING batch b
  WHERE t.scope_id = b.scope_id AND t.generation_id = b.generation_id
    AND t.document_id = b.document_id
)
DELETE FROM eshu_search_index_documents d USING batch b
WHERE d.scope_id = b.scope_id AND d.generation_id = b.generation_id
  AND d.document_id = b.document_id;

-- Statement B: fact payloads, keyed on the facts alone.
WITH batch AS (
  SELECT f.fact_id
  FROM fact_records f
  JOIN ingestion_scopes sc ON sc.scope_id = f.scope_id
                          AND sc.active_generation_id = f.generation_id
  JOIN scope_generations g ON g.generation_id = f.generation_id
  WHERE f.fact_kind = 'reducer_eshu_search_document'
    AND f.payload->>'repo_id' = :'repo'
    AND lower(f.payload->'document'->>'Path') ~ :'pred'
    AND f.payload->'document'->>'ContextText' <> ''
    AND g.ingested_at < :'w'
  ORDER BY f.fact_id
  LIMIT 500
  FOR UPDATE OF f SKIP LOCKED
)
DELETE FROM fact_records f USING batch b WHERE f.fact_id = b.fact_id;
```

## What the cleanup does not reach

- **Superseded generations.** Earlier generations keep their `content` facts,
  content-entity facts, and search documents until generation retention prunes
  them. The default policy keeps the newest 24 superseded generations, or any
  generation newer than 7 days, whichever keeps more. Nothing is kept past the
  90-day ceiling. Reads serve only the active generation, so these copies are
  not returned while they wait.
- **Dead tuples and WAL.** A blanked row's old version stays on disk until
  autovacuum reclaims it, and the WAL that recorded it stays until it is
  recycled or archived. Run `VACUUM` on `content_files`, `content_entities`,
  `content_file_references`, `fact_records`, and the two search index tables
  after the second run if you need the space reclaimed sooner.
- **Embedding vectors.** Statement A deletes the active generation's index
  documents but not their rows in `eshu_search_vector_values` and
  `eshu_search_vector_metadata`. These hold vector numbers and a content
  hash, not text. The default `local-hash-v1` embedder sums signed term hashes
  into 256 buckets, so a vector cannot be read back as the file, but it could
  help confirm a guess. A configured external embedding provider produces its
  own model's vectors instead. No read uses these rows once the document is
  gone: vectors load only for the documents a scope still holds. They leave
  with their generation, under the retention policy above.
- **Backups.** Base backups and archived WAL taken before the cleanup keep the
  verbatim files until they expire under your backup retention.
