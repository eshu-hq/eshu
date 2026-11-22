# #11213 to #11216: fact id owner checks beyond the collector save

Refs #11213, #11214, #11215, #11216 (epic #10860). Follow-up to #11203, which
guarded the collector save path (`upsertFactBatchSuffix`) and named these four gaps.

## Census of fact id mints (the #11213 and #11214 acceptance item)

Every package-level function named `...FactID` that returns one string, read with
the producer guard (`go/internal/facts`, `TestFactIDProducersDeriveFromScopeAndGeneration`):

- 18 reducer id functions (service catalog, container image identity and its
  legacy twin, kubernetes correlation, three package families, AWS and
  multi-cloud runtime drift, supply chain impact, secrets/IAM, SBOM attachment,
  security alert, incident repository correlation, observability coverage, CI/CD
  run correlation, cloud inventory admission) hash **both the scope and the
  generation** through their `*Identity` function (the guard follows a helper up
  to three same-package calls to the function that names them). None omits the
  scope and none is stable across generations.
- The search document writer's `eshuSearchDocumentFactID` hashes scope, generation
  and document (#11214: unreachable by construction, pinned by
  `TestEshuSearchDocumentFactIDHashesScopeGenerationAndDocument`).
- The terraform config-state drift and code-divergence rows build
  `kind + ":" + StableID({scope_id, generation_id, ...})` inline.
- Three writers use the reducer **intent id** as the fact id (workload identity,
  cloud asset resolution, platform materialization). It is `reducerWorkItemID`:
  scope, generation, domain and entity key joined with `_` after `:` and `/`
  become `_`, so it is not injective (#11215).
- Allowlisted by reason: the suppression store (one constant scope, generation
  hashed), two read paths, a row builder that forwards ids minted by three
  checked helpers.

Result: no reducer id function omits the scope today, so there is no failing
regression to write for #11213's "each id that omits the scope" item. The owner
leg is defense in depth plus the intent-id writers' protection while #11215 stays
open.

## What changed

| Issue | Change | Proof |
| --- | --- | --- |
| #11213 | Scope-only owner leg (`fact_records.scope_id = EXCLUDED.scope_id`) on `SingleInsertQuery`, `BatchInsertConflict` and the versioned clause, after the fencing predicate. **No generation leg**: an id stable across generations of one scope keeps updating. A skipped row is reported: `CheckOwned` (single, zero rows affected) and `resolveShortfall` (batch, one primary-key probe on a shortfall) return `*OwnerConflictError`, non-retryable, failure class `fact_id_owner_conflict`, so the intent dead-letters. The reducer service counts it on `eshu_dp_fact_id_scope_conflicts_total{reason="scope", writer=reducer_single\|reducer_batch\|reducer_versioned}`; the ingestion path now carries `writer="ingestion"`. No new log line: the failed intent already logs its failure class. | `owner_guard_test.go`, `service_fact_id_owner_conflict_test.go`, `reducer_fact_owner_live_test.go`, frozen SQL pins in `reducer_fact_batch_insert*_guard_test.go` |
| #11214 | No SQL change: the id is a hash of scope, generation and document, so a collision is unreachable by construction; pinned by a test and by the producer guard. | `eshu_search_document_fact_id_test.go` |
| #11215 | No id format change (see the migration plan). `enqueueReducerBatch` resolves a shortfall in inserted rows with one primary-key probe: a held row whose stored scope, generation, domain or entity key differs from the incoming intent is refused with `*ReducerWorkItemCollisionError` (non-retryable, failure class `reducer_work_item_id_collision`); a plain duplicate stays a benign coalesce. | `reducer_work_item_collision_test.go`, `TestEnqueueRefusesAForeignIntentWorkItemIDLive`, the #11203 characterization `TestReducerWorkItemIDIsNotInjective` |
| #11216 | Guard reads `factwrite.Row` / `VersionedRow` literals, the id after `factwrite.SingleInsertQuery`, ids assigned after construction (`x.FactID = ...`) and every `...FactID` helper, follows helpers and `kind + ":" + hash(...)`; seven reasoned allowlist rows; zero-site failure kept. | `TestFactIDProducerGuardOtherShapes`, `TestFactIDProducerGuardSeededReducerViolations` |

## #11215 migration plan for an injective work item id (not done here)

Why it is not in this PR: `reducerWorkItemID` is the primary key of `fact_work_items`
for reducer rows **and** the fact id of workload identity, cloud asset resolution
and platform materialization facts. Changing its format changes both for rows that
already exist, so it needs a data migration and a rollout order, which is a
decision for the owner. The loud guard above makes a collision visible without it.

What would change: an encoding that cannot alias, for example a length-prefixed or
hashed form of the four parts (`reducer_` + sha256 over a structured identity),
used for new rows.

Backfill and rollout, in order:

1. Ship a dual-read lookup: every statement that resolves a work item by id
   (`RetouchWorkItem`, the replay schedule, ack, fail, claim fences) accepts the
   legacy id and the new id for the same intent, so a row queued under the old id
   is still found.
2. Ship the new writer behind a version switch: new enqueues use the new id; the
   enqueue coalesce checks the legacy id as well, so an intent already queued
   under the old id is not queued twice (idempotency across the cutover).
3. Backfill: rewrite the primary key of non-terminal reducer rows (`pending`,
   `claimed`, `running`, `retrying`) from the legacy id to the new id in bounded
   batches under the existing lease fence, skipping a row that holds a live lease
   until it settles; terminal rows keep their ids.
4. For the three intent-id fact writers, re-key their `fact_records` rows
   (`fact_id` is the primary key and referenced by readers and the graph writers
   by id), or keep the legacy id for those three kinds.
5. Remove the legacy lookup after a full retention window.

Risks to prove before shipping: a live lease on a row being re-keyed (the lease
fence compares `work_item_id`), two enqueues of one intent straddling the cutover,
readers that join `fact_records` on the intent id, and the dead-letter replay path
that reconstructs the id from the stored columns.

## Performance and concurrency evidence

Performance Evidence: the owner leg adds one predicate to the conflict arm of the
three reducer fact statements; it adds no scan, join or index. Measured on a
disposable `postgres:18` with 200,000 `fact_records` rows (`ANALYZE`d),
`EXPLAIN (ANALYZE, BUFFERS)` of a 1,000-row all-conflicting batch
(`TestReducerOwnerLegCostLive`, statements before the change derived from the
shipped constants by removing the leg):

| Statement | Arbiter index | Conflict filter | Shared buffers hit, pre / post (second run) |
| --- | --- | --- | --- |
| batch (1,000 conflicting rows) | `fact_records_pkey` both | `fencing_token <= excluded.fencing_token` / `... AND scope_id = excluded.scope_id` | 18,638 / 18,648 (+0.05%) |
| batch, first run | `fact_records_pkey` both | same | 18,088 / 18,548 (cold first run, dirtied 30 / 28) |
| single row | `fact_records_pkey` both | none / `scope_id = excluded.scope_id` | 20 / 20 |

The plan shape, the conflict arbiter index and the per-row buffer cost are
unchanged: the leg reads `scope_id` from the heap tuple the arbiter index probe
already fetched for the fencing predicate.

No-Regression Evidence: the paired timing in the same test (15 runs after 2
warmups, strict alternation, order flipped each run, 1,000-row batches) on a
host with load average 30 to 44 from other lanes, so the wall times are
**not gated** and only recorded: all changed rows, pre median 0.0539 s
[0.0310..0.0927], post median 0.0677 s [0.0350..0.4817]; identical replay, pre
0.0355 s [0.0241..0.4743], post 0.0338 s [0.0232..0.0511]; identical replay with
one foreign row in 1,000, pre 0.0345 s [0.0236..0.1065], post 0.0385 s
[0.0239..0.0661]. Every post median is inside the pre spread; the buffer counts
above are the load-independent proof. Enqueue: the new probe
(`heldWorkItemsQuery`) runs only when an enqueue admits fewer rows than it
attempted, one primary-key join over the batch, so the common path (all rows
inserted) adds no query.

Concurrency Evidence: `TestReducerFactOwnerRaceTwoScopesLive` holds one writer's
uncommitted insert of a new fact id while a writer from another scope arrives, in
both orders over six rounds: the late writer waits on the first, re-evaluates the
conflict `WHERE` after the first commits, is refused with the typed
`*OwnerConflictError` (writer `reducer_batch`, one row), and the first committer
keeps the row (three wins per scope). No deadlock, no lost write.
`TestReducerFactStatementsRefuseAForeignScopeRowLive` proves the same refusal for
the single, batch and versioned statements, and
`TestEnqueueRefusesAForeignIntentWorkItemIDLive` proves the queue case on the
real `fact_work_items` table.

Observability Evidence: a refused reducer write fails the intent with the failure
class `fact_id_owner_conflict` (dead letter), increments
`eshu_dp_fact_id_scope_conflicts_total{reason="scope", writer=...}`, and the ERROR
log of the failed intent names the failure class; an enqueue collision fails the
enqueue with failure class `reducer_work_item_id_collision`. No new per-call log.
The ingestion path keeps its single ERROR log and now labels the counter
`writer="ingestion"`. Documented in the metrics reference and the telemetry
coverage registry.

## Known limits

- The producer guard cannot see ids minted in SQL, or built in a function not named
  like an id helper and handed through a parameter to a row literal (the allowlist
  names those mints).
- `resolveShortfall` resolves a batch shortfall only when the handle can query:
  a `db.Queryer`, or a wrapper that lends a read through
  `factwrite.OwnerProbeQueryer.QueryFactOwners`. Four transaction wrappers have a
  `QueryContext` of another shape or none (container image identity, AWS runtime
  drift admission, service materialization, supply chain impact) and now
  implement `QueryFactOwners`; `TestFactOwnerWriteHandlesCanProbe` pins them and
  `TestEveryTransactionWrapperIsAFactOwnerWriteHandle` fails when a new
  `db.Transaction` wrapper is added without being listed. The recording fakes of
  the writers' own tests cannot query, and a shortfall without a probe is not an
  error, because a fenced-out stale row is a legitimate shortfall.
- The container image identity atomic statements
  (`containerImageIdentityPublishAndLegacyCleanupQuery` and the completed-cutover
  pair) compose `BatchInsertConflict`, so they carry the owner leg, but their rows
  affected count the legacy cleanup `DELETE`, not the insert, so the final chunk of
  a publish does not report a foreign-held row (earlier chunks go through
  `ExecChunk` and do). Its ids hash scope and generation (census above), so the leg
  is defense in depth there.
- A batch whose statement is skipped for several rows reports up to ten samples and
  the full count of the foreign-held rows.
