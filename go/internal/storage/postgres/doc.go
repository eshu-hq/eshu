// Package postgres owns Eshu's relational persistence: facts, queue state,
// content store, status, recovery data, decisions, webhook refresh triggers,
// AWS scan status, and workflow coordination tables.
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
// rows and their scope generations must remain atomic. Schema and queue
// contract changes require migration and a matching update to the recovery and
// status surfaces. Status readers include pending shared projection intents and
// lease-only active shared-projection lanes in domain backlog aggregates because
// those rows gate whether reducer-owned graph edges are ready for query truth,
// and ReducerGraphDrain gives local NornicDB code-call projection a read-only
// view of reducer graph-domain backlog before it starts its edge write lane.
// FactStore kind-filtered reads use bounded, stable keyset pages and scan the
// same facts.Envelope metadata shape as full fact loads. Payload value filters
// are available only for top-level payload fields that are part of a reducer
// domain's truth contract. Shared projection intent writes use bounded
// multi-row upserts so high-cardinality package, code-call, and correlation
// facts reduce Postgres round trips without changing idempotency semantics.
// Package correlation indexes cover reducer ownership, publication, and
// consumption fact rows under package_id and repository_id anchors; their v2
// names force existing databases to build the expanded publication predicate.
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
// config commit owning a state snapshot, and the second performs the
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
// AWSCloudRuntimeDriftFindingStore is the fact-backed read side for that
// publication path; it rejects unscoped filters, keeps reads on the
// active generation, validates account and region values before building the
// account-scope LIKE predicate, and caps direct list pages at 500 rows so
// internal callers cannot bypass the query API's bounds.
//
// State-only addresses absent from the prior-config address set keep
// PreviouslyDeclaredInConfig=false and surface as added_in_state — the
// conservative outside-window fallback for operator-imported resources or
// addresses first declared beyond the depth window. The drift queries gate
// on jsonb_array_length > 0 so files whose parser buckets are empty (the
// base-payload default) are not scanned.
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
// received_at order, and records handed-off rows or failed rows with failed_at
// without making repository or graph freshness claims.
package postgres
