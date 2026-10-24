# Cleanup runbook and census for #8813

Companion to [the #8813 record](8813-credential-file-exposure.md). It holds the
operator-facing parts: the order in which the cleanup runs, the read-only census
query, the one-shot purge SQL with what was run, the fact-payload ruling, the
exit criterion, and the #8833 proof list. It approves no deployment. If this
page and an issue body differ, the issue body wins.

## Cleanup order

`.yarnrc.yml` and `*.tfvars` stay `store` until #8833 binds them (see "Rule per
file" and item 1 of "Decisions for the arbiter" in the record). #8812 alone
changes the fingerprint but not the stored form of either file, so a
re-collection after the #8812 deploy still writes both verbatim.
**Between the #8812 and #8833 deploys the tfvars rows and the `.yarnrc.yml`
rows stay verbatim, and every re-collection rewrites them verbatim.** The
operator SQL must not run in that gap, because the next re-collection would
write the credential back. The fingerprint flip does not force a sweep. It only
makes a forced full snapshot store a new generation instead of re-projecting
the active one, so the flip alone visits nothing; the sweep or a reindex
request does (decision table in `docs/public/reference/reconciliation-sweep.md`).
The order is:

1. Deploy #8833 (the binding). Wait until every ingester runs the new binary.
2. Run the operator SQL (floor). It covers repositories the sweep will not reach
   soon.
3. Send `POST /api/v0/admin/reindex`. It records a fleet watermark that every git
   ingester shard reads each cycle, honored even with the interval at `0`. Send
   it only after step 1: a scope re-parsed by an old binary after the watermark
   counts as `fresh` and is not re-parsed again
   (`docs/public/reference/reconciliation-sweep.md`, "Reindex requests"). Raise
   `ESHU_REPO_RECONCILE_MAX_PER_CYCLE` temporarily (for example to 50) so the
   fleet drains faster. Do not set it to `0`: `0` removes the per-cycle cap, so
   every due scope is forced in one cycle, the stampede the cap exists to
   prevent. It does not disable the sweep either. Reconciliation is off only
   with `ESHU_REPO_RECONCILE_INTERVAL_HOURS=0` (`go/internal/collector/repo/git/selection_baseline.go`,
   `reconcileBudgetRemaining`; `docs/public/reference/env-registry/collectors-platform.md`).
4. Watch `eshu_dp_collector_reconciliation_due_unvisited`. When it reads zero
   for every reason, run the operator SQL again. It catches rows an ingester on
   the old binary wrote during the rollout and any scope whose forced full
   failed and was retried. A scope the sync never visits (a sweep-off or
   webhook-only ingester) is not covered by the reindex; the second SQL run is
   what reaches it.
5. Restore the cap.

Expected duration is `ceil(repositories / cap)` selection cycles, because the
sweep visits scopes in rotating order within `ceil(due / limit)` cycles
(`docs/public/reference/reconciliation-sweep.md`, "Visiting order"). That is a
bounded cycle count, not days. A scope with a pending full that has live
projector work waits one activation first (the `in_flight_expired_held` row in
the same doc).

## Deployment census

No deployment was queried. The two quoted samples are in the record, under
"Evidence on how common it is". Neither is a census. The detector has the shape
gaps in the evidence page ("Detector probe (the gap corpus)"), so zero hits does
not prove zero credentials. An operator runs the read-only queries below. They
return counts and never values. They were not run against a database.

```sql
-- 1. Is the side table current? Zero hits are only meaningful when ready.
SELECT state FROM content_file_secret_lines_state;

-- 2. Files and detector hits per file type. Counts only.
SELECT
  CASE WHEN lower(f.relative_path) ~ '(^|/)\.yarnrc\.yml$' THEN 'yarnrc_yml'
       ELSE 'tfvars' END                                   AS file_kind,
  count(*)                                                 AS files,
  count(DISTINCT f.repo_id)                                AS repos,
  count(*) FILTER (WHERE EXISTS (
    SELECT 1 FROM content_file_secret_lines s
    WHERE s.repo_id = f.repo_id AND s.relative_path = f.relative_path
      AND NOT s.suppressed))                               AS files_with_detector_hit
FROM content_files f
WHERE lower(f.relative_path) ~ '(^|/)\.yarnrc\.yml$|\.tfvars(\.json)?$'
GROUP BY 1;

-- 3. Derived copies that must also be cleaned. Counts only.
SELECT
  (SELECT count(*) FROM content_entities
    WHERE lower(relative_path) ~ '(^|/)\.yarnrc\.yml$|\.tfvars(\.json)?$'
      AND source_cache <> '')                              AS entity_rows_with_source,
  (SELECT count(*) FROM content_file_references
    WHERE lower(relative_path) ~ '(^|/)\.yarnrc\.yml$|\.tfvars(\.json)?$')
                                                           AS reference_rows,
  -- Search index copy: active-generation documents with non-empty context text.
  -- Counted from the documents alone, so a document with no fact row still counts.
  (SELECT count(*) FROM eshu_search_index_documents d
    JOIN ingestion_scopes sc ON sc.scope_id = d.scope_id
                            AND sc.active_generation_id = d.generation_id
    WHERE d.document->>'ContextText' <> ''
      AND lower(d.document->>'Path') ~ '(^|/)\.yarnrc\.yml$|\.tfvars(\.json)?$')
                                                           AS search_documents,
  -- Search fact payload copy: active-generation reducer_eshu_search_document
  -- facts with non-empty context text. Served by the semantic and hybrid read
  -- when embeddings are enabled. Counted from the facts alone, so a fact with no
  -- index document still counts. The document sits under the lowercase
  -- `document` key; its fields are Go field names (no JSON tags).
  (SELECT count(*) FROM fact_records f
    JOIN ingestion_scopes sc ON sc.scope_id = f.scope_id
                            AND sc.active_generation_id = f.generation_id
    WHERE f.fact_kind = 'reducer_eshu_search_document'
      AND NOT f.is_tombstone
      AND f.payload->'document'->>'ContextText' <> ''
      AND lower(f.payload->'document'->>'Path') ~ '(^|/)\.yarnrc\.yml$|\.tfvars(\.json)?$')
                                                           AS search_fact_payloads;
```

These count candidates by path. A `redact` row keeps a non-empty, redacted body,
so the zero criterion below applies to the exposure predicate #8833 defines once
for the gauge ("Operator signals" under "Rows already stored" in the record:
path predicate, and a body that is not in the policy's output form). The census
reuses that predicate for each copy. The document keys are Go field names
(`Labels` is read as `doc.document->'Labels'` in
`go/internal/storage/postgres/eshu_search_vector_documents.go`), so the context
text is `ContextText` and the path is `Path`. The fact payload wraps the same
document under a lowercase `document` key (`eshuSearchDocumentPayload`; the
reader selects `fact.payload->'document'->'Labels'` in
`go/internal/storage/postgres/eshu_search_document.go`), so the census reads
`payload->'document'->>'Path'`. The `search_fact_payloads` column is part of the
exit criterion. It counts a copy that the semantic and hybrid read serves when
embeddings are enabled. This query was not run.

The search predicate tests `Path`, never `document_id`. A content-file
document id is `searchdoc:content_file:<repo>:<path>`
(`go/internal/searchdocs/project.go`, `ProjectContentFile`), so a `:` precedes
a root-level `.yarnrc.yml` and the `(^|/)` alternative cannot match the id. The
census would read zero and the purge would skip the most common location. The
key is also case-sensitive: `document->>'path'` (lowercase) matches nothing,
because `searchdocs.Document` has no JSON tags and the keys are `Path` and
`ContextText`. `ProjectContentFile` and `ProjectContentEntity` both set `Path`
to the repository-relative path, so one predicate covers both document kinds. A
scratch Python `re` check of the path predicate (not Postgres ARE) on invented
paths matched `.yarnrc.yml`, `app/.yarnrc.yml`, `prod.tfvars`,
`a/b/prod.auto.tfvars` and `envs/x.tfvars.json`. It did not match
`x.yarnrc.yml.bak`, `my.yarnrc.yml` or `tfvars`. The old id-based form missed
the root `.yarnrc.yml`.

## Operator SQL scope

#8833 writes the one-shot SQL. It runs per repository, 500 rows per statement,
and a second run touches 0 rows. It blanks `content_files.content`, deletes
`content_file_references`, blanks `content_entities.source_cache` and purges the
active-generation search copies: index documents, index terms and the matching
`reducer_eshu_search_document` fact rows (ruling in
[the fact payload cleanup](#the-fact-payload-cleanup-arbiter-ruling)). The
search purge is two statements, A and B, each locking one table and each run
until it deletes 0 rows. This replaces one statement that joined the two
tables. It refines the mechanism; the arbiter's ruling (delete the fact row by
`fact_id`) is unchanged.

The split is needed because the writer commits the fact and index rows in
separate autocommit statements: `InsertPage` inserts facts, then index rows;
`Finalize` retires facts (`retireSearchDocumentFacts`), then index documents
(`finalizeSearchIndex`); `Cancel` does the same
(`go/internal/reducer/eshusearch/eshu_search_document_writer.go`). A failure or
cancel between the two leaves an index document with no fact row, or a fact row
with no index document. An inner join between the two tables misses both. The
keyword read serves the first state: it walks terms to
`eshu_search_index_documents` and never touches `fact_records`
(`go/internal/storage/postgres/search/index/store.go`). The semantic and hybrid
read serves the second. A reviewer reproduced both on PostgreSQL 18.6
(`orphan/.yarnrc.yml` and `lonely.tfvars` survived the joined statement).

```sql
-- Statement A: index documents and terms, keyed on the documents alone.
WITH batch AS (
  SELECT d.scope_id, d.generation_id, d.document_id
  FROM eshu_search_index_documents d
  JOIN ingestion_scopes sc ON sc.scope_id = d.scope_id
                          AND sc.active_generation_id = d.generation_id
  WHERE d.repo_id = $1
    AND lower(d.document->>'Path') ~ $2                -- $2: the path predicate
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
  WHERE f.fact_kind = 'reducer_eshu_search_document'
    AND f.payload->>'repo_id' = $1
    AND lower(f.payload->'document'->>'Path') ~ $2
  ORDER BY f.fact_id
  LIMIT 500
  FOR UPDATE OF f SKIP LOCKED
)
DELETE FROM fact_records f USING batch b WHERE f.fact_id = b.fact_id;
```

`repo_id` is a top-level payload key (`eshuSearchDocumentPayload` in the writer
file above). Each statement locks one table, so the purge adds no lock-order
question between the fact and index tables. In A, the two deletes are siblings in
one statement. PostgreSQL runs data-modifying CTEs against one snapshot and does
not order them, so neither reads the other's effect; both take their rows from
`batch`, and no row is targeted twice (terms and documents are different
tables). The terms primary key is `(scope_id, generation_id, term_key,
document_id)` (migration `003b`, kept by `039a`; `191` drops only the generation
foreign key), so the `gone_terms` match on `(scope_id, generation_id,
document_id)` is expected to prune to one hash partition by `scope_id`. Runtime
pruning is NOT_CHECKED.

Both statements filter on `repo_id`. The existing index on
`eshu_search_index_documents` is `(scope_id, generation_id, repo_id,
source_kind)`, so A may scan sequentially per batch when `$1` alone is the
filter. B has no `repo_id` index on `fact_records`. The `EXPLAIN (ANALYZE,
BUFFERS)` item at 145k rows must cover both, and #8833 may need to iterate A per
scope (add `AND d.scope_id = $3`). They were run on scratch data (next paragraph), not at that size.

**What was run.** Statements A and B, copied from this page, ran on a scratch
PostgreSQL 18.6 built from migrations 001, 002, 003, 003b, 039a and 191. The seed
held 1,214 index documents, 2,428 terms and 1,215 fact rows: five matching path
shapes, one mixed-case path, 1,200 bulk rows, an index document with no fact row,
a fact row with no index document, a second repository, a superseded generation,
a file-kind fact and the non-matching names `my.yarnrc.yml`,
`x.yarnrc.yml.bak`, `tfvars` and a README. Before the purge the census read 1,209
search documents and 1,209 search fact payloads. A deleted 500, 500, 207 and 0
rows over four runs, and B deleted 500, 500, 207 and 0. Both orphans were gone,
a second pass deleted nothing, and the census read zero on every search column
afterwards. The other repository, the superseded generation, the non-matching
names and the file-kind fact survived. With the document and fact locks of one
row held by a second session, A and B each deleted the other row in 63 ms
without waiting, and deleted the held row once it was released. It was not run
at 145k rows. Runtime partition pruning of the terms delete, the interleaving
with the search-document writer, and term rows whose document row is already
gone (the census has no terms column; readers join through documents, so such a
row is not served) are NOT_CHECKED.

Superseded generations stay out of reach of the active reader until the
retention cascade removes them ("Decisions for the arbiter", item 2, and gap 3
under "Rows already stored" in the record). The vector tables
hold ids, hashes and embedding vectors, not text (migrations `003c`, `003d`).
A vector row for a purged document is left in place and is ignored by the
read, which asks only for the ids of the documents it loaded
(`readyVectors`). #8833 confirms that with a test before the purge is accepted.

### Index statistics after the purge

The purge does not touch `eshu_search_index_stats` or
`eshu_search_document_projection_state`. Only the document writer sets them
(`eshuSearchIndexStatsUpsertQuery` in
`go/internal/reducer/eshusearch/eshu_search_document_writer_queries.go`), once
per generation. Three readers depend on the counts:

- BM25 scoring reads `document_count` and `average_document_length`
  (`go/internal/storage/postgres/search/index/store.go`). After a purge both
  are off by at most the purged documents. That is a small, bounded ranking
  drift, not a correctness fault, and it ends when the next generation writes
  fresh stats. The forced re-collection in the runbook produces that
  generation.
- The pending lister re-lists a scope when
  `projection_state.document_count <> index_stats.document_count`
  (`go/internal/storage/postgres/eshu_search_document_pending.go`). Lowering
  only the stats row would queue a rebuild nobody asked for. Leaving both rows
  alone keeps them equal.
- The vector readiness gate reads only `eshu_search_document_projection_state`
  and `eshu_search_vector_scope_state`. It fails when `projection.state` is not
  `ready`, or when `projection.document_count > 0` and the vector scope state is
  missing, not `ready`, or carries a different `projection_revision`
  (`go/internal/query/semanticsearch/semantic_search_vector_ready_store.go`).
  The purge leaves `projection_state` untouched, so neither test changes and the
  purge does not trip the gate. This is about the gate only. The per-document
  vector check in the persisted hybrid read is a separate path (see
  [the fact payload cleanup](#the-fact-payload-cleanup-arbiter-ruling)).

#8833 proves this with a test: after the purge, stats and projection state are
unchanged and equal, the pending lister does not re-list the scope, and after
the next generation `document_count` equals the active document count.

## The fact payload cleanup (arbiter ruling)

The writer also stores each document, including `ContextText`, in the
`reducer_eshu_search_document` fact payload in `fact_records`. The semantic and
hybrid read serves it when embeddings are enabled (see "Where the body is then
readable" in the record), so the census and the exit criterion count it. The
arbiter ruled on 2026-10-24 (binding; the observations below are the evidence
it rests on).

**Ruling.** The #8833 purge statement deletes the active
`reducer_eshu_search_document` fact row by `fact_id` in the same statement as the
index document and term rows. The reindex rebuilds it from redacted rows. There
is no `UPDATE`, no rebuild trigger and no read-side filter. The sequence is
unchanged: #8922, #8812, #8833, deploy, SQL, reindex, second SQL.

**Why a `DELETE`.**

- The writer already deletes this kind inside a generation
  (`eshuSearchDocumentRetireQuery` in
  `go/internal/reducer/eshusearch/eshu_search_document_writer_queries.go`), so a
  row `DELETE` is an established operation on it.
- The kind owns no key sidecar rows. `factKeySidecarKinds` in
  `go/internal/storage/postgres/fact_delete_key_guard_test.go` lists `content`,
  `file`, `gcp_cloud_relationship`, `content_entity` and
  `package_registry.package`, and the storage README ("Gotchas / invariants",
  key sidecar deletes) says the same, so an unpaired delete is safe.
- The index row carries `fact_id` (migration `003b`), so one batch addresses both.
- A `DELETE` fires neither migration 088 trigger. Both are statement triggers on
  `AFTER UPDATE` and `AFTER INSERT` only.

**Why not the others.** (a) An `UPDATE` that removes `ContextText` changes the
document hash. `content_hash` is `searchhybrid.DocumentContentHash(doc)`; a
recomputed Go hash in SQL is an accuracy risk, and a mismatch for any loaded
document makes the whole scope `index_unready` (`compatiblePersistedVector`,
`semantic_search_persisted_vector.go`). (b) No operator trigger exists for an
already-active generation. The pending lister re-lists only on an unequal
`projection_state` and `index_stats` count (`eshu_search_document_pending.go`),
and abusing that is a hack. (c) A read-side filter was rejected in the #8812
ruling.

**Immutability.** The storage README premise "a `(scope, generation)`
partition's fact set is immutable" backs the deferred-backfill memo for
collector-fact partitions. This reducer kind already breaks strict immutability
through its own retire, and no doc makes `fact_records` append-only. Kind-scoped
operator SQL has precedent: the upgrade runbook runs `UPDATE fact_records SET
fencing_token = 0 WHERE fact_kind = 'reducer_supply_chain_impact_finding'` "in
batches" (`docs/public/deploy/kubernetes/upgrades-rollbacks.md`), and the delete
guard test acknowledges one non-Go `DELETE`. Whether the memo reads this kind is
NOT_CHECKED (low risk: the kind is reducer-derived).

**Readers of the kind.** Exactly two non-test readers, both joined on
`active_generation_id`: `EshuSearchDocumentStore.ListActiveDocuments`
(`eshu_search_document.go`) and the vector pending lister
`listPendingEshuSearchVectorScopesSQL` (`eshu_search_vector_pending.go`, a `NOT
EXISTS` per active fact document). Both tolerate absence: a deleted document is
never loaded, never compared and never listed pending.

**Missing document on a semantic read.** The remaining documents stay `ready`
(the `len(vectors) == len(docs)` check in `semantic_search_persisted_vector.go`
holds). A scope with no documents left gives an empty response, not an error.
Stale vector metadata and value rows are ignored and fall with the generation
(`003c` and `003d` cascade from `scope_generations`).

**Retention.** `fact_records` cascades from `scope_generations`
(`003_fact_records.sql`). Supersede leaves rows in place
(`eshu_search_document_supersede.go`), and the window is the one in
"Decisions for the arbiter", item 2 of the record. Prior-generation payloads are
not served, under the same accepted window.

**Embeddings census.** No gate. The Go default is disabled (`autoLocalConfig`
returns a zero config unless `auto_hash`, in
`go/internal/searchembedruntime/config.go`), and `docker-compose.yaml` defaults
`auto_hash` on the API, MCP server and reducer. The purge is unconditional,
because the copy persists in the database and backups and a flag can flip later.
The runbook records each service's embedder value as a fact in the exit note only.

**Raw observations** (read from source; none run against a database):

1. The writer's insert is an upsert that overwrites the payload
   (`ON CONFLICT (fact_id) DO UPDATE SET ... payload = EXCLUDED.payload` in
   `eshuSearchDocumentBatchFactInsertQuery`). The fact id is deterministic from
   `(scope_id, generation_id, document_id)` (`eshuSearchDocumentFactID`).
2. The writer's retire is scoped to its own generation
   (`fact_kind = $1 AND scope_id = $2 AND generation_id = $3 AND fact_id <> ALL($4)`).
   It never touches a prior generation; retention does, by cascade. Other
   non-test `DELETE FROM fact_records` sites were not each read, so "no other
   deleter" is NOT_CHECKED.
3. The read joins the scope's active generation when no `GenerationID` is set
   (`buildEshuSearchDocumentQuery`), and the semantic path sets none
   (`semantic_search_postgres.go`). A forced re-collection that activates a new
   generation rebuilds the documents from the redacted `content_files` and
   `content_entities` rows (`eshu_search_document_source_loader.go`) without
   operator SQL for every repository it reaches. The purge covers the rest.
4. In-place `UPDATE` of reducer-derived fact rows has a precedent for tombstone
   and fencing columns (`retractSupersededSupplyChainImpactFindingsQuery`).
5. Any `UPDATE` on `fact_records` fires the migration 088 `AFTER UPDATE`
   statement trigger. A `DELETE` does not, so that cost does not apply to the
   chosen mechanism.
6. The vector tables hold ids, hashes and a `DOUBLE PRECISION[]` vector, no text.

**NOT_CHECKED.** The deferred-backfill memo reading this kind. The writer's lock
order between fact upserts and index rows (statements A and B lock one table
each, so the purge takes no lock across the two). Any deployment's embedder
setting. Whether a hash embedding can confirm a guessed value. The purge at
145k rows, and runtime partition pruning of the terms delete. A fact row and an
index document can each outlive the other (separate writer commits, reproduced
on scratch data); statements A and B cover both.

## Exit criterion

The cleanup is done when both hold:

- The exposure gauge from "Rows already stored" in the record reads zero on two
  consecutive scrapes.
- The census counts are zero for `content_files`, `content_entities.source_cache`,
  `content_file_references`, the active search documents and terms, and the
  active `reducer_eshu_search_document` fact payloads (`search_fact_payloads`
  reads zero; an API serves that copy when embeddings are enabled).

Days are not a criterion. Record the expected duration as
`ceil(repositories / cap)` cycles once
`eshu_dp_collector_reconciliation_due_unvisited` starts draining.

## Migration and lock-window considerations

These are the questions #8833 must answer with measurements. They are not
measured here.

| Point | What the repository shows | What #8833 must record |
| --- | --- | --- |
| Detector extras change `eshu_secret_line_findings` | Migration 138 says a pattern change needs a new migration that replaces the function, re-derives the side table, and keeps the Go binding test in sync. It also says never edit a shipped file. | One new migration so the store and `investigate_hardcoded_secrets` cannot disagree (see "One new detector migration (#8833)" in the record). |
| Re-derivation cost | Migration 138 measured about 0.27 ms per file for its backfill (38 s at 145k files), and its `CREATE TRIGGER` and foreign key held SHARE ROW EXCLUSIVE on `content_files` for that time. A re-derive over the side table alone does not recreate the trigger or key, so its lock footprint is expected to be smaller. That is a theory. | Per-file cost and lock window on a representative corpus, before the migration is built, plus the finding-count delta the extras add. |
| Migration order | A migration that re-derives from verbatim bodies, shipped before the collector change, widens the side table with new raw credential lines. | The extras migration lands with or after the redactor, never alone. Confirm the order in the PR. |
| Bulk-load sessions | `eshu.secret_lines_derive = 'deferred'` skips the triggers and a finalizer rebuilds the table. | State that the new migration and the finalizer use the same function. |
| Single-statement indexes | `CREATE INDEX CONCURRENTLY` must be alone in its file. | Applies only if #8833 adds an index. It should not. |
| `content_lower` and trigram indexes | `content_lower` has its own trigram index (migrations 176, 177), so blanking `content` alone leaves the lowered copy searchable. | Prove `content_lower` is emptied or redacted together with `content`. |

## What #8833 proves for the operator purge

The acceptance lists are on [the acceptance page](8813-credential-file-exposure-acceptance.md).
For the purge, #8833 also proves:

1. Live Postgres: a seeded active fact row, index document and terms for a
   matching path. One run of statements A and B removes all three and leaves
   non-matching rows; a second run touches 0 rows. Two seeded orphans, an index
   document with no fact row and a fact row with no index document, are gone
   after the runbook. `EXPLAIN (ANALYZE, BUFFERS)` of both statements at 145k
   rows, including the per-batch `repo_id` scan (A may need to run per scope).
2. With `ESHU_SEMANTIC_SEARCH_LOCAL_EMBEDDER=hash`, after the purge the remaining
   documents are `ready`, the purged document is absent in `semantic` and
   `hybrid` mode, and no scope reports `index_unready`. A fully purged scope
   returns empty hits.
3. The vector pending lister does not re-list the scope. The document pending
   lister is unchanged.
4. Concurrency: the purge runs interleaved with the search-document writer on the
   same active generation and on a new generation. Statements A and B each lock
   one table, so there is no cross-table lock order. No deadlock.
5. Census column `search_fact_payloads` reads zero after one cycle, and the
   exposure gauge covers it.
6. A test asserts the kind is not in `factKeySidecarKinds`.
