# storage/postgres Evidence Notes

Keep this file for scoped evidence that is too detailed for the package
orientation README.

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
