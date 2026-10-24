# Cleanup runbook and census for #8813

Companion to [the #8813 record](8813-credential-file-exposure.md). It holds the
operator-facing parts: the order in which the cleanup runs, the fact-payload
ruling, the exit criterion, and the #8833 proof list. The read-only census and
the one-shot purge SQL, with what was run, are on
[the purge SQL page](8813-credential-file-exposure-purge-sql.md). It approves no deployment. If this
page and an issue body differ, the issue body wins.

## Cleanup order

`.yarnrc.yml` and `*.tfvars` stay `store` until #8833 binds them (see "Rule per
file" and item 1 of "Decisions for the arbiter" in the record). #8812 alone
changes the fingerprint but not the stored form of either file, so a
re-collection after the #8812 deploy still writes both verbatim. The fingerprint
flip does not force a sweep. It only makes a forced full snapshot store a new
generation instead of re-projecting the active one, so the flip alone visits
nothing; the sweep or a reindex request does (decision table in
`docs/public/reference/reconciliation-sweep.md`). The order is:

1. Deploy #8833 (the binding). Wait until every ingester runs the new binary. A
   scope re-parsed by an old binary after step 2 counts as `fresh` and is not
   re-parsed again, so its raw copy would be written after the watermark.
2. Send `POST /api/v0/admin/reindex` and record the response `requested_at` as
   the watermark `W` (`runtime_ingester_control.reindex_request_requested_at` for
   the `repository` ingester, migration 009). Every git ingester shard reads it
   each cycle, honored even with the interval at `0`
   (`docs/public/reference/reconciliation-sweep.md`, "Reindex requests"). Raise
   `ESHU_REPO_RECONCILE_MAX_PER_CYCLE` temporarily (for example to 50) so the
   fleet drains faster. Do not set it to `0`: `0` removes the per-cycle cap, so
   every due scope is forced in one cycle, the stampede the cap exists to
   prevent. It does not disable the sweep either. Reconciliation is off only
   with `ESHU_REPO_RECONCILE_INTERVAL_HOURS=0` (`go/internal/collector/repo/git/selection_baseline.go`,
   `reconcileBudgetRemaining`; `docs/public/reference/env-registry/collectors-platform.md`).
3. Run the operator SQL fleet-wide: statement L (on the purge SQL page) lists the repositories, and
   every other statement runs per repository with `w = W`. Each statement
   carries the guard "the copy's writing generation was ingested before `W` and
   the copy is non-empty", so it can run while the reindex rebuilds. The
   statements that rewrite a row in place lock a batch first and test the guard
   again in a second statement of the same transaction. A writer upsert that
   committed first shows its post-`W` generation to that second statement, which
   skips the row; one that lands after the lock overwrites the blank. A single
   CTE statement does not do this (measured on the purge SQL page, "Race"). The
   SQL never blanks a body the reindex rebuilt.
4. Watch `eshu_dp_collector_reconciliation_due_unvisited`. When it reads zero
   for every reason, run the same statements again. They catch rows an ingester
   on the old binary wrote before `W` during the rollout and any scope whose
   forced full failed and was retried. A scope the sync never visits (a
   sweep-off or webhook-only ingester) is not covered by the reindex; this
   second run is what reaches it.
5. Restore the cap.

Expected duration is `ceil(repositories / cap)` selection cycles, because the
sweep visits scopes in rotating order within `ceil(due / limit)` cycles
(`docs/public/reference/reconciliation-sweep.md`, "Visiting order"). That is a
bounded cycle count, not days. A scope with a pending full that has live
projector work waits one activation first (the `in_flight_expired_held` row in
the same doc). The cycle period is NOT_CHECKED (it is not in the env registry).

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

**Ruling.** The #8833 purge deletes the active `reducer_eshu_search_document`
fact row by `fact_id`. Statement B on the purge SQL page does it, beside
statement A for the index documents and terms; each statement locks one table,
which replaced the single joined statement the first ruling described. The
reindex rebuilds the row from redacted rows. There is no `UPDATE`, no rebuild
trigger and no read-side filter. The sequence is the one in "Cleanup order"
above (deploy #8833, reindex request, guarded SQL, guarded SQL again); the later
exit-criterion ruling superseded the earlier order of SQL before reindex.

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

Done when, on two consecutive scrapes, both hold:

- `eshu_dp_content_policy_raw_rows` reads zero. It is test (b) from
  [the purge SQL page](8813-credential-file-exposure-purge-sql.md): an
  unsuppressed `content_file_secret_lines` row on a policy path whose
  `line_text` carries no `REDACTED_<finding_kind>` marker. #8833 defines it once;
  the record's "Operator signals" cites it.
- The census reads zero in `raw_files`, `raw_entity_rows`, `raw_reference_rows`,
  `raw_search_documents` and `raw_search_fact_payloads` (test (a): the copy is
  non-empty and its writing generation was ingested before the watermark; a NULL
  generation counts raw) and in `files_with_raw_finding` (test (b) by file).

`files`, `repos`, `reference_rows`, `search_documents` and `search_fact_payloads`
size the work and are not exit columns: a `redact` row keeps a redacted body, so
they never reach zero. A path-count zero is not the criterion, and neither is a
body-only test: marker mode is a fixed point, so no body test tells a raw
`.yarnrc.yml` from its redacted form.

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
4. Concurrency: the purge runs interleaved with the content writer and the
   search-document writer on the same active generation and on a new generation,
   with the real writers rather than a sleep. A row the writer rewrote after
   `W` is never blanked or deleted, and a row the purge locked is overwritten by
   the writer's later upsert. Statements A and B each lock one table, so there is
   no cross-table lock order. No deadlock.
5. Census columns `raw_search_documents` and `raw_search_fact_payloads` read zero
   after the second run, and `eshu_dp_content_policy_raw_rows` covers the
   rebuilt rows.
6. A test asserts the kind is not in `factKeySidecarKinds`.
