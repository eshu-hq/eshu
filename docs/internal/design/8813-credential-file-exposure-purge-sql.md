# Census and purge SQL for #8813

Companion to [the cleanup runbook](8813-credential-file-exposure-cleanup.md)
and [the #8813 record](8813-credential-file-exposure.md). It holds the read-only
census and the one-shot purge SQL, with what was executed. The runbook holds the
order, the fact-payload ruling, the exit criterion and the #8833 proof list. It
approves no deployment. If this page and an issue body differ, the issue body
wins.

## Deployment census

No deployment was queried. The two quoted samples are in the record, under
"Evidence on how common it is". Neither is a census. The detector has the shape
gaps in the evidence page ("Detector probe (the gap corpus)"), so zero hits does
not prove zero credentials. An operator runs the read-only queries below. They
return counts and never values. They ran against a scratch database (see "What
was run"), not a deployment.

The path predicate, used by every statement on this page, is
`(^|/)\.yarnrc\.yml$|\.tfvars(\.json)?$` applied to the lowercased path.

A copy is **raw** when it fails either of two independent tests, and the exit
criterion needs both at zero.

- Test (a), structural: the copy's writing generation predates the watermark and
  the copy is non-empty. `content_files.generation_id` is the writing generation
  (migration 169; NULL for a row never backfilled). The search documents and fact
  rows carry a scope generation. A copy tagged with a NULL or pruned generation
  counts raw.
- Test (b), detector-bounded: an unsuppressed `content_file_secret_lines` row on
  a policy path whose `line_text` carries no `REDACTED_<finding_kind>` marker.
  Marker lines keep a finding by design, so "body matches the detector" can never
  read zero. #8833 defines test (b) once as the gauge
  `eshu_dp_content_policy_raw_rows`.

A `redact` row keeps a non-empty, redacted body. So `files`, `repos`,
`reference_rows`, `search_documents` and `search_fact_payloads` never read zero.
They size the work and are **not exit columns**.

The census statements are on the operator page
[Credential File Cleanup](../../public/deployment/credential-file-cleanup.md#census),
which the #8833 live tests run verbatim.

The document keys are Go field names (`Labels` is read as
`doc.document->'Labels'` in
`go/internal/storage/postgres/eshu_search_vector_documents.go`), so the context
text is `ContextText` and the path is `Path`. The fact payload wraps the same
document under a lowercase `document` key (`eshuSearchDocumentPayload`; the
reader selects `fact.payload->'document'->'Labels'` in
`go/internal/storage/postgres/eshu_search_document.go`). `content_entities` has
no `generation_id` (`schema_content_store.go`), so census 3 and the purge join
its file row on `(repo_id, relative_path)`; a missing file row counts raw.
`raw_search_fact_payloads` counts a copy that the semantic and hybrid read
serves when embeddings are enabled.

The search predicate tests `Path`, never `document_id`. A content-file
document id is `searchdoc:content_file:<repo>:<path>`
(`go/internal/searchdocs/project.go`, `ProjectContentFile`), so a `:` precedes
a root-level `.yarnrc.yml` and the `(^|/)` alternative cannot match the id. The
key is also case-sensitive: `document->>'path'` (lowercase) matches nothing,
because `searchdocs.Document` has no JSON tags. `ProjectContentFile` and
`ProjectContentEntity` both set `Path` to the repository-relative path, so one
predicate covers both document kinds.

The marker names in test (b) are the six detector kinds
(`eshu_secret_line_findings`, migration 233 since #8833). The marker writers
are the redactors in `go/internal/collector/git/content` (#8812, #8833). Their
kinds come from `go/internal/secret/detect`, which
`TestHardcodedSecretDetectPackageIsBoundToMigration` binds to the migration,
and `TestRedactMarkerPerKindKeepsKindUnderMarkerArm` pins the marked line per
kind. Migration 233's marker arm makes every marked line keep its raw kind, and
`TestContentPolicyKeepsEveryRawFindingLive` proves it on the production write
path. The gauge `eshu_dp_content_policy_raw_rows` (#8833) implements test (b).

## Operator SQL scope

#8833 writes the one-shot SQL; the statements below are the proposal this page
executed. Statement L lists the repositories to visit. Each other statement runs
per repository, 500 rows per statement, repeated until it touches 0 rows. They
blank `content_files.content`, blank `content_entities.source_cache`, delete
`content_file_references`, and purge the active-generation search copies: index
documents, index terms and the matching `reducer_eshu_search_document` fact rows
(ruling in [the fact payload cleanup](8813-credential-file-exposure-cleanup.md#the-fact-payload-cleanup-arbiter-ruling)).
The statements are `psql` scripts with three variables: `repo` (repository id),
`pred` (the path predicate above) and `w` (the watermark `W`), for example
`psql -v repo=... -v pred=... -v w=... -f statement.sql`.

Every statement carries the guard: the copy's generation was ingested before
`w` (`COALESCE(g.ingested_at, '-infinity') < :'w'`, so a NULL or pruned tag
counts raw) and the copy is non-empty. "Non-empty" is the row's own copy: the
file `content`, the entity `source_cache`, the document `ContextText`. A
reference row has no body, so the row is the copy. The guard tests the row's own
copy, not the file body, so no statement depends on another having run first: if
entities required a non-empty file body, blanking the file first would hide its
entities from the purge for good.

**Why refs, entities and files are two statements in one transaction.** The
first statement locks the batch (`FOR UPDATE SKIP LOCKED`) into a temporary
table. The second rewrites it and tests the guard again against the row's
current generation. The split is needed because, in a single statement with the
lock and the rewrite in one CTE, the recheck of a row whose version changed
re-evaluates only that locked row and reuses the generation row the statement
joined at its start (or the absence of one). When the writer commits a new
generation for the row between that join and the lock, the guard does not see
it, reads a NULL tag as raw, and blanks the rebuilt row. That reproduced on
PostgreSQL 18.6 with both a `LEFT JOIN` guard and a `NOT EXISTS` guard, in two
independent runs (see "What was run"). The second statement of the transaction gets
a fresh snapshot after the locks are held, so it sees the new generation and
skips the row. Once the locks are held, a later writer upsert waits for the
commit and then overwrites the blank. A and B stay single statements: a search
document or fact row is keyed on its generation and is never rewritten across
generations, so the guard column cannot change under the lock.

The search purge is two statements, A and B, each locking one table and each run
until it deletes 0 rows. This replaces one statement that joined the two
tables. The writer commits the fact and index rows in separate autocommit
statements: `InsertPage` inserts facts, then index rows; `Finalize` retires facts
(`retireSearchDocumentFacts`), then index documents (`finalizeSearchIndex`);
`Cancel` does the same
(`go/internal/reducer/eshusearch/eshu_search_document_writer.go`). A failure or
cancel between the two leaves an index document with no fact row, or a fact row
with no index document, and an inner join misses both. The keyword read serves
the first state: it walks terms to `eshu_search_index_documents` and never
touches `fact_records` (`go/internal/storage/postgres/search/index/store.go`).
The semantic and hybrid read serves the second.

The purge statements are on the operator page
[Credential File Cleanup](../../public/deployment/credential-file-cleanup.md#purge),
which the #8833 live tests run verbatim.

`repo_id` is a top-level payload key (`eshuSearchDocumentPayload` in the writer
file above). Each statement takes row locks on one table (A also deletes the matching term
rows, which locks those rows), so the purge adds no lock-order question between
the fact and index tables. In A, the two deletes are siblings in
one statement. PostgreSQL runs data-modifying CTEs against one snapshot and does
not order them, so neither reads the other's effect; both take their rows from
`batch`, and no row is targeted twice. The terms primary key is `(scope_id,
generation_id, term_key, document_id)` (migration `003b`, kept by `039a`; `191`
drops only the generation foreign key), so the `gone_terms` match is expected to
prune to one hash partition by `scope_id`. Runtime pruning is NOT_CHECKED.

Statements A and B filter on `repo_id` without a leading index. The
index on `eshu_search_index_documents` is `(scope_id, generation_id, repo_id,
source_kind)`, so A may scan sequentially per batch, and `fact_records` has no
`repo_id` index for B. The `EXPLAIN (ANALYZE, BUFFERS)` item at 145k rows must
cover all five, and #8833 may need to iterate A per scope (add `AND d.scope_id =
:'scope'`).

**What was run.** Every statement above ran from this page's text (a script
extracted the fences; nothing was retyped) on a scratch PostgreSQL 18.6, the
image pinned in the live-Postgres readiness workflow. The schema is the real
migrations 001, 002, 003, 003b, 004, 009, 039a, 138, 169, 176 and 191 applied
with `psql -f`; no table is stubbed, and the migration 138 triggers were live.
The seed: two repositories with five policy-path shapes (`.yarnrc.yml`,
`app/.yarnrc.yml`, `prod.tfvars`, `a/b/prod.auto.tfvars`, `envs/x.tfvars.json`),
a NULL-generation row, a marker-body row, an empty body, an entity with no file
row, an index document with no fact, a fact with no document, a superseded
generation with its own document, five non-matching names (`my.yarnrc.yml`,
`x.yarnrc.yml.bak`, `tfvars`, a README, a `.tf`), a `file` fact and 1,200 bulk rows
of each kind.

| Check | Result |
| --- | --- |
| Census before, watermark set | raw_files 1,204 (tfvars) and 5 (yarnrc); files_with_raw_finding 1,204 and 4; raw_entity_rows 1,210; raw_reference_rows, raw_search_documents, raw_search_fact_payloads 1,209 each |
| Test (a) and (b) differ | The marker-body file counts in raw_files (5) and not in files_with_raw_finding (4) |
| Statement L | Returned both repositories |
| Pass 1, bulk repository | Each of the five statements touched 500, 500, 202, 0 rows; the other repository 7, 0 (entities 8, 0) |
| Pass 1, census after | Every exit column 0; non-matching files unchanged (content md5), their documents and entities unchanged (counts); the superseded generation's document and the `file` fact survived |
| Pass 1 repeated | 0 rows in all ten runs |
| After the reindex rebuild | r1 gets a post-`W` generation with redacted, non-empty file, entity, reference, document and fact rows; r2 gets one late pre-`W` row of each kind |
| The defect | The previous revision of A and B, run in a rolled-back transaction on r1, would delete the 2 rebuilt documents and 2 rebuilt facts; 2 rebuilt files match the path alone |
| Pass 2 (guarded) | r1: 0 on all five. r2: 1 on each (the late rows), then 0. An md5 over the rebuilt files, entities, references, documents and facts is equal before and after |
| Census after pass 2 | Every raw column 0; sizing columns still read files 1,206 and 5, reference_rows 2, search_documents 2, search_fact_payloads 2 |
| Pass 2 repeated | 0 rows in all ten runs |
| Test (b) on a post-`W` file with a raw line | files_with_raw_finding 1, raw_files 0; the guarded statements touched 0 rows. The SQL does not fix a rebuild that wrote a raw line; the gauge reports it |
| `SKIP LOCKED` | A second session held one row of each of the five tables; each statement touched 2 of 3 rows and the five finished in about 0.3 s while the holder slept 6 s. After the release, each touched the last row |
| Order | Files, entities and references run in that order on a pre-`W` repository: 1, 1, 1. No statement needs another to run first |
| Orphans and terms | The orphan document and fact were deleted and the entity with no file row was blanked; 0 index terms were left without a document |
| Race | See below |

**Race, measured.** A first draft ran each statement as one CTE. To place a
writer commit between the statement's snapshot and its row lock, a
`pg_sleep(4)` ran before the lock, and a second session upserted the row under a
new post-`W` generation (and a redacted body) during the sleep. The single-
statement draft touched 1 row and blanked the rebuilt body, with the `LEFT JOIN`
guard and again with a `NOT EXISTS` guard. The transaction form on this page,
with the same sleep, touched 0 rows for files, entities and references and left
the rebuilt rows intact. The sleep is not in the page text.

**Not run.** The purge at 145k rows and any `EXPLAIN (ANALYZE, BUFFERS)`;
runtime partition pruning of the terms delete; the real search-document writer
and content writer interleaved with the purge (only one second session, and the
sleep above, stood in); migration 088 (no statement here updates `fact_records`);
repositories with more than one scope; the `repo_id` payload key and the marker
names against the writer code rather than reading. One gap the guard cannot
see: the content writer does not rewrite a file's entities atomically with the
file row (it uses a pool, and the reaper does not remove a path that drops to
zero entities), so a raw entity can sit under a file row from a post-`W`
generation. The guard then skips it and `raw_entity_rows` reads zero. #8833's
proof item on interleaving must cover this state.
