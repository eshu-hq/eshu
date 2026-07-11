# storage/postgres Evidence Notes

Keep this file for scoped evidence that is too detailed for the package
orientation README.

## Secrets/IAM Trust-Chain Loader Typed Decode (#4796)

No-Regression Evidence: `go test ./internal/storage/postgres -run
TestFactStoreLoadSecretsIAMTrustChainEvidenceClassifiesMalformedAnchor
-count=1` failed before `LoadSecretsIAMTrustChainEvidence` routed anchor
extraction through factschema decoders, returning nil for an
`aws_iam_trust_policy` payload missing required `account_id`; it passed after
the loader returned a non-retryable `input_invalid` decode error naming that
field. `go test ./internal/storage/postgres -run
'TestFactStoreLoadSecretsIAMTrustChainEvidence|TestServiceIncidentEvidence'
-count=1` and `go test ./internal/storage/postgres -count=1` prove the previous
service-account, role ARN, Vault policy/path, GCP principal/email/subject
anchor expansion still holds with contract-shaped fixtures, and the incident
SQL payload literals stay locked to the `incident/v1` JSON tags. The
secrets/IAM SQL predicate, page size, active-generation join, tombstone filter,
ordering, expansion-pass cap, queue domains, worker counts, and reducer claim
latency path are unchanged.

Gate Evidence: `ESHU_POSTGRES_PORT=25432 bash
scripts/verify-golden-corpus-gate.sh` passed after an initial default-port run
found 15432 already allocated by another worktree's Compose stack; the rerun
reported residual=0, dead_letter=0, shared_projection nonterminal=0, demo q1-q5
PASS, summary 413 pass / 0 required-fail / 0 advisory-warn, and a 92s
golden-corpus elapsed time under the 1800s ceiling. `make pre-pr` passed
gofumpt, golangci-lint, build, vet, changed-package tests, file cap, package
docs, exactness/telemetry gates, coverage report generation, performance
evidence, and the scoped race lane.

No-Observability-Change: #4796 adds no table, index, queue domain, worker,
lease, runtime knob, route, metric, span, or log field. Operators continue to
diagnose this path through existing Postgres query spans and
`eshu_dp_postgres_query_duration_seconds`, reducer queue status and failure
class, the reducer's `input_invalid` dead-letter handling, and
`SecretsIAMTrustChainLoadStats` seed/loaded/truncated counts.

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

## Identity Subject Schema (#3454)

No-Regression Evidence: `go test ./internal/storage/postgres -run 'TestBootstrapDefinitionsIncludeIdentitySubjects|TestIdentitySubjectStoreEnsureSchemaUsesDefinitionSQL|TestBootstrapDefinitionsAreOrderedAndComplete|TestBootstrapSQLFilesMirrorDefinitions' -count=1` failed before `IdentitySubjectStore` and the `identity_subjects` bootstrap DDL existed, then passed after adding the idempotent schema definition and mirror SQL file. The tables are additive and dormant: they model users, provider configs and revisions, external subject links, email history, local credential hashes, MFA factor handles, tenant memberships, roles, grants, sessions, service principals, service-principal role assignments, and token metadata without changing existing shared-token, scoped-token, fact, queue, graph, API, MCP, collector, workflow, or dashboard behavior.

No-Observability-Change: #3454 adds no route, worker, queue domain, graph write,
metric name, metric label, span name, runtime default, Helm value, API/MCP
response field, dashboard surface, or enforcement path. Future callers that opt
into `IdentitySubjectStore` can use the existing `InstrumentedDB` wrapper for
`postgres.exec` spans and `eshu_dp_postgres_query_duration_seconds`; until then
the schema is diagnosable through bootstrap/apply failures and ordinary
Postgres catalog inspection only.

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

## Refresh-Intent Batch Starvation Fix (#3451)

Performance Evidence: Before the fix, the `SharedProjectionRunner` for
`inheritance_edges` and `sql_relationships` was caught in a permanent stall.
Baseline (live local stack, `eshu-resolution-engine-1`, Up 40+ minutes):
`inheritance_edges` pending: 12,227; `sql_relationships` pending: 2,804;
`shared_projection_intents` total pending: 15,031; active generations: 982;
completed generations: 3; zero completions since reducer restart at ~22:00 UTC
(prior process last completed at 02:04 UTC, 28,620+ deferred cycles later).

Root cause: `listPendingDomainPartitionIntentsSQL` ordered rows by
`(created_at ASC, intent_id ASC)`. When the reducer emits both a per-repo
`refresh` intent and hundreds of per-edge `upsert` intents at the same
millisecond timestamp, the upsert rows' partition keys sorted before the refresh
rows' lexicographically. With `batchLimit=200`, the 200-slot window filled
entirely with upsert rows and pushed every refresh row past position 200 — the
first refresh row in partition 1 landed at position 226. The refresh row never
entered any batch. The repo-wide retract fence
(`HasCompletedAcceptanceUnitSourceRunPartitionDomainIntents`) checks whether the
refresh intent's `completed_at IS NOT NULL`; with the refresh row never
processing, the fence never opened, so all per-edge upsert rows deferred
indefinitely on every cycle.

Fix: add a stored generated BOOLEAN column `is_refresh_intent` defined as
`COALESCE(payload->>'action' = 'refresh', false)`. The column is computed by
Postgres on every INSERT/UPDATE from the existing `payload` JSONB field — no
write-path changes required. A new partial index
`shared_projection_intents_domain_partition_refresh_first_idx` on
`(projection_domain, created_at ASC, is_refresh_intent DESC, intent_id ASC)
WHERE completed_at IS NULL AND partition_hash IS NOT NULL` makes the sort
order index-backed. `listPendingDomainPartitionIntentsSQL` changes
`ORDER BY created_at ASC, intent_id ASC` to
`ORDER BY created_at ASC, is_refresh_intent DESC, intent_id ASC`.
`DESC` on a boolean puts `true` (refresh rows) before `false` (upsert rows)
within the same `created_at` group. The refresh row moves to position 1 in its
partition, enters the first batch, completes normally, and opens the fence.
Per-edge upsert rows then drain on subsequent cycles at full batch throughput.
Input shape: 66 refresh intents + 12,227 upsert intents for `inheritance_edges`;
5 refresh + 2,804 upsert for `sql_relationships`, across 8 partitions each.

The generated column approach replaces a JSONB CASE expression sort key that
the existing partial index could not serve. With the stored column the planner
can use an ordered index scan instead of a full sort on large pending backlogs.
Verified via `EXPLAIN ... SET enable_sort=off`: planner picks
`Index Scan using shared_projection_intents_domain_partition_refresh_first_idx`.

No-Regression Evidence: `TestListPendingDomainPartitionIntentsRefreshFirst`
failed before the fix (first row was an upsert, query lacked the refresh-first
sort key) and passed after. `go test ./internal/storage/postgres/ -count=1` →
passed. `go test ./internal/reducer/ -count=1` → passed. `go vet
./internal/storage/postgres/ ./internal/reducer/` → clean. `golangci-lint run
./internal/storage/postgres/ ./internal/reducer/` → 0 issues. The change is
backward-compatible: domains without refresh intents emit `is_refresh_intent=false`
for all rows, preserving the prior `created_at, intent_id` order.

No-Observability-Change: #3451 adds no queue domain, graph write, metric
instrument, metric label, span name, worker, lease, route, or log field. It adds
one stored generated column and one partial index on an existing table. Operators
diagnose the batch-starvation condition through the existing
`shared projection deferred per-edge rows behind repo refresh fence` log line
(count drops to zero once the fence opens), `shared projection completed intents`
log field, the `eshu_dp_shared_projection_intents_processed_total` counter, and
`pending_projection` DB counts (`SELECT projection_domain, COUNT(*) FROM
shared_projection_intents WHERE completed_at IS NULL GROUP BY projection_domain`).

## Refresh-Intent Starvation: Later-Timestamp General Case (#3474)

Performance Evidence: After #3451/#3467 deployed, the shared projection still
did not drain. Baseline (live local stack, `eshu-resolution-engine-1`, Up 10+
minutes after #3467 restart): `inheritance_edges` pending: 12,227;
`sql_relationships` pending: 2,804; `shared_projection_intents` total pending:
15,031; active generations: 982; completed generations: 3.

Root cause: #3451's fix assumed refresh and upsert intents share the same
`created_at` millisecond, making `is_refresh_intent DESC` a tiebreaker that
promotes refresh to position 1. The live data disproves that assumption.
Querying partition 5 of `inheritance_edges` (5 refresh + 1,475 upsert pending):
refresh intents rank at positions 979, 980, 1,176, 1,475, 1,477 under the
`(created_at ASC, is_refresh_intent DESC)` ordering — far beyond `batchLimit`.
The 1,475 upsert edges were created at 01:43 UTC; the 5 refresh intents were
created at 01:48–11:38 UTC. With `created_at ASC` as the primary key, the
batch head fills entirely with the oldest upsert edges (confirmed: first 100
rows under old ordering = 100% upsert). Those edges defer every cycle (fence
closed), stay pending, and re-fill the same head indefinitely. The refresh
intents are never selected, the fence never opens, and pending_projection does
not drain.

This is the general case the #3451 same-timestamp tiebreaker cannot rescue:
when deferred head edges are older than the paired refresh intent, no
tiebreaker on the secondary key can help.

Fix: promote `is_refresh_intent DESC` to the PRIMARY sort key (before
`created_at`) in both `listPendingDomainPartitionIntentsSQL` (hashed lane) and
`listPendingDomainUnhashedIntentsSQL` (legacy NULL-hash lane). Drop index
`shared_projection_intents_domain_partition_refresh_first_idx` (created_at
primary) and replace with
`shared_projection_intents_domain_partition_refresh_primary_idx`
`(projection_domain, is_refresh_intent DESC, created_at ASC, intent_id ASC)
WHERE completed_at IS NULL AND partition_hash IS NOT NULL`.
Same for the unhashed lane:
`shared_projection_intents_domain_unhashed_refresh_primary_idx`
`(projection_domain, is_refresh_intent DESC, created_at ASC, intent_id ASC)
WHERE completed_at IS NULL AND partition_hash IS NULL`.

With the fix, all 5 refresh intents in partition 5 appear in the first 100
batch positions (confirmed by live DB query with the corrected ORDER BY).
Refresh rows complete in the first cycle, the fence opens, and the deferred
edges drain at full batch throughput. Refresh intents are few (max 14 in any
partition across the 900-repo corpus) so promoting them cannot starve edges.

No-Regression Evidence: `TestListPendingDomainPartitionIntentsRefreshFirstLaterTimestamp`
failed under the old `(created_at ASC, is_refresh_intent DESC)` ordering (first
row was an upsert edge despite the later-timestamp refresh being in the set)
and passed after the `(is_refresh_intent DESC, created_at ASC)` fix.
`TestListPendingDomainPartitionIntentsRefreshFirst` (same-timestamp case from
#3451) continues to pass. `go test ./internal/storage/postgres ./internal/reducer
-count=1` → 3,212 passed. `go vet ./internal/storage/postgres ./internal/reducer`
→ clean. `golangci-lint run ./internal/storage/postgres ./internal/reducer` →
0 issues. `scripts/verify-performance-evidence.sh` and
`scripts/test-verify-performance-evidence.sh` → both exit 0. The change is
backward-compatible: domains without refresh intents have `is_refresh_intent=false`
on all rows, so their relative `created_at ASC, intent_id ASC` order is
unchanged.

No-Observability-Change: #3474 adds no queue domain, graph write, metric
instrument, metric label, span name, worker, lease, route, or log field. It
replaces two partial indexes and changes the ORDER BY in two SQL constants.
Operators diagnose the starvation condition through the same signals as #3451:
`shared projection deferred per-edge rows behind repo refresh fence` log line
(count drops to zero once the fence opens), `eshu_dp_shared_projection_intents_processed_total`,
and `pending_projection` DB counts.

## Write-Conflict Handling Proof Under Concurrent Claimers (#3558)

`reducer_queue_conflict_claim_proof_test.go` is the live-concurrency proof that
the reducer claim path handles MERGE / commit-time uniqueness / write-conflict
races by partition-by-conflict-key fencing, not by serialization. The contested
resource is the `(conflict_domain, conflict_key)` fence computed by
`reducerConflictDomainKey`; the proof exercises the `code_graph` conflict domain
(`reducerConflictDomainCodeGraph`) where two `code_call_materialization` work
items share one scope conflict key. Lease settings: `LeaseDuration=1m`,
distinct `LeaseOwner` per claimer, `Now` pinned so no claimed lease expires
during a race. The proofs drive real `ReducerQueue.Claim`/`Ack` against the
production `claimReducerWorkQuery` SQL on a live Postgres in a throwaway schema
that is dropped on cleanup.

Three scenarios, each a failing-first guard against a regression that drops or
weakens the fence:

- Shared conflict key, 8 concurrent claimers: at most one live lease across the
  key at any instant (`maxLive <= 1`) and no work item claimed twice
  concurrently. A non-atomic or removed fence would let a second claimer grab
  the sibling while the first lease is live — the concurrent-MERGE / commit-time
  uniqueness conflict this issue targets.
- Disjoint conflict keys, 2 claimers: both distinct items claimed concurrently
  (`len(claimed) == 2`). This proves the fence is partition-by-conflict-key and
  not serialization-as-a-fix; a single-threaded drain "fix" would fail it.
- Convergence after ack: a sibling fenced behind a live lease on the same key is
  deferred (not lost), then becomes claimable once the holder acks and releases
  the lease, and the post-ack claim returns the deferred sibling rather than
  re-claiming the acked item. This is the no-lost-write / ordering / idempotent-
  retry half of the proof.

True-concurrency requirement: each concurrent claimer is given its OWN
single-connection `*sql.DB` handle bound to the shared throwaway schema
(`openReducerFairnessClaimerDB`), so N claimers hold N live Postgres connections
and their `claimReducerWorkQuery` statements truly interleave at the database.
An earlier revision shared one pooled handle capped at `MaxOpenConns(1)`, which
serialized every claimer behind a single connection and never exercised the
concurrent lock/commit interleavings the fence guards — a non-atomic conflict
check could have passed vacuously. `search_path` is connection-local, so the
per-claimer handles cannot simply raise the pool cap; each handle pins its own
`SET search_path` on its single connection to the shared schema.

No-Regression Evidence: this change adds only proof tests and their test-only
connection helpers; it does not alter the production claim SQL, conflict-key
derivation, lease semantics, worker count, batch size, or any write path, so it
cannot regress runtime behavior. Hermetic run without a DSN: the three proofs
SKIP cleanly (verified with `-v`), and `go test ./internal/storage/cypher
./internal/storage/postgres -count=1` passes (1,595 tests); `go build ./...`,
`go vet ./internal/storage/postgres/...`, and `golangci-lint run
./internal/storage/postgres/... ./internal/storage/cypher/...` are all clean.
The live `-race` proof run against a Postgres DSN has NOT been re-executed in
this environment after the per-claimer-connection fix because no Postgres
(`ESHU_POSTGRES_DSN` / `ESHU_REDUCER_FAIRNESS_PROOF_DSN`) or Docker daemon was
available here; the proofs compile and skip, and require a live DSN to assert
the fence. The proofs reuse the existing `reducer_queue_domain_fairness_test.go`
schema/seed helpers, so they add no new DDL or fixtures.

No-Observability-Change: #3558 adds no queue domain, conflict domain, graph
write, metric instrument, metric label, span name, worker, lease, route, status
field, or log field. The claim path's existing instrumentation is unchanged;
operators continue to diagnose conflict-fence contention through reducer claim
spans/counters, `fact_work_items` status/`conflict_domain`/`conflict_key`
columns, lease-owner/`claim_until` rows, and reducer queue status counts.

## Graph MERGE Commit-Time Conflict Proof (#3558)

The companion graph-layer proof lives in
`go/internal/storage/cypher/retrying_executor_concurrency_proof_test.go`. It
proves the second write-conflict layer #3558 targets: two canonical writers that
MERGE the SAME shared `uid` (the Repository / Directory / Module class of node
that multiple source-repo partitions legitimately write) are driven into a typed
NornicDB commit-time UNIQUE conflict
(`Neo.ClientError.Transaction.TransactionCommitFailed`) and converge through
`RetryingExecutor.ExecuteGroup`: exactly one node is created (no duplicate), both
writers' contributions land (no silent loss), both calls return nil (convergence
via retry), and the `eshu_dp_neo4j_deadlock_retries_total` counter fires
(operator-visible contention). The conflict domain is one shared canonical
`uid`; the transaction scope is one `ExecuteGroup` call; the retry scope is
`RetryingExecutor.runWithRetry`. A failing-first companion drives the SAME race
through the bare group executor with NO retry layer and shows the loser's write
is silently lost — proving the conflict is real and that the retry layer, not
serialization, is what makes the system converge.

No-Regression Evidence: test-only change; no production graph-write path,
Cypher shape, batching, transaction scope, or retry classifier is modified.
`go test ./internal/storage/cypher -run
'RetryingExecutor|ConcurrentMergeConflict|BareGroupExecutorLoses' -race
-count=1` passes (17 tests), and the full `go test ./internal/storage/cypher
-count=1` package suite passes (583 tests). The proof asserts the production
classifier (`isNornicDBCommitTimeUniqueConflictError`) by constructing the same
typed `neo4jdriver.Neo4jError` the live binary surfaces, so it stays faithful to
`retrying_executor.go` without touching it.

Observability Evidence: the proof asserts the existing
`eshu_dp_neo4j_deadlock_retries_total` retry counter (write-phase label) fires
at least once under the concurrent MERGE conflict via a manual metric reader.
The instrument and its label are unchanged; the test only reads it, so operators
keep the same retry-visibility signal in production.

## OIDC Static Grant Fallback For Empty Persisted Mappings (#3457)

No-Regression Evidence: `go test ./internal/storage/postgres -run
'TestOIDCLoginStore|TestBootstrapDefinitionsIncludeOIDCLogin' -count=1` fails
before this fix because an empty persisted
`identity_provider_group_role_mappings` result is reported as a policy revision
error. After the fix, zero matching persisted OIDC group-role rows returns an
empty not-mapped result (`ok=false`) before scope/repository target reads, so
`cmd/api` can continue to its private static fallback resolver. The existing
mixed-policy test still proves real multi-policy role rows fail closed.

No-Observability-Change: #3457 changes only the empty-row branch after the
bounded `ResolveGroupRoleGrants` role lookup. It adds no table, index, query
shape, route, worker, metric, span, log field, runtime knob, provider call,
token persistence, or raw group persistence. Operators continue to diagnose the
path through existing OIDC login HTTP status, Postgres query spans and
`eshu_dp_postgres_query_duration_seconds`, browser-session rows, and the OIDC
session refresh counters/logs.

## Active-Docs-Bound Search-Vector Pending Query Rewrite (#4233)

Date: 2026-06-29

File changed: `go/internal/storage/postgres/eshu_search_vector_pending.go`,
const `listPendingEshuSearchVectorScopesSQL`.

### Root Cause

The original query built a `ready_docs` CTE using `SELECT DISTINCT` over the
entire `eshu_search_vector_metadata` table filtered only by
`(provider_profile_id, source_class, embedding_model_id, vector_index_version)`
— a corpus-wide materialisation. On a 43 GB local corpus (478k metadata rows,
~200k active search-document facts across ~117 active repository scopes), the
planner emitted a Merge Anti Join with a full-corpus Unique+Sort pass over the
metadata table regardless of how many active scopes needed checking.

### Before (old `ready_docs` CTE + Merge Anti Join)

EXPLAIN (cost only) on local corpus:
```
Limit  (cost=9465.36..9798.60 rows=100 width=139)
  ->  Group  (cost=9465.36..16606.75 rows=2143 ...)
        ->  Incremental Sort  (cost=9457.93..16585.32 ...)
              ->  Merge Anti Join  (cost=9457.93..16526.95 ...)
                    ->  Nested Loop  [active_docs]  (cost=0.83..7058.97 ...)
                    ->  Subquery Scan on ready_docs  (cost=9457.10..9459.10 ...)
                          ->  Unique  (cost=9457.10..9459.10 ...)
                            [materialises full metadata table ~478k rows]
```

EXPLAIN ANALYZE (warm buffers): Execution Time ~122 ms, shared hits 53 215.
EXPLAIN ANALYZE (cold buffers, pre-fix): many minutes on large corpora
(per issue #4233 root-cause investigation).
Planner LIMIT cost: ~9 465. Full-scan cost: ~225 399.

### After (NOT EXISTS correlated probe)

The `ready_docs` CTE is replaced with a correlated `NOT EXISTS` subquery.
The planner drives the readiness probe per `active_docs` row using a covering
metadata index (the primary key or `eshu_search_vector_metadata_model_v2_idx`,
both keyed by scope/generation + the provider tuple); the planner observed
using `model_v2_idx` on the 43 GB corpus. No schema change required.

EXPLAIN (cost only) on same corpus:
```
Limit  (cost=16.97..688.08 rows=100 width=139)
  ->  Group  (cost=16.97..14398.94 rows=2143 ...)
        ->  Incremental Sort  (cost=16.97..14377.51 ...)
              ->  Nested Loop Anti Join  (cost=1.93..14319.13 ...)
                    ->  Nested Loop  [active_docs]  (cost=0.83..7058.97 ...)
                    ->  Nested Loop Left Join  (cost=1.09..3.38 per active row)
                          ->  Index Scan using eshu_search_vector_metadata_model_v2_idx
                                Index Cond: (scope_id=..., generation_id=...,
                                  provider_profile_id=..., source_class=...,
                                  embedding_model_id=..., vector_index_version=...)
                          ->  Index Scan using eshu_search_vector_values_pkey
```

EXPLAIN ANALYZE (cold buffers, post-fix): Execution Time 579 ms (48 850 blocks
read, first run cold). Planner LIMIT cost: ~16.97. Full-scan cost: ~14 399.
Planner cost improvement at LIMIT: ~9 465 → ~17 (~556x estimated reduction).
No corpus-wide `Unique` or full-set `Sort` over `eshu_search_vector_metadata`
in the outer plan.

### Equivalence Argument

`eshu_search_vector_metadata` has at most one row per
`(scope_id, generation_id, document_id, provider_profile_id, source_class,
embedding_model_id, vector_index_version)` (primary key unique). The original
`SELECT DISTINCT ready_docs` therefore produced at most one row per
`(scope_id, generation_id, document_id, provider_profile_id, source_class,
embedding_content_hash)` tuple. The NOT EXISTS probe is semantically identical:
it finds a metadata row for the same doc that satisfies the same
`build_state`/value-present/content_hash conditions, and the LEFT JOIN to
`eshu_search_vector_values` uses the same columns as the original. The
`GROUP BY + ORDER BY docs.scope_id` and `LIMIT $6` are preserved unchanged.

### Live Equivalence Proof

`TestEshuSearchVectorPendingBoundedPlanLive` (gated on
`ESHU_SEARCH_VECTOR_PENDING_PLAN_LIVE=1` + `ESHU_POSTGRES_DSN`) seeds eight
equivalence cases under a throwaway scope/generation, asserts
`ListPendingSearchVectorScopes` returns exactly the scopes with ≥1 pending doc,
and verifies the EXPLAIN plan contains no corpus-wide `Unique` over
`eshu_search_vector_metadata`.

Result against local corpus: PASS (1 test, ~0.6 s).

### Performance Evidence

- Before: planner LIMIT cost 9 465, full cost 225 399, ANALYZE many minutes
  on large corpora (cold).
- After: planner LIMIT cost 17, full cost 14 399, ANALYZE ~580 ms (cold
  buffers, 43 GB corpus, ~200k active search-document facts, ~117 active
  repository scopes). No corpus-wide Unique/Sort over metadata table.

### No-Observability-Change

`listPendingEshuSearchVectorScopesSQL` is an internal readiness-probe query
called by `EshuSearchVectorPendingStore.ListPendingSearchVectorScopes` from the
`searchVectorPendingAdapter` in `cmd/reducer`. It has no metric instrument, span
name, log field, wire-contract column, API route, CLI flag, or environment
variable. The Go API surface, parameter order ($1..$6), scan columns, and return
type are identical. No metric, span, trace, or log change is needed.

## Login-Time require_mfa_for_all_users Enforcement (#5001)

`AuthenticateLocalIdentity` now reads the tenant sign-in policy's
`require_mfa_for_all_users` flag (via the existing
`signInPolicyRequiresMFAForUsers` helper / `selectSignInPolicyRequireMFAQuery`)
after the password compare, and folds the admin-only MFA gate and the new
non-admin-under-policy gate into one shared enforcement block. A non-admin local
user on a tenant with `require_mfa_for_all_users=true` is now MFA-challenged at
login (previously only at invitation-accept), closing the pre-existing-user
bypass that only bit when `require_sso=false`. The policy read fails CLOSED: a
read error denies the login rather than silently skipping the check.

### No-Regression Evidence

Performance Evidence: the change adds exactly one bounded, indexed single-row
read per local login attempt — `SELECT require_mfa_for_all_users FROM
identity_sign_in_policies WHERE tenant_id = $1` (primary-key point lookup on
`tenant_id`, ≤1 row). Baseline: the local-login path issued its credential
select plus, for non-admins, the role/grant selects; after: the same path plus
one PK-point read that admins previously skipped. Backend: Postgres (all
supported versions); NornicDB/Cypher is not on this path — no graph read, graph
write, queue claim, lease, batch, or worker knob is added or changed. Input
shape: one `tenant_id` parameter; terminal row counts on every downstream query
are unchanged. The added read is the same bounded lookup already performed at
invitation-accept (`AcceptLocalIdentityInvitation`), so it introduces no new
query class and no hot-path cost — local login is human-frequency, not a
reducer/ingester hot path. No regression is possible from a single extra PK read
on a non-hot path.

### No-Observability-Change

No-Observability-Change: the new denial reuses the existing
`LocalIdentityAuthMFARequired` status, which
`LocalIdentityHandler` already maps to a governance-audit MFALifecycle
`DecisionDenied` event and to the existing `mfa_required` value in the wire
`status` enum (`openapi_components_auth.go`) — so the widened population reaching
`mfa_required` is already observable through the current audit and wire
contract, with no new metric, span, or wire field required. The one added
operator signal is a distinct `slog.ErrorContext` line — `"local login
mfa-for-all policy read failed; login denied"` with `subject_class`, `tenant_id`,
and `error` fields — emitted only on the fail-closed policy-read error path, so
an operator can tell a sign-in-policy read outage from any other login 500. No
`eshu_dp_*` metric shape, span name, or telemetry contract row changes.

## Sign-In Policy require_sso Flip: Local Session Bulk Revoke (#5002)

Deferred P2 design item from PR #5000 review: `ResolveSessionHash`
(`browser_sessions.go`) checks hash/CSRF/idle/absolute expiry but never
re-evaluates `require_sso`, so a password-authenticated (`subject_class=
'local_user'`) browser session issued before a `require_sso` false->true flip
kept working until it naturally expired — a break-glass-shaped window that
stayed open after an admin locked a tenant down to SSO-only.

The fix adds `revokeLocalBrowserSessionsForTenantQuery`
(`browser_sessions_schema.go`) and runs it inside `UpsertSignInPolicy`'s
existing row-locked transaction (`identity_sign_in_policy.go`) whenever the
RESULTING policy has `require_sso=true`: `UPDATE browser_sessions SET
revoked_at = $2, updated_at = $2 WHERE tenant_id = $1 AND subject_class =
'local_user' AND revoked_at IS NULL`. `subject_class='break_glass'` is
excluded by construction (break-glass must stay reachable under lockdown) and
`subject_class='external_oidc_user'` is excluded because an SSO session
already satisfies the policy being enabled. The predicate is unconditional on
the resulting value, not the prior one, so it is idempotent under repeated
or concurrent flips.

No-Regression Evidence: `go test ./internal/storage/postgres -run
'TestUpsertSignInPolicyRevokesLocalBrowserSessionsWhenRequireSSOBecomesTrue|TestUpsertSignInPolicyDoesNotRevokeSessionsWhenRequireSSOStaysFalse|TestUpsertSignInPolicyRevokeIsUnconditionalWhenRequireSSOAlreadyTrue'
-count=1 -v` failed before the fix (missing bulk-revoke exec) and passed
after: the false->true flip issues the revoke inside the same transaction
after the policy upsert exec, a false->false update issues no revoke exec at
all, and an already-true tenant still issues the (idempotent) revoke on an
unrelated-field edit. Against real Postgres (a throwaway `postgres:18-alpine`
container, schema-per-run fixture matching the package's other DSN-gated
proofs), the new `TestSignInPolicyConcurrencyGate/RequireSSOFlipRevokesOnlyLocalUserSessions`
subtest seeded one `local_user`, one `break_glass`, and one
`external_oidc_user` session for the flipped tenant plus a `local_user`
session for a DIFFERENT tenant, flipped `require_sso` to true, and asserted
only the same-tenant `local_user` session's `revoked_at` was set — the
break-glass, OIDC, and other-tenant rows were untouched. A second
`UpsertSignInPolicy` call against the now-already-true policy (editing
`require_mfa_for_all_users`) did not error and left the session revoked
exactly once. Full run: `TestSignInPolicyConcurrencyGate` — 6/6 subtests
passed in 0.26s (`ESHU_SIGN_IN_POLICY_PROOF_DSN` pointed at the throwaway
container).

Concurrency/transaction scope: the revoke and the policy write share one
transaction opened by `beginLocalIdentityTx` and commit or roll back
together — a store error on either statement rolls back both (proven by the
existing `committed`/`rolledBack` assertions in the guardrail-rejection unit
tests, which still pass and rollback on rejection before the revoke exec is
ever reached). No new lease, queue, worker, or batching path is introduced;
the write stays a single bounded UPDATE inside an existing transaction
boundary.

Index reasoning (no index added): `revokeLocalBrowserSessionsForTenantQuery`
filters on `(tenant_id, subject_class, revoked_at IS NULL)`.
`browser_sessions_active_idx` (`session_hash, tenant_id, workspace_id,
updated_at DESC`) cannot serve this predicate — `session_hash` is not in the
WHERE clause, so Postgres cannot use it as a leading-column index scan — and
`browser_sessions_external_auth_stale_idx` is unrelated. This query is
reached only from an admin sign-in-policy PATCH (`PATCH
/api/v0/auth/admin/sign-in-policy`), an admin-rate-limited configuration
action, not a request-path or worker hot path; per the Prove-The-Theory-First
rule, no index is added on an unmeasured theory. A bounded scan of one
self-hosted tenant's `browser_sessions` rows at admin frequency is within
budget without a dedicated index. If a future corpus shows this table grown
large enough to matter, a partial index on `(tenant_id, subject_class) WHERE
revoked_at IS NULL` would directly serve this predicate — but that is a
measured follow-up, not a speculative addition here.

No-Observability-Change: the revoke reuses `browser_sessions`'s existing
`revoked_at`/`updated_at` columns and the same "mark revoked" mechanism
`RevokeSession` and `revokeStaleOIDCBrowserSessionQuery` already use — no new
table, column, index, metric instrument, span name, log field, queue, worker,
lease, or route is added. Operators can already diagnose a require_sso flip's
effect by reading `browser_sessions.revoked_at`/`updated_at` for the tenant
and by the existing `sign_in_policy_updated` governance audit event emitted
by `SignInPolicyMutationHandler.handleUpdate`
(`go/internal/query/sign_in_policy_mutations.go`); a per-revoke count metric
was considered and rejected for this change — the audit event already records
every successful flip, and revoke volume is bounded by session count for one
tenant at admin frequency, not a rate an operator needs a dedicated counter
to notice at 3 AM.

## Forced Password Rotation On First Login (#4976)

Adds `identity_local_credentials.must_change_password BOOLEAN NOT NULL
DEFAULT false` via a new additive migration
(`migrations/053_identity_local_credentials_must_change_password.sql`,
`ALTER TABLE ... ADD COLUMN IF NOT EXISTS`, idempotent on every startup like
every other bootstrap definition). Set true only for the
`ESHU_ADMIN_USERNAME`/`ESHU_ADMIN_PASSWORD[_FILE]`-seeded bootstrap admin
(`cmd/api/seed_initial_admin.go` `seedBootstrapAdminFromEnv`); the
`ESHU_AUTH_BOOTSTRAP_MODE=generated` path and invitation acceptance both stay
false. `AuthenticateLocalIdentity` checks the column after password and MFA
both verify and before issuing a session, returning the new
`must_change_password` status instead. The self-service `POST
/api/v0/auth/local/password/rotate` route re-verifies the current password
(and MFA, when the account has one) and rotates the credential — revoke old +
insert new (with `must_change_password=false`) — inside one transaction using
a `FOR UPDATE OF c` row lock on the credential row
(`selectLocalIdentityCredentialForUpdateQuery`), so a concurrent second
rotation attempt against the same credential blocks on the row lock and, under
Read Committed's EvalPlanQual recheck, sees the row already revoked once the
first rotation commits — a stale password can never be accepted twice.

### No-Regression Evidence

Performance Evidence: the added column is read as one extra scalar in the
existing single-row `selectLocalIdentityCredentialQuery`
(`identity_local_credentials`, PK/unique-scoped join already returning ≤1 row)
— no new query, no new join, no new index. The row-locked
`selectLocalIdentityCredentialForUpdateQuery` variant is used only by the new
low-frequency (human-rate, not reducer/ingester hot-path) rotation endpoint,
never by login. `go test ./internal/storage/postgres/ -run
'LocalIdentity|Bootstrap|MustChangePassword|RotateLocalIdentityPassword' -count=1`
and the full `go test ./internal/storage/postgres/... -count=1` (1401 tests)
both pass, proving the existing credential-select/insert/authenticate paths
are unchanged for `must_change_password=false` (every credential before this
issue, and the `generated`-mode bootstrap admin).

### No-Observability-Change

The new `must_change_password` wire status reuses the existing local-identity
governance-audit event type (`governanceaudit.EventTypeIdentityAuthentication`,
already used by login) with a distinct `reason_code` (`"must_change_password"`
for the blocked-login denial, `"local_password_rotation_forced"` for a
successful rotation), and a new value in the existing `status` enum
(`LocalIdentitySessionResponse.status`, `openapi_components_auth.go`) that the
console already knows how to render generically (it is the same response
shape `mfa_required` already uses). No new `eshu_dp_*` metric, span name, or
telemetry contract row: the rotation endpoint's writes (revoke, insert, clear
lockout, consume bootstrap credential) reuse the exact same instrumented
`ExecQueryer`/`InstrumentedDB` paths login and admin password-reset already go
through.

## UpdateProviderConfig Returns Post-Transaction Status (#4988)

`UpdateProviderConfig` (`identity_provider_config_writes.go`) resets the active
provider-config row to `draft` in the same transaction as a config revision
(an update invalidates the prior test-connection, so the provider must be
re-tested + re-enabled), but returned `current.status` — the pre-update value —
so callers saw a stale status. The fix adds `RETURNING status` to the existing
active-revision reset UPDATE (`activateProviderConfigActiveRevisionQuery`) and
scans it back inside the same transaction, returning the actual persisted
post-update status (`draft`).

### No-Regression Evidence

Performance Evidence: no new query, join, index, or round-trip. `RETURNING
status` reads one column of the row the UPDATE already writes, in the same
statement — the reset UPDATE was already executed; this only surfaces its
result. The write path, transaction scope, and row cardinality are unchanged.
`go test ./internal/storage/postgres/ -run 'ProviderConfig|UpdateProvider'
-count=1` passes, and the RED-before/GREEN-after regression test proves the
returned status now equals the persisted status after an update.

### No-Observability-Change

The change is a returned-struct-value correctness fix on an existing storage
method. No metric instrument, span name, log field, wire-contract column, API
route, CLI flag, or environment variable is added or changed; the same
instrumented `ExecQueryer` path the method already used carries the UPDATE.
