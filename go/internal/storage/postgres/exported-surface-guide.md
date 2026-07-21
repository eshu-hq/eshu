# storage/postgres exported surface guide

This guide keeps the package's exported store and helper inventory. Keep
`README.md` focused on package orientation; update this file when callers gain,
lose, or materially change a Postgres store, constructor, schema helper, or
reducer/query adapter.

## Exported surface

**Database interfaces**

- `ExecQueryer` — combined read/write adapter; accepted by all store
  constructors
- `Transaction` — `ExecQueryer` + `Commit`/`Rollback`
- `Beginner` — `Begin(ctx) (Transaction, error)`; implemented by `SQLDB`
- `SQLDB` — adapts `*sql.DB`; `SQLTx` adapts `*sql.Tx`
- `InstrumentedDB` — wraps `ExecQueryer` with OTEL spans and
  `eshu_dp_postgres_query_duration_seconds`

**Fact store**

- `FactStore` / `NewFactStore` — `UpsertFacts`, `LoadFacts`, `ListFacts`,
  `ListFactsByKind`, `ListFactsByKindAndPayloadValue`,
  `LoadActiveCodeCallSymbolDefinitionFacts`, `LoadIncidentRoutingRawEvidence`,
  `ListActiveRepositoryFacts`, `ListActivePackageOwnershipFacts`, `CountFacts`,
  `ListOSPackageAdvisoryTargets`, and `ListSBOMComponentAdvisoryTargets`
- `ServiceIncidentEvidenceLoader` / `NewServiceIncidentEvidenceLoader` —
  service-scoped incidents evidence loader for reducer service materialization;
  it resolves PagerDuty provider service ids to catalog service ids through
  active exact/derived reducer correlation facts and returns StableFactKey-based
  routing evidence identity.
- `AWSCloudRuntimeDriftFindingStore` /
  `NewAWSCloudRuntimeDriftFindingStore` — active-generation reads over
  `reducer_aws_cloud_runtime_drift_finding` facts for the IaC management API;
  filters must include `scope_id` or a 12-digit `account_id`, optional regions
  must use AWS region characters only, exact `arn` filters use payload equality,
  and direct list reads cap at 500 rows.
  The decoded row preserves optional #124 read-model payload fields such as
  `management_status`, matched Terraform state/config handles, candidate
  service/environment labels, dependency paths, warning flags, missing
  evidence, and recommended action. Older facts without those fields still
  decode and let the query layer derive the current AWS drift statuses.
- `EshuSearchDocumentStore` / `NewEshuSearchDocumentStore` — active-generation
  reads over `reducer_eshu_search_document` facts for the design-430 curated
  search lane; filters must include `scope_id`, accept optional `repo_id` and
  `source_kind` predicates, and direct list reads cap at 500 rows. The decoded
  row carries the curated `searchdocs.Document`; documents stay derived
  retrieval evidence and never canonical graph truth.
- `EshuSearchVectorMetadataStore` / `NewEshuSearchVectorMetadataStore` —
  additive vector lifecycle metadata for active curated search documents, keyed
  by scope, generation, document, embedding model, and vector index version.
- `EshuSearchVectorValueStore` / `NewEshuSearchVectorValueStore` — bounded
  derived vector payload persistence for active curated search documents.
  Reads require scope, embedding model, and vector index version, join through
  `ingestion_scopes.active_generation_id`, cap pages at 500 rows, and return
  deterministic document ordering.
- `FunctionSummaryStore` / `NewFunctionSummaryStore` — additive durable
  persistence for value-flow `summary.Snapshot` rows keyed by
  generation-independent `FunctionID`. Upserts are idempotent on `function_id`
  and reject blank repo components so future cross-repo recomposition cannot
  silently collide.
- `FunctionSourceStore` / `NewFunctionSourceStore` — durable value-flow source
  ports keyed by FunctionID and parameter index; repository replacements delete
  stale rows before idempotent upsert.
- `FunctionGraphIDStore` / `NewFunctionGraphIDStore` — durable FunctionID to
  graph UID mapping for post-summary `TAINT_FLOWS_TO` projection.
- `ValueFlowFixpointComponentStore` / `NewValueFlowFixpointComponentStore` —
  durable solved weak-component results keyed by the reducer's content-derived
  component key. Loads are bounded to requested keys and writes converge through
  `ON CONFLICT (component_key) DO UPDATE`.

**Queue stores**

- `ProjectorQueue` / `NewProjectorQueue` — `Claim`, `Ack`, `Heartbeat`, `Fail`,
  `Enqueue`; `ErrProjectorClaimRejected`
- `ReducerQueue` / `NewReducerQueue` — `Claim`, `Ack`, `Fail`, `Enqueue`
  (batch); `ErrReducerClaimRejected`
- `QueueObserverStore` / `NewQueueObserverStore` — queue depth, age, and
  blockage queries for the status surface
- `AWSScanStatusStore` / `NewAWSScanStatusStore` — per AWS tuple scanner and
  commit status for `/admin/status`
- `PostgresAWSCloudRuntimeDriftEvidenceLoader` — bounded AWS resource →
  active Terraform state → owned Terraform config join for the
  `aws_cloud_runtime_drift` reducer domain, including explicit unknown and
  ambiguous owner evidence when coverage or deterministic owner signals are
  insufficient
- `PostgresCloudInventoryEvidenceLoader` — bounded read of the three provider
  inventory source fact kinds (`aws_resource`, `gcp_cloud_resource`,
  `azure_cloud_resource`) for one scope generation, mapped into the shared
  `reducer.CloudInventoryRecord` shape for the `cloud_inventory_admission`
  reducer domain (issues #1997, #1998)
- `PostgresCloudIdentityPolicyEvidenceLoader` — bounded read of Azure identity
  observation facts for one scope generation, mapped into safe
  `reducer.CloudIdentityPolicyEvidenceRecord` rows. The loader keeps only
  bounded identity/role classes and keyed principal/client/object/tenant
  fingerprints, never raw principal GUIDs or assignment scopes.
- `PostgresMultiCloudRuntimeDriftEvidenceLoader` — bounded observed →
  active Terraform state → owned Terraform config join for the
  `multi_cloud_runtime_drift` reducer domain, keyed on canonical
  `cloud_resource_uid` so AWS, GCP, and Azure share one drift path; resolves
  every layer through `cloudinventory` without inventing a second keyspace and
  reuses the shared `tfstatebackend` config-owner resolution (issues #1997,
  #1998)

**Content stores**

- `ContentStore` / `NewContentStore` — `GetFileContent`, `GetEntityContent`,
  `SearchFileContent`, `SearchEntityContent`; `FileContentRow`, `EntityContentRow`
- `ContentWriter` / `NewContentWriter` — writes `content_files` and
  `content_entities`. Entity-batch upserts fan out through
  `runConcurrentBatches` in `content_writer_batch.go`; the per-file batch
  loop stays serial because each file batch is preceded by a per-batch
  `delete_content_references` whose interleaving the existing tests gate.
  Auto-default concurrency is `runtime.NumCPU()` clamped to
  `contentWriterBatchConcurrencyAutoCap` (4); operators can opt up to
  `contentWriterBatchConcurrencyCap` (8) via
  `ESHU_CONTENT_WRITER_BATCH_CONCURRENCY`. The env value is resolved once in
  `NewContentWriter`, so a long-running ingester does not pick up live env
  changes mid-run; explicit overrides flow through
  `WithBatchConcurrency(int)`. The `upsert_entities` `logStage` line carries
  a `batch_concurrency` attribute so operators reading the log can
  reconcile the new wall-clock value with the per-batch
  `eshu_dp_postgres_query_duration_seconds` metric.

  Pool budgeting: peak Postgres demand is `ESHU_PROJECTOR_WORKERS *
  ESHU_CONTENT_WRITER_BATCH_CONCURRENCY` plus connections held by
  collector, status reads, and heartbeats. The auto cap of 4 reduces
  pressure relative to the prior unbounded fan-out, but does not on
  its own guarantee the product stays under the 30-connection default
  pool (`internal/runtime/data_stores.go`). Hosts with more than 7 CPUs
  (and the `local_authoritative` + NornicDB ingester wiring, which sets
  `ESHU_PROJECTOR_WORKERS = runtime.NumCPU()` uncapped) will see
  `4 * NumCPU` peak demand. When that exceeds the pool, `database/sql`
  queues new acquires rather than failing — throughput drops while the
  writer waits for a connection. Operators on high-core hosts, or
  operators raising the env knob, should raise the Postgres pool
  ceiling (the ESHU_POSTGRES_MAX_OPEN_CONNS env in
  `internal/runtime/data_stores.go`) or lower
  `ESHU_PROJECTOR_WORKERS` so the product stays inside the configured
  pool.

**Phase state**

- `GraphProjectionPhaseStateStore` / `NewGraphProjectionPhaseStateStore` —
  batched upsert of phase state rows
- `GraphProjectionPhaseRepairQueueStore` / `NewGraphProjectionPhaseRepairQueueStore`
  — repair queue for phase re-publish
- `NewGraphProjectionReadinessLookup` / `NewGraphProjectionReadinessPrefetch`
  — implement `reducer.GraphProjectionReadinessLookup`

**Shared projection**

- `GraphNodeOwnerStore` / `NewGraphNodeOwnerStore` — per-uid advisory-lock and
  max-order-key resolver for canonical CloudResource and KubernetesWorkload
  writers.
- `GraphNodeOwnerBackfillStore` / `NewGraphNodeOwnerBackfillStore` — bounded,
  monotonic seed and durable completion marker for CloudResource graph rows
  written before the owner ledger existed.

- `SharedIntentStore` / `NewSharedIntentStore` — reads
  `shared_projection_intents` and writes shared projection intents in bounded
  multi-row batches (`shared_intents_upsert.go:62`). It also exposes history
  lookups for prior acceptance-unit completion and current source-run chunk
  completion.
- `SharedIntentAcceptanceWriter` / `NewSharedIntentAcceptanceWriter` — writes
  intent acceptance rows; `NewSharedIntentAcceptanceWriterWithInstruments` adds
  metrics
- `CodeCallIntentWriter` / `NewCodeCallIntentWriter` — type alias for
  `SharedIntentAcceptanceWriter`
- `SharedProjectionAcceptanceStore` / `NewSharedProjectionAcceptanceStore`

**Status**

- `StatusStore` / `NewStatusStore` — reads scope, generation, queue, blockage,
  failure, coordinator, registry collector, and domain backlog aggregates.
  `status_queries.go` merges `fact_work_items` with pending
  `shared_projection_intents` and active `shared_projection_partition_leases`
  for domain backlog rows. Lease-only rows stay visible even after the last
  pending intent is claimed, so `/admin/status` does not report healthy while
  reducer-owned shared projection work is still becoming graph-visible and does
  not report stalled while a reducer lease is actively moving that domain.
  `status_registry.go` derives OCI and package-registry aggregate counts from
  workflow tables without reading private registry object names. The same store
  also runs the bounded
  `terraformStateLastSerialQuery` and `terraformStateRecentWarningsQuery` from
  `tfstate_status.go` so the admin status response carries one row per
  Terraform-state safe locator hash or Git backend-source handle plus up to
  `MaxTerraformStateRecentWarnings` recent warning facts grouped by
  `warning_kind`. Git-scope unresolved-backend warnings use repo id plus
  repo-relative source path as the safe handle and do not invent a state
  locator.

**AWS pagination checkpoints**

- `AWSPaginationCheckpointStore` / `NewAWSPaginationCheckpointStore` — persists
  claim-fenced AWS page tokens in `aws_scan_pagination_checkpoints`.
  `Save` rejects older fencing tokens, `ExpireStale` removes prior-generation
  rows for one AWS claim boundary, and `Complete` deletes operation state after
  a terminal page.

**Decision store**

- `DecisionStore` / `NewDecisionStore` — upserts `projection_decisions` and
  `projection_decision_evidence`; `DecisionFilter` for scoped reads
- `AdmissionDecisionStore` / `NewAdmissionDecisionStore` — upserts
  `admission_decisions` and `admission_decision_evidence`; reads require
  domain, scope, and generation bounds before optional state/anchor filters.
  `AdmissionDecisionSchemaSQL` registers the schema in bootstrap as
  `007a_admission_decisions.sql`, preserving compatibility with existing
  projection decisions and reducer fact payloads.

**Hosted tenant/workspace grants**

- `TenantWorkspaceGrantStore` / `NewTenantWorkspaceGrantStore` — additive
  hosted isolation storage for `tenants`, `workspaces`,
  `tenant_scope_grants`, and `tenant_repository_grants`
- `TenantRecord`, `WorkspaceRecord`, `TenantScopeGrant`,
  `TenantRepositoryGrant`, and `TenantWorkspaceGrantQuery` — typed rows and
  bounded active-grant read filters for future hosted enforcement
- `ScopedAPITokenStore` / `NewScopedAPITokenStore` — hash-only hosted token
  registry backed by tenant/workspace rows; reads return active scoped subject
  bounds only and never accept raw token values
- `OIDCLoginStore` / `NewOIDCLoginStore` — hash-only OIDC Authorization Code
  state and active group-hash to role/scope/repository grant resolution for
  dashboard login
- `OIDCLoginStateRecord`, `OIDCGroupGrantQuery`, and
  `OIDCGroupGrantResolution` — typed state and grant-resolution rows; group
  inputs are hashes, never raw group names
- `IdentitySubjectStore` / `NewIdentitySubjectStore` — dormant user-management
  schema for users, provider configs, external identity links, local
  credential hashes, MFA factor handles, tenant memberships, roles, grants,
  sessions, service principals, service-principal role assignments, and token
  metadata. The schema uses opaque IDs, hashes, and credential handles only and
  leaves shared-token/scoped-token behavior unchanged until later enforcement
  slices opt in.
- `SAMLSSOStore` / `NewSAMLSSOStore` — hash-only SAML AuthnRequest and replay
  ledgers used by API SAML login. Request consume is a single guarded update,
  replay reservation is an insert-conflict ledger, and rows store digests,
  status, and timestamps only.
- `SAMLAuthnRequestRecord` and `SAMLReplayKeyRecord` — typed hash-only rows for
  SAML request creation and replay reservation.

**Recovery**

- `RecoveryStore` / `NewRecoveryStore` — replays `dead_letter` and `failed`
  work items to `pending` and marks collector generation commit failures for
  source-level replay
- `CollectorGenerationDeadLetterStore` /
  `NewCollectorGenerationDeadLetterStore` — records commit failures before
  normal projector work items exist, reports unresolved status aggregates, and
  updates matching generations to `replay_requested` or `replayed`

**Status**

- `StatusStore` / `NewStatusStore` — scope counts, generation counts, stage
  counts, queue depth
- `StatusRequestStore` / `NewStatusRequestStore` — async status request
  persistence
- `GovernanceAuditStore` / `NewGovernanceAuditStore` — validation-safe hosted
  governance audit persistence with retry-idempotent `Append`, private
  operator-authorized bounded `List`, aggregate-only `Summary`, and
  retention-oriented `DeleteExpired`
- `GovernanceAuditEventsSchemaSQL` — idempotent DDL for the private
  `governance_audit_events` sink

**Ingestion**

- `IngestionStore` / `NewIngestionStore` — scope and generation upserts plus
  claim-fenced fact commits that re-check and lock hosted tenant grants before
  writing source facts when the claim mutation carries a tenant boundary

**Relationships**

- `RelationshipStore` / `NewRelationshipStore` — relationship evidence facts
  and backfill
- `RepoScopeResolver` — resolves scope IDs from repository identifiers

**Workflow coordination**

- `WorkflowControlStore` / `NewWorkflowControlStore` — claim, heartbeat,
  release with lease fencing; `ErrWorkflowClaimRejected`, `ClaimSelector`,
  `ClaimMutation`, including optional hosted tenant boundary fields copied into
  claimed fact commits
- `WebhookTriggerStore` / `NewWebhookTriggerStore` —
  `StoreTrigger`, `ClaimQueuedTriggers`, `MarkTriggersHandedOff`,
  `MarkTriggersFailed`, and `WebhookTriggerSchemaSQL`
- `AWSFreshnessStore` / `NewAWSFreshnessStore` —
  coalesced AWS Config/EventBridge freshness triggers with
  `AWSFreshnessSchemaSQL`; `StatusStore` also reads aggregate freshness trigger
  counts and oldest queued age for `/admin/status`
- `VulnerabilitySourceStateStore` / `NewVulnerabilitySourceStateStore` —
  durable OSV/NVD/KEV/EPSS source freshness, checkpoint, retry, and terminal
  state with `VulnerabilitySourceStateSchemaSQL`; `StatusStore` reads the
  bounded source-state rows for `/admin/status`

**Schema bootstrap**

- `BootstrapDefinitions`, `ApplyBootstrap`,
  `ApplyBootstrapWithoutContentSearchIndexes`, `EnsureContentSearchIndexes`,
  `ValidateDefinitions`, `ApplyDefinitions`, `ApplyDefinitionsWithLockTimeout`
- Per-table DDL helpers: `DecisionSchemaSQL`, `RelationshipSchemaSQL`,
  `SharedIntentSchemaSQL`, `SharedProjectionAcceptanceSchemaSQL`,
  `GraphProjectionPhaseStateSchemaSQL`, `GraphProjectionPhaseRepairQueueSchemaSQL`,
  `WorkflowControlSchemaSQL`, `WorkflowCoordinatorStateSchemaSQL`,
  `IaCReachabilitySchemaSQL`, `CodeReachabilitySchemaSQL`,
  `VulnerabilitySourceStateSchemaSQL`, `TenantWorkspaceGrantSchemaSQL`,
  `ScopedAPITokenSchemaSQL`, `IdentitySubjectSchemaSQL`, `OIDCLoginSchemaSQL`,
  `SAMLSSOSchemaSQL`,
  `EshuSearchVectorMetadataSchemaSQL`, `EshuSearchVectorValuesSchemaSQL`,
  `FunctionSummarySchemaSQL`

**IaC reachability**

- `IaCReachabilityStore` / `NewIaCReachabilityStore` — IaC-to-workload
  reachability rows; `IaCReachabilityRow`, `IaCReachability`, `IaCFinding`
- `CodeReachabilityStore` / `NewCodeReachabilityStore` — reducer-materialized
  code reachable-set rows keyed by active source generation; dead-code reads use
  `ListLatestByEntities` before falling back to completed shared intents.

**Freshness checks** (implement `reducer` interfaces)

- `NewAcceptedGenerationLookup` / `NewAcceptedGenerationPrefetch`
- `NewGenerationFreshnessCheck` / `NewPriorGenerationCheck`

**Terraform drift adapters** (implement reducer drift ports for chunk #163)

- `PostgresTerraformBackendQuery` (`tfstate_backend_canonical.go:68`) — answers
  `tfstatebackend.TerraformBackendQuery` from durable parser facts; recomputes
  each row's locator hash with `terraformstate.ScopeLocatorHash` (the
  version-agnostic join key) so the join stays aligned with the state-snapshot
  scope ID built by `scope.NewTerraformStateSnapshotScope`. Using
  `terraformstate.LocatorHash` here would silently reject every drift candidate
  (issue #203).
- `PostgresDriftEvidenceLoader` (`tfstate_drift_evidence.go:56`) — builds the
  per-address `tfconfigstate.AddressedRow` slice from four logical inputs:
  config facts, active state facts, prior-generation state facts (skipped when
  current serial is zero), and prior-config-snapshot addresses. The config and
  backend queries gate on `jsonb_array_length > 0` so files with empty parser
  buckets are not decoded. `PriorConfigDepth` (default 10, set from
  `ESHU_DRIFT_PRIOR_CONFIG_DEPTH`) controls how many prior repo-snapshot
  generations the prior-config walk covers.
  As of issue #169 the loader also walks `terraform_modules` parser facts
  (`buildModulePrefixMap` in
  `tfstate_drift_evidence_module_prefix.go`) to learn which `.tf` files
  live under a `module {}` callee directory. Resources whose path matches a
  callee inherit the canonical
  `module.<name>[.module.<name>...]` prefix so their config-side address
  matches the state-side `terraform state list` shape. The prior-config walk
  builds a prefix map per prior generation before calling
  `collectPriorConfigAddresses` so module-nested `removed_from_config`
  detection stays alive even when a module block is renamed across
  generations. Local-source modules resolve; registry, git, archive, and
  cross-repo sources fall back to `added_in_state` and increment
  `eshu_dp_drift_unresolved_module_calls_total{reason}`. Module rename
  detection increments the same counter with `reason="module_renamed"` once
  per prior generation and callee path.
  Row construction is split across four sibling files:
  - `configRowFromParserEntry` (`tfstate_drift_evidence_config_row.go:22`) —
    maps one HCL-parser `terraform_resources` JSON entry to a
    `tfconfigstate.ResourceRow`; copies the flat dot-path `attributes` map and
    decodes `unknown_attributes` as `ResourceRow.UnknownAttributes`.
  - `stateRowFromCollectorPayload` (`tfstate_drift_evidence_state_row.go:29`)
    — decodes the collector's `terraform_state_resource` payload and calls
    `flattenStateAttributes` (same file, line 90) to produce a flat dot-path
    `map[string]string`. Singleton repeated blocks (e.g. `versioning`,
    `server_side_encryption_configuration`) arrive as `[]any` of length 1
    whose element is `map[string]any`; the flattener unwraps the array and
    recurses into the object so paths align with the parser's dot-path form.
    Multi-element repeated blocks (`len(typed) > 1`) hit the same first-wins
    unwrap and emit a debug-level slog record with
    `LogKeyDriftMultiElementPrefix`, `LogKeyDriftMultiElementCount`, and
    `LogKeyDriftMultiElementSource="state_flatten"` so the dropped signal is
    observable. The dot-path encoding MUST stay byte-identical to
    `ctyValueToDriftString` in
    `go/internal/parser/hcl/terraform_resource_attributes.go` so the
    classifier's value-equality check fires deterministically.
  - `loadPriorConfigAddresses` (`tfstate_drift_evidence_prior_config.go`)
    — walks the most recent `PriorConfigDepth` prior repo-snapshot generations
    for the config scope and returns the union of all declared resource
    addresses. `mergeDriftRows` sets `PreviouslyDeclaredInConfig=true` on
    state-only addresses present in this set, activating `removed_from_config`
    classification as of issue #168. Addresses outside the depth window keep
    `PreviouslyDeclaredInConfig=false` and surface as `added_in_state`. The
    walk is bounded by `listPriorConfigAddressesQuery`'s `LIMIT` so cost
    stays proportional to depth. It builds one prior-generation module-prefix
    map per generation returned by the bounded walk.
  - `buildModulePrefixMap` (`tfstate_drift_evidence_module_prefix.go`) —
    walks `terraform_modules` facts in the same `(scope_id, generation_id)`
    and returns a callee-directory to module-prefix map keyed by
    forward-slash paths. Uses `path.Clean` (NOT `path/filepath.Clean`)
    because the inputs are Postgres-stored strings, not live filesystem
    paths. Bounds the chain at `maxModulePrefixDepth = 10` (hard-coded
    const, no env knob) and breaks cycles with a per-expansion visited
    set. Multiple distinct callers of the same callee produce a slice of
    prefix strings; the loader's emission loop fans out to one
    `ResourceRow` per prefix.
- `IngestionStore.EnqueueConfigStateDriftIntents` (`drift_enqueue.go:61`) —
  Phase 3.5 trigger that enqueues one `config_state_drift` reducer intent per
  active `state_snapshot:*` scope after bootstrap Phase 3 finishes. It records
  `eshu_dp_correlation_drift_intents_enqueued_total` with the number of intents
  attempted so operators can compare queue trigger volume with downstream drift
  admission volume.
