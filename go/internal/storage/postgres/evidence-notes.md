# storage/postgres Evidence Notes

Keep this file for scoped evidence that is too detailed for the package
orientation README.

## Code-Call Symbol Definition JSONB Guards (#3122)

No-Regression Evidence: `go test ./internal/storage/postgres -run
'TestFactStoreLoadActiveCodeCallSymbolDefinitionFacts' -count=1` failed before
the loader guarded non-array parsed definition fields, then passed after
`functions`, `classes`, `structs`, `interfaces`, and `type_aliases` are
expanded only when `jsonb_typeof(...) = 'array'`. The live Helm proof on the
public `eshu-hq/eshu` repository exercised 8,681 active file facts with 703
symbol-definition fact rows; before the fix `code_call_materialization`
dead-lettered with Postgres `SQLSTATE 22023`, and after the fix it succeeded
and emitted 139,352 `code_calls` shared intents. The change preserves the
existing active-generation, tombstone, symbol-key, ordering, and page-limit
predicates, and treats malformed or JSON-null definition fields as empty
candidate sets instead of widening graph truth.

No-Observability-Change: #3122 changes only the guarded JSONB expression inside
the existing active code-call symbol-definition fact read. It adds no table,
route, queue domain, worker, lease, runtime knob, graph write, metric
instrument, metric label, span name, or log field. Operators still diagnose
this path through existing reducer execution spans/counters, the `code call
materialization completed` log fields (`fact_count`,
`symbol_definition_fact_count`, `code_call_row_count`, `intent_row_count`, and
duration fields), reducer queue status, shared-intent backlog counts, and
existing Postgres query instrumentation.

## Function Summary Store (#2890)

No-Regression Evidence: `go test ./internal/parser/summary -count=1` and
`go test ./internal/storage/postgres -run 'Test(FunctionSummary|BootstrapDefinitionsIncludeFunctionSummaries)' -count=1`
prove the additive `function_summaries` DDL is registered in bootstrap, mirrors
its SQL file, persists `summary.Snapshot` rows through stale-write-guarded
`ON CONFLICT (function_id) DO UPDATE`, rejects blank repository components
before write so cross-repo summary rows cannot silently collide, and reloads
snapshots into `summary.Load` without recomputing unchanged effects. The table
is keyed by generation-independent `FunctionID`; it adds no graph truth,
reducer queue domain, API/MCP route, parser emission default, or canonical
projection.

No-Observability-Change: #2890 adds no worker, route, queue claim, lease,
graph write, metric instrument, metric label, span name, status field, runtime
default, or provider call. Store calls remain covered by the existing
Postgres instrumentation wrapper (`postgres.exec`, `postgres.query`, and
`eshu_dp_postgres_query_duration_seconds{store=...,operation=...}`) when the
caller wraps the `ExecQueryer`; reducer/runtime activation remains tracked by
#2823 follow-up work.

No-Regression Evidence: #2947 adds full-snapshot cleanup for
`code_function_summary` materialization. The projector now queues the summary
domain from the full-only `code_dataflow_scanned` marker, including marker-only
generations where the value-flow gate produced zero summaries. The reducer
keeps delta generations on the existing upsert path, but full-snapshot intents
replace only the target repo's `function_summaries` rows after filtering that
repo out of the durable baseline and recomputing the current snapshot. The
store replacement stage deletes stale rows with `repo = $1 AND updated_at <=
$2`, then upserts current rows in the same transaction, so a rollback restores
the pre-replacement state and stale writers cannot delete newer summaries. The
operator-facing row counts are the reducer completion log fields:
`full_snapshot`, `repo_id`, `function_count`, and the existing result summary's
persisted-row count. `go test ./internal/projector -run
TestBuildCodeFunctionSummaryReducerIntent -count=1`, `go test
./internal/reducer -run 'TestCodeFunctionSummaryHandler(Replaces|Preserves)'
-count=1`, and `go test ./internal/storage/postgres -run
TestFunctionSummaryStoreReplaceSnapshot -count=1` cover marker-only cleanup,
delta no-delete behavior, full snapshot delete/rename pruning, empty snapshots,
transaction use, repo validation, and timestamp-guarded deletes.

No-Observability-Change: #2947 adds no metric instrument, metric label key,
span name, worker, queue domain, lease, route, graph write, runtime knob, or
status field. Operators diagnose the cleanup through the existing reducer
execution span/counters, durable reducer queue rows, the updated reducer
completion log fields listed above, and instrumented Postgres exec/query spans
when the store is wrapped by the existing Postgres instrumentation.

## Admission Decision Evidence Bounds (#2694)

No-Regression Evidence: `go test ./internal/query -run 'TestAdmissionDecision|TestOpenAPISpecIncludesAdmissionDecisions' -count=1` and `go test ./internal/storage/postgres -run 'TestAdmissionDecisionStore|TestAdmissionDecisionSchema|TestAdmissionDecisionStates' -count=1` prove `admission_decisions.list` now rejects unsupported lightweight profiles before store reads, requests `limit+1` evidence rows per decision, reports `evidence_limit` and `evidence_truncated`, and pushes the per-decision evidence cap into the Postgres read query. The list page limit remains capped at 200 decisions and the embedded evidence preview is capped at 20 rows per decision.

No-Observability-Change: the fix adds no route, worker, queue, graph write, metric, span, runtime default, or new high-cardinality label. Operators continue to diagnose the read path through existing HTTP route attribution, query truth envelopes, and instrumented Postgres query spans/`eshu_dp_postgres_query_duration_seconds` when the query store is wrapped by the existing Postgres instrumentation.

## Search Vector Payload Storage (#2594)

No-Regression Evidence: `go test ./internal/storage/postgres -run 'TestEshuSearchVectorValue|TestBootstrapDefinitionsIncludeEshuSearchVectorValues|TestBootstrapDefinitionsAreOrderedAndComplete|TestBootstrapDefinitionsIncludeEshuSearchVectorMetadata' -count=1` failed before `EshuSearchVectorValueStore` and `eshu_search_vector_values` bootstrap DDL existed, then passed after adding idempotent vector payload upserts, active-generation readback, finite-value and dimension validation, deterministic document ordering, and bounded list limits. The table is additive and does not change API, MCP, reducer graph writes, BM25 search reads, hosted providers, credentials, egress, or canonical graph truth.

No-Observability-Change: #2594 adds no route, worker, queue domain, graph write,
metric name, metric label, runtime default, or API/MCP response field. Store
calls remain covered by the existing Postgres instrumentation wrapper
(`postgres.exec`, `postgres.query`, and
`eshu_dp_postgres_query_duration_seconds{store=...,operation=...}`) when callers
wrap the `ExecQueryer`; ANN serving and semantic/hybrid API use are left to
follow-up issues.

No-Regression Evidence: `go test ./internal/searchvector ./internal/storage/postgres
-run 'TestBuilderPagesThroughAllActiveDocuments|TestBuilderPersistsReadyVectorsForActiveDocuments|TestBuilderRecordsEmbeddingFailureAsBoundedMetadata|TestEshuSearchVectorValueStoreListsOnlyActiveGeneration|TestEshuSearchVectorValueStoreClampsActiveListLimit'
-count=1` failed before vector builds paged through all active documents and
before vector value reads were gated by ready metadata with matching content
hash, then passed. The change keeps vector payload rows as derived read-model
state and prevents failed or stale metadata from being served by `ListActive`.

No-Observability-Change: this follow-up adds no route, worker, queue domain,
graph write, metric name, metric label, runtime default, or API/MCP response
field. Existing builder result counts and instrumented Postgres query/exec
spans remain the operator-facing signals for vector build progress and read
state.

## Reducer Claim Readiness-Gate Benchmark (#2529)

Benchmark Evidence: `BenchmarkReducerQueueClaimReadinessGateGrowth` seeds the
existing reducer claim query with readiness-gated graph-write domains and
`graph_projection_phase_state` rows. The benchmark varies queue depth,
readiness row count, and gated-domain count through
`ESHU_REDUCER_CLAIM_READINESS_BENCH_CASES` values formatted as
`queue_depth:phase_rows:gated_domain_count`; it uses
`ESHU_REDUCER_CLAIM_BENCH_DSN` or `ESHU_POSTGRES_DSN` and skips when neither is
set.

Local Compose measurement on Postgres 18-alpine, Darwin arm64, Apple M4 Pro,
run with `go test ./internal/storage/postgres -run '^$' -bench
BenchmarkReducerQueueClaimReadinessGateGrowth -benchtime=3x -count=1` and
reduced cases `1000:1000:1,1000:5000:4`: `queue_1000_phase_1000_domains_1`
measured `84497208 ns/op`, `17968 B/op`, `102 allocs/op`;
`queue_1000_phase_5000_domains_4` measured `188810236 ns/op`, `17957 B/op`,
`102 allocs/op`.

No-Regression Evidence: this slice adds benchmark seed and benchmark tests only.
It does not change production claim SQL, queue status transitions, worker
counts, lease timing, graph writes, API/MCP reads, runtime defaults, or
reducer domain handlers. The benchmark uses the existing `ReducerQueue.Claim`
path and resets the claimed row between iterations.

No-Observability-Change: no runtime signal changes. Operators still diagnose
claim latency and contention through existing Postgres query spans and
`eshu_dp_postgres_query_duration_seconds{store="queue",operation="read"}`,
queue status, failure class, retry/dead-letter state, and reducer logs.

## Reducer Claim Bounded Readiness Lookup (#2587)

Benchmark Evidence: `BenchmarkReducerQueueClaimReadinessGateGrowth` now runs
the single reducer claim path through one data-shaped readiness requirements
lookup shared with batch claim and status blockage reporting, replacing the
prior per-domain predicate branches. Local Compose measurement on Postgres
18-alpine, Darwin arm64, Apple M4 Pro, run with
`ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:15432/eshu?sslmode=disable
ESHU_REDUCER_CLAIM_READINESS_BENCH_CASES=1000:1000:1,1000:5000:4 go test
./internal/storage/postgres -run '^$' -bench
BenchmarkReducerQueueClaimReadinessGateGrowth -benchtime=3x -count=1`:
`queue_1000_phase_1000_domains_1` measured `15141958 ns/op`, `13872 B/op`,
`102 allocs/op`; `queue_1000_phase_5000_domains_4` measured `14127125 ns/op`,
`13861 B/op`, `102 allocs/op`.

No-Regression Evidence: focused reducer readiness tests prove missing
readiness still holds pending and retrying work, multi-phase EC2 and
security-group domains still require every phase, batch claims still re-check
the per-conflict-key representative, and `/admin/status` still emits bounded
readiness blockage keys. The claim update, lease owner, attempt count,
retry/dead-letter filtering, expired-claim replay, and conflict-domain fencing
SQL are unchanged.

No-Observability-Change: no new metric, span, log, route, worker, lease, batch
size, or runtime default is added. Operators continue to diagnose claim latency
and readiness waits through existing Postgres query spans,
`eshu_dp_postgres_query_duration_seconds{store="queue",operation="read"}`,
queue status, bounded `/admin/status` blockage rows, failure class,
retry/dead-letter state, and reducer logs.

## Resource Reducer Conflict Policy (#2754)

No-Regression Evidence: `go test ./internal/storage/postgres -run
'TestReducerConflictDomainKey(ClassifiesResourceMaterializationDomains|RejectsRawProviderLocators)|TestReducerResourceConflictPolicyCoversIssue2754Domains|TestClaimBatchFencesSameConflictCandidates|TestReducerClaimBenchmarkWorkShapeMatchesReducerConflictDerivation'
-count=1` failed before resource materialization domains had an audited
safe/risky/blocked conflict policy, then passed after adding versioned hashed
resource conflict keys. `aws_resource_materialization` is the only promoted
resource-node conflict family in this slice because its handler writes
idempotent CloudResource nodes and does not scope-wide retract. GCP, Azure,
EC2, Kubernetes, and security-group node materializers remain risky
resource-scope fallbacks until partition-filtered load/write proof exists.
Relationship, posture, IAM, S3, RDS, Kubernetes-correlation, and
security-group reachability domains stay blocked behind the explicit
resource-scope fallback because their handlers still use scope-wide load,
readiness, write, or retract semantics.

No-Observability-Change: no route, worker, lease duration, retry policy,
metric, span, or runtime default changed. Operators see the policy through the
existing durable `fact_work_items.conflict_domain` and hashed `conflict_key`
columns plus `/admin/status` queue blockage rows. Conflict keys never copy raw
provider locators, paths, credential-shaped values, provider excerpts, or IP
address-shaped values; unsafe AWS resource-node inputs fall back to the hashed
resource-scope fence.

## Tenant Workspace Grants (#2047)

No-Regression Evidence: `go test ./internal/storage/postgres -run 'Test(BootstrapDefinitionsIncludeTenantWorkspaceGrants|TenantWorkspaceGrantStore)' -count=1` failed before `TenantWorkspaceGrantStore` and `tenant_workspace_grants` bootstrap DDL existed, then passed after adding idempotent tenant/workspace/scope/repository grant upserts, active bounded grant reads, tombstone/expiry/effective-time predicates, and the privacy guard that prevents raw display names or credential-shaped columns in the schema. The tables are additive and do not change existing fact, queue, graph, API, MCP, collector, or workflow behavior.

No-Observability-Change: #2047 adds no route, worker, queue domain, graph write,
metric name, metric label, runtime default, or API/MCP response field. Operators
diagnose store calls through the existing Postgres instrumentation wrapper
(`postgres.exec`, `postgres.query`, and
`eshu_dp_postgres_query_duration_seconds{store=...,operation=...}`) once callers
wrap the store's `ExecQueryer`; runtime enforcement and status/audit surfacing
are left to follow-up enforcement issues.

## Scoped API Tokens (#1852)

No-Regression Evidence: `go test ./internal/storage/postgres -run 'Test(BootstrapDefinitionsIncludeScopedAPITokens|ScopedAPITokenStore)' -count=1` failed before `ScopedAPITokenStore` and `scoped_api_tokens` bootstrap DDL existed, then passed after adding hash-only token upserts, active tenant/workspace bounded lookup, expiry/revocation predicates, and validation that rejects blank token hashes. The table is additive and does not store raw bearer tokens, tenant names, workspace names, provider credentials, API/MCP response fields, or graph truth.

No-Observability-Change: #1852 scoped-token registry plumbing adds no route,
worker, queue domain, graph write, metric name, metric label, runtime default,
or API/MCP response field. Runtime wiring and per-request enforcement are
follow-up work; when callers opt into the store through an instrumented
Postgres adapter, existing query/exec spans and
`eshu_dp_postgres_query_duration_seconds` cover the SQL.

## Workflow Tenant Grant Fencing (#2050)

No-Regression Evidence: `go test ./internal/workflow ./internal/coordinator ./internal/storage/postgres ./cmd/workflow-coordinator -count=1` proves workflow work items preserve optional tenant, workspace, subject-class, and policy-revision identity; coordinator planning denies configured hosted work without an active matching grant; guarded target eligibility treats the tenant boundary as part of duplicate convergence; and claim heartbeat/complete SQL re-checks active, non-tombstoned, non-expired tenant scope grants before stale hosted claims can finish.

Observability Evidence: the change adds no high-cardinality metrics. Denied
planning uses the existing workflow coordinator structured log path with bounded
reason `tenant_scope_missing_or_stale_policy` plus planned/authorized/denied
counts, while existing `eshu_dp_workflow_coordinator_reconcile_total`,
`eshu_dp_workflow_coordinator_reconcile_duration_seconds`,
`workflow_runs`, `workflow_work_items`, `workflow_claims`, and
`eshu_dp_postgres_query_duration_seconds{store="tenant_workspace_grants"}` show
queue progress, duplicate convergence, grant reads, and stale-claim rejection.

## Claimed Fact Commit Tenant Grant Fencing (#2059)

No-Regression Evidence: `go test ./internal/collector ./internal/collector/scannerworker ./internal/storage/postgres -run 'TestClaimedServiceClaimsHeartbeatsCommitsAndCompletes|TestServiceProcessesClaimAndCommitsSourceFacts|TestIngestionStoreCommitClaimedScopeGeneration|TestValidateClaimMutationTenantBoundary|TestHeartbeatWorkflowClaimQueryLocksActiveTenantGrant|TestWorkflowControlStoreHeartbeatClaimRejectsInactiveTenantGrant|TestCompleteWorkflowClaimQueryChecksActiveTenantGrant' -count=1` proves claim-aware collectors carry hosted tenant boundaries into commit mutations and claimed fact commits reject revoked, stale-policy, deleted-workspace, or expired grants before fact or projector work rows are written.

No-Observability-Change: #2059 adds no metric name, label, worker, queue, route,
runtime default, or API/MCP field. Denials return the existing bounded
`ErrWorkflowClaimRejected` path and remain visible through workflow retry or
dead-letter state plus existing Postgres query/exec spans and
`eshu_dp_postgres_query_duration_seconds`.

## Collector Backpressure Status (#2750)

No-Regression Evidence: `go test ./internal/status ./internal/storage/postgres
-run 'Test(RenderStatusIncludesCollectorBackpressure|ReadWorkflowCollectorBackpressureStatus|ReadCoordinatorSnapshotIncludesCollectorBackpressure|ReadCoordinatorSnapshotHandlesNullableDeactivatedAtAndCreatedAtBacklogFallback|ReadCoordinatorSnapshotClampsNegativeOldestPendingAge)'
-count=1` proves `/admin/status` text and JSON render bounded
`coordinator.collector_backpressure` rows, `StatusStore` wires the rows into the
coordinator snapshot, workflow retry/terminal/expired counts come from
`workflow_work_items`, collector-generation dead letters come from
`collector_generation_dead_letters`, and the SQL does not project scope ids,
source-run ids, generation ids, acceptance-unit ids, payloads, or raw failure
messages.

No-Observability-Change: #2750 adds no route, worker, queue mutation, lease
mutation, runtime knob, metric name, metric label, span name, log field, or
high-cardinality telemetry value. Operators diagnose provider throttling,
retry storms, terminal collector failures, expired claims, and recovery pressure
through the existing `/admin/status` text/JSON surface plus existing
`workflow_work_items`, `workflow_claims`, `collector_generation_dead_letters`,
and Postgres query spans / `eshu_dp_postgres_query_duration_seconds`.

## Reducer Endpoint Readiness Retry (#1391)

No-Regression Evidence: `go test ./internal/storage/postgres -run 'TestReducerQueueFailDefersSecretsIAMEndpointReadinessPastAttemptBudget|TestReducerQueueClaimDoesNotCountSecretsIAMEndpointReadinessDefers|TestClaimBatchDoesNotCountSecretsIAMEndpointReadinessDefers' -count=1` failed before over-budget `secrets_iam_endpoint_not_ready` dead-lettered and both claim paths consumed `attempt_count`; it passed after the class became a deferred retry and both claim SQL shapes preserved the attempt budget.

Observability Evidence: the change adds no metric or status field. Existing
queue status, latest-failure, queue-blockage, and
`eshu_dp_postgres_query_duration_seconds{store="queue"}` signals keep exposing
retrying/dead-letter counts, `visible_at` backoff, claim latency, and the
specific `failure_class=secrets_iam_endpoint_not_ready` needed to diagnose
blocked cross-scope endpoint readiness.

## Storage README Archived Evidence

Incident freshness store coverage includes `go test ./internal/storage/postgres -run 'TestIncidentFreshness|TestBootstrapSQLFilesMirrorDefinitions' -count=1`. The queue keeps at-least-once webhook delivery coalesced by source freshness key, preserves claimed rows during duplicate upserts, and uses `FOR UPDATE SKIP LOCKED` for concurrent coordinator handoff without changing fact emission, reducer lanes, worker counts, or graph writes. Incident freshness storage is wrapped by `InstrumentedDB` as `store="incident_freshness_triggers"` in the webhook listener and workflow coordinator; existing query-duration metrics and spans expose read/write latency without adding delivery IDs, issue keys, incident IDs, URLs, or provider payload fields to metric labels.

Incident-routing evidence loading is covered by `go test ./internal/storage/postgres -run 'IncidentRoutingEvidence' -count=1`. The read path stays bounded to one scope/generation fact query plus one service-name allowlisted `content_entities` query and adds no table, schema migration, queue behavior, worker count, or graph query.

Workflow terminal failure mutation coverage includes `go test ./internal/storage/postgres -run TestWorkflowControlStoreFailClaimTerminalUsesDensePostgresParameters -count=1` and a remote Postgres integration run of `TestWorkflowControlStoreIntegrationFailClaimTerminalRecordsFailureWithoutParameterHole`. The change preserves claim fencing, retryable requeue `visible_at`, claim ordering, worker counts, and workflow status semantics. Existing `workflow_work_items.last_failure_class`, `workflow_claims.failure_class`, fenced mutation errors, collector logs, and `/api/v0/index-status` continue to expose terminal workflow failures and active claim counts; no new telemetry dimension was required.

AWS relationship readiness gating is covered by `go test ./internal/storage/postgres -run 'TestReducerQueueClaim(GatesAWSRelationshipsOnCanonicalCloudResourceReadiness|WaitsForAWSRelationshipReadinessBehavior|WaitsForRetryingAWSRelationshipReadinessBehavior|AWSRelationshipAlreadyReadyBehavior)|TestClaimBatchGatesAWSRelationshipsOnCanonicalCloudResourceReadiness|TestReducerQueueBlockagesReportAWSRelationshipReadinessWait' -count=1`. The same CloudResource readiness gate also covers RDS posture, S3 internet-exposure, and EC2 internet-exposure readiness. The claim path keeps pending and retrying CloudResource-consuming reducer rows unclaimed until the matching `cloud_resource_uid` / `canonical_nodes_committed` phase exists, then makes the same row claimable without changing worker counts, retry delays, or conflict-key fencing. `/admin/status` queue blockages include bounded readiness conflict keys while existing queue gauges and domain backlog rows expose pending, retrying, in-flight, and oldest-age counts without adding a high-cardinality metric label.

Owned dependency target selection is covered by `go test ./internal/storage/postgres -run 'TestListOwnedPackageDependencyTargetsQuery|TestOwnedPackageDependencyTargetLimit' -count=1`. The query remains scoped to active Git dependency facts, adds package-level selection for package-registry derivation, keeps package-version selection for vulnerability derivation, and rotates bounded reads by caller-provided offset. Existing Postgres query-duration telemetry, workflow-run `requested_scope_set`, workflow work-item status rows, collector claim status, and `/api/v0/index-status` expose whether derived targets were planned, repeated, completed, retried, or failed. The target reader adds no new metric labels and does not include package names or versions in telemetry labels.

`go test ./internal/storage/postgres -run 'List.*AdvisoryTargets' -count=1` proves installed advisory target SQL stays active-generation scoped, bounded, ecosystem-filtered, and attached to SBOM subject evidence before the coordinator admits exact OSV targets. Installed advisory target readers use the existing `InstrumentedDB` query spans and `eshu_dp_postgres_query_duration_seconds` histogram. Store labels stay bounded to the configured store name and operation; package names, versions, PURLs, document IDs, subject digests, and advisory payloads are not metric labels.

## Cloud Inventory Evidence Loader (issues #1997, #1998)

`PostgresCloudInventoryEvidenceLoader` is the concrete
`reducer.CloudInventoryEvidenceLoader` that backs `DomainCloudInventoryAdmission`.
It reads the three provider inventory source fact kinds (`aws_resource`,
`gcp_cloud_resource`, `azure_cloud_resource`) for one scope generation and maps
each provider payload into the shared admission record. The admission handler
owns identity resolution, evidence folding, and the canonical
`reducer_cloud_resource_identity` upsert; the loader stays read-only so a stale
generation it happens to read is still superseded before any canonical write.

No-Regression Evidence: `go test ./internal/storage/postgres -run 'TestPostgresCloudInventoryEvidenceLoader|TestCloudInventoryAdmissionEndToEnd' -count=1` proves the loader reads exactly the three inventory source fact kinds for one `(scope_id, generation_id)`, drops blank-identity and undecodable rows, rejects a blank scope or generation, and that the end-to-end loader -> admission -> writer path is idempotent under retries and concurrent workers.

No-Observability-Change: the loader adds no table, route, worker, queue domain,
graph write, metric name, or metric label. The admission handler already emits
bounded cloud-inventory admission counters and the canonical
`reducer_cloud_resource_identity` read-model payload; the Postgres
instrumentation wrapper still emits `eshu_dp_postgres_query_duration_seconds`
for the load.

## Terraform Backend Interpolation (#2400)

Terraform-state backend discovery reads active Git parser facts through the
same bounded generation joins as other graph discovery. Repo-scoped discovery
merges `terraform_backends`, `terraform_variables`, and `terraform_locals` from
the requested repo's active generation; filter and canonical resolver reads
first identify active generations that contain backend blocks, then bring in
variable/local rows from those same generations. S3 backend attributes can
recover exact same-module variable/local literal values without evaluating
Terraform or widening to repository-global names. Unresolved expressions,
duplicate names, `module.*`, and `terraform.workspace` remain non-candidates;
issue #2438 owns a separate warning/evidence channel for those lower-confidence
observations.

No-Regression Evidence: `go test ./internal/storage/postgres -run 'TestTerraformStateBackendFactReader|TestPostgresTerraformBackendQuery|TestTerraformStatePriorSnapshotReader|TestTerraformStateGitReadinessChecker' -count=1`
proves literal backend candidates, same-module interpolation recovery across
separate backend/variable/local file facts, filters, canonical locator-hash
ownership, prior snapshot metadata, and readiness still agree.

No-Observability-Change: #2400 adds no table, queue, graph write, metric, span,
or log shape. Exact candidate counts continue through the existing
Terraform-state discovery metrics.

## Reducer Batch Per-Domain Claim Fairness (#3385)

The batch reducer claim ordered candidates by a single global `updated_at ASC`.
When one lane claims several domains (the `collector-reducer` lane claims 14,
including the AWS cloud producers `cloud_inventory_admission`,
`aws_resource_materialization`, `aws_cloud_runtime_drift`), a high-volume domain
with an older, continuously regenerated backlog (`supply_chain_impact`,
`package_source_correlation`) kept every batch slot. The AWS producer rows were
always newer, so they sat `status='pending', attempt_count=0` indefinitely,
`CloudResource` nodes never materialized, and `GET /api/v0/cloud/resources`
returned 0. The fix adds a per-domain fairness rank to
`claimReducerWorkBatchQuery`: each eligible conflict-group representative is
ranked by its age WITHIN its own domain (correlated count of strictly-older
same-domain representatives; a window function cannot be combined with
`FOR UPDATE SKIP LOCKED`), and the final `ORDER BY` places that rank before the
global `updated_at`. This round-robins ready domains so each contributes its
oldest representative before any contributes a second. Conflict fencing and the
single same-group representative are unchanged, so per-conflict-key concurrency
is identical; only which ready rows a batch takes changes.

Performance Evidence:
`ESHU_REDUCER_FAIRNESS_PROOF_DSN=<dsn> go test ./internal/storage/postgres -run
'TestClaimBatchDoesNotStarveNewerDomainsBehindOlderBacklog' -count=1` fails on
the pre-fix query (a 16-item batch over a 40-row older backlog plus 8 newer
starved-domain rows claims 0 starved-domain items) and passes after the fairness
rank lands. Against the live local stack (`collector-reducer` lane allowlist, 14
domains, batch size 16, NornicDB backend, `aws:%` scopes), one batch returned
only `package_source_correlation`/`supply_chain_impact` before the change and
returned `aws_cloud_runtime_drift`, `aws_resource_materialization`, and
`workload_cloud_relationship_materialization` rows alongside the others after the
change. Added cost is one bounded correlated count per candidate over the same
already-indexed reducer conflict columns
(`fact_work_items_reducer_conflict_claim_idx`); no new scan over an unindexed
column and no change to the locked-row footprint (still `LIMIT $8`).

No-Observability-Change: #3385 adds no table, queue, graph write, metric, span,
or log shape. The existing `eshu_dp_queue_depth` / `eshu_dp_queue_oldest_age_seconds`
gauges already expose per-stage backlog and oldest age, which now drains across
all lane domains instead of one.
