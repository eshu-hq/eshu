# AGENTS.md — storage/postgres guidance for LLM assistants

## Read first

1. `go/internal/storage/postgres/README.md` — pipeline position, store
   inventory, queue lifecycle, and operational notes
2. `go/internal/storage/postgres/db.go` — `ExecQueryer`, `Transaction`,
   `Beginner`, `SQLDB`, `SQLTx`; understand the interface hierarchy before
   touching any store
3. `go/internal/storage/postgres/projector_queue.go` — `ProjectorQueue.Claim`
   and `Ack`; the five-step atomic ack transaction is the most sensitive path
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
8. `go/internal/storage/postgres/aws_pagination_checkpoint.go` — AWS
   checkpoint fencing and stale-generation expiry
9. `go/internal/storage/postgres/aws_scan_status.go` and
   `status_aws_cloud.go` — AWS scanner status persistence and admin status
   projection

## Invariants this package enforces

- **Idempotency** — all INSERT paths use `ON CONFLICT DO NOTHING` or
  `ON CONFLICT DO UPDATE`; schema DDL uses `IF NOT EXISTS`. Do not add
  non-idempotent INSERTs or CREATE TABLE without IF NOT EXISTS.
- **Fact deduplication before batching** — `upsertFacts` calls
  `deduplicateEnvelopes` before each batch to prevent `SQLSTATE 21000` on
  `ON CONFLICT DO UPDATE` when the same `fact_id` appears twice in one batch
  (`facts.go:206`).
- **Fact-record cross-batch fencing** — `upsertFactBatchSuffix` must keep the
  `WHERE fact_records.fencing_token <= EXCLUDED.fencing_token` conflict guard
  (issue #4444). Without it, a stale or out-of-order batch that commits after a
  newer batch silently overwrites the newer fact_id row (and resurrects its
  payload) purely on commit order; `deduplicateEnvelopes` only protects against
  duplicate `fact_id` values inside one batch, not across batches.
- **Freshness de-dupe covers in-flight generations** —
  `CommitScopeGeneration` must compare the incoming `FreshnessHint` with the
  newest same-scope `pending` or `active` generation. Restricting the check to
  `ingestion_scopes.active_generation_id` lets local polling recommit the same
  snapshot while projection is still in flight, which creates avoidable
  supersession churn. Do not include `failed` generations in this skip path; a
  failed first projection must remain retryable by a later snapshot.
- **JSONB sanitization** — `sanitizeJSONB` removes JSON-encoded U+0000
  characters and raw control bytes before every fact INSERT. It preserves
  literal source text such as the six characters `\u0000`. Skipping this causes
  Postgres errors on repositories with binary or non-UTF-8 content.
- **Ack atomicity** — `ProjectorQueue.Ack` wraps five SQL statements in a
  single transaction. If any step fails, the transaction rolls back. Always
  pass a `SQLDB` or `InstrumentedDB(SQLDB)` to `NewProjectorQueue`; a bare
  `ExecQueryer` without `Beginner` will fail.
- **Lease fencing** — `ProjectorQueue.Heartbeat` and `WorkflowControlStore`
  claims check `lease_owner` on UPDATE. A zero `RowsAffected` returns
  `ErrProjectorClaimRejected` or `ErrWorkflowClaimRejected`. Callers must stop
  processing on these errors and must not retry the ack.
- **Projector scope ordering** — `ProjectorQueue.Claim` must preserve one
  active source-local generation per `scope_id`. Keep the oldest-ready-row
  subquery with `FOR UPDATE SKIP LOCKED`; without it, parallel claimers can skip
  a locked older row and start a newer generation for the same repository.
  Expired `claimed` or `running` rows must stay ahead of ordinary pending rows
  in the claim ordering, or stale leases remain overdue while newer generations
  drain. Keep the stale duplicate reclaim CTEs in the claim path: they demote
  expired same-scope siblings to `retrying` when another live or newly claimed
  sibling owns the scope. Keep the `ProjectorQueue.Claim`
  stale-generation coalescing path (`projector_queue.go:74`) and the companion
  CTEs together; they move older same-scope projector rows and pending or
  failed `scope_generations` to `superseded` so durable snapshot history
  remains available without reprocessing obsolete local polling generations or
  reporting superseded terminal failures as current health.
  Keep the `ProjectorQueue.Heartbeat` supersede check with that claim behavior:
  live older same-scope generations must return `projector.ErrWorkSuperseded`
  once a newer generation is visible, or local polling can spend minutes writing
  graph state that will be immediately obsolete.
- **NornicDB semantic gate** — `ReducerQueue.Claim` blocks
  `semantic_entity_materialization` while source-local projection is in-flight
  when the NornicDB gate parameter is true. Do not remove or bypass this gate
  without an ADR.
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
  reduce worker concurrency as a substitute.
- **Schema ordering** — tables with foreign key constraints must appear after
  their referenced tables in `bootstrapDefinitions`. Current FK dependencies:
  `graph_projection_phase_state` → `ingestion_scopes` + `scope_generations`.
- **AWS checkpoint fencing** — `AWSPaginationCheckpointStore.Save` must keep the
  `fencing_token <= EXCLUDED.fencing_token` conflict guard. A stale AWS worker
  must not overwrite page state from a newer claim.
- **AWS scan-status fencing** — `AWSScanStatusStore` mutations must keep their
  fencing guards. A stale AWS worker must not overwrite per-tuple status from a
  newer claim.
- **AWS runtime drift joins stay bounded** —
  `PostgresAWSCloudRuntimeDriftEvidenceLoader` must load AWS rows from one
  `(scope_id, generation_id)` and must join Terraform state through the current
  AWS ARN allowlist. Do not scan all active Terraform state to discover matches.
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

## Evidence notes

No-Regression Evidence: #4444 review (codex P1) changes upsertStreamingFacts's
write call from upsertFactBatch (ExecContext) to upsertFactBatchReturningAccepted
(QueryContext with a RETURNING fact_id clause appended to the existing
ON CONFLICT ... WHERE fencing_token <= EXCLUDED.fencing_token guard), so
afterBatch's repository-catalog and relationship-evidence derivation only sees
envelopes whose fact_records write actually accepted. Baseline: before this
change, a fenced-out stale batch's payload still reached afterBatch and could
insert stale relationship_evidence_facts rows in the same transaction even
though its fact_records row was correctly rejected. After: filterAcceptedEnvelopes
narrows every batch to the RETURNING-reported accepted set before afterBatch
runs. Backend/proof shape: local Go unit tests
(`go test ./internal/storage/postgres -count=1`, 1199 passed, no regression
across the whole package) plus a live-Postgres two-sided proof —
`postgres:16-alpine` throwaway container,
`go test ./internal/storage/postgres -run
'TestUpsertFactBatchCrossBatchFencingTokenGuard|TestIngestionStoreCommitScopeGenerationFencesDerivedRelationshipEvidence'
-v -count=1` — reproduces the pre-fix leak (both repo-target-a and
repo-target-b evidence rows land after a stale second commit) and proves the
post-fix guard (only repo-target-a remains). The write shape adds one
RETURNING clause to an existing primary-key-scoped ON CONFLICT statement; no
new query, index, batch size, or worker/lease/concurrency knob changes. The
non-streaming upsertFactBatch/upsertFacts path (no afterBatch consumer) is
byte-identical to before.

No-Observability-Change: #4444 adds no new metric, span, log key, route,
worker, lease, or runtime knob. The fact_records upsert is still covered by
the existing `eshu_dp_postgres_query_duration_seconds` Postgres query-span
instrumentation regardless of whether the statement runs through ExecContext
or QueryContext; operators diagnose fencing/derived-evidence correctness
through the same row-shape truth (fencing_token, payload,
relationship_evidence_facts) the new tests assert directly.

No-Regression Evidence: #2718 code reachability watermarks use the
repository-generation row key `(scope_id, generation_id, repository_id)`.
`go test ./internal/reducer -run 'CodeReachabilityProjectionRunner' -count=1`
covers non-empty replacements and empty snapshots writing the loaded completion
watermark. `go test ./internal/storage/postgres -run 'CodeReachability'
-count=1` covers the watermark DDL, empty replacement watermark writes, and
pending selection across `code_calls` plus `inheritance_edges`.

No-Observability-Change: #2718 keeps the existing `code reachability projection
completed` structured log with input count, row count, and duration for the
same runner cycle; the fix changes durable progress tracking and does not add
worker knobs, retries, or high-cardinality telemetry labels.

Performance Evidence: #2050 changes workflow claim admission and completion
guards from an unfenced baseline to the same indexed workflow row shape plus one
active tenant-scope grant predicate keyed by opaque tenant/workspace/scope and
policy revision. The focused workflow/coordinator/Postgres package suite proves
the after shape on Postgres-backed workflow rows, including denied planning,
one admitted hosted work item, duplicate convergence with tenant boundary keys,
and terminal completion guarded by non-tombstoned, non-expired grant rows.

Observability Evidence: #2050 adds no high-cardinality metric label or runtime
worker knob. Operators diagnose the path through bounded coordinator logs for
`tenant_scope_missing_or_stale_policy`, planned/authorized/denied counts,
`workflow_runs`, `workflow_work_items`, `workflow_claims`, coordinator reconcile
metrics, and existing Postgres query spans and duration metrics for the
`tenant_workspace_grants` and workflow stores.

Benchmark Evidence: #2587 changes reducer readiness claim admission from
per-domain correlated predicates to one bounded requirements lookup against
`graph_projection_phase_state`. Local Compose Postgres 18-alpine on Darwin arm64
with `ESHU_REDUCER_CLAIM_READINESS_BENCH_CASES=1000:1000:1,1000:5000:4` measured
`BenchmarkReducerQueueClaimReadinessGateGrowth` at `15141958 ns/op` for 1,000
queue rows and 1,000 phase rows across one domain, and `14127125 ns/op` for
1,000 queue rows and 5,000 phase rows across four domains, both with unchanged
`102 allocs/op`.

No-Regression Evidence: #2587 keeps the claim update, lease, retry,
dead-letter, expired-claim replay, and conflict-domain fencing SQL unchanged
while focused readiness tests prove single claim, batch representative claim,
and `/admin/status` blockage reporting still wait on every required
scope/generation readiness phase.

No-Observability-Change: #2587 adds no metric, span, log, route, worker, lease,
batch size, or runtime default. Operators continue to diagnose readiness waits
through Postgres query spans, `eshu_dp_postgres_query_duration_seconds`, queue
status, bounded `/admin/status` blockage rows, failure class, retry/dead-letter
state, and reducer logs.

No-Regression Evidence: #2589 adds the `eshu_search_vector_metadata` schema and
store only. Focused storage tests prove idempotent upsert, active-generation
filtering through `ingestion_scopes.active_generation_id`, bounded status
aggregation by scope/model/index version, and bootstrap SQL/file parity. The
slice does not change API/MCP behavior, reducer workers, graph writes,
embedding providers, ranking, query routing, or runtime defaults.

No-Observability-Change: #2589 adds no route, worker, queue domain, metric,
label, span name, log shape, runtime knob, or external network dependency.
When a caller later wires the store through the instrumented Postgres adapter,
existing query/exec spans and `eshu_dp_postgres_query_duration_seconds` cover
the SQL path.

No-Regression Evidence: #2947 changes `code_function_summary` full-snapshot
cleanup from a marker-only no-op for stale rows to repo-scoped replacement of
`function_summaries`, `function_sources`, and `function_graph_ids`. Baseline:
a full value-flow scan that emitted only `code_dataflow_scanned` could enqueue
no summary replacement and leave deleted or renamed functions in the durable
stores. After: projector intents carry `repo_id` and `full_snapshot=true`, the
reducer filters the durable baseline outside that repo, validates every
replacement function id belongs to the repo, and the store deletes rows with
`repo = $1 AND updated_at <= $2` before upserting current rows in one
transaction when a transaction-capable DB is present. Backend/proof shape:
local Go unit tests against the Postgres store SQL fakes and reducer/projector
fakes, with one stale same-repo function, one current same-repo function, one
unrelated-repo function, and one marker-only empty full scan. Terminal row
assertions: the same-repo stale row is absent, the unrelated repo is excluded
from the replacement set, empty full snapshots replace with zero summary rows,
and empty full snapshots issue one zero-row replacement each for companion
source and graph-id stores. Verification: `go test ./internal/projector
./internal/reducer ./internal/storage/postgres ./cmd/reducer -count=1`.

No-Observability-Change: #2947 adds no metric instrument, metric label, span
name, worker, queue domain, lease, route, graph write, runtime knob, status
field, or batch/concurrency setting. Operators diagnose the path through
existing reducer execution spans/counters, durable reducer queue rows, the
`code function summary persistence completed` log fields `repo_id`,
`full_snapshot`, `function_count`, `source_count`, and `graph_id_count`, plus
instrumented Postgres exec/query spans and
`eshu_dp_postgres_query_duration_seconds` when the store is wrapped by
`InstrumentedDB`.

No-Regression Evidence: #2633 adds a nullable `partition_hash` column, partial
pending indexes for hashed and unhashed rows, and Postgres selectors for pending
shared intents that hash into a leased code-call partition or still need the
legacy unhashed path. `go test ./internal/storage/postgres -run
'TestSharedIntentStoreListPendingDomain(Partition|Unhashed)Intents|TestSharedIntentSchemaSQLIncludesPartitionCandidateIndexes'
-count=1` failed before the selectors existed, then passed after the SQL
filtered by projection domain, non-null partition hash plus modulo
partition id/count, null partition hash for legacy rows, uncompleted state, and
deterministic `created_at, intent_id` ordering. `go test
./internal/storage/postgres -run 'TestSharedIntent(Store|Schema|SQL)' -count=1`
and `go test ./internal/storage/postgres -count=1` prove shared-intent upsert
batching, schema SQL parity, acceptance lookup, partition loading, history
lookup, completion marking, and lease behavior still work with the new column.

No-Observability-Change: the selector adds no table, route, queue domain,
worker, lease, runtime knob, metric instrument, or metric label. The new read is
covered by the existing `InstrumentedDB` Postgres query spans and
`eshu_dp_postgres_query_duration_seconds`; operators still pair those with
shared-intent backlog/status queries and reducer code-call cycle logs.

## Common changes and how to scope them

- **Add a new Postgres store** → implement against `ExecQueryer`; add a
  `New*Store(db ExecQueryer)` constructor; add a `*SchemaSQL()` function
  returning idempotent DDL when the store owns a table; register owned tables in
  `BootstrapDefinitions` in `schema.go` with the correct position in the slice;
  wrap with `InstrumentedDB` in `cmd/` wiring for observability.

- **Change AWS checkpoint persistence** → edit
  `aws_pagination_checkpoint.go`; keep the primary key scoped to collector
  instance, account, region, service, resource parent, and operation; keep
  generation as invalidation state; and keep resource parents and page tokens
  out of telemetry labels.

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
  consumer-side interface (`internal/projector/runtime.go`).

- **Add a new fact kind or column** → update `upsertFactBatch` column list and
  `columnsPerFactRow`; update `scanFactEnvelope`; update the schema DDL; add a
  migration if the column is non-nullable without a default.

- **Add a new graph projection phase** → add the phase constant in
  `internal/reducer`; batch-upsert it via `GraphProjectionPhaseStateStore`; add
  a matching readiness lookup path if reducer domains gate on it.

- **Add Postgres telemetry** → wrap the `ExecQueryer` with `InstrumentedDB`;
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
  transaction is bounded to five SQL statements; do not add graph or network
  calls inside it.
- **Do not add `if backend == "nornicdb"` branches** here. Backend-specific
  queue gate logic is isolated to `ReducerQueue.Claim`'s parameterized gate
  (`reducer_queue.go`). New backend gates must go in the same parameterized
  pattern.
- **Do not skip `WorkflowControlStore` lease fencing**. Always check the
  returned error from claim mutations; silently ignoring `ErrWorkflowClaimRejected`
  causes split-brain workflow state.
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

## Evidence Notes

No-Regression Evidence: admission-decision evidence bounds are covered by
`go test ./internal/query -run 'TestAdmissionDecision|TestOpenAPISpecIncludesAdmissionDecisions' -count=1`
and `go test ./internal/storage/postgres -run 'TestAdmissionDecisionStore|TestAdmissionDecisionSchema|TestAdmissionDecisionStates' -count=1`.
The route rejects unsupported lightweight profiles before store reads and caps
embedded evidence at 20 rows per decision with truncation metadata.

No-Observability-Change: the admission-decision evidence cap adds no route,
worker, queue, graph write, metric, span, runtime default, or high-cardinality
label. Existing HTTP route attribution, truth envelopes, and Postgres query
spans/`eshu_dp_postgres_query_duration_seconds` diagnose the read.

No-Regression Evidence: the #2842/#2903-review `graph_endpoint_presence` migration
NON-DESTRUCTIVELY backfills pre-#2842 `repo_workload` rows with
`UPDATE ... SET repo_id = uid WHERE keyspace = 'repo_workload' AND repo_id = ''`,
so the repo-scoped runtime retract (keyed `repo_id = ANY(...)`) can finally match
and clean legacy stale rows once their repo re-materializes. It deliberately does
NOT delete blank-provenance rows: a delete would also remove still-CURRENT target
presence, and because `filterRowsByReadiness` terminalizes (does not defer) a
handles_route/runs_in row whose presence is absent, that would silently drop a live
edge until the next re-materialization (the #2903 P1). The hashed
`api_endpoint_repo_path` uid (#2844) makes repo_id unrecoverable, so those legacy
rows are left in place and are bounded-safe — the HANDLES_ROUTE MERGE re-MATCHes the
actual `:Endpoint`, so a stale-present row never creates an edge to a removed/
re-pathed endpoint, and a current endpoint re-upserts proper provenance next
materialization. The backfill matches zero rows once migrated, so it is idempotent
on every `EnsureSchema`. `go test ./internal/storage/postgres -run
'TestGraphEndpointPresenceSchemaSQL|TestGraphEndpointPresenceStoreRetractStaleRepoGenerations|TestGraphEndpointPresenceStoreUpsertIsIdempotent'
-count=1` covers the backfill assertion and the upsert/retract contract; the Go DDL
const and `024_graph_endpoint_presence.sql` stay byte-identical.

No-Observability-Change: the backfill only sets `repo_id` on blank-provenance
`repo_workload` rows in an existing table on schema apply; it deletes nothing and
drops no presence. It adds no route, worker, queue domain, graph write, lease,
runtime knob, metric instrument, metric label, span, or log key; operators still
diagnose presence through the existing workload-materialization completion logs,
the shared-projection blocked/terminal counters, and
`eshu_dp_postgres_query_duration_seconds`.

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

`CodeInterprocProjectedEdgeStore` (`code_interproc_projected_edge`, migration 044)
and `CodeTaintEvidenceProjectedNodeStore` (`code_taint_evidence_projected_node`,
migration 045) record the source-Function uid / CodeTaintEvidence node uid of
every projected value-flow artifact so the reducer retracts by indexed uid
instead of scanning the whole graph on NornicDB. Both follow the store
conventions here: `ExecQueryer`, idempotent batched upsert with in-batch dedupe,
`SchemaSQL` const mirrored by the migration, and `SELECT DISTINCT ... ORDER BY`
enumeration / bounded `DELETE` prune, all parameterized. Written before the graph
write (superset invariant); see `go/internal/reducer/AGENTS.md` (#4893).

No-Regression Evidence: `go test ./internal/storage/postgres -run
'CodeInterprocProjectedEdge|CodeTaintEvidenceProjectedNode' -count=1` covers
schema-SQL parity, in-batch dedupe/blank skipping, enumeration query shapes, and
prune shapes. Full runtime evidence in `go/internal/reducer/AGENTS.md` (#4893).

No-Observability-Change: the ledger reads/writes are covered by the existing
`InstrumentedDB` Postgres query spans and `eshu_dp_postgres_query_duration_seconds`;
no new metric, label, worker, queue domain, lease, or runtime knob.

## #4861 — Drop redundant graph_projection_phase_state_lookup_idx

Performance Evidence: `graph_projection_phase_state` has PRIMARY KEY
`(scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase)`.
Postgres auto-creates a unique B-tree index for that PK. A separate index
`graph_projection_phase_state_lookup_idx` was defined on the identical six
columns in identical order — fully redundant. Every INSERT/UPDATE maintained
two identical indexes. The only lookup query (`lookupGraphProjectionPhaseStateSQL`)
and the reducer readiness gate use the full six-column key, already covered by
the PK index.

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

No-Observability-Change: this change only drops a redundant Postgres index.
No metric, span, log key, route, worker, queue domain, lease, graph write,
or runtime knob changes. The `graph_projection_phase_state` table upsert
is still covered by the existing `InstrumentedDB` Postgres query spans and
`eshu_dp_postgres_query_duration_seconds`. Operators continue to diagnose
readiness waits through the same Postgres query spans, queue status,
`/admin/status` blockage rows, and reducer logs.

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

Performance Evidence: measured with a counting `fakeExecQueryer` that records
every `ExecContext` DELETE (deterministic, no DB needed), via
`TestContentWriterBatchesTombstoneDeletes`.

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

No-Observability-Change: the batched deletes target the same tables and rows as
before and add no metric instrument, metric label, span, log key, route, queue
domain, worker, lease, or runtime knob. Operators still diagnose the content
write path through the existing `InstrumentedDB` Postgres exec/query spans and
`eshu_dp_postgres_query_duration_seconds`, and the existing content-write stage
timing logs.

## #4859 — Drop unused fact_records_stable_key_idx

Performance Evidence: `fact_records_stable_key_idx` on `(stable_fact_key,
generation_id)` is the single biggest fact-insert write-amplification contributor
and has no query reader. It is the only general/all-row `idx_scan=0` secondary
index on `fact_records` (2132 MB on the live `e2e3586persist` stack, 22h uptime,
`idx_scan=0`). Every other `idx_scan=0` index is a fact-kind/tombstone/expression
partial that only writes for matching facts.

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

No-Observability-Change: dropping an unused index. No metric, span, log key,
route, worker, queue domain, lease, graph write, or runtime knob changes. The
`fact_records` upsert is still covered by the existing `InstrumentedDB` Postgres
query spans and `eshu_dp_postgres_query_duration_seconds`. Changed-since and all
reads are unaffected (they use `fact_records_scope_generation_idx`).

## #4862 — Keep content pg_trgm GIN indexes (drop disproven)

A proposal to drop the two `pg_trgm` GIN indexes on `content_files.content`
and `content_entities.source_cache` was audited against a live 838-repo Postgres
stack and disproven. Both indexes are actively used by the all-repo / code-topic
search read path: `investigateCodeTopic` (`content_reader_code_topic.go`) and
the `SearchFileContentAnyRepo` / `SearchEntityContentAnyRepo` content readers.
Repo-scoped searches do not use these GINs (the selective `repo_id` equality
wins), so the all-repo `ILIKE '%term%'` reads are the only load-bearing consumers.

### Performance Evidence

Live audit on a drained `e2e3586persist` stack (Postgres 18-alpine, 838 repos):

| metric | value |
|---|---|
| `content_entities` live rows | 2,504,903 (2312 MB) |
| `content_files` live rows | 137,373 (746 MB) |
| `content_entities_source_trgm_idx` size | 518 MB |
| `content_files_content_trgm_idx` size | 233 MB |
| total GIN write-tax | 751 MB |
| `content_entities_source_trgm_idx` idx_scan | 1 (post-search) |
| `content_files_content_trgm_idx` idx_scan | 2 (post-search) |

The planner selects both indexes via **Bitmap Index Scan** for the all-repo path.
Session-local drop simulation (no actual DROP, `SET enable_bitmapscan=off;
SET enable_indexscan=off`):

| query | WITH GIN | forced seq scan | penalty |
|---|---|---|---|
| `content_entities.source_cache ILIKE '%authenticate%'` | 737 ms | 3580 ms | ~4.9× slower |
| planner cost (entities) | 347 | 153158 | ~440× |
| `content_files.content ILIKE '%authenticate%'` | 4203 ms | 5388 ms | ~1.3× slower |
| planner cost (files) | 138 | 15108 | ~100× |

The seq-scan penalty scales with table size, so it widens on larger corpora.
Issue #4980 disproved replacing these reads with the curated BM25 tables:
search documents cap context at 4,096 bytes, omit governed rows, and implement
token rather than arbitrary-substring semantics. The accuracy-preserving fix
keeps the exact GINs but defers their initial creation until cold content
projection drains. `content_substring_index_state` fences unscoped reads until
both exact indexes validate and bootstrap-index has run `ANALYZE`. Existing
indexes are never dropped during restart, upgrade, or steady-state ingestion.
See `evidence-4980-deferred-content-gin.md` for the before/after, exactness,
plan, restart, failure, and lock-exclusion proof.

Issue #4980 adds the bounded `content_index_finalization` value to the existing
bootstrap phase histogram, start/terminal logs with state, duration, and failure
class, and the durable lifecycle row. Operators still diagnose content reads
through the existing `postgres.query` spans. The schema-constant guard test
(`TestContentStoreSearchIndexSchemaSQLKeepsExactTrigramGINs`) remains the
compile-time safety lock that prevents an inaccurate BM25 substitution.

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
touch points that reference `facts.AWSTagObservationFactKind` — the AWS runtime
collector's emission counter and scan-status summarizer — only switch on the
fact kind to count occurrences), so a decode struct for it would be a hollow,
never-validated contract.

Benchmark Evidence: `go test ./internal/storage/postgres -run '^$' -bench
'BenchmarkCloudTagEvidenceRecordFromRow' -benchmem -count=5` (darwin/arm64,
Apple M1 Max; one representative azure_tag_observation row + one
gcp_tag_observation row per iteration, each carrying 3 tag fingerprints).
BEFORE (raw map + coerceJSONString, commit f51260fb5, measured in a throwaway
`git worktree` at that commit with the same benchmark file copied in
unmodified — the function signature did not change) -> AFTER (typed decode):
5629 ns/op -> 6647 ns/op (+18.1% time), 3312 B/op -> 4224 B/op (+27.5% bytes),
64 allocs/op -> 72 allocs/op (+12.5% allocs), all for the 2-row batch (halve
for per-row cost: ~2815 ns/row -> ~3323 ns/row). This exceeds the ~10%
diagnostic-rigor band; it is accepted rather than optimized away because (a)
the cost is the price of `decodeAndValidate`'s reflection-based required-field
check and struct decode replacing an ad hoc map type-switch, the same shape
every prior Contract System v1 typed-decode migration in this file pays, and
(b) tag-evidence rows are a small fraction of one generation's fact volume —
bounded by the count of azure/gcp resources that carry at least one tag, never
the dominant row count next to `azure_cloud_resource`/`gcp_cloud_resource` or
the relationship kinds — so a few added microseconds per row does not create a
queue or admission backlog risk. Result class: correctness win (closes a
silent-coercion accuracy gap) with a measured, bounded, non-hot-path cost.

No-Regression Evidence: `go test ./internal/storage/postgres -run
'CloudTag|TagEvidence' -count=1 -v` covers the existing azure/GCP mapping
tests (now updated with the collector-required `normalized_resource_id`/
`resource_type` fields the typed struct requires), the SQL allowlist lockstep
test, and the blank scope/generation rejection test unchanged. A new test,
`TestPostgresCloudTagEvidenceLoaderDropsNonStringFingerprintValues`, is RED
against the pre-change raw path (a `tag_value_fingerprints` value of `42`
decodes to `1` record carrying the coerced string `"42"`) and GREEN after (the
row is dropped, `0` records) — the concrete regression this change fixes.

No-Observability-Change: #4686 adds no new metric, span, route, worker, queue
domain, or runtime knob. The existing `logSkippedRow` structured log ("cloud
tag evidence loader skipped source fact", `cloud_tag_evidence_source_fact_decode`
failure class, scope/generation/fact_kind attributes) still fires on every
dropped row, decode failures included, so an operator's existing diagnostic
path is unchanged.

## #5438 — identity-fact load epoch cache

### Concurrency design

The identity epoch cache (`identityEpochCache`) holds the full set of active
container-image identity facts, reloaded under singleflight on epoch mismatch.

- **Shared state**: `epoch` (count + max_observed_at + active_fingerprint),
  `facts` slice, `loading` channel. All guarded by `sync.Mutex mu`.
- **Lock scope**: `mu` is held ONLY for map/state reads, state swaps, and the
  channel handoff. It is NEVER held across DB loads or epoch probes — both
  release the lock before I/O.
- **Singleflight**: the first cache-miss caller sets `c.loading = make(chan
  struct{})` under lock, releases the lock, performs the DB load + post-load
  probe, re-acquires the lock, populates the cache, `close(c.loading)`, and sets
  `loading = nil`. Concurrent callers see non-nil `loading`, release the lock,
  `<-waitCh`, and retry `get()` which then serves from the newly populated
  cache.
- **TOCTOU analysis**: the probe→serve window (between epoch probe and cache
  hit) is a bounded probe-interval gap. Today's baseline does a full paginated
  O(corpus) scan inside every call, mixing facts from different commit
  snapshots across pages. The cache serves a read-committed snapshot of ONE
  coherent load with post-load validation. A mid-load commit is detected by
  the post-load probe and the rows are served uncached — no worse than today's
  mixed-epoch pagination, and the next call retries the cache. The cache is
  strictly more consistent per serving window than the unbounded pagination it
  replaces.
- **Staleness bound**: one probe window (~65 ms on 500k-fact shim). Self-heals
  on the next reducer intent execution.
- **Epoch semantics (3-tuple)**: The cached epoch is `(fact_count_all,
  fact_max_observed_all, active_fingerprint)`:
  * `fact_count_all` + `fact_max_observed_all` — computed FROM fact_records
    alone (no JOIN) using the partial index `fact_records_identity_epoch_idx`
    (Index Only Scan, ~51 ms on 500k facts). These detect raw fact insertions/
    deletions that change the identity set.
  * `active_fingerprint` — `COALESCE(md5(string_agg(scope_id::text || ':' ||
    active_generation_id::text, '|' ORDER BY scope_id)), '') FROM
    ingestion_scopes` (Seq Scan + aggregate on tiny table, ~2 ms on 2000
    scopes — same cost class as the prior summed hash; it scans the same
    small table either way, so this is not a performance regression). This
    detects a supersession (active_generation_id flip) that changes the
    active identity set without changing the raw fact count or max
    observed_at.
  * **Collision resistance**: with only (count, max), a supersession that
    swaps `active_generation_id` to a new generation with equal
    identity-fact cardinality would produce a false cache hit (stale
    evidence). The fingerprint closes this gap. Earlier this fingerprint was
    `sum(hashtext(scope_id || ':' || active_generation_id))`, a 32-bit hash
    summed across scopes — that shape has two failure modes an md5 digest of
    the ordered mapping does not: a 32-bit `hashtext` collision between two
    different active mappings, and two offsetting per-scope deltas that
    cancel out in the sum (e.g. one scope's hash increases by exactly as
    much as another's decreases, which a sum cannot distinguish from no
    change at all — no birthday-bound analysis bounds that case, since it
    is a structural property of summation, not a random collision). The
    current fingerprint instead hashes the full ordered mapping
    (`ORDER BY scope_id`, joined with `'|'`) as one string, so any change to
    any scope's active generation changes the input to `md5` and therefore
    the digest deterministically. There is no "self-heals on the next
    probe" residual risk to rely on for this fingerprint.
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

Performance Evidence: #5438 adds an epoch-cached identity fact set to
`ListActiveContainerImageIdentityFacts`, replacing ~2,000 O(corpus) paginated
loads per worst-case reducer drain with 1 reload + ~2,000 cheap index-only
epoch probes. The probe is backed by the new partial index
`fact_records_identity_epoch_idx ON fact_records (observed_at, fact_id)
WHERE <6-arm identity filter> AND is_tombstone = FALSE` and runs FROM
fact_records alone (no ingestion_scopes/scope_generations JOIN), so it
leverages an Index Only Scan. Proved locally with the identity-epoch test
suite (7 new tests, all green, including concurrent-singleflight under 32
goroutines and commit-mid-load rejection).

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
(50 ms, 0 heap fetches, 3,016 buffers). Partial index size: 24 MB for
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

Observability Evidence: six new `eshu_dp_` instruments registered in
`go/internal/telemetry/instruments.go` (IdentityCacheHitTotal,
IdentityCacheMissTotal, IdentityCacheReloadTotal,
IdentityCachePassthroughTotal, IdentityCacheReloadDuration,
IdentityCacheProbeDuration). The cache receives `*telemetry.Instruments`
via `NewIdentityEpochCache`, wired from the reducer's real `instruments`
in `buildObservedReducerService`. Metrics appear on the `/metrics`
endpoint via the shared Prometheus exporter. All are documented in
`docs/public/observability/telemetry-coverage.md`. The blocking
telemetry-coverage gate (`ESHU_TELEMETRY_COVERAGE_BASE=origin/main
scripts/verify-telemetry-coverage.sh`) is green. X4 dashboard:
diagnostic cache-internal signals; not headline-alarm-worthy per the
skill's conditional ("If the metric should appear"). The cache path
adds no route, worker, queue domain, lease, graph write, or runtime
knob beyond `ESHU_IDENTITY_CACHE_MAX_BYTES`. Operators diagnose cache
effectiveness through the hit/miss/reload/passthrough counters and
probe/reload duration histograms, plus the existing
`eshu_dp_postgres_query_duration_seconds` for the underlying DB queries.

## #5476 — Crossplane cross-scope SATISFIED_BY redrive sweep

Do NOT re-introduce an inline fan-out inside `ProjectorQueue.Ack`'s
transaction for this feature — that design was proposed, reviewed, and
rejected (stale-lease commit risk, unbounded transaction/lock hold time,
missing telemetry). The durable shape is: a claim/completion marker table
(`crossplane_satisfied_by_redrive_state`), a bounded keyset-paginated
target-discovery query backed by a partial index
(`fact_records_active_k8s_claim_redrive_idx`), and a sweep
(`CrossplaneSatisfiedByRedriveSweeper.Sweep`) triggered AFTER Ack commits
(`runCrossplaneRedriveHook`), never inside it. See `README.md`'s "Crossplane
cross-scope SATISFIED_BY redrive sweep (#5476)" section for the full design,
fences, and evidence.

When touching this surface:

- The three fences (active-generation, active-claim, already-satisfied-for-
  this-identity) are load-bearing correctness, not defensive extras.
- The already-satisfied fence is `crossplane_satisfied_by_redrive_target_ledger`,
  keyed by (target scope, XRD group, XRD claim kind) — NOT by timestamp. A
  first review round shipped a timestamp-anchored fence
  (`fact_work_items.updated_at > xrd_fact.observed_at`) that looked correct but
  was silently ineffective: the XRD fact's own `observed_at` strictly advances
  on every resync of the platform repo even when its (group, claim_kind) is
  unchanged, so the fence never actually skipped anything across repeated
  syncs. Do not reintroduce a timestamp-based already-satisfied fence; keep it
  keyed on the stable (target scope, group, claim_kind) identity.
- **`RecordRedriven` MUST be called ONLY from
  `reducer.CrossplaneSatisfiedByMaterializationHandler`, strictly after it
  commits a SATISFIED_BY edge — NEVER from the sweep's enqueue path.** A
  SECOND review round caught the sweep itself calling `RecordRedriven`
  immediately after `ReplayCrossplaneSatisfiedByMaterialization` (which is
  enqueue-only and returns before the reducer handler runs). Because
  auto-retry-on-dead-letter is disabled by default
  (`cmd/reducer/poison_liveness_wiring.go`), an intent that enqueues but later
  dead-letters would leave that design's ledger entry PERMANENTLY and
  SILENTLY marking the target satisfied — reopening the exact false-negative
  window #5476 exists to close, worse than the original bug because it is
  silent and permanent (the ledger PK never expires). Do not move
  `RecordRedriven` back into the sweep.
- `Sweep` is the live post-Ack trigger path; `SweepBatch` is the
  startup/periodic catch-up path that reclaims stale/expired claims and
  re-runs the fan-out. **Both must stay wired.** `Sweep` alone cannot recover
  from a transient DB error or process crash mid-fan-out — nothing else
  revisits a `'claimed'` row once its lease expires unless a caller invokes
  `ClaimBatch`/`SweepBatch` periodically. `cmd/projector`'s
  `runCrossplaneRedriveCatchUpLoop` is that caller; do not remove it or the
  durable-resumability claim in this package's docs becomes false again.
- `ReplayCrossplaneSatisfiedByMaterialization` MUST reuse the exact same
  per-scope `EntityKey` (`"crossplane_satisfied_by_materialization:" +
  scopeID`) the projector's own trigger uses. A divergent per-XRD-generation
  identity would break the queue's natural dedupe and could double-process a
  target scope.
- `CrossplaneRedriveStateStore.MarkCompleted` is fenced by the bumped
  `claim_fencing_token` (mirroring `aws_freshness_triggers`, #4576), NOT by
  `claimed_by`. `Owner` is a static per-process-class string shared by every
  replica and the catch-up scanner (mirroring `ProjectorQueue`/`ReducerQueue`'s
  own `LeaseOwner` convention), so a `claimed_by`-only fence would let a stale
  invocation whose lease was reclaimed by another invocation under the SAME
  owner string silently complete a claim it no longer holds. Do not weaken
  this back to an owner-string fence.
- Operator dashboard decision: the three new counters
  (`eshu_dp_crossplane_redrive_sweeps_total`,
  `..._targets_enqueued_total`, `..._pages_processed_total`) are
  intentionally NOT added to
  `docs/public/observability/dashboards/eshu-operator-overview.json`.
  They are diagnostic sweep-throughput signals, not headline-alarm-worthy,
  consistent with the sibling `eshu_dp_crossplane_satisfied_by_edges_total`
  (#5347) and the identity-cache counters elsewhere in this file, neither of
  which is on the dashboard either. An operator diagnoses redrive health
  through these counters directly (e.g. a Prometheus query or the structured
  `crossplane redrive catch-up sweep failed`/`completed` logs), not a
  dashboard panel. Revisit this decision if a production incident shows the
  redrive path silently stalling without an operator noticing in time.
