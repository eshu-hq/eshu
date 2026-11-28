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
- **The content_files upsert is guarded (#8915, #8950, #8942)** — both file
  upserts share `upsertContentFileConflictClause`, whose `WHERE ... IS DISTINCT
  FROM` skips rows differing only in `indexed_at`. Keep the SET list, guard
  tuples, and insert columns in lockstep (`TestUpsertContentFileConflictClauseLockstep`),
  keep comparisons NULL-safe, and do not read `RowsAffected` as "files written".
  The two-statement writer pre-reads batches without locks or bodies
  (`contentFileOutcomePreReadSQL`) to route `metadata_only` rows through a
  HOT-eligible metadata-only UPDATE with bounded `40P01` retry and records
  `eshu_dp_content_file_upserts_total` (`batch_class` label required; Weyl-sampled
  for small batches). Keep `classifyContentFileRow` aligned with every guard
  column (`TestClassifierComparesEveryGuardColumn`).
- **Fact-record cross-batch fencing, unchanged-row skip, and owner guard
  (#4444, #10873, #11203)** — `upsertFactBatchSuffix` keeps
  `fact_records.fencing_token <= EXCLUDED.fencing_token` first, followed by
  `factOwnerGuardClause` (`scope_id = EXCLUDED.scope_id AND generation_id =
  EXCLUDED.generation_id`), then `IS DISTINCT FROM` across SET columns except
  `observed_at`/`ingested_at` (payload last). Keep SET, guard, and INSERT lists
  in lockstep (`TestUpsertFactBatchGuardLockstep`). `selectUnchangedFactIDs`
  splits RETURNING-absent ids into unchanged (admitted to `afterBatch`), foreign
  owner (returns `*FactIDOwnerConflictError`, triggering
  `refuseFactIDOwnerConflict` rollback + `fact_id_owner_conflict` refusal), or
  fenced out (excluded). Do not wire `upsertFactBatch` into production
  (`TestUpsertFactBatchHasNoProductionCaller`), and do not widen the owner legs
  to `factwrite` or search-document writers (`reducer/factwrite` carries its own
  scope-only owner guard). `enqueueReducerBatch` probes inserted-row shortfalls
  with `refuseWorkItemCollisions` so non-injective `reducerWorkItemID` clashes
  fail closed with `*ReducerWorkItemCollisionError` rather than coalescing.
- **Pacing latch writes are transition-exact (#10882)** — `claimPacingLatchUpsert` (the depth, global, and reducer-source latch rows) writes only when the stored `engaged` bit differs from the desired one (`DO UPDATE ... WHERE latch.engaged IS DISTINCT FROM EXCLUDED.engaged`), and `writeClaimPacingLatch` (`pacing_latch_transition.go`) records telemetry only when `RowsAffected` is 1. A consult that raced a transition and lost changes nothing: no second `eshu_dp_workflow_claim_pacing_engaged_total`, `engaged_at` stays the first writer's, `updated_at` is the last real transition (nothing reads it as a heartbeat). The active gauge follows ownership: the process whose statement engaged the latch raises it, every process that holds a raise takes it back when a read that began after the raise shows the engagement over (`observeClaimPacingLatch`, called from every consult and `SourcePacingStates`), and a process that never raised it never lowers it. Never count a transition from a verdict difference or write the latch unconditionally; the race proofs are `pacing_latch_race_live_test.go` and `pacing_latch_interleave_live_test.go`; note: #10882 (retired evidence record).
- **The supports verification is sliced, never unbounded (#10880 decision 6)** — `supplyChainImpactSupportsBackfillVerifySliceSQL` (`supply_chain_impact_supports_verify.go`) checks one (scope_id, generation_id, fact_id) interval per step: up to `VerifySliceSize` supports and live findings, the interval ending at the smaller last key of a full side so slices tile the key space. Keep both halves' predicates the unsliced ones (missing = no support of the fact_id; orphan = support whose fact is tombstoned, probed only for supports with no live finding of their key), keep the cursor guarded by `verify_sweep = sweeps` (migration 348; an older binary's restart bumps sweeps without clearing it), restart the sweep on any miss, and keep the fence recheck in the completion statement. Completion is one transaction: `supplyChainImpactSupportsBackfillVerifyClearSQL` drops the cursor guarded by the sweep read, then the unchanged, contract-pinned completion statement runs (zero rows from either rolls back), so a marker reset without a sweep bump re-verifies from the start. Never replace it with a whole-table statement or a per-support `fact_records` probe (QA: 143 min projected). Proof: `supply_chain_impact_supports_verify_test.go`, `supply_chain_impact_supports_verify_live_test.go`; note: #10880 (retired evidence record).
- **Acceptance rows are advance-only (#6679)** — the upsert `WHERE` compares
  `EXCLUDED` with the row's own `(generation_ingested_at, generation_id)`; only a
  NULL (legacy) key may fall back to a lookup, which sees the statement snapshot
  (review F1). No bulk backfill (it stalls writers); see the SQL doc comment.
- **Freshness de-dupe and destructive-successor guard** —
  `CommitScopeGeneration` compares incoming `FreshnessHint` and commit against
  the newest same-scope `pending`/`active` generation (excluding `failed`) and
  rewrites only `pending` rows. `successor_guard.go` sizes full successors
  against collector-only prior rows and git deltas by file union against their
  `resolveDeltaBaseAtCommit` anchor, rechecking on the transaction after
  `upsertIngestionScope` locks the scope row. Generation outcomes mint once
  after `tx.Commit()`, skips funnel through `RecordSkippedRefresh`, and
  `SuccessorHoldTracker` stays a process-local leaf mutex.
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
- **Projector scope ordering** — `ProjectorQueue.Claim` enforces one active
  source-local generation per `scope_id`: keep the oldest-ready-row subquery
  with `FOR UPDATE SKIP LOCKED`, prioritize expired `claimed`/`running` leases
  ahead of fresh `pending` rows, and keep stale-duplicate reclaim and
  stale-generation coalescing CTEs intact. Every maintenance CTE in
  `projector_queue_claim_sql.go` must lock rows first with
  `FOR NO KEY UPDATE ... SKIP LOCKED` and repeat row-self predicates for
  EvalPlanQual recheck (never add a blocking multi-row UPDATE to Claim).
  Lock and bump `projector_scope_claim_fences` rather than `ingestion_scopes`
  (`claimProjectorWorkQuery`), keep `superseded` generations terminal across
  Claim/Heartbeat/Ack/replay, and preserve `failure_details.prior_failure` via
  `priorFailureStaleSQL`/`priorFailureWorkSQL` (`TestSupersedeStatementsFoldPriorFailure`,
  `TestReclaimStatementsFoldPriorFailure`). Keep the delta-baseline fence read
  as its own statement after the scope lock in Ack, keep the Heartbeat
  supersede marker gate on `locked_generation` (`SKIP LOCKED`), and keep the
  oldest-ready holds that spare marked writing generations and full generations
  inside the oldest-ready subquery (never an outer WHERE).
- **NornicDB semantic gate and reducer claim invariants** — `ReducerQueue.Claim`
  blocks `semantic_entity_materialization` while source-local projection is in
  flight when the NornicDB gate is enabled, and fences unleased older-generation
  rows read-only via `reducerClaimStaleGateCTEs` (never re-embed the supersede
  sweep in Claim; terminalization belongs in `SupersedeStaleReducerGenerations`).
  Every row of `WaitingDeferralMap` must declare `ProviderLocality`
  (`TestWaitingDeferralMapDeclaresProviderLocality`); keep `wakeProducerTerminalCrossScopeQuery`
  filtered by `failure_class = ANY($3)` and locked in `COLLATE "C"` order.
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
  not retry count** — a nil `AWSCloudRuntimeDriftHandler.ReadinessChecker`
  writes best-available classifications unconditionally. When wired, the defer
  is bounded by `awsCloudRuntimeDriftStatePendingMaxWait` against
  `Intent.CycleStartedAt` (`COALESCE(reopened_at, created_at)`, refreshed by
  `ReopenSucceeded`/`ReplayDomain`), never `Intent.AttemptCount` (which is
  frozen by `nonCountingReducerRetryFailureClasses`). Without an elapsed-time
  bound anchored on reopen time, genuine orphan resources would defer forever
  or lose their grace window on reopen.
- **Projected-edge / projected-node ledgers** — `taintstore.CodeInterprocProjectedEdgeStore`
  (`code_interproc_projected_edge`) and `taintstore.CodeTaintEvidenceProjectedNodeStore`
  (`code_taint_evidence_projected_node`) must be written before the graph write
  so each ledger stays a superset of graph artifacts for uid-anchored retracts.
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

  **No redrive/retry on `ErrNoConfigRepoOwnsBackend`.** Do not add a
  ledger-backed redrive loop for unowned backends (operator-owned backends are
  the common case and a redrive creates a perpetual retry loop); a raced
  snapshot self-heals on the next `terraform apply`. See
  `ConfigStateDriftRuntimeTrigger` in `drift_runtime_trigger.go`.

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

- **Module-aware drift joining and prior-config walk** → edit
  `tfstate_drift_evidence_module_prefix.go` (`buildModulePrefixMap`,
  `maxModulePrefixDepth = 10`, cycle-breaking `visited` set, unresolved module
  telemetry `eshu_dp_drift_unresolved_module_calls_total{reason}`),
  `tfstate_drift_evidence_module_confidence.go` (downgrades `external_registry`
  and `depth_exceeded` matches from `"exact"` to `"derived"`), and
  `tfstate_drift_evidence_prior_config.go` (`loadPriorConfigAddresses`, bounded
  by `ESHU_DRIFT_PRIOR_CONFIG_DEPTH` default 10, keeping `defaultPriorConfigDepth`
  and `go/cmd/reducer/config.go` in sync).

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
