# AGENTS.md — storage/postgres guidance for LLM assistants

## Read first

1. `go/internal/storage/postgres/README.md` — pipeline position, store
   inventory, queue lifecycle, and operational notes
2. `go/internal/storage/postgres/db/contracts.go` — `Rows`, `Queryer`,
   `Executor`, `ExecQueryer`, `Transaction`, `Beginner`,
   `ReadOnlyRepeatableReadBeginner` (adapters and bootstrap lock stay in root)
3. `go/internal/storage/postgres/projector_queue.go` — `ProjectorQueue.Claim`
   (bounded 40P01/40001 retry; helpers in `projector_queue_scan.go`), and
   `projector_queue_ack.go` — `Ack`; the atomic, lock-timeout-bounded ack transaction is the most sensitive path
   in this package
4. `go/internal/storage/postgres/projector_queue_sql.go` — projector claim,
   stale-generation coalescing, duplicate-lease reclaim, and lifecycle SQL
5. `go/internal/storage/postgres/facts.go` — `upsertFacts`,
   `deduplicateEnvelopes`, `sanitizeJSONB`; understand the batching and
   deduplication constraints before changing fact write paths
6. `go/internal/storage/postgres/status_queries.go` and
   `go/internal/storage/postgres/status_registry.go` — status aggregate SQL,
   including fact queue, shared projection domain backlog, and bounded registry
   collector aggregates
7. `go/internal/storage/postgres/schema.go` — `BootstrapDefinitions`,
   `ApplyDefinitions`; DDL ordering and idempotency rules
8. `go/internal/storage/postgres/cloud/aws/pagination_checkpoint.go` — AWS
   checkpoint fencing and stale-generation expiry
9. `go/internal/storage/postgres/cloud/aws/scan_status.go` and
   `status_aws_cloud.go` — AWS scanner status persistence and admin status
   projection

## Invariants this package enforces

- **Idempotency** — all INSERT paths use `ON CONFLICT DO NOTHING` or
  `ON CONFLICT DO UPDATE`; schema DDL uses `IF NOT EXISTS`. Do not add
  non-idempotent INSERTs or CREATE TABLE without IF NOT EXISTS. The terminal-failure acknowledgement covering rule has one definition (`workflowTerminalFailureAckCovers`, see `terminal-failure-acknowledgement.md`); never copy it into another query.
- **Go DDL never outruns the migrations (#7777, #7792)** — a column, index,
  table, named constraint (table-level or inline), trigger, function,
  sequence, view, materialized view, procedure, type, or domain added to a
  store's Go DDL (`EnsureSchema`, `*SchemaSQL`) needs a numbered migration
  in `migrations/` in the same change; the schema Job applies only those
  files. `TestDDLMigrationParity` checks those names. It fails on Postgres
  DDL it cannot read, including an ALTER TABLE action outside
  `ddlAlterForms` and every `ALTER INDEX`, `ALTER SEQUENCE`, `ALTER TYPE`,
  `ALTER DOMAIN`, or `ALTER [MATERIALIZED] VIEW`, unless
  `ddlParityDynamicDDL` lists it with a reason, and it fails on stale
  allowlist entries. Review these by hand, because the test does not:
  definitions and `ALTER COLUMN` changes; any other statement kind
  (`CREATE EXTENSION`, `CREATE SCHEMA`, `CREATE POLICY`, `DROP`, and so
  on); a trailing clause in a separate constant or `strings.Join` element
  after a complete first literal; and a later `DROP` or `RENAME` in the
  migrations, which the test does not apply.
- **Fact deduplication before batching** — `upsertFacts` calls `deduplicateEnvelopes` before each batch to prevent `SQLSTATE 21000` on `ON CONFLICT DO UPDATE` when the same `fact_id` appears twice in one batch (`facts.go:206`).
- **The content_files upsert is guarded (#8915)** — both file upserts share `upsertContentFileConflictClause`, whose `WHERE ... IS DISTINCT FROM` skips a row that differs only in `indexed_at`. Keep the SET list, the guard tuples, and the insert columns in lockstep (`TestUpsertContentFileConflictClauseLockstep` fails on drift), keep the compare NULL-safe, and keep one constant for both statements. A skipped row keeps its old `indexed_at`; the statement-level UPDATE triggers still fire but see no changed row. Do not read `RowsAffected` as "files written". The batch writer rejects a duplicate (repo_id, path) in one batch but accepts one path under two repos (`TestContentWriterRejectsDuplicatePathInBatchLive`, `TestUpsertContentFileBatchDuplicateKeyIsRepoAndPath`). Proof: `content_file_upsert_guard_*_live_test.go`; note: (#8915). The writer also counts each batch row as `inserted`, `unchanged_skipped`, `metadata_only`, or `content_changed` (`eshu_dp_content_file_upserts_total`, #8950) from one lock-free, content-free pre-read (`contentFileOutcomePreReadSQL`, body read only through `octet_length`) run before the upsert and emitted after it succeeds; the pre-read is a digest-and-size proxy, never fails the Write, is sampled by a Weyl sequence for batches under `contentFileOutcomeMinBatchRows` (rows and bytes skipped are counted as unclassified, and every counter carries `batch_class` so shares can be weighted back; a share without it is wrong), and must not gain a row lock or a `content` read. `TestClassifierComparesEveryGuardColumn` derives the guard columns from `upsertContentFileConflictClause` and fails when `classifyContentFileRow` ignores one. Proof: `content_writer_upsert_outcome*_test.go`; note: (#8950); reference: `docs/public/reference/telemetry/content-file-upserts.md`. The two-statement writer (#8950 prototype, always on since #8942) pre-reads every batch and routes `metadata_only` rows through a metadata-only UPDATE (no `content`/`indexed_at` write, HOT-eligible) with the guarded upsert as fall-through plus bounded 40P01 retry, counted by `eshu_dp_content_file_write_conflict_retries_total`; only a failed pre-read keeps the single statement. The sampling policy still gates the counters only, never the write. The single-row `ContentStore` path tries the same UPDATE first; migration 237 gives new heap pages the fillfactor-50 headroom. A mixed batch bumps the config-artifact fence once per touching statement (one implicit transaction each, the fence's per-transaction contract); readers compare versions by inequality. Proof: (#8950) and (#8942).
- **Fact-record cross-batch fencing, and unchanged-row skip** — `upsertFactBatchSuffix` keeps the `WHERE fact_records.fencing_token <= EXCLUDED.fencing_token` conflict guard (#4444) ANDed with an OR-chained `IS DISTINCT FROM` leg per SET column except `observed_at`/`ingested_at` (#10873: both move on every save, so comparing them would stop the guard from ever skipping). Without the fencing guard, a stale or out-of-order batch that commits after a newer batch silently overwrites the newer fact_id row (and resurrects its payload) purely on commit order; `deduplicateEnvelopes` only protects against duplicate `fact_id` values inside one batch, not across batches.
  Keep the SET list, the guard legs, and the INSERT columns in lockstep (`TestUpsertFactBatchGuardLockstep` fails on drift, with seeded RED/GREEN in `TestFactGuardClauseDriftSeededViolations`), keep the fencing predicate verbatim and first, keep `fencing_token` inside the compared tuple (a higher-token same-content write must still land to advance the stored token), keep `is_tombstone` inside it (flips always write), and keep payload last so cheap scalars short-circuit. A skipped row keeps its old timestamps and its old xmin (no heap rewrite); `selectUnchangedFactIDs` (`unchanged_guard.go`) re-reads RETURNING-absent ids on the same transaction and admits the still-equal ones for afterBatch derivation, while fenced-out rows stay excluded — never read `RowsAffected` as "rows written". Do not strip volatile payload keys (those kinds simply do not skip), do not widen the guard to `SingleInsertQuery` or the factwrite batch/versioned paths, and do not add an index or DDL. Proof: `unchanged_skip_proof_live_test.go`, `unchanged_skip_fencing_live_test.go`, `unchanged_skip_bench_live_test.go`; note: (#10873). **Owner guard (#11203)**: `fact_records` is keyed on `fact_id` alone and the SET list never touches `scope_id`/`generation_id`, so the conflict WHERE also requires `fact_records.scope_id = EXCLUDED.scope_id AND fact_records.generation_id = EXCLUDED.generation_id` (`factOwnerGuardClause`, right after the fencing predicate; `TestUpsertFactBatchGuardLockstep` and `TestFactOwnerGuardDriftSeededViolations` pin both legs). A row held by another scope or generation is skipped, never rewritten, whatever its token or content. The unchanged-row discriminator (`selectUnchangedFactIDs`) reads the stored scope and generation and splits RETURNING-absent ids three ways in one statement: unchanged (admitted), held by another owner (conflict), fenced out (excluded); a foreign row whose guarded columns and token equal the owner's must never read as unchanged, or the SQL guard alone leaves a derivation hole. `upsertStreamingFacts` returns `*FactIDOwnerConflictError` at the first conflicting batch (before `afterBatch`, so nothing is derived from a foreign write); `IngestionStore.commitScopeGeneration` answers with `refuseFactIDOwnerConflict` (`ingestion_fact_id_owner_refusal.go`): roll back, drain the stream, `collector.MarkCommitRefused` with the closed reason `fact_id_owner_conflict`, the generation outcome, `eshu_dp_fact_id_scope_conflicts_total{reason=scope|generation}`, one ERROR log (ids, count, 10 samples, no payload), and return nil, exactly like `refuseDestructiveSuccessor`. It does not skip-and-continue (a generation that activates with a fact missing is a wrong graph nothing can detect) and does not return a plain error (that ends the unclaimed collector loop, or retries without bound with the attempt budget off). There is no hold: the next collection tick re-collects, re-collides, and refuses again with a fresh ERROR log until the producer's id is fixed. Other callers of `upsertStreamingFacts` (`vulnerability_suppression_store.go`) propagate the typed error and roll back their own transaction. `upsertFactBatch` (no RETURNING) has the SQL protection but cannot report, and has no production caller (`TestUpsertFactBatchHasNoProductionCaller`); do not wire it. The owner legs are deliberately NOT on `factwrite.BatchInsertConflict`, the reducer single/versioned inserts, or the search-document writer: a reducer id that is stable across generations would freeze at its first generation (#11213, #11214 own that census). Do not add scope or generation to the SET list to make a foreign write land. `relationship_reference_candidate_keys` closes transitively: `refreshRelationshipReferenceCandidateKeys` runs in `afterBatch` on the accepted set only, and a conflicting batch never reaches `afterBatch`. The reducer fact writers (`reducer/factwrite`) carry a scope-only owner leg and the same refusal as a typed non-retryable error (#11213; see its AGENTS.md); the counter's `writer` label separates `ingestion` from `reducer_single`, `reducer_batch` and `reducer_versioned`. `reducerWorkItemID` is not injective (#11215), so `enqueueReducerBatch` resolves a shortfall in inserted rows with one primary-key probe (`refuseWorkItemCollisions`, `reducer_work_item_collision.go`): a held row whose stored scope, generation, domain or entity key differs from the incoming intent is refused with `*ReducerWorkItemCollisionError` (non-retryable, failure class `reducer_work_item_id_collision`) instead of being coalesced away by `ON CONFLICT (work_item_id) DO NOTHING`; a plain duplicate stays a benign coalesce. The id format itself is unchanged (an injective id needs a data migration; the plan is in #11213 (retired evidence record)). Proof: `fact_id_scope_guard_live_test.go`, `fact_id_owner_refusal_live_test.go`, `fact_id_owner_race_live_test.go`, `fact_id_owner_conflict_test.go`, `fact_id_owner_guard_bench_live_test.go`; note: (#11203).
- **Pacing latch writes are transition-exact (#10882)** — `claimPacingLatchUpsert` (the depth, global, and reducer-source latch rows) writes only when the stored `engaged` bit differs from the desired one (`DO UPDATE ... WHERE latch.engaged IS DISTINCT FROM EXCLUDED.engaged`), and `writeClaimPacingLatch` (`pacing_latch_transition.go`) records telemetry only when `RowsAffected` is 1. A consult that raced a transition and lost changes nothing: no second `eshu_dp_workflow_claim_pacing_engaged_total`, `engaged_at` stays the first writer's, `updated_at` is the last real transition (nothing reads it as a heartbeat). The active gauge follows ownership: the process whose statement engaged the latch raises it, every process that holds a raise takes it back when a read that began after the raise shows the engagement over (`observeClaimPacingLatch`, called from every consult and `SourcePacingStates`), and a process that never raised it never lowers it. Never count a transition from a verdict difference or write the latch unconditionally; the race proofs are `pacing_latch_race_live_test.go` and `pacing_latch_interleave_live_test.go`; note: #10882 (retired evidence record).
- **The supports verification is sliced, never unbounded (#10880 decision 6)** — `supplyChainImpactSupportsBackfillVerifySliceSQL` (`supply_chain_impact_supports_verify.go`) checks one (scope_id, generation_id, fact_id) interval per step: up to `VerifySliceSize` supports and live findings, the interval ending at the smaller last key of a full side so slices tile the key space. Keep both halves' predicates the unsliced ones (missing = no support of the fact_id; orphan = support whose fact is tombstoned, probed only for supports with no live finding of their key), keep the cursor guarded by `verify_sweep = sweeps` (migration 348; an older binary's restart bumps sweeps without clearing it), restart the sweep on any miss, and keep the fence recheck in the completion statement. Completion is one transaction: `supplyChainImpactSupportsBackfillVerifyClearSQL` drops the cursor guarded by the sweep read, then the unchanged, contract-pinned completion statement runs (zero rows from either rolls back), so a marker reset without a sweep bump re-verifies from the start. Never replace it with a whole-table statement or a per-support `fact_records` probe (QA: 143 min projected). Proof: `supply_chain_impact_supports_verify_test.go`, `supply_chain_impact_supports_verify_live_test.go`; note: #10880 (retired evidence record).
- **Acceptance rows are advance-only (#6679)** — the upsert `WHERE` compares
  `EXCLUDED` with the row's own `(generation_ingested_at, generation_id)`; only a
  NULL (legacy) key may fall back to a lookup, which sees the statement snapshot
  (review F1). No bulk backfill (it stalls writers); see the SQL doc comment.
- **Freshness de-dupe covers in-flight generations** —
  `CommitScopeGeneration` compares the incoming `FreshnessHint` and commit with
  the newest same-scope `pending` or `active` generation; a matching reconcile
  re-opens the active generation's projector work (#7978). Exclude `failed`
  generations so a later snapshot retries a failed first projection. Separately
  (#6738), the generation upsert rewrites only `pending` rows and never refreshes
  `observed_at`/`ingested_at`; a finalized same-ID retry rolls back and drains. The #7926 destructive-successor backstop sizes like for like (#8750): `successor_guard.go` compares a full successor with the prior's collector rows (`linksfreshnessstore.ExcludeReducerDerivedKinds` strips the `reducer_` rows a reducer writes back into a generation), and sizes a git delta successor by its file-level union against its anchor, which must come from `resolveDeltaBaseAtCommit` at the delta's union-base commit (`deltaUnionCommitsFor`, COALESCE to the baseline, #8780), the resolver the projector overlays on, never a second copy of that rule; path matching must use `facts.DeltaScope.FilePath`, the overlay's own qualifier. Keep `successorGuardCountQuery` one index-only pass, keep the anchor probe off the common path (it runs only when the zero-query bound fails), and keep the rule, kind, and outcome labels on `eshu_dp_ingestion_successor_guard_decisions_total` inside the closed `telemetry.SuccessorGuardRule*` sets. The prior is sized before the commit transaction opens, so `recheckSuccessorPrior` re-reads it on the transaction after `upsertIngestionScope` locks the scope row and before `upsertScopeGeneration`, and re-sizes on any change (#8750 window); keep it there, and close every guard cursor before the next statement on the transaction; see (#8750). The #10020 generation outcome counter has one mint site and one skip funnel: `recordMintedGeneration` runs only after `tx.Commit()` succeeds (a rolled-back generation must never count a mint; `TestCommitFailedTransactionCountsNoOutcome` guards it), and every skip goes through `backfill.Store.RecordSkippedRefresh`. The #9694 held-stale signal is process-local: `recordSuccessorRefusal` and `clearSuccessorHold` (`ingestion_backfill_seam.go`) feed `telemetry.SuccessorHoldTracker` through `Instruments.SuccessorHolds`. A refusal records after `tx.Rollback()`; a passing guard decision and a duplicate skip clear. The tracker is a leaf mutex: it adds no statement, lock, or transaction to the commit path. Keep it that way, and do not persist the run without an owner ruling.
- **JSONB sanitization** — `sanitizeJSONB` removes JSON-encoded U+0000
  characters and raw control bytes before every fact INSERT. It preserves
  literal source text such as the six characters `\u0000`. Skipping this causes
  Postgres errors on repositories with binary or non-UTF-8 content.
- **Ack atomicity and lock order (#6738)** — one `Ack` transaction, scope first,
  under a local `lock_timeout` (55P03 → `projector.ErrWorkAckDeferred`; retry via
  `AckWhenScopeFree`). Scope precedes generation/work everywhere; Heartbeat uses
  `FOR NO KEY UPDATE SKIP LOCKED`, never waiting on ingestion. Else 40P01.
- **Ack journal (#7127 PR-3b, dark)** — with `ChangedSinceJournalEnabled`
  false, Ack sends exactly the statements it sent before
  (`TestProjectorAckWithJournalOffSendsTheShippedStatements`). On, only two
  statements change, both derived from the shipped constants (prefix guard
  `TestAckJournalStatementsDeriveFromShippedQueries`): the active supersede
  gains `RETURNING generation_id`, and the activate statement becomes the
  gated CTE `activateAndJournalProjectorGenerationQuery`. Keep the gate
  `AS MATERIALIZED`, keep it on `linksfreshnessstore.AckJournalGateSQL`
  (never an advisory literal in this package), keep it a try-lock, and never
  move the insert out of the activate statement or drop the gate: both
  invert `activation_seq` against commit order
  (`TestProjectorAckJournalInversion`). A missed gate writes no row and
  does not wait. Run the `TestProjectorAckJournal*` live proofs after any
  change to Ack.
- **Lease fencing** — projector Heartbeat/Ack/Fail match `lease_owner` and
  `attempt_count`; zero rows is `ErrProjectorClaimRejected` (wraps
  `projector.ErrWorkClaimLost`; drop the attempt). `WorkflowControlStore` checks
  `lease_owner`; callers stop on `ErrWorkflowClaimRejected`. Rejections are final.
- **Projector scope ordering** — `ProjectorQueue.Claim` must preserve one
  active source-local generation per `scope_id`. Keep the oldest-ready-row
  subquery with `FOR UPDATE SKIP LOCKED`; without it, parallel claimers can skip
  a locked older row and start a newer generation for the same repository.
  Expired `claimed` or `running` rows must stay ahead of ordinary pending rows
  in the claim ordering, or stale leases remain overdue while newer generations
  drain. Keep the stale duplicate reclaim CTEs in the claim path: they demote
  expired same-scope siblings to `retrying` when another live or newly claimed
  sibling owns the scope. Every maintenance CTE in
  `projector_queue_claim_sql.go` locks its rows first with
  `FOR NO KEY UPDATE ... SKIP LOCKED`, repeating the row-self predicates for the
  EvalPlanQual recheck, and updates only the locked ids (#7108). Never add a
  blocking multi-row UPDATE to the claim: concurrent claimers deadlocked (40P01)
  on one. #7115: the claim locks and bumps the scope's `projector_scope_claim_fences` row and never locks `ingestion_scopes`; only the claim and the write-start marker (#7819: SKIP LOCKED fence lock before the generation row, bump when the marker sets) may bump fence rows, while the claim sweep and Ack's obsolete-generation maintenance (#7187) may additionally lock (never bump) the fence row SKIP LOCKED before their generation rows (the insert trigger, backfill and cascade delete aside), and no table may reference them (see `claimProjectorWorkQuery`'s doc comment). Keep the stale-generation coalescing CTEs together; they move older
  same-scope projector rows and pending or failed `scope_generations` to `superseded` so durable snapshot history
  remains available without reprocessing obsolete local polling generations or
  reporting superseded terminal failures as current health.
  Keep the `ProjectorQueue.Heartbeat` supersede check with that claim behavior:
  live older same-scope generations must return `projector.ErrWorkSuperseded`
  once a newer generation is visible, or local polling can spend minutes writing
  graph state that will be immediately obsolete. It demotes only a `pending`
  generation; an `active` one keeps the scope pointer until successor Ack. #7130: a `superseded` generation is terminal for Claim (second `supersedable_projector_generations` branch sweeps its pending, retrying and expired-lease rows; keep the branches un-ORed and SKIP LOCKED), Heartbeat (`projector_heartbeat_generation_superseded`), Ack and replay; see (#7130). #7320: every statement that sets a `fact_work_items` row to `superseded` (claim sweep, Ack obsolete supersede, Heartbeat supersede, Ack refusal, reducer inactive-generation CTE) must append the fragment constant for its own alias (`priorFailureStaleSQL` or `priorFailureWorkSQL`) to its `failure_details` so `failure_details.prior_failure` carries the old status, class, message, details and `updated_at`; never cast the old details to jsonb, never add a new writer without it (`TestSupersedeStatementsFoldPriorFailure` enumerates writers from source); see (#7320). #7388: both `projector_stale_scope_reclaim` UPDATEs keep the marker class and message and fold the prior failure into `failure_details` with `priorFailureStaleSQL`, except a row that already carries the marker keeps its details as they are (no nesting; `TestReclaimStatementsFoldPriorFailure` enumerates them); the admin operator note wraps `{"operator_note": n}` around `PriorFailureWorkSQL`. Do not truncate inside the fold; the size bound is at the Fail-time writer (`queuestore.QueueFailureMetadata`/`DeadLetterTriageMetadata`, #7407: details 4096 B, message 1024 B, marker counted inside the limit), so the stored evidence and its copy stay equal. #7319: the delta-baseline fence read in Ack must stay its own statement after the scope-row lock statement returns (never folded into the lock statement or a lock CTE, which would read a pre-wait snapshot), placed after the work-row update and before the first statement that changes another generation; the refusal mark never takes the scope row. #7389: the heartbeat supersede's marker gate must stay on the locked `locked_generation` row (EvalPlanQual rechecks only locked or updated rows; a gate on the unlocked joined generation lost 341 of 1,000 interleaved races), take scope, then `locked_work` (full lease fence), then `locked_generation`, all SKIP LOCKED, so the heartbeat never waits and cannot deadlock with the work-then-generation refusal (`TestProjectorHeartbeatNeverDeadlocksWithBaselineRefusal`), keep the full predicate on the generation so the steady-state heartbeat locks no generation row, and keep the work UPDATE's copy identical (`TestSupersedeRunningGateLocksGenerationWithFullPredicate`); the write-start marker locks the scope's fence row SKIP LOCKED and then only its own generation row, never the work row (#7819); run `TestProjectorHeartbeatWriteMarkerInterleave` after any change to either statement. #7469: the claim sweep's first branch and Ack's obsolete supersede spare a generation with `projection_write_started_at` set, and the oldest-ready subquery holds a newer row behind an older same-scope marked generation with `pending` or `retrying` work (snapshot guard plus lock-time `marker_spared` guard; keep both holds inside the oldest-ready subquery, never as an outer-WHERE copy, which stalls); terminal marked rows never hold; run `TestProjectorClaimRetryAgainstNewerInterleave` and `TestProjectorClaimMarkerSweepRace` after any change to the claim. #7473: the claim sweep's first branch spares a full generation (`is_delta` false) unless a newer full exists, and the oldest-ready subquery holds a newer delta behind an older same-scope full with `pending` or `retrying` work (keep the hold inside the oldest-ready subquery, never as an outer-WHERE copy, which stalls); terminal fulls never hold; Ack's obsolete supersede deliberately stays full-blind as the misordering backstop (see (#7473)); run the full-guard proofs (`TestProjectorClaimFullSurvivesNewerDelta` and the `*FullGuard*` tests) after any change to the claim.
- **NornicDB semantic gate** — `ReducerQueue.Claim` blocks
  `semantic_entity_materialization` while source-local projection is in-flight
  when the NornicDB gate parameter is true. Do not remove or bypass this gate
  without an ADR. #10863: reducer claims fence stale rows read-only via `reducerClaimStaleGateCTEs` — never re-embed the supersede sweep in a claim; terminalization belongs only to the bounded standalone sweep (`ReducerQueue.SupersedeStaleReducerGenerations`). #11114: every narrowed producer-terminal row of `WaitingDeferralMap` declares a `ProviderLocality` (no valid zero value; `TestWaitingDeferralMapDeclaresProviderLocality`); the post-ack cross-scope leg (`wakeProducerTerminalCrossScopeQuery`) runs only for a domain with a `cross_scope` class and filters `failure_class = ANY($3)` in both CTEs. Declare `same_scope_generation` only when the handler probe reads the waiter's own scope and generation, else the wake is lost (`TestProducerTerminalWakeCatalogRuntime*Live`); never drop the filter or the leg, and keep its reopen clause and `COLLATE "C"` lock order identical to `reopenWaitingRowsQuery`; see #11114 (retired evidence record).
- **Status domain backlog includes shared projection.** `StatusStore` merges
  `fact_work_items` backlog with pending `shared_projection_intents` so
  `/admin/status` remains `progressing` until reducer-owned shared edges are
  graph-visible. Do not remove that union when editing `status.go`.
- **Generation liveness respects shared-resolver ownership.** Exact
  `repo_dependency` / `repo_dependency:<scope>` source runs in that projection
  domain must not reopen `source_local`, even after backward evidence commits.
  Source-local replay cannot advance the shared resolver and can launch an
  obsolete full canonical retract. Keep recovery and stuck-age predicates in
  lockstep, preserve other domains and prefix-collision source runs, and do not
  reduce worker concurrency as a substitute. #7265: both liveness queries MUST embed the one `generationIntentProgressingPredicate` const; its progress probe is per `projection_domain` queue and MUST NOT filter completions on `source_run_id` (the exact `repo_dependency` family is excluded only from actionability). Keep `liveness_progress` unfenced in the recovery query and `OFFSET 0`-fenced in the gauge query; both shapes are measured (join order vs. JIT cost) in (#7265). Skips never touch `liveness_recovery_attempts`.
- **Schema ordering** — tables with foreign key constraints must appear after
  their referenced tables in `bootstrapDefinitions`. Current FK dependencies:
  `graph_projection_phase_state` → `ingestion_scopes` + `scope_generations`.
- **AWS checkpoint fencing** — `awsstore.AWSPaginationCheckpointStore.Save`
  must keep the `fencing_token <= EXCLUDED.fencing_token` conflict guard. A
  stale AWS worker must not overwrite page state from a newer claim.
- **AWS scan-status fencing** — `awsstore.AWSScanStatusStore` mutations must
  keep their fencing guards. A stale AWS worker must not overwrite per-tuple
  status from a newer claim.
- **AWS runtime drift joins stay bounded** —
  `PostgresAWSCloudRuntimeDriftEvidenceLoader` must load AWS rows from one
  `(scope_id, generation_id)` and must join Terraform state through the current
  AWS ARN allowlist. Do not scan all active Terraform state to discover matches.
  The state join probes `fact_records_terraform_state_resource_arn_idx`
  (migration 180) once per ARN through an `OFFSET 0` LATERAL inside a
  `MATERIALIZED` CTE, and joins `ingestion_scopes` only after that fence.
  Keep both fences and keep the probe's predicate identical to the index
  predicate (#7666): without them the planner looped the active-scope join per
  ARN (11.16M buffers) or walked the whole index on stale statistics. The
  plan proof is `aws_cloud_runtime_drift_state_arn_plan_live_test.go`; the row
  differential against the pre-#7666 join is
  `aws_cloud_runtime_drift_state_arn_differential_live_test.go`. Do not add an
  expression statistics object to fix a plan here; a stale one left the
  unchanged statement 2,358x slower than a fresh one.
  If backend ownership is missing or ambiguous, suppress unmanaged
  classification for that state-backed ARN; unknown config is not proof of
  absent config.

- **AWS runtime drift finding reads stay active and scoped** —
  `AWSCloudRuntimeDriftFindingStore` reads
  `reducer_aws_cloud_runtime_drift_finding` rows through
  `ingestion_scopes.active_generation_id`. It rejects filters without
  `scope_id` or valid AWS account scope, rejects wildcard-capable account or
  region values before building the `LIKE` prefix, and caps list reads at 500
  rows before querying; do not add unbounded fact-table scans for management
  APIs.

- **AWS runtime drift write admission (#5848)** — the
  `aws_cloud_runtime_drift_write_admission` begin-before-mutate check
  (`AWSCloudRuntimeDriftAdmissionBeginner`,
  `hasPendingStateSnapshotGenerationQuery`) MUST run as the FIRST statement in
  `PostgresAWSCloudRuntimeDriftWriter`'s transaction, before the versioned
  insert and the retire. A pass whose evidence-read watermark is older than one
  already admitted for the same `(scope_id, generation_id)` must be rejected
  before any other statement runs — checking admission after the insert, or in
  a separate unwrapped transaction, reopens the exact race #5848 closed (a
  reclassified ARN mints a different `fact_id`, so the insert's own
  `ON CONFLICT` guard cannot catch it). Do not add a domain-wide exclusive lock
  or reduce reducer worker/batch concurrency as a substitute; the conflict
  domain is already partitioned by `(scope_id, generation_id)`.
- **AWS runtime drift retire is bounded to evaluated ARNs** —
  `retireAWSCloudRuntimeDriftFindings`'s `evaluatedARNs` argument must be every
  ARN the CURRENT pass's evidence load covered, not only the ones that produced
  an admitted candidate. Narrowing it to admitted-candidate ARNs would stop
  retiring a converged (no-longer-drifted) ARN's stale prior finding.
- **AWS runtime drift readiness defer is opt-in and bounded by elapsed time,
  not retry count** — `AWSCloudRuntimeDriftHandler.ReadinessChecker` being nil
  must leave `Handle` writing its best-available classification
  unconditionally (pre-#5848 behavior). When wired, the defer must stay
  bounded by `awsCloudRuntimeDriftStatePendingMaxWait`, an ELAPSED-TIME bound
  compared against `Intent.CycleStartedAt` (falling back to `Intent.EnqueuedAt`
  only for a hand-built `Intent` that does not populate it) — never a
  `Intent.AttemptCount` comparison. `AttemptCount` is frozen by this defer's
  own non-counting failure class the moment the row is retried once
  (`nonCountingReducerRetryFailureClasses` in
  `reducer_queue_readiness_sql.go`), so a retry-count bound can never fire
  against the real queue. `CycleStartedAt` (not `EnqueuedAt` alone) is the
  correct anchor because `EnqueuedAt`/`created_at` is immutable across a
  reopen, and this domain is unconditionally reopened on every ingester shard
  drain and bootstrap maintenance pass
  (`postgres.CrossScopeCorrelationReopenDomains`) — anchoring on `EnqueuedAt`
  alone would give a maintenance-triggered reopen NO grace window at all,
  since the original enqueue is almost always already past the bound by the
  time a reopen happens. `ReopenSucceeded`/`ReplayDomain`
  (`reducer_queue_replay.go`) reset `fact_work_items.reopened_at` in the same
  statement that resets `attempt_count = 0`, so a reopened row's
  `CycleStartedAt` (`COALESCE(reopened_at, created_at)`, computed by the claim
  query and by the status and queue-observer oldest-age aggregates, #9292) gets a genuinely fresh window (every terminal reopen writer stamps it since #9320); see
  `go/internal/reducer/awscloud/aws_cloud_runtime_drift_readiness.go`'s
  `awsCloudRuntimeDriftStatePendingMaxWait` doc comment,
  `TestAWSCloudRuntimeDriftHandlerConvergesAfterElapsedBoundOverRealQueueLive`
  for the attempt_count-freeze mechanism and real-queue proof, and
  `TestAWSCloudRuntimeDriftReopenGetsFreshElapsedBoundWhileStatePendingLive`
  for the reopen-anchor proof. An unbounded (or unreachably-bounded, or
  no-grace-window-on-reopen) defer would starve a genuine orphan (a resource
  with no Terraform state anywhere, ever) forever.
- **Package-consumption backfill rebuilds a scope one page per transaction
  (#7663)** — `PackageManifestConsumptionKeyBackfill.Run` hands each scope to
  `consumption.Rebuilder`. That rebuilder takes the scope's `ingestion_scopes`
  row `FOR NO KEY UPDATE` under a 5 s `lock_timeout`, once in the start
  transaction and once for each 500-fact page. Never fold the pages back into
  one transaction: that held a 63k-fact scope's row for over an hour and
  blocked every foreign-key insert for the scope. A writer that sets
  `eshu.package_manifest_consumption_keys_writer` must take that row lock
  first, as `upsertIngestionScope` does. Keep
  `listPackageManifestConsumptionKeyScopeFactsSQL`'s predicate byte-aligned
  with migration 181's partial index. See `consumption/AGENTS.md` and
  (#7663).
- **Derived scopes retire with their repository (#10054).** Resolve a repository's scopes only through `retirementScopesOfReposSQL`/`retirementScopesOfRepoSQL` and attribute them through `retirementScopeRepoIDSQL`; never select by a derived scope's `partition_key` (the shared fairness key) or a scope-id prefix. A derived commit takes its parent's shared key in sorted order and refuses on any open parent tombstone; a non-derived commit must keep its one barrier and no extra read. See `derived-scope-retirement.md`; run the `repository_retirements_derived*_live_test.go` proofs after changing either side.
- **Key sidecar deletes (#7420).** A new statement that deletes `fact_records` (directly, or through a `scope_generations` or `ingestion_scopes` cascade) runs the `facts/keys` delete first in the same transaction, or touches only fact kinds that own no key rows, and is classified in `factDeleteSites`. Read `facts/keys/AGENTS.md` first.

## Common changes and how to scope them

- **Add a new Postgres store** → implement against `db.ExecQueryer`; add a
  `New*Store(database db.ExecQueryer)` constructor; add a `*SchemaSQL()` function
  returning idempotent DDL when the store owns a table; register owned tables in
  `BootstrapDefinitions` in `schema.go` with the correct position in the slice;
  wrap with `InstrumentedDB` in `cmd/` wiring for observability.

- **Change AWS checkpoint persistence** → edit
  `cloud/aws/pagination_checkpoint.go`; keep the primary key scoped to
  collector instance, account, region, service, resource parent, and
  operation; keep generation as invalidation state; and keep resource
  parents and page tokens out of telemetry labels.

- **`config_state_drift` enqueue triggers (when drift gets evaluated, not how)**
  → bootstrap's one-shot Phase 3.5 sweep is `drift_enqueue.go`
  (`IngestionStore.EnqueueConfigStateDriftIntents`, scans every
  `state_snapshot:*` scope with an active generation). The runtime delta-trigger
  (issue #5593) is `drift_runtime_trigger.go`
  (`ConfigStateDriftRuntimeTrigger.TriggerConfigStateDrift`), wired only onto
  the ingester's `ProjectorQueue.ConfigStateDriftTrigger` in
  `cmd/ingester/wiring.go` and fired from
  `projector_queue_config_state_drift_trigger_hook.go`'s
  `runConfigStateDriftTriggerHook` after `Ack` commits a `state_snapshot:*`
  scope generation. Both producers build the reducer intent with the same
  `(scope_id, generation_id, domain=config_state_drift)` shape and no
  `EntityKey`, so `reducerWorkItemID` collapses them onto the same
  `work_item_id` and the second producer's enqueue is a harmless
  `ON CONFLICT DO NOTHING` no-op — do not add an `EntityKey` or other
  differentiator to either producer without re-proving that de-dupe (see
  `TestConfigStateDriftRuntimeTriggerAndBootstrapProduceSameConflictKey`).
  Do NOT wire `ConfigStateDriftTrigger` onto bootstrap-index's own
  `ProjectorQueue` (`cmd/bootstrap-index/wiring.go`) — it would evaluate
  drift before bootstrap's finite corpus has necessarily finished activating
  every repo; see `runConfigStateDriftTriggerHook`'s doc comment for the
  full reasoning.

  **No redrive/retry — this was tried and removed (issue #5593).** A
  bounded ledger-backed redrive for the `tfstatebackend.ErrNoConfigRepoOwnsBackend`
  rejection went through three review rounds: unconditional scheduling on
  every activation (wrong — re-evaluated generations that never raced);
  narrowed to schedule only on the observed rejection, from inside
  `Handle()` (still wrong — `Handle()` re-runs on every redrive replay, and
  the ledger row was deleted on exhaustion, so `EnsureScheduled`'s
  `ON CONFLICT DO NOTHING` had nothing to conflict with and inserted a
  FRESH row every cycle: an unbounded ~20-minute retry loop for every
  operator-owned backend, which the resolver's own doc comment names as the
  dominant real-world cause of this rejection). Removed rather than adding a
  fourth guard (a permanent tombstone table), because that table would need
  its own unbounded-growth proof — the same class of bug as the ledger it
  would replace, just relocated. See `ConfigStateDriftRuntimeTrigger`'s doc
  comment in `drift_runtime_trigger.go` for the full history. The race this
  would have covered self-heals on the next real `terraform apply` (a new
  generation, evaluated independently); a state that never changes again
  after racing once is the accepted residual gap. Issue #5593's acceptance
  is an OR of two criteria; this trigger satisfies criterion 1 alone
  ("a snapshot results in a drift evaluation"). Criterion 2 ("the read
  model reports an explicit not-yet-evaluated state") is deliberately OUT
  OF SCOPE here and tracked in sibling branch
  `5594-local-backend-default-path` — do not try to satisfy it by re-adding
  a redrive or a terminal/non-terminal distinction on this rejection.

- **State-attribute decoding or flattening** → edit
  `tfstate_drift_evidence_state_row.go`. `stateRowFromCollectorPayload`
  (`tfstate_drift_evidence_state_row.go:29`) decodes the collector payload and
  calls `flattenStateAttributes` (same file, line 90) to produce the flat
  dot-path `map[string]string`. The dot-path encoding MUST stay byte-identical
  to `ctyValueToDriftString` in
  `go/internal/parser/hcl/terraform_resource_attributes.go`; the classifier's
  value-equality check in `go/internal/correlation/drift/tfconfigstate/classify.go`
  fires across both sides. Add a cross-package regression test when changing any
  encoding rule. Singleton repeated blocks arrive from the state collector as
  `[]any` of length 1 whose first element is `map[string]any`;
  `flattenStateAttributes` unwraps the outer array and recurses into the object
  so paths like `versioning.enabled` align with the parser-emitted form.

- **Parser-entry bridging (config side)** → edit
  `tfstate_drift_evidence_config_row.go`. `configRowFromParserEntry`
  maps one `terraform_resources` JSON entry from the HCL parser into a
  `tfconfigstate.ResourceRow`. The `attributes` field is already flat
  dot-path from the parser; this function copies it to
  `ResourceRow.Attributes`. `unknown_attributes` is a JSON array of dot-path
  strings; it becomes `ResourceRow.UnknownAttributes` so
  `classifyAttributeDrift` can skip non-literal expressions. The function
  takes a `modulePrefix string` parameter (issue #169) — empty for
  root-module resources, `module.<name>[.module.<name>...]` for resources
  inside a module {} block. The helper stays strictly 1:1; the 1→N
  projection (one callee resource referenced by multiple module {} blocks)
  lives in the loader's emission loop, not here, so future readers cannot
  mistake the row builder for the projection seam.
  Note: `loadPriorConfigAddresses` (see below) also calls
  `configRowFromParserEntry` (through `collectPriorConfigAddresses`) with a
  generation-appropriate prefix map. Current config uses the current map;
  prior config uses a prior-generation map so module renames do not silently
  regress `removed_from_config` on module-nested addresses.

- **Module-aware drift joining** → edit
  `tfstate_drift_evidence_module_prefix.go`. `buildModulePrefixMap` walks
  `terraform_modules` parser facts (`listModuleCallsForCommitQuery`) and
  produces a callee-directory to prefix-string slice. The loader applies
  the map in two places: `emitConfigRowsForEntry` (current generation) and
  `collectPriorConfigAddresses` (prior generations). Local-source modules
  resolve to a callee directory with `path.Clean(path.Join(callerDir,
  source))`; registry, git, archive, and cross-repo sources fall back to
  `added_in_state` and increment
  `eshu_dp_drift_unresolved_module_calls_total{reason}` through
  `loggingUnresolvedRecorder` (a thin wrapper over `*telemetry.Instruments`).
  Prior-config module rename detection records `reason="module_renamed"` on
  the same counter once per prior generation and callee path when the prior
  and current prefix sets differ.
  Depth bound is `maxModulePrefixDepth = 10`, hard-coded with no env
  override — see the constant's doc comment. Cycles are broken by the
  per-expansion `visited` set tracked in `walkModulePrefixChain`.

- **Per-address module-resolution confidence (issue #5572)** → edit
  `tfstate_drift_evidence_module_confidence.go`. Companion to
  `buildModulePrefixMap`: tracks a `moduleResolutionConfidenceMap`
  (directory → unresolved reason) alongside the real `modulePrefixMap`,
  populated for exactly two cases — `classifyModuleSource`'s
  `external_registry` misclassification (speculatively re-resolved via
  `resolveLocalCallee` purely to learn the candidate directory) and
  `walkModulePrefixChain`'s `depth_exceeded` abandonment. `emitConfigRowsForEntry`
  consults it through `moduleResolutionReasonForEntry`, comparing MATCH
  DEPTH against `modulePrefixMap`'s own match (`modulePrefixForPathWithDepth`)
  rather than trusting "zero prefixes" — `modulePrefixForPath`'s
  longest-ancestor-match walk means an unresolved call is not reliably
  visible as an empty result; see the file's package doc comment for why. A
  non-empty `ResourceRow.ModuleResolutionReason` flows through
  `tfconfigstate.BuildCandidates`'s `EvidenceTypeModuleResolutionConfidence`
  atom to the reducer writer, which downgrades the finding's `Outcome` from
  `"exact"` to `"derived"`.

- **Prior-config walk for `removed_from_config`** → edit
  `tfstate_drift_evidence_prior_config.go`. `loadPriorConfigAddresses`
  queries prior repo-snapshot generations bounded by
  `PostgresDriftEvidenceLoader.PriorConfigDepth` (default 10, set from
  `ESHU_DRIFT_PRIOR_CONFIG_DEPTH`). It returns the address set that
  `mergeDriftRows` uses to set `PreviouslyDeclaredInConfig` on state-only
  addresses. The walk groups rows by prior generation, builds a module-prefix
  map from each prior generation's `terraform_modules` facts, and then
  projects prior `terraform_resources` entries with that generation's module
  names. Current-generation and prior-generation prefix differences emit the
  `module_renamed` telemetry reason so operators can size rename frequency.
  When changing depth semantics, also update the `defaultPriorConfigDepth`
  constant in the same file and the `parsePriorConfigDepth` helper in
  `go/cmd/reducer/config.go`.

- **Add a new queue domain to ReducerQueue** → add the domain constant in
  `internal/reducer`; extend the `domain = $2` filter handling in
  `ReducerQueue.Claim`; add tests for claim, ack, and retry paths.

- **Add a new enqueue-only call site for `ReducerQueue`** → construct the
  struct directly (`ReducerQueue{db: s.db}`) without `LeaseOwner` or
  `LeaseDuration`. Both fields remain NULL on insert per
  `enqueueReducerBatchPrefix`; the enqueue SQL never reads them. The
  internal `validateEnqueue` runs without lease fields and `validateClaim`
  adds the lease-owner fence used by `Claim`, `Heartbeat`, `Ack`, and
  `Fail`. Both delegate the shared db-nil and `ClaimDomain.Validate()`
  checks to `validateShared`, which stamps the calling side onto the
  error so wrapped failures self-locate to enqueue vs claim. Do not
  invent a parallel ReducerQueueEnqueuer port —
  `projector.ReducerIntentWriter` already provides the narrower
  consumer-side interface (`internal/projector/runtime/projection.go`).

- **Add a new fact kind or column** → update `upsertFactBatch` column list and
  `columnsPerFactRow`; update `scanFactEnvelope`; update the schema DDL; add a
  migration if the column is non-nullable without a default.

- **Add a new graph projection phase** → add the constant in
  `internal/reducer/gpphase` (`phasekey.go`), NOT the root alias layer, else leaf
  families cannot use it; batch-upsert via `GraphProjectionPhaseStateStore`.

- **Add Postgres telemetry** → wrap the `db.ExecQueryer` with `InstrumentedDB`;
  set `StoreName` to a short descriptive label; the metric
  `eshu_dp_postgres_query_duration_seconds{store=...,operation=...}` is emitted
  automatically.

## Failure modes and how to debug

- Symptom: claim latency high (`eshu_dp_postgres_query_duration_seconds{store="queue"}`)
  → check index coverage on `fact_work_items(stage, status, visible_at,
  claim_until)` and `FOR UPDATE SKIP LOCKED` contention.

- Symptom: multiple `projector` rows are `running` for the same `scope_id` →
  check the oldest-ready-row guard in `ProjectorQueue.Claim`. Same-scope
  duplicate running rows can fence pending generations and make local progress
  look stalled even when processes are alive. If overdue claims stay visible
  while pending rows continue to move, check that expired-lease priority still
  precedes `updated_at` ordering and that stale duplicate reclaim still demotes
  expired siblings to `retrying`.

- Symptom: `ErrProjectorClaimRejected` or `ErrReducerClaimRejected` in logs →
  lease expired before ack; increase `LeaseDuration` or reduce projection time;
  check `eshu_dp_projector_stage_duration_seconds` for slow phases.

- Symptom: `dead_letter` items accumulating → check `failure_class` in
  `fact_work_items`; replay via `RecoveryStore` after root-cause investigation.

- Activation wake (#7597) → every statement that sets a
  `relationship_generations` row active runs through `activateWithConsumerWake`
  (`TestEveryRelationshipActivationWakesConsumers` enumerates the module): one
  transaction of the advisory exclusive lock on the scope (#11256), activation,
  `SET LOCAL lock_timeout`, a plain `FOR UPDATE` on the in-flight own-gate
  consumers, then the wake UPDATE (dead letters locked `SKIP LOCKED`). Lock
  order: advisory X(scope) (`activationRendezvousKey`, before any row lock, so a
  parking wait can order itself against this transaction; a parker holds the
  shared lock for one statement and waits on one key at a time), claimant work
  row, relationship generation,
  in-flight consumers by `work_item_id COLLATE "C"` (the `AckBatch` order; `TestOwnGateLocksShareTheBatchAckOrder`), dead-lettered consumers. Keep both the
  in-flight lock and `wakeAfterProducerTerminalDeadLetter` in the Fail path:
  dropping either produced lost wakes in `TestActivationWakeInterleaveLive`
  (go/internal/query/admin). Keep the wake SET list in lockstep with the replay
  reset list (`TestActivationWakeSetClauseMatchesReplayReset`). Do not enroll
  the own-gate counting classes as non-counting
  (`TestDeliberatelyCountingClassesAreNeverEnrolled`).

- Replay and claim-sweep row locks (#8205): keep the sort and status checks, and keep the ordered lock steps blocking (never `SKIP LOCKED` there; the bounded drain's `SKIP LOCKED` is intentional); see README.

- Symptom: `graph_projection_phase_state` rows missing for a scope generation →
  projector `publish_phases` stage failed; check `GraphProjectionPhaseRepairQueueStore`
  depth; check projector structured logs for `stage=canonical_write` error fields.

- Symptom: `SQLSTATE 22P05` or `SQLSTATE 22P02` on fact INSERT → non-UTF-8 or
  binary payload; `sanitizeJSONB` should handle this; check whether the repo
  emits raw binary in fact payloads.

- Symptom: `SQLSTATE 21000` on fact INSERT → duplicate `fact_id` in a batch;
  check `deduplicateEnvelopes` is being called; should not happen in normal
  operation.

## Anti-patterns

- **Do not use `path/filepath` in the drift evidence module helpers.** The
  `tfstate_drift_evidence_module_prefix.go` helper deals with
  Postgres-stored forward-slash strings (the parser's `path` field), not
  live filesystem paths. `path/filepath.Clean` uses OS-specific separators
  (`\` on Windows) and would silently mis-bucket callee directories on
  Windows builds while passing every macOS/Linux test. Use `path.Clean`,
  `path.Dir`, `path.Join` from the standard `path` package. The
  `TestBuildModulePrefixMapForwardSlashSemanticsRegression` test locks the
  contract in.
- **Do not bypass `deduplicateEnvelopes`** when calling `upsertFactBatch`
  directly. Duplicate `fact_id` values in a single multi-row INSERT trigger
  `SQLSTATE 21000`.
- **Do not use raw SQL string building** when adding new stores. Use parameterized
  queries (`$1`, `$2`, ...) exclusively to prevent injection.
- **Do not hold long transactions** across graph writes. The projector ack
  transaction is bounded to six SQL statements; do not add graph or network
  calls inside it.
- **Do not add `if backend == "nornicdb"` branches** here. Backend-specific
  queue gate logic is isolated to `ReducerQueue.Claim`'s parameterized gate
  (`reducer_queue.go`). New backend gates must go in the same parameterized
  pattern.
- **Do not skip `WorkflowControlStore` lease fencing**. Always check the
  returned error from claim mutations; silently ignoring `ErrWorkflowClaimRejected`
  causes split-brain workflow state. On the claim-plane path (`IngestionStore.ClaimPlane`, #9376) the fence is `claim/fence`: never fall back from a failed pin to `HeartbeatClaim(tx)`, keep the fence row write after the generation upsert and under the scope row lock, and never use SQL `now()` for the deadline check.
- **Do not raise `contentWriterBatchConcurrencyAutoCap` or
  `contentWriterBatchConcurrencyCap` without re-running the pool-budget
  math** (`content_writer_batch.go`). Peak Postgres connection demand is
  `ESHU_PROJECTOR_WORKERS * batch_concurrency`; the auto cap is set
  against the 30-conn default pool from `internal/runtime/data_stores.go`.
  Raising one without raising the other can starve collector, status, and
  heartbeat paths.
- **Do not re-introduce per-call `os.Getenv` reads in `ContentWriter`.**
  The env override is resolved once in `NewContentWriter`; per-call reads
  let a long-running ingester pick up live env changes the operator never
  expected to be hot-reloaded.
- **Do not assert positional `db.execs[i]` order on the entity-batch
  path.** Entity batches fan out through `runConcurrentBatches` and
  arrive in non-deterministic order. Assert on the sorted multiset of
  batch sizes or on the per-batch query shape instead.
- **Do not diverge the dot-path encoding between `coerceJSONString` /
  `flattenStateAttributes` (`tfstate_drift_evidence_state_row.go:29`) and
  `ctyValueToDriftString`
  (`go/internal/parser/hcl/terraform_resource_attributes.go`).** The
  classifier's value-equality check in
  `go/internal/correlation/drift/tfconfigstate/classify.go` compares strings
  produced by both sides; silent divergence causes false-positive or
  false-negative `attribute_drift` detection without test failures in either
  package alone. Add a cross-package test before changing any encoding rule.
  The same address-space contract governs `loadPriorConfigAddresses`: it
  calls `configRowFromParserEntry` on prior-generation facts, so the address
  strings it produces must match what the current-config path produces. A
  divergence silently misclassifies resources as `added_in_state` instead of
  `removed_from_config`.

  Multi-element repeated nested blocks (`len(typed) > 1` with a map first
  element) are truncated to their first element on both sides and emit a
  debug log so the dropped signal is observable. State side fires at
  `flattenStateAttributes` (`tfstate_drift_evidence_state_row.go:90`) with
  `multi_element.source="state_flatten"` and a `multi_element.count`
  attr. Parser side fires at `walkBlockAttributes`
  (`go/internal/parser/hcl/terraform_resource_attributes.go:132`) with
  `multi_element.source="parser_walk"` and no count (recursion sees
  duplicates one-at-a-time). Keep both sides in lockstep when changing
  the truncation policy.

## What NOT to change without an ADR

- `fact_work_items` table schema (columns, indexes, conflict keys) — the projector
  and reducer queue claim queries are tightly coupled to this schema; changes
  require coordinated migration and claim query updates.
- `graph_projection_phase_state` schema and phase semantics — reducer edge
  domains gate on specific phase values; changing phase names or semantics
  breaks the readiness contract across `internal/reducer`.
- `ReducerQueue.Claim` NornicDB semantic gate — its presence and activation
  condition are evidence-backed; see
  `docs/public/reference/backend-conformance.md`.
- `BootstrapDefinitions` ordering — FK constraints enforce ordering; reordering
  without verifying all FK dependencies will break bootstrap in fresh
  deployments.

## #4893 — projected-edge / projected-node ledgers

`taintstore.CodeInterprocProjectedEdgeStore` (`code_interproc_projected_edge`,
migration 044) and `taintstore.CodeTaintEvidenceProjectedNodeStore`
(`code_taint_evidence_projected_node`, migration 045), in
`internal/storage/postgres/code/taint`, record the source-Function uid /
CodeTaintEvidence node uid of every projected value-flow artifact so the
reducer retracts by indexed uid instead of scanning the whole graph on
NornicDB. Both follow the store conventions here: `ExecQueryer`, idempotent
batched upsert with in-batch dedupe, `SchemaSQL` const mirrored by the
migration, and `SELECT DISTINCT ... ORDER BY` enumeration / bounded `DELETE`
prune, all parameterized. Written before the graph write (superset
invariant); see `go/internal/reducer/AGENTS.md` (#4893).

## #4861 — Drop redundant graph_projection_phase_state_lookup_idx

| metric | BEFORE (PK+lookup+updated) | AFTER (PK+updated) | delta |
|---|---|---|---|
| secondary-index count on table | 3 | 2 | −1 (33% fewer) |
| 50k-row upsert wall-time | 789 ms | 652 ms | −137 ms (−17.4%) |
| B-tree index writes per INSERT | 3 | 2 | −1 (one fewer index to maintain) |

Live Postgres proof method: `postgres:18-alpine` on Darwin arm64,
`generate_series(1, 50000)` with `ON CONFLICT ... DO UPDATE SET updated_at`,
`\timing on`, measured BEFORE (PK + lookup_idx + updated_idx) vs AFTER
(PK + updated_idx only). Output equivalence: row count identical (50k)
before and after trunate + re-insert. EXPLAIN proof: the PK index serves
the identical lookup query; plan shape and `Index Cond` are byte-identical
except the index name changes from `graph_projection_phase_state_lookup_idx`
to `graph_projection_phase_state_pkey` — same `Index Only Scan`, same
`cost=0.41..8.44`, same `Execution Time ~0.03ms`, output-preserving (0/0 diff).

## #4860 — Batch ContentWriter tombstone DELETEs

`ContentWriter.Write` accumulated tombstoned `(repo_id, relative_path)` file
keys and `(repo_id, entity_id)` entity keys during its record/entity loops and
now issues one batched `DELETE ... WHERE (col, col) IN (...)` per target table
(chunked at `contentFileBatchSize`), replacing the per-row `DELETE` round trips.
The delete order (content_entities → content_file_references → content_files)
and the exact key columns are byte-identical to the prior per-row queries
(`deleteContentEntityQuery`/`deleteContentFileQuery` key on `relative_path`;
`deleteContentEntityByIDQuery` keys on `entity_id`), so the same rows are
deleted; only the round-trip count changes. Entity `StartLine` validation is
unchanged (still enforced for every entity before the deleted-branch, matching
origin/main). The parallel `ContentStore` per-row delete path
(`content_store_writes.go`) is a separate type and out of scope.

| scenario | BEFORE (per-row) | AFTER (batched) | reduction |
| --- | --- | --- | --- |
| 100 deleted files (entities+refs+files) | 300 DELETE execs | 3 DELETE execs | 99% |
| 5 deleted entities (by entity_id) | 5 DELETE execs | 1 DELETE exec | 80% |
| combined (100 files + 5 entities) | 305 DELETE execs | 4 DELETE execs | 98.7% |
| 1 deleted file | 3 execs | 3 execs (1-row IN batch) | same execs, batched shape |

Output-equivalence: the batched `IN (...)` key set equals the per-row key set
(same `(repo_id, relative_path)` / `(repo_id, entity_id)` tuples), asserted by
the batched-query value-group count in the test; `result.DeletedCount` is
incremented identically. `DELETE ... IN` is idempotent and order-independent.

Live Postgres proof (prove-theory-first, addresses the concern that a fake exec
count does not prove index behavior): `postgres:18-alpine`, 100,000 rows per
table across 5 repos, batch size 500, `EXPLAIN (ANALYZE, BUFFERS)` on the exact
explicit row-constructor `WHERE (col, col) IN ((v1,v2),...)` shape the Go code
emits (500 tuples), all in rolled-back transactions. Every batched delete is
index-backed — NO sequential scan, buffers hit-only:

| batched DELETE (500 tuples) | plan node | exec time |
| --- | --- | --- |
| content_files by (repo_id, relative_path) | Index Scan `content_files_pkey` | 1.16 ms |
| content_entities by (repo_id, relative_path) | Index Scan `content_entities_path_idx` | 1.30 ms |
| content_entities by (repo_id, entity_id) | Index Scan `content_entities_pkey` | 0.87 ms |
| content_file_references by (repo_id, relative_path) | Index Scan `content_file_references_repo_path_idx` | 1.15 ms |

Server-side wall-time for the same 500 content_files deletes (clock_timestamp
around a rolled-back tx): OLD 500 per-row `DELETE` = **3.501 ms** vs NEW one
batched `IN` = **2.108 ms** (~1.66x faster server-side), before counting the
499 eliminated client↔server round trips (the dominant win over a network,
quantified by the exec-count table above). Postgres rewrites the same-repo
row-list to `repo_id = $ AND relative_path = ANY(array)` and still index-scans;
with mixed repos the row-wise `IN` remains index-backed via the same keys.

## #4859 — Drop unused fact_records_stable_key_idx

No query filters `WHERE stable_fact_key = ...` (repo-wide `rg`: every
`stable_fact_key` reference is an `EXCLUDED.stable_fact_key` upsert SET or a
cypher graph-node property write — never a `fact_records` read/lookup). The
fact upsert conflicts on `fact_id` (PK), not `stable_fact_key`, so the index
is not needed for write dedup.

Its only plausible reader, the changed-since diff (`changed_since_sql.go`),
filters `WHERE scope_id=$ AND generation_id=$` and hash-joins CTEs by
`stable_fact_key` — it uses `fact_records_scope_generation_idx`, NOT
`stable_key_idx`. Proven by EXPLAIN both locally (all 82 indexes present →
Bitmap Index Scan on `scope_generation_idx` + Hash Full Join) and on the live
6.2M-row stack (Bitmap Index Scan on `fact_records_scope_generation_idx`).
Structurally the index can't serve that query (it leads with `stable_fact_key`,
which is never in a WHERE).

Local write-amp test (`postgres:18-alpine`, 100k representative facts on the
real 82-index schema):

| metric | BEFORE (with stable_key_idx) | AFTER (dropped) | delta |
| --- | --- | --- | --- |
| INSERT 100k facts (real 82-index schema) | 3116.8 ms | 2035.8 ms | −34.7% |
| index size reclaimed (live stack) | 2132 MB | 0 | reclaimed |
| changed-since query plan | scope_generation_idx + hash join (NOT stable_key_idx) | identical | output-preserving |

Dropping this ONE index captures 99% of the achievable insert win from all
idx_scan=0 indexes (dropping ALL 77: 2024.3 ms; −35.0%).

## #4862 — Keep content pg_trgm GIN indexes (drop disproven)

A proposal to drop the two `pg_trgm` GIN indexes on `content_files.content`
and `content_entities.source_cache` was audited against a live 838-repo Postgres
stack and disproven. Both indexes are actively used by the all-repo / code-topic
search read path: `InvestigateCodeTopic` (`content_reader_code_topic.go`) and
the `SearchFileContentAnyRepo` / `SearchEntityContentAnyRepo` content readers.
Repository-scoped probes can also use content GIN indexes; the measured #7246
file probe intersects repository and content indexes. All-repository reads are not their only consumers.

## #4686 — Typed decode for azure_tag_observation / gcp_tag_observation in cloud_tag_evidence.go

`PostgresCloudTagEvidenceLoader.cloudTagEvidenceRecordFromRow` (the shared
admission-side loader for the multi-cloud tag-evidence domain, #2192/#2334)
used to decode `azure_tag_observation`/`gcp_tag_observation` payloads into a raw
`map[string]any` and read `tag_value_fingerprints` through `coerceJSONString`,
which formats ANY JSON value (a number, bool, nested object) into a string
instead of rejecting it — a malformed or renamed-shape fingerprint value was
silently coerced and attached as tag evidence rather than dropped. The loader
now decodes both kinds through the `sdk/go/factschema` typed seam
(`factschema.DecodeAzureTagObservation`/`DecodeGCPTagObservation`, wrapped by
`decodeAzureTagObservation`/`decodeGCPTagObservation` in
`factschema_decode_cloud_tag_evidence.go`), so a non-string fingerprint value or
a missing required field (`arm_resource_id`, `normalized_resource_id`,
`resource_type`, `full_resource_name`, `asset_type`, `tag_value_fingerprints`)
now fails decode and the row is dropped and logged like any other undecodable
fact, matching the loader's existing visible-failure contract instead of
silently attaching wrong evidence. `aws_tag_observation` is intentionally left
untyped: no reducer/query/mcp consumer reads its payload fields raw (the two
touch points that reference `cloud.AWSTagObservationFactKind` — the AWS runtime
collector's emission counter and scan-status summarizer — only switch on the
fact kind to count occurrences), so a decode struct for it would be a hollow,
never-validated contract.

## #5438 — identity-fact load epoch cache

### Concurrency design

The identity epoch cache (`identityEpochCache`) holds the full set of active
container-image identity facts, reloaded under singleflight on epoch mismatch.

- **Shared state**: `epoch`, `facts`, `loading` (`*identityFlight`), all under
  `mu`, NEVER held across a DB load or an epoch probe.
- **Shared flight (#7805)**: a caller whose probe equals a running flight's
  `startEpoch` gets that flight's rows; one whose probe differs waits, retries.
  If the post-load probe differs the flight loads once more (max 2 attempts); a
  flight still moving, or whose post-load probe failed, is "torn": waiters
  re-probe, the leader's item fails with a retryable `identityLoadUnstableError`
  (class `identity_epoch_unstable`), nothing is decided on it. Cached only when
  the post-load probe equals `startEpoch` and the set fits the cap. A waiter
  gives up after 3 unserved flights or one heartbeat interval (30 s,
  `WithHeartbeatInterval`, from the claim lease in `cmd/reducer`) after ONE
  final probe that serves a filled cache (a budget-spent caller at the lead decision relies on the same pass's top probe). Outcomes `gave_up_flights` (churn),
  `gave_up_wall` (slow flight). A call's time in the cache is bounded by one
  heartbeat plus two load attempts and a few probes; a call with its budget spent never
  leads: it fails retryably and the next caller leads (#7805).
- **TOCTOU analysis**: the probe→serve window (between epoch probe and cache
  hit) is a bounded probe-interval gap. Today's baseline does a full paginated
  O(corpus) scan inside every call, mixing facts from different commit
  snapshots across pages. The cache serves a read-committed snapshot of ONE
  coherent load with post-load validation. A mid-load commit is detected by
  the post-load probe and the rows are served uncached — no worse than today's
  mixed-epoch pagination, and the next call retries the cache. The cache is
  strictly more consistent per serving window than the unbounded pagination it
  replaces.
- **Staleness bound**: one probe window (~120-135 ms on the 1.0M-identity-fact
  shim, #7805). A caller that probes a moved epoch never joins an older flight.
- **Epoch semantics (3-tuple)**: `(active_fact_count, active_fact_max_observed,
  active_fingerprint)` (#7805):
  * count + max cover identity facts of each scope's ACTIVE generation only
    (the page query's set, same hashed-SubPlan filter with `OR FALSE`), so
    retention deletes of superseded rows and pending-generation writes do not
    move the epoch; an insert, delete, or tombstone on an active generation
    does. The probe reads the heap (the partial index holds only
    `(observed_at, fact_id)`): ~120-135 ms on the 1.0M-fact shim, not an Index
    Only Scan. The all-generations / ~51 ms description is superseded.
  * `active_fingerprint` — SHA-256 hex digest of the DISTINCT active
    `(scope_id, generation_id)` pairs in the filtered identity set
    (`string_agg(DISTINCT fact.scope_id::text || ':' || fact.generation_id::text,
    '|' ORDER BY ...)` over the same active-generation filter count/max use).
    This avoids MD5, unavailable on FIPS-configured PostgreSQL, and detects an
    identity-bearing supersession (active_generation_id flip) that leaves count
    and max observed_at unchanged. A flip in a scope with no identity facts
    leaves the pair set unchanged and does not move the epoch (#7825). An empty
    identity set digests the empty string, so the digest is always 64 hex.
  * **Collision resistance**: with only (count, max), a supersession to a new
    generation of equal cardinality would false-hit. The fingerprint hashes the
    full ordered pair set (ordered, joined with `'|'`) with SHA-256, so any
    change to the covered pair set changes the digest input. The earlier
    `sum(hashtext(...))` fingerprint could miss a 32-bit collision or
    offsetting per-scope deltas; a digest of the ordered set cannot.
- **Cap behavior**: `ESHU_IDENTITY_CACHE_MAX_BYTES` (default 500 MiB, measured).
  Loaded set exceeding the cap is served DIRECTLY (passthrough, never partial,
  never cached) and increments `eshu_dp_identity_cache_passthrough_total`.
  Set to 0 or unset for the default; negative disables the cache entirely.
- **Wiring topology**: the cache is wired default-ON at the reducer
  (`go/cmd/reducer/main.go`) via `NewIdentityEpochCache`. The cache serves the
  3 identity-load call sites (container_image_identity + kubernetes_correlation
  handlers). Bootstrap-index (`go/cmd/bootstrap-index/wiring.go`) uses its
  `factStore` only for projector-level `LoadFacts` (scope-generation-scoped
  loads), never the cross-scope identity loader — left uncached.
  Other constructors (ingester, workflow-coordinator, eshu docs) are also
  uncached; they do not call the identity loader.
- **Retry/idempotency**: loads are pure reads (no mutating side effects).
  Serving an older epoch for one call is equivalent to today's racing reader
  getting rows from different snapshot points.
- **Defensive copy**: every cache hit returns a deep copy of the Payload map
  (`defensiveCopyEnvelopes`). Callers cannot mutate the shared cache.
  Note: the copy is one-level — nested map values inside Payload (e.g.
  `payload["entity_metadata"]`) share the underlying `map[string]any`
  reference. All identity-load callers (container_image_identity,
  kubernetes_correlation handlers) were audited: they read the Payload
  immutably and do not mutate nested maps. A follow-up can add deep-clone
  for nested payloads if a future caller needs mutation safety.

- **Cap sizing overhead**: `estimateEnvelopesByteSize` counts string field
  lengths + JSON-serialized payload + 40 bytes fixed overhead. It omits
  Go `map[string]any` interface-boxing overhead (~16 bytes per key/value
  entry) and `interface{}` wrapping for nested values. The 2.2× headroom
  (500 MiB cap vs ~226 MB measured Postgres bytes) absorbs this gap.
  A follow-up heap-profile pass can tighten the estimate if needed.

### Evidence notes

Shim measurements (postgres:18-alpine, 2.5M rows, 500k identity facts,
2000 scopes):

| metric | OLD (per call) | NEW (per call) | total per drain |
| --- | --- | --- | --- |
| load (paginated, with JOIN) | 1,476 ms | 1,476 ms | — |
| probe (index-only, no JOIN) | — | 50 ms | — |
| per-drain: 2,000 calls | 2,000 × 1,476 ms = 2,952 s | 1 load + 2,000 probes = 1.5 s + 100 s = 101.5 s | 29× faster |
| per-drain: epoch stable | 2,952 s | 1 load + 1,999 cache hits + 2,000 probes = 1.5 s + 100 s = 101.5 s | 29× faster |

The epoch probe is unconditional: it runs on every `get()` call, including
cache hits, so 2,000 probes set a ~100 s floor per drain regardless of hit
rate. The ~2,000× reduction applies only to DB *load* work (2,952 s → 1.5 s
across 2,000 calls); end-to-end per-drain wall time, including probes, is
29× faster (2,952 s → 101.5 s).

Probe EXPLAIN: `Index Only Scan using fact_records_identity_epoch_idx`
(pre-#6543 name; 50 ms, 0 heap fetches, 3,016 buffers). Index size 24 MB for
500k matching rows. Write tax: 0% for non-identity facts (partial index
excludes them); ~1 index entry per identity fact insert.
Byte-identical row-set: cached load (JOIN-filtered) == direct load for
the stable shim setup (all 2000 scopes active, 500k facts match both
the JOIN and no-JOIN probes).

### Cap sizing evidence (500 MiB default)

Payload size measured per arm on the 2.5M-row shim (500k identity rows):

| arm | count | avg bytes | total MB | p95 bytes |
| --- | --- | --- | --- | --- |
| oci_registry | 300,000 | 278 | 83.5 | 342 |
| aws_image_reference | 50,000 | 328 | 16.4 | 329 |
| azure_image_reference | 50,000 | 480 | 24.0 | 485 |
| gcp_image_reference | 50,000 | 313 | 15.7 | 314 |
| aws_relationship | 25,000 | 378 | 9.4 | 379 |
| content_entity | 25,000 | 189 | 4.7 | 192 |
| **total** | **500,000** | **308** | **153.7** | — |

Total Postgres on-disk row bytes (all columns): 226.5 MB (453 bytes/row avg).
Estimated in-process Go struct bytes (string fields + JSON payload + 40-byte
fixed overhead): ~342 MB. Default cap = 500 MiB = 2.2× in-process estimate,
keeping the 500k-fact shim comfortably cached while a pathological set above
this bound passes through observably via the passthrough counter.
