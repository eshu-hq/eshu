// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package postgres owns Eshu's relational persistence: facts, queue state,
// content store, source-backed repository refs, status, recovery data,
// projection and admission decisions, webhook refresh triggers, incident
// freshness triggers, AWS scan status, hosted tenant/workspace grant state, and
// workflow coordination tables.
//
// The package wraps the Postgres driver with OTEL-instrumented helpers and
// exposes typed access to queue claim, lease, batch, and recovery
// operations. Callers must respect transaction scope, lease timing,
// per-scope projector ordering, pending-or-active generation freshness checks,
// stale-generation coalescing, terminal-failure supersession, live-generation
// supersession, expired-lease priority, duplicate-lease reclaim, idempotency
// keys, and partial-failure behavior documented on each helper; queue and
// status writes are retry-safe by design and must stay that way. ReducerQueue
// lifecycle methods and helper methods are split across sibling files, but
// share the same lease, retry, and scan contract. Supersession of projector
// rows and their scope generations must remain atomic. Reducer claims
// supersede unleased stale same-scope rows from generations older than the
// active generation, and status/drain/observer reads exclude those inactive
// rows from live readiness while preserving durable audit rows. Schema and
// queue contract changes require migration and a matching update to the
// recovery and status surfaces. Status readers include pending shared
// projection intents and lease-only active shared-projection lanes in domain
// backlog aggregates because those rows gate whether reducer-owned graph edges
// are ready for query truth. Generation liveness leaves exact cross-repository
// repo_dependency source runs with that shared resolver instead of reopening
// source-local projection; stuck-age reporting uses the same ownership rule.
// Content substring index readiness requires exact full trigram GIN indexes
// for file content, entity source, and entity names; bulk bootstrap defers all
// three and publishes readiness only after catalog-shape validation succeeds.
// SQLDB bootstrap owns one bounded session-level advisory lock on one
// connection for the complete definition sequence, while each definition
// retains its own lock timeout. Migration 062 also takes a transaction-scoped
// advisory lock around the entity-name index check-and-create boundary so
// concurrent migrators cannot race on the same catalog name.
// ReducerGraphDrain gives local NornicDB
// code-call projection a read-only view of reducer graph-domain backlog before
// it starts its edge write lane. Shared projection partition leases use a
// domain-scoped advisory lock and reject active partition-count rescaling for
// one domain so remapped file partitions cannot overlap old in-flight writer
// claims. CloudResource-consuming reducer edge domains stay unclaimed until
// their matching cloud_resource_uid canonical-nodes phase exists, including
// provider relationship domains such as AWS and Azure.
// Resource materialization conflict keys are versioned and hashed before they
// reach durable queue rows; domains whose handlers still load, write, or
// retract whole scope generations stay behind an explicit resource-scope
// fallback fence until partition-filtered semantics are proven.
// CodeValueFlowCurrentGenerationStore pages active repository scope generations
// from ingestion_scopes so reducer-owned value-flow stale cleanup can run even
// when a current generation emits no value-flow facts.
// ValueFlowProgramInputStore loads active completed CALLS edges with repo-scoped
// function summaries and function_sources so reducer Program assembly has
// durable param-level source ports without reading transient parser memory.
// ValueFlowFixpointComponentStore persists solved value-flow component results
// by content-derived component key so unchanged components can be reused across
// reducer restarts without widening reducer transactions around the solve.
// CollectorGenerationDeadLetterStore persists bounded commit-failure metadata
// for generations that failed before projector work rows existed and exposes a
// source-level replay request/completion path plus unresolved status aggregates
// without storing consumed fact payloads.
// GenerationRetentionStore prunes superseded source-generation history in
// bounded transactions after recording safe hashed retention events; changed
// since queries use those events to report retention_expired instead of a false
// empty delta when a prior generation has aged out.
// FactStore kind-filtered reads use bounded, stable keyset pages and scan the
// same facts.Envelope metadata shape as full fact loads. Fact writes remove
// JSONB-incompatible U+0000 characters without changing literal source text
// such as `\u0000`. Payload value filters are available only for top-level
// payload fields that are part of a reducer domain's truth contract. Active
// code-call symbol definition reads join
// through ingestion_scopes.active_generation_id and only return non-tombstoned
// file facts whose parsed definitions match the requested stable symbol
// allowlist. Shared projection intent writes use bounded multi-row upserts so
// high-cardinality package, code-call, and correlation facts reduce Postgres
// round trips without changing idempotency semantics.
// Exact shared-intent retries preserve completed_at; generation-scoped refresh
// history prevents an older completion from opening a later generation's
// repo-wide retract fence when source_run_id is reused.
// AdmissionDecisionStore persists reducer-owned correlation admission outcomes
// and redaction-safe evidence handles under a scope/generation/domain boundary;
// rejected, ambiguous, stale, hidden, unsupported, and unsafe rows explain why
// canonical graph writes were skipped without promoting those candidates to
// truth. The store rejects unsupported admission states before write execution,
// and SQL keeps the same closed vocabulary as a migration-level guardrail.
// Relationship evidence backfill reads latest file/content facts plus
// gcp_cloud_relationship facts so cloud provider relationships without file
// content can still flow through the resolver's catalog-admission contract.
// Streaming commit-time relationship discovery remains repository-scope only.
// LoadSecretsIAMTrustChainEvidence expands active facts through explicit
// redaction-safe anchors only: Kubernetes service-account joins, AWS role and
// web-identity subject fingerprints, Vault policy/path joins, GCP principal
// fingerprints, GCP service-account email digests, and GKE Workload Identity
// subject fingerprints. New secrets/IAM hops must update both the SQL predicate
// and in-memory anchor extractor rather than broad-scanning active facts.
// PackageRegistryIdentityLocker uses transaction-scoped advisory locks to
// coordinate package UID graph writes across projector processes; callers must
// acquire sorted, de-duplicated keys and keep the protected callback bounded to
// the canonical graph write.
// Package correlation indexes cover reducer ownership, publication, and
// consumption fact rows under package_id and repository_id anchors; their v2
// names force existing databases to build the expanded publication predicate.
// Service-catalog correlation indexes cover exact repository IDs and ambiguous
// candidate repository IDs so repository-scoped API and MCP reads stay bounded
// while explaining ambiguous catalog evidence.
// Container-image identity active reads are similarly bounded by a partial
// active-reference index over OCI digest/tag observations and Git/AWS image
// references, so an OCI scan can re-evaluate existing runtime references
// without scanning unrelated facts.
// Documentation fact readbacks use visible finding, source, packet, and
// target-reference indexes so repo- or service-scoped documentation queries can
// distinguish raw target facts from admissible findings without a broad JSONB
// scan.
// Eshu search-document reads use reducer-maintained BM25 postings and stats for
// the active generation so API/MCP semantic-search requests do not rebuild a
// full repository corpus index. Vector metadata rows track additive ANN build
// state and vector value rows persist derived numeric embeddings by active
// generation, model, content hash, and index version without changing API/MCP
// query behavior.
// EshuSearchVectorScopeStateStore lists active curated search-document scopes
// from versioned projection/scope state and exact-checks readiness against the
// persisted search-index projection. Vector metadata and value batch writes
// carrying a projection revision and build fence are accepted only for the
// current active generation, ready projection, building vector scope, and
// projected document content hash. EshuSearchVectorPendingStore retains the
// retired corpus-wide fact scan only as an equivalence-test reference; neither
// store builds vectors itself.
// CodeReachabilityStore persists reducer-materialized code reachable-set rows
// keyed by active generation plus per-repository completion watermarks so
// dead-code reads can use a standing lookup before falling back to completed
// relationship intents, and empty reachable-set snapshots still make durable
// progress.
// FactStore.LoadIncidentRoutingRawEvidence serves the PagerDuty incident-routing
// graph materialization domain by returning the raw incident and routing fact
// envelopes undecoded (the reducer decodes them through the typed
// sdk/go/factschema seam) plus the Terraform-source PagerDutyDeclaration content
// rows resolved through a bounded service-name allowlist.
// Current
// source-run history lookups let chunked code-call projection avoid retracting
// edges written by earlier chunks from the same accepted run. StatusStore also
// runs the bounded Terraform-state admin queries from tfstate_status.go: one
// row per state_snapshot scope keyed by safe locator hash, plus up to
// MaxTerraformStateRecentWarnings recent warning_fact rows per locator so the
// admin status surface shows tfstate liveness without scanning the fact stream.
// PostgresTerraformBackendQuery and PostgresDriftEvidenceLoader serve the
// reducer's Terraform config-vs-state drift handler: the first answers
// tfstatebackend.TerraformBackendQuery from durable parser facts (recomputing
// each row's locator hash with terraformstate.ScopeLocatorHash so the join
// stays aligned with the version-agnostic state-snapshot scope ID — see
// issue #203) so the resolver can deterministically pick the latest sealed
// config commit owning a state snapshot. It shares the Terraform backend
// candidate helper with graph discovery, including same-module literal
// variable/local recovery and the fail-closed treatment of unresolved backend
// expressions, so discovery and config-owner lookup cannot disagree on a
// locator. The second performs the
// four-input join across
// terraform_resources (config), the active terraform_state_resource rows,
// the prior generation (skipping the prior lookup when current serial is
// zero), and prior-config-snapshot addresses (the union of declared
// addresses across the most recent PriorConfigDepth prior repo-snapshot
// generations). Row construction is split across three sibling files:
// tfstate_drift_evidence_config_row.go provides configRowFromParserEntry,
// which maps each HCL-parser terraform_resources entry to a
// tfconfigstate.ResourceRow by copying the flat dot-path attributes map and
// decoding unknown_attributes; tfstate_drift_evidence_state_row.go provides
// stateRowFromCollectorPayload and flattenStateAttributes, which decode the
// collector payload and recursively produce a flat dot-path map so singleton
// repeated blocks (e.g. versioning, server_side_encryption_configuration)
// produce paths that match the parser's config-side dot-path form;
// tfstate_drift_evidence_prior_config.go provides loadPriorConfigAddresses,
// which walks prior repo-snapshot generations, builds one module-prefix map
// per prior generation, and returns the address set used by mergeDriftRows to
// set PreviouslyDeclaredInConfig=true on state-only addresses — activating
// removed_from_config classification as of issue #168. The dot-path encoding
// produced by coerceJSONString and
// flattenStateAttributes must stay byte-identical to ctyValueToDriftString
// in go/internal/parser/hcl/terraform_resource_attributes.go; the
// classifier's value-equality check depends on both sides agreeing at the
// leaf level. IngestionStore.EnqueueConfigStateDriftIntents is the bootstrap
// Phase 3.5 trigger that enqueues one config_state_drift reducer intent per
// active state_snapshot:* scope and records
// eshu_dp_correlation_drift_intents_enqueued_total for enqueue-volume
// diagnostics.
// PostgresAWSCloudRuntimeDriftEvidenceLoader serves the reducer's AWS
// runtime drift handler: it loads current aws_resource facts for one AWS
// scope generation, joins only active terraform_state_resource facts whose
// attributes.arn is in that AWS allowlist, resolves each state_snapshot
// backend to the owning config snapshot, and reuses the Terraform
// config-resource loader to decide whether a state-backed resource is truly
// absent from config. Unresolved backend ownership produces unknown-management
// evidence and ambiguous backend ownership produces ambiguous-management
// evidence because config absence is not proven.
// PostgresMultiCloudRuntimeDriftEvidenceLoader serves the reducer's
// provider-neutral runtime drift handler: it loads observed AWS, GCP, and Azure
// inventory facts for one scope generation, resolves each provider raw identity
// into the shared cloud_resource_uid keyspace, joins active
// terraform_state_resource facts whose provider-native identity re-resolves to
// the same uid, and reuses the AWS loader's state-backend config resolution to
// decide config presence. It keys every layer on the canonical uid instead of an
// ARN so AWS, GCP, and Azure share one drift path, and it never fabricates a uid
// for an identity that cannot key into the shared keyspace.
// AWSCloudRuntimeDriftFindingStore is the fact-backed read side for that
// publication path; it rejects unscoped filters, keeps reads on the
// active generation, validates account and region values before building the
// account-scope LIKE predicate, and caps direct list pages at 500 rows so
// internal callers cannot bypass the query API's bounds. Decode-failure logs
// for this path use telemetry safe-resource fields so operator logs can
// correlate the row without exposing raw cloud resource identifiers.
// SBOM/attestation attachment readers use the same active-generation keyset
// page shape, with referrer-subject, subject-digest, document, document-digest,
// and status indexes so MCP/API reads stay digest-first and bounded.
// TenantWorkspaceGrantStore persists the additive hosted isolation control
// plane: tenants, workspaces, scope grants, and repository grants. It stores
// opaque IDs plus redacted display-handle hashes, requires tenant/workspace
// bounds on reads, filters out inactive, tombstoned, not-yet-effective, and
// expired rows inside SQL, and keeps repository grants joined to an active
// scope grant so later read enforcement cannot widen beyond source-scope
// truth. PrimaryWorkspaceForTenant resolves a tenant's single active
// workspace_id for callers that hold a tenant-scoped record with no workspace
// of its own (a DB-backed OIDC provider login-start); it fails closed with
// ErrTenantWorkspaceAmbiguous when a tenant has more than one active workspace
// and ErrTenantWorkspaceNotFound when it has none, so the caller must require
// an explicit workspace_id rather than guess.
// ScopedAPITokenStore adds the default-off hash-only token registry for that
// control plane. It stores bearer-token digests, scoped subject hashes, expiry,
// revocation, and policy revision hashes while relying on the tenant/workspace
// tables for active boundary checks.
// BrowserSessionStore adds server-managed dashboard session persistence for the
// API console. It stores only session and CSRF digests, copies the active
// workspace policy revision when a scoped-token registry entry omits optional
// audit metadata, and rejects resolution if the tenant, workspace, expiry,
// revocation, CSRF proof, or policy revision no longer matches.
// IdentitySubjectStore owns the user-management schema for users, provider
// configs, external subject links, email history, credential hashes, MFA factor
// handles, memberships, roles, grants, sessions, service principals,
// service-principal role assignments, and token metadata. Local identity writes
// stay hash-only: first-owner bootstrap is advisory-lock serialized, invited
// signup row-locks the invite, failed-attempt lockouts increment atomically,
// and break-glass recovery windows are consumed on session creation.
// OIDCLoginStore persists hash-only Authorization Code state and resolves
// external group hashes through active Eshu role, scope, and repository grants
// at login time without storing raw provider tokens or group names.
// SAMLSSOStore adds hash-only AuthnRequest and replay ledgers for SAML browser
// login. IdentitySubjectStore resolves SAML external subjects from hash-only
// provider, subject, and group-claim inputs through active provider, user,
// membership, role, and all-scope role-grant rows. These paths keep raw SAML
// assertions, NameID, group values, certificates, provider secrets, and IdP
// metadata XML out of Postgres.
// WorkflowControlStore persists optional
// tenant/workspace/policy revision identity on workflow work items; guarded
// planning treats that identity as part of target eligibility, and claim
// heartbeat/complete paths re-check active tenant scope grants before accepting
// stale hosted work. IngestionStore re-checks and locks the same active tenant
// grant before claimed source facts are written, so grant revocation waits for
// the commit transaction instead of racing past it.
//
// State-only addresses absent from the prior-config address set keep
// PreviouslyDeclaredInConfig=false and surface as added_in_state — the
// conservative outside-window fallback for operator-imported resources or
// addresses first declared beyond the depth window. The drift queries gate
// on jsonb_array_length > 0 so files whose parser buckets are empty (the
// base-payload default) are not scanned.
// Terraform-state admin status warning reads also carry warning
// severity/actionability from fact payloads so API status can distinguish
// blocking source evidence from accepted parser guardrails without inspecting
// raw warning fact history.
//
// As of issue #169 the loader also walks terraform_modules facts within the
// active commit anchor and builds a callee-directory to module-prefix map
// (tfstate_drift_evidence_module_prefix.go) so resources declared inside a
// local-source module {} block join on the canonical state-side address
// shape `module.<name>[.module.<name>...].<type>.<name>`. Local sources only
// in v1; registry, git, archive, and cross-repo sources fall back to
// added_in_state with a per-call increment of
// eshu_dp_drift_unresolved_module_calls_total{reason}. Module renames across
// generations increment that counter with reason module_renamed once per prior
// generation and callee path. The module-prefix helpers normalize
// forward-slash paths exclusively (path package, not
// path/filepath) because terraform_modules.path is a Postgres-stored string,
// not a live filesystem path.
// WebhookTriggerStore persists provider webhook trigger decisions in
// webhook_refresh_triggers, deduplicates refresh requests by refresh_key, moves
// a prior ignored row back to queued when a later accepted delivery has the
// same refresh key, claims queued triggers with FOR UPDATE SKIP LOCKED in
// received_at order, records handed-off rows or failed rows with failed_at,
// and preserves merged pull-request provenance without making repository or
// graph freshness claims.
// IncidentFreshnessStore persists PagerDuty and Jira webhook wake-ups in
// incident_freshness_triggers, coalesces duplicate source events by
// freshness_key, claims queued rows with FOR UPDATE SKIP LOCKED, and records
// coordinator handoff or failure without storing provider payloads or emitting
// facts. ReducerQueue readiness gates keep EC2 internet-exposure work waiting
// for the EC2 instance CloudResource canonical-nodes phase before graph writes
// are claimed.
// GovernanceAuditStore persists validation-safe hosted governance audit events
// in a private sink, derives deterministic event ids for retry idempotency, and
// exposes only authorized bounded detailed reads plus aggregate summaries for
// status surfaces.
// StatusStore.ListGenerationLifecycle serves the bounded generation lifecycle
// drilldown: one ordered page (observed_at DESC, generation_id ASC) of
// scope_generations joined with ingestion_scopes, a correlated LATERAL queue
// rollup over fact_work_items per (scope_id, generation_id), and the latest
// per-generation failure. It fetches the page limit plus one row to set
// Truncated and clamps the requested limit through the status filter so a broad
// scan cannot return an unbounded payload.
// StatusStore.ComputeChangedSinceDelta serves the bounded repository-scope
// changed-since delta. It resolves the scope and its current active generation,
// resolves the prior generation named by since_generation_id or observed at or
// before since_observed_at, then diffs the two fact_records sets keyed by
// (scope_id, generation_id, stable_fact_key) via a FULL OUTER JOIN on
// (fact_category, stable_fact_key) using md5(payload::text) for payload
// identity. Counts are exact per category and verdict (added, updated,
// unchanged, retired, superseded); sample reads run only for non-empty buckets
// and are ordered by stable_fact_key and capped at the sample limit plus one to
// set Truncated. An unknown scope returns an empty ScopeID, an unresolved since
// reference returns an empty SinceGenerationID, and a scope with no current
// active generation returns Unavailable so callers never read zero deltas as
// confident truth.
// ReducerInputInvalidFactStore persists durable reducer_input_invalid_facts
// rows (issue #4630) and implements reducer.QuarantinedFactWriter directly,
// mirroring GraphProjectionPhaseStateStore's direct implementation of
// reducer.GraphProjectionPhasePublisher. WriteQuarantinedFacts batches records
// into bounded multi-row INSERT statements with an ON CONFLICT (scope_id,
// generation_id, fact_id, missing_field) DO NOTHING clause, so replaying the
// same reduction (a retried intent or a re-projected generation) converges on
// one row per fact/field rather than duplicating it or erroring.
package postgres
