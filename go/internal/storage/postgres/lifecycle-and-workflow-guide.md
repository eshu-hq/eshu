# storage/postgres lifecycle and workflow guide

This guide keeps the durable lifecycle, queue, fencing, workflow, and read-model
notes for `storage/postgres`. Keep `README.md` focused on package orientation;
update this file when a Postgres change alters bootstrap ordering, fact
persistence, queue semantics, workflow fencing, status rows, webhook triggers,
or AWS runtime drift storage behavior.

## Lifecycle / workflow

### Schema bootstrap

`ApplyBootstrap` (or `ApplyBootstrapWithoutContentSearchIndexes`) applies all
`BootstrapDefinitions` in order. Each `Definition` carries a name and SQL DDL.
`ValidateDefinitions` enforces uniqueness. Schema DDL is idempotent
(`CREATE TABLE IF NOT EXISTS`, `CREATE INDEX IF NOT EXISTS`).
The large `fact_records` DDL lives in `schema_fact_records.go` so
`schema.go` can stay focused on bootstrap ordering and exported helpers.
`graph_schema_applications` stores the graph backend/schema fingerprint and the
explicit compatible writer-fingerprint list after `eshu-bootstrap-data-plane`
successfully applies graph DDL. Preserved-volume restarts use that durable
marker to skip repeated NornicDB constraint/index checks when the graph schema
is unchanged. Graph-writing runtimes read the latest backend marker from
Postgres at startup and refuse when their compiled fingerprint is neither exact
nor listed as compatible by the latest marker.

No-Regression Evidence: `go test ./internal/storage/postgres -run
'TestBootstrapDefinitions(AreOrderedAndComplete|IncludeGraphSchemaApplications|IncludeContentStoreTables)|TestBootstrapSQLFilesMirrorDefinitions'`
proves the marker table is registered in ordered bootstrap definitions and the
checked-in SQL file mirrors the Go definition. `go test
./internal/graphschemacompat -count=1` proves stale writers reject the latest
incompatible marker, additive-compatible markers pass, and missing markers fail
closed before graph writes.

Observability Evidence: graph schema marker reads and writes are routed through
the bootstrap data-plane Postgres connection; the operator-facing signal is the
`bootstrap.graph.skipped`, `bootstrap.graph.applied`, or
`runtime.startup.failed` structured log from the bootstrap or graph-writing
binary, plus normal Postgres query/exec errors if the marker table is
unavailable. No runtime performs steady-state `SHOW CONSTRAINTS` or
`SHOW INDEXES` checks against the graph backend.

### Fact persistence

`FactStore.UpsertFacts` batches facts into multi-row INSERT statements of up
to 500 rows (17 columns each, well under the Postgres 65535-parameter limit).
`deduplicateEnvelopes` removes duplicate `fact_id` values within each batch
before sending to avoid `SQLSTATE 21000` on `ON CONFLICT DO UPDATE` when a
generation contains self-overwrites.

`FactStore.ListFactsByKind` uses the same 500-row page size for kind-filtered
reads (`facts_filtered.go:77`). Reducer domains such as semantic entities and
code calls use this path to avoid full-generation loads and thousands of tiny
Postgres round trips on large repositories. `ListFactsByKindAndPayloadValue`
adds a top-level JSON payload allowlist (`facts_filtered.go:115`) for reducer
domains whose correctness contract is tied to `content_entity.entity_type`,
such as inheritance and SQL relationships. Both paths select the full
`facts.Envelope` column shape before calling the shared scanner, so filtered
reads keep schema version, collector, fencing, and source-confidence metadata.
`ListActiveRepositoryFacts` pages active, non-tombstoned Git repository facts
through the partial `fact_records_active_repository_idx` index so package source
correlation reads one row per active repository scope, not every fact row. It
filters `is_tombstone = FALSE` as a residual predicate so a repository fact
tombstoned within a still-active generation is never returned as live, matching
every sibling active source-local reader.
`ListActivePackageManifestDependencyFacts` uses
`fact_records_active_package_dependency_entity_idx` to load only active,
non-tombstoned Git manifest dependency entities for the ecosystem/name set in
the current package-registry reducer intent. It filters `is_tombstone = FALSE`
as a residual predicate on the rows the index scan already visits, so a manifest
dependency fact tombstoned within a still-active generation is never returned as
live; the index choice, keyset pagination, and scan shape are unchanged, and the
guard matches every sibling active source-local reader.
`ListOwnedPackageDependencyTargets` uses the
same active-generation dependency predicate with a small ecosystem allowlist
and hard result cap so the workflow coordinator can derive package-registry and
vulnerability-intelligence targets without scanning stale generations or the
full fact table. It carries dependency `source_location` alongside the package
identity so Swift vulnerability-intelligence planning can call OSV `SwiftURL`
with the source Git URL instead of an internal package ID. Package correlation
reads use
`fact_records_package_correlations_v2_lookup_idx` for package-scoped reads and
`fact_records_package_correlations_v2_repository_lookup_idx` for
repository-scoped reads across ownership, publication, and consumption rows so
API and MCP callers stay bounded by `package_id` or `repository_id`. The v2
names force existing bootstrapped databases to create indexes with the expanded
publication predicate instead of keeping the older ownership/consumption-only
partial indexes.
Container-image identity active reads use
`fact_records_active_container_image_refs_idx` to page only active OCI digest
catalog rows plus Git/AWS image-reference rows that can participate in the
digest-first reducer join. The query excludes unrelated content entities and
AWS relationships before the reducer sees them, so an OCI registry generation
can re-evaluate existing source/runtime image references without scanning every
fact in the corpus.
CI/CD run correlation reads use
`fact_records_ci_cd_run_correlations_lookup_idx` and
`fact_records_ci_cd_run_correlations_run_lookup_idx` for repository/run scoped
reducer facts. Commit, artifact-digest, and environment-only reads have their
own partial indexes; image-reference follow-up reads use
`fact_records_ci_cd_run_correlations_image_ref_idx` so supply-chain impact can
connect SBOM/image evidence to runtime facts without scanning unrelated CI/CD
payloads. Each advertised API/MCP anchor stays bounded. The
`fact_records_container_image_identity_digest_idx` index lets the reducer join
CI artifact digests to active image identity rows without scanning unrelated
fact payloads.
Service-catalog correlation reads use
`fact_records_service_catalog_correlations_entity_idx`,
`fact_records_service_catalog_correlations_repository_idx`,
`fact_records_service_catalog_correlations_candidate_repository_idx`, and
`fact_records_service_catalog_correlations_owner_idx` so API/MCP filters by
scope, provider, entity, repository, ambiguous candidate repository, service,
workload, owner, outcome, and drift status stay bounded to
`reducer_service_catalog_correlation` facts.
Repository-language inventory reads use `content_files_language_repo_idx` so
content-index questions such as "how many TypeScript repos?" can count and page
by language family without scanning every repository coverage response.
SBOM/attestation attachment reads use
`fact_records_oci_image_referrer_subject_idx`,
`fact_records_sbom_attestation_attachments_subject_idx`,
`fact_records_sbom_attestation_attachments_document_idx`,
`fact_records_sbom_attestation_attachments_document_digest_idx`, and
`fact_records_sbom_attestation_attachments_status_idx` for referrer-subject,
digest, document, document-digest, and status-scoped facts.
`ListActiveSBOMAttestationAttachmentFacts` loads only active referrer and image
identity facts for the subject digests in the current reducer intent, so
attachment admission does not scan unrelated SBOM or OCI evidence.
Supply-chain impact reads use `fact_records_supply_chain_impact_lookup_idx`,
`fact_records_supply_chain_impact_status_lookup_idx`, and
`fact_records_supply_chain_impact_package_lookup_idx` for CVE, status, package,
repository, and subject-digest scoped reads. The reducer's active evidence
loader uses `fact_records_vulnerability_affected_package_lookup_idx` and
`fact_records_sbom_component_purl_idx`, plus the package, SBOM attachment, and
image-identity indexes above, so impact correlation stays bounded by the CVE,
package ID, PURL, SBOM document ID, or digest discovered in the triggering
intent.
Advisory evidence reads additionally use
`fact_records_vulnerability_active_cve_lookup_v2_idx`,
`fact_records_vulnerability_active_advisory_lookup_v2_idx`,
`fact_records_vulnerability_active_ghsa_lookup_v2_idx`,
and `fact_records_vulnerability_package_purl_lookup_idx` so source-only
advisory/CVE/package drilldowns stay anchored to first-class advisory identity
fields, package IDs, or PURLs before active-generation validation. Aliases and
correlation anchors remain returned evidence, but they are not top-level
Postgres scan predicates for this read model.
Provider security alert reads use
`fact_records_security_alert_repository_lookup_idx`,
`fact_records_security_alert_cve_ids_idx`,
`fact_records_security_alert_ghsa_ids_idx`,
`fact_records_security_alert_reconciliation_lookup_idx`,
`fact_records_security_alert_reconciliation_provider_repository_idx`,
`fact_records_security_alert_reconciliation_scope_idx`,
`fact_records_security_alert_reconciliation_provider_idx`,
`fact_records_security_alert_reconciliation_cve_ids_idx`, and
`fact_records_security_alert_reconciliation_ghsa_ids_idx` so source provider
alerts and reducer reconciliation rows stay bounded by repository, provider,
package, alert state, reconciliation status, CVE, or GHSA. The reducer evidence
loader pages only active package-consumption and impact facts for identifiers
seen in the triggering provider alert intent.

`sanitizeJSONB` strips JSON-encoded U+0000 characters and raw control bytes
(`0x00–0x1F` except tab/newline/CR) from payloads before INSERT to prevent
`SQLSTATE 22P05` and `SQLSTATE 22P02` errors on repositories with binary or
non-UTF-8 content. It preserves literal source text such as the six characters
`\u0000`.

`CommitScopeGeneration` compares the incoming generation `FreshnessHint` with
the newest pending or active generation for the same scope. When the hint is
unchanged, the commit path logs and skips the redundant write so local polling
can observe files without recommitting identical snapshots or superseding
in-flight projector work. Failed generations do not satisfy this check, so a
failed first projection can still be retried by the next snapshot.

`CollectorGenerationDeadLetterStore` covers the narrower failure point where a
collector generation reaches `CommitScopeGeneration` but the durable commit
returns before projector work rows exist. The store writes one idempotent row to
`collector_generation_dead_letters` keyed by `generation_id`, with safe
scope/generation replay metadata and a bounded failure message. It does not
persist the consumed fact stream; replay marking is a source-level handoff for
the collector or operator after the commit failure is fixed. A later successful
commit for the same scope marks unresolved rows `replayed`.

No-Regression Evidence: collector generation dead-letter storage is covered by
`go test ./internal/storage/postgres -run 'TestCollectorGenerationDeadLetter|TestStatusStoreReadRawSnapshot|TestBootstrapDefinitionsAreOrderedAndComplete' -count=1`.
The table is idempotent, keyed by generation id, indexed by status/scope/kind,
and replay marking updates matching `dead_letter` rows to `replay_requested`
while successful source commits update unresolved rows to `replayed`, without
touching `fact_work_items` or changing projector/reducer worker counts.

Observability Evidence: `StatusStore.ReadStatusSnapshot` reads
`collector_generation_dead_letters` into the runtime status report. Operators
see `collector_generation_dead_letters.dead_letter`,
`collector_generation_dead_letters.replay_requested`,
`collector_generation_dead_letters.replay_attempts`, and
`collector_generation_dead_letters.oldest_dead_letter_age_seconds` for
unresolved `dead_letter` or `replay_requested` rows, plus matching
`eshu_runtime_collector_generation_*` gauges without raw fact payloads,
repository names, local paths, credential handles, or provider response bodies.

`IngestionStore.CurrentScopeGeneration` exposes the same newest pending or
active `(generation_id, freshness_hint)` lookup for callers that need a
bounded preflight before doing expensive deterministic work. `eshu docs verify
--persist` uses this to reuse unchanged documentation finding facts while still
letting changed documents commit through `CommitScopeGeneration`.

### Projector queue

`ProjectorQueue.Claim` uses `SELECT ... FOR UPDATE SKIP LOCKED` with a
per-scope in-flight conflict guard and an oldest-ready-row guard. Concurrent
claimers for the same `scope_id` must all target the same oldest ready work
item, so a worker cannot skip a locked older row and start a newer generation
for the same repository. Before selecting a candidate, claim coalesces older
same-scope projector rows and their pending or failed `scope_generations` to
`superseded` when a newer generation exists. That covers waiting rows and
obsolete terminal failures, so durable snapshot history remains available
without leaving stale local polling generations in the live backlog or health
summary.
`ProjectorQueue.Heartbeat` applies the same freshness check to a live claimed
or running row. When a newer pending or active generation exists for the scope,
heartbeat marks the older row and its generation `superseded` in one statement
and returns `projector.ErrWorkSuperseded` so the worker stops without acking
stale graph state.
Expired `claimed` or `running` rows are ordered ahead of ordinary pending rows
so stale leases are reclaimed before fresh work makes the status surface look
permanently overdue. Claim also demotes expired same-scope duplicate in-flight
rows back to `retrying` when a live sibling or a newly claimed sibling owns the
scope, which repairs queue state left by older owner crashes or claim races
without breaking the one-active-generation invariant. `Ack` runs a five-step
atomic transaction: supersede stale active generation → supersede older
terminal same-scope generations → activate target generation → update scope
pointer → mark work succeeded. This keeps obsolete failed or dead-letter
projector rows out of current health after a newer source-local generation has
successfully become active. If `projector.IsRetryable(cause)` returns true and
`attempt_count < MaxAttempts`, `Fail` transitions to `retrying` instead of
`dead_letter`.

No-Regression Evidence: `go test ./internal/storage/postgres -run
'TestProjectorQueueAck(SupersedesObsoleteTerminalGenerations|PromotesGenerationAndSupersedesPriorActive)'
-count=1` proves the ack transaction still promotes the current generation and
now coalesces older terminal same-scope projector failures before status can
report them as active health.

Observability Evidence: no new telemetry name was needed. The existing
instrumented Postgres query spans and
`eshu_dp_postgres_query_duration_seconds{store="queue"}` metric cover each ack
SQL statement, while `/admin/status` fact-work and generation counts expose
whether obsolete terminal rows still affect service health.

### Reducer queue

`ReducerQueue.Claim` extends the projector model with single-domain legacy
filtering and multi-domain allowlists for reducer deployment lanes, plus
NornicDB-specific semantic gates. When the NornicDB gate is active (`$5 = true`),
`semantic_entity_materialization` items are blocked while any source-local
projection is in-flight, preventing cross-scope contention on NornicDB label
indexes. The gate is disabled for Neo4j.
`reducer_queue.go` keeps lifecycle/validation; `reducer_queue_helpers.go`
keeps scan/default/retry/ID helpers shared by single-claim, batch, and replay paths.

Benchmark Evidence: `BenchmarkReducerQueueClaimDeepQueue` measures the current
single-claim reducer path against live Postgres with the existing
`fact_work_items_reducer_conflict_claim_idx` and
`graph_projection_phase_state_updated_idx` DDL. On local Compose Postgres 18.4
from the remote-e2e stack, Apple M4 Pro, one reducer claim worker, 1,024 active
repository scopes/generations, no graph-readiness rows, no projector work, and
workload-identity rows using production `platform_graph` conflict keys derived
from scope id, `go test ./internal/storage/postgres -run '^$' -bench
BenchmarkReducerQueueClaimDeepQueue -benchtime=1x -count=1 -timeout=20m`
recorded 0.225766500 s/op at 100,000 queued reducer rows and 7.215437167 s/op
at 1,000,000 rows. The benchmark creates an isolated temporary schema on a
pinned Postgres connection, seeds active scopes/generations and
`fact_work_items` through `generate_series`, runs untimed `ANALYZE` on the
fixture tables, runs the real `ReducerQueue.Claim` SQL, resets the claimed row
outside the timed region, and is DSN-gated by
`ESHU_REDUCER_CLAIM_BENCH_DSN` or `ESHU_POSTGRES_DSN` so normal unit gates do
not require Postgres.

No-Regression Evidence: the #2253 slice adds the benchmark harness,
depth-parser coverage for its default 100k/1M depths, and fixture-shape
coverage that checks the benchmark workload-identity conflict keys against
`ReducerQueue.Enqueue`'s production derivation. It does not change reducer
claim SQL, queue DDL, worker counts, retry behavior, lease ownership, batch
size, or domain conflict semantics. Existing schema tests still verify the
reducer conflict claim index and graph readiness lookup index are present in
the bootstrap DDL and checked-in SQL mirror.

No-Observability-Change: reducer claim latency and failures remain visible
through the existing instrumented Postgres query spans, the
`eshu_dp_postgres_query_duration_seconds{store="queue"}` metric, reducer queue
status rows, and `/admin/status` backlog fields. The benchmark introduces no
runtime metric, span, log field, status field, queue stage, or worker knob.

`NewReducerGraphDrain` exposes a small read-side gate for code-call projection.
It checks whether reducer-owned graph domains are still pending, claimed,
running, or retrying so the local-authoritative NornicDB profile can avoid
overlapping code-call edge writes with semantic, inheritance, SQL relationship,
deployment, or workload graph materialization.

### Shared projection intents

`SharedIntentStore` stores durable shared projection intents for reducer-owned
edge domains. `ListPendingAcceptanceUnitIntents` reads one bounded
scope/unit/run slice, while
`HasCompletedAcceptanceUnitSourceRunDomainIntents` answers whether that exact
source run has already completed a chunk. Code-call projection uses the latter
lookup to process very large accepted units in chunks without retracting edges
written by earlier chunks from the same run.

The generic repo-refresh fence uses the narrower
`HasCompletedAcceptanceUnitSourceRunGenerationPartitionDomainIntents` lookup.
It requires a completion for the exact generation, bounded by acceptance
identity and partition key. Exact same-generation delivery reuses deterministic
intent IDs, and `SharedIntentStore.UpsertIntents` preserves an existing
`completed_at`, so it does not reopen the refresh or edge rows. A later
generation that reuses `source_run_id` still gets distinct IDs and must complete
its own refresh before its per-edge rows proceed. This preserves at-least-once
delivery without serializing unrelated repositories or partitions.

### Graph projection phase state

`GraphProjectionPhaseStateStore` persists `canonical_nodes_committed` phase
markers after `cypher.CanonicalNodeWriter.Write` completes. The
`NewGraphProjectionReadinessLookup` and `NewGraphProjectionReadinessPrefetch`
factories return `reducer.GraphProjectionReadinessLookup` implementations used
by edge-domain reducer workers to gate on canonical node availability before
writing edges.

### Projector queue claim ownership

`ProjectorQueue` claims source-local projection work in per-scope order with
`FOR UPDATE SKIP LOCKED`, stale-lease reclaim, and newer-generation
supersession. Long-running runtimes use the default unfiltered claim path.
One-shot bootstrap callers may set `WithClaimSourceSystem` so the claim query
only touches work from the source system that the bootstrap run owns.

No-Regression Evidence: `go test ./internal/storage/postgres -run
'TestProjectorQueueClaim(ScopesBySourceSystem|IncludesExpiredLeaseReclaimPredicates)'`
proves source-system scoped claims still preserve stale-lease reclaim,
same-scope ordering, and the unfiltered queue contract. `go test
./cmd/bootstrap-index -run
'TestBuildBootstrapProjector(ClaimsOnlyGitScopes|WiresPhasePublisherAndRepairQueue)'`
proves bootstrap-index wires the scoped claim path for git repository scopes.

Observability Evidence: no new telemetry was needed because queue claim latency
continues through `eshu_dp_queue_claim_duration_seconds{queue="projector"}`,
claim SQL continues through `InstrumentedDB` Postgres query spans and duration
metrics, and bootstrap projection logs still include `scope_id`,
`generation_id`, `source_system`, worker id, status, fact count, duration, and
failure class.

### Workflow control

`WorkflowControlStore` persists workflow coordinator control-plane state with
fenced claim leases. `ErrWorkflowClaimRejected` is returned when a claim
mutation is rejected because the current owner no longer holds the lease.
`CompleteClaim` can atomically replace a planned work item's phase identity
with a resolved reducer checkpoint tuple while the same claim fence is still
active. Terraform-state collectors use this to move from candidate planning
IDs to the real state-snapshot generation before workflow-run reconciliation
joins `workflow_work_items` to `graph_projection_phase_state`.

No-Regression Evidence: `go test ./internal/storage/postgres -run 'TestWorkflowControlStoreGuardedRun(SkipsOpenScheduledTarget|CreatesEligibleScheduledTarget|ComputesEligibleTargetsInOneQuery|LocksCollectorInstanceOnceForTargetBatch|SkipsSameRunTargetReplay|SkipsTerminalSameRunReplay)' -count=1`
proves scheduled target admission skips non-terminal duplicate targets, allows
eligible targets, and treats the same deterministic run id plus
`(collector_kind, collector_instance_id, scope_id, acceptance_unit_id)` as an
idempotency key during preserved-volume restarts. The same gate proves guarded
admission computes target eligibility with one `VALUES`-backed query per run
and acquires one sorted transaction-scoped Postgres advisory planning lock per
`(collector_kind, collector_instance_id)` batch instead of one lock per target.
It also proves newly discovered targets are not appended to an already-terminal
deterministic run id.

Observability Evidence: no new storage metric was needed because workflow
target suppression remains visible through workflow work-item rows,
workflow-run state, workflow completeness rows, and coordinator structured logs
with `reason=target_already_planned`, `planned_work_items`,
`enqueued_work_items`, `skipped_work_items`, and `trigger_kind`.

No-Regression Evidence: `go test ./internal/storage/postgres -run TestListOwnedPackageDependencyTargetsQueryIsActiveAndBounded -count=1` proves owned dependency target planning uses active Git dependency facts, the existing package dependency predicate, an ecosystem allowlist, preserved dependency source locations, and a bounded `LIMIT`. The broader touched-package proof ran `go test ./internal/coordinator ./internal/workflow ./internal/storage/postgres ./internal/collector/packageregistry/packageruntime ./internal/collector/vulnerabilityintelligence/vulnruntime ./cmd/workflow-coordinator ./cmd/collector-package-registry ./cmd/collector-vulnerability-intelligence -count=1`.

No-Regression Evidence: `go test ./internal/storage/postgres -run 'TestWorkflowControlStoreGuardedRunRetriesDeadlockTransaction|TestWorkflowControlStoreGuardedRunCreatesEligibleScheduledTarget' -count=1` proves guarded workflow run admission retries Postgres `40P01` deadlock failures without duplicating target work, while preserving the normal eligible-target insert path.

Observability Evidence: no new telemetry names were needed. Existing workflow coordinator `reconcile_total` / `reconcile_duration_seconds` outcomes, workflow run/work-item status rows, guarded-run error wrapping, and container restart counters expose whether planning is retrying, failing, or progressing after a database serialization/deadlock race.

Observability Evidence: no new storage metric was needed. The owned-target read
uses the existing instrumented Postgres query duration metric through the
workflow-coordinator wiring, while derived target outcomes remain visible in
workflow run scope payloads, workflow work-item statuses, collector claim
status, collector metrics, and `/api/v0/index-status`.

### AWS scan status

`AWSScanStatusStore` persists one row per AWS
`(collector_instance_id, account_id, region, service_kind)` tuple in
`aws_scan_status`. Scanner-side updates record `running`, `succeeded`,
`partial`, `credential_failed`, or `failed` along with bounded API call,
throttle, warning, and fact counts. The `collector-aws-cloud` command records a
separate commit status after the fenced ingestion transaction so operators can
distinguish scanner failures from commit failures.

### AWS runtime drift evidence

`PostgresAWSCloudRuntimeDriftEvidenceLoader` powers the
`aws_cloud_runtime_drift` reducer domain. It first loads `aws_resource` facts for
one AWS scope generation, then joins active `terraform_state_resource` facts by
an ARN allowlist derived from that generation. For state-backed ARNs it resolves
the `state_snapshot:<backend_kind>:<locator_hash>` owner through
`tfstatebackend.Resolver`, loads the owning config snapshot's
`terraform_resources`, and marks config present only when the Terraform address
matches. Missing backend ownership produces `unknown_cloud_resource` /
`unknown_management` evidence; ambiguous backend ownership or multiple active
state owners for the same ARN produces `ambiguous_cloud_resource` /
`ambiguous_management` evidence. Neither path treats unknown config as absent.

The AWS runtime drift findings reader uses the same bounded active fact read
shape and only closes result sets with the package-standard checked defer
pattern. No-Regression Evidence: `golangci-lint run ./...` catches unchecked
result-set closes, and `go test ./internal/storage/postgres` keeps the
Postgres store package compiling and exercising its existing storage contracts.
No-Observability-Change: the SQL text, filters, row counts, status surfaces, and
query instrumentation are unchanged; existing `InstrumentedDB` query spans and
duration metrics remain the operator signal for this read path.

### Webhook refresh triggers

`WebhookTriggerStore` persists provider webhook decisions in
`webhook_refresh_triggers`. Accepted triggers enter `queued`; ignored triggers
stay audit-only unless a later accepted delivery resolves the same refresh key,
which moves the row back to `queued`. `StoreTrigger` upserts on `refresh_key`
so dedupe follows the provider/repository/default-branch/target-SHA identity
even if the derived `trigger_id` algorithm changes. Claimers use
`FOR UPDATE SKIP LOCKED` in `received_at` order, then mark claimed rows
`handed_off` after the Git selector receives the targeted repository list or
`failed` with `failed_at`, `failure_class`, and `failure_message` when the
compatibility handoff cannot complete.

### Service materialization lineage and service-scope changed-since (#1943)

`service_materialization_generations` is the per-service generation lineage and
`service_evidence_snapshots` the generation-stable evidence snapshot the
service-scope changed-since delta diffs. The reducer commits them through
`reducer.PostgresServiceMaterializationWriter` (over the instrumented reducer DB
via `ServiceMaterializationBeginner`); the read path is
`StatusStore.ComputeServiceChangedSinceDelta`. This is additive: the existing
`reducer_service_catalog_correlation` fact and its `stable_fact_key` are
unchanged.

Conflict domain and concurrency: the conflict key is `service_id`. At most one
`status = 'active'` generation exists per service, enforced by the partial unique
index `service_materialization_generations_active_service_idx` (mirroring
`scope_generations_active_scope_idx`). One commit runs the new-generation insert,
the prior-active supersession, and the snapshot-row writes in a single
transaction, so a reader never observes zero or two active generations for a
service. The generation id is deterministic in the evidence set
(`md5`-fingerprinted), so an identical re-materialization inserts zero rows
(`ON CONFLICT (generation_id) DO NOTHING`) and skips supersession and snapshot
writes — a true no-op with no churn. A dropped owner is written as an
`is_tombstone = TRUE` snapshot row so the delta classifies it `retired`, never
silently absent. No serialization-as-fix: services partition by `service_id`, so
concurrent commits for different services never contend.

Cardinality and query cost: snapshot rows are one per `(generation_id,
service_evidence_key)`; ownership cardinality is the number of distinct owners
per service (small, single digits in practice). The delta is a FULL OUTER JOIN of
two single-generation row sets filtered by `generation_id` and
`is_tombstone`, served by `service_evidence_snapshots_diff_idx (generation_id,
evidence_family, service_evidence_key)`; sample reads are bounded by
`sample_limit + 1` and ordered by `service_evidence_key` through the same index.
The scope-resolution and prior-generation lookups are single-row index probes on
`service_materialization_generations`.

Performance Evidence: `go test ./internal/storage/postgres -run
'ComputeServiceChangedSinceDelta'` exercises the bounded counts + sample read
shape against the fake Queryer/Rows harness; `go test ./internal/reducer -run
'TestServiceMaterialization|TestBuildServiceOwnership'` proves the commit,
supersede, idempotent no-op, and tombstone write paths.
No-Regression Evidence: the reducer write path is additive and the existing
`reducer_service_catalog_correlation` contract is unchanged
(`go test ./internal/reducer -run 'TestServiceCatalogCorrelationHandlerLoads'`
and `TestPostgresServiceCatalogCorrelationWriterPersistsReducerFacts` stay green).
Observability Evidence: the lineage commit runs inside the already-instrumented
`service_catalog_correlation` reducer intent (`reducer.run` span, the
`ServiceCatalogCorrelations` counter, and `postgres.exec` spans over the
instrumented DB for every INSERT/UPDATE in the commit transaction). The read
path is wrapped by the `query.freshness_service_changed_since` span with
`service_id`, `since_generation_id`, `current_generation_id`, `changed_count`,
and `unavailable` attributes. No new parallel reducer counter was added; the
commit is an additive sub-step of an instrumented intent, so reusing its span and
counter keeps the operator signal coherent rather than fragmenting it.
