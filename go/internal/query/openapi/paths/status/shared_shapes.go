// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// Shared JSON schema snippets for the status, admin, and operator path
// fragments in this package. Every snippet mirrors the Go map builder that
// actually emits the payload (see go/internal/query/status_mappers.go,
// go/internal/query/operator_control_plane.go, and
// go/internal/query/status_hosted_readiness.go); keep each snippet in
// lockstep with its owner. The fragments splice these constants into their
// raw JSON with string concatenation so the queue family is defined once
// instead of drifting across six copies.
const (
	// queueSnapshotSchema is the queueToMap read model served on the
	// operations board, the index status summary (and its legacy alias),
	// and hosted readiness. Provenance upgrade counters exist on the stored
	// QueueSnapshot but this read model does not project them.
	queueSnapshotSchema = `{"type": "object", "description": "Aggregate queue pressure and progress signals. Operations and index status omit this section for scoped callers (see withheld_sections); hosted readiness always serves it.", "properties": {
    "total": {"type": "integer", "description": "Work items known to the queue across all states."},
    "outstanding": {"type": "integer", "description": "Work items not in a terminal state."},
    "pending": {"type": "integer", "description": "Outstanding items with no current claim or lease."},
    "in_flight": {"type": "integer", "description": "Items under an active claim or lease."},
    "retrying": {"type": "integer", "description": "Items scheduled for another attempt after a counted failure."},
    "succeeded": {"type": "integer", "description": "Items that reached a terminal success state."},
    "dead_letter": {"type": "integer", "description": "Items in dead_letter after exhausting retries or a non-retryable failure."},
    "failed": {"type": "integer", "description": "Items in a terminal failure state that is not dead_letter."},
    "oldest_outstanding_age": {"type": "number", "description": "Age in seconds of the oldest outstanding item."},
    "oldest_outstanding_age_ms": {"type": "integer", "description": "Age in whole milliseconds of the oldest outstanding item."},
    "overdue_claims": {"type": "integer", "description": "Claims past their claim_until without a terminal update."}
  }}`

	// healthSchema is the healthToMap read model: a bounded state token
	// plus machine-readable reasons, never backend error text.
	healthSchema = `{"type": "object", "description": "Process health summary: a bounded state token plus machine-readable reasons.", "properties": {
    "state": {"type": "string", "description": "Aggregate health state token."},
    "reasons": {"type": "array", "items": {"type": "string"}, "description": "Machine-readable reasons behind a non-healthy state; empty when healthy."}
  }}`

	// stageSummaryItemSchema is one row of stageSummariesToSlice.
	stageSummaryItemSchema = `{"type": "object", "description": "One pipeline stage row: queued, claimed, running, and terminal counts. Owner: status_mappers.go stageSummariesToSlice.", "properties": {
    "stage": {"type": "string", "description": "Pipeline stage name."},
    "pending": {"type": "integer", "description": "Queued items for this stage with no active claim."},
    "claimed": {"type": "integer", "description": "Items claimed but not yet running."},
    "running": {"type": "integer", "description": "Items actively executing."},
    "retrying": {"type": "integer", "description": "Items scheduled for another attempt."},
    "succeeded": {"type": "integer", "description": "Terminal successes."},
    "dead_letter": {"type": "integer", "description": "Terminal dead-letter items."},
    "failed": {"type": "integer", "description": "Terminal failures outside dead_letter."}
  }}`

	// domainBacklogItemSchema is one row of domainBacklogsToSlice: the
	// domain backlog with its blocked share. blocked counts queue blockages
	// attributed to the domain; scoped callers omit this section.
	domainBacklogItemSchema = `{"type": "object", "description": "One domain backlog row with its blocked share. Owner: status_mappers.go domainBacklogsToSlice.", "properties": {
    "domain": {"type": "string", "description": "Reducer or projection domain name."},
    "outstanding": {"type": "integer", "description": "Backlog items not in a terminal state."},
    "pending": {"type": "integer", "description": "Outstanding items with no active claim (outstanding minus in-flight minus retrying, floored at zero)."},
    "in_flight": {"type": "integer", "description": "Items under an active claim or lease."},
    "blocked": {"type": "integer", "description": "Items held by a queue blockage on this domain."},
    "retrying": {"type": "integer", "description": "Items scheduled for another attempt."},
    "dead_letter": {"type": "integer", "description": "Terminal dead-letter items."},
    "failed": {"type": "integer", "description": "Terminal failures outside dead_letter."},
    "oldest_age": {"type": "number", "description": "Age in seconds of the oldest backlog item."}
  }}`

	// queueBlockageItemSchema is one row of queueBlockagesToSlice. The
	// conflict_key is a raw scope id, which is why scoped callers have this
	// section withheld rather than redacted. oldest_age is seconds as a
	// number on this read model (the runtime RenderJSON pair renders a
	// duration string plus oldest_age_seconds instead).
	queueBlockageItemSchema = `{"type": "object", "description": "One queue blockage: conflicting claims on the same conflict key. Withheld from scoped callers.", "properties": {
    "stage": {"type": "string", "description": "Pipeline stage the blockage sits on. Owner: queueBlockagesToSlice."},
    "domain": {"type": "string", "description": "Reducer or projection domain the blocked items belong to. Owner: queueBlockagesToSlice."},
    "conflict_domain": {"type": "string", "description": "Domain holding the conflicting claim. Owner: queueBlockagesToSlice."},
    "conflict_key": {"type": "string", "description": "Raw scope id both sides claim; a raw identifier, withheld from scoped callers."},
    "holder_domain": {"type": "string", "description": "Domain currently holding the conflict key."},
    "blocked": {"type": "integer", "description": "Items held by this blockage."},
    "oldest_age": {"type": "number", "description": "Age in seconds of the oldest held item."}
  }}`

	// scopeActivitySchema is the scopeActivityToMap read model.
	scopeActivitySchema = `{"type": "object", "description": "Scope generation activity counters. Operations and index status omit this section for scoped callers (see withheld_sections).", "properties": {
    "active": {"type": "integer", "description": "Scopes on the current active generation."},
    "changed": {"type": "integer", "description": "Scopes whose generation changed since the last observation."},
    "unchanged": {"type": "integer", "description": "Scopes steady on the same generation."}
  }}`

	// namedCountItemSchema is one row of namedCountsToSlice: a machine
	// vocabulary name with its count, projected as an array so the empty
	// set renders [] rather than null.
	namedCountItemSchema = `{"type": "object", "description": "One named count: a machine vocabulary name with its count, projected as an array so the empty set renders [] rather than null. Owner: namedCountsToSlice over shared.NamedCount.", "properties": {
    "name": {"type": "string", "description": "Machine vocabulary name of the counted class."},
    "count": {"type": "integer", "description": "Items in this class."}
  }}`

	// coordinatorSchema is the coordinatorToMap read model served on hosted
	// readiness and the index status summary. Optional members are absent,
	// never null, when the underlying snapshot has no value for them.
	coordinatorSchema = `{"type": "object", "description": "Workflow coordinator snapshot: registered instances, claim backpressure, source pacing, status counts, and claim age. Scoped callers receive a reduced variant carrying collector_instance_count with the count blocks, claim totals, and recency instead of the per-instance, backpressure, and pacing detail.", "properties": {
    "collector_instance_count": {"type": "integer", "description": "Number of registered collector instances; present only on the scoped variant, which omits collector_instances."},
    "collector_instances": {"type": "array", "description": "Registered collector instances. Owner: status_mappers.go coordinatorToMap.", "items": {"type": "object", "description": "One registered collector instance. Owner: status_mappers.go coordinatorToMap.", "properties": {
      "instance_id": {"type": "string", "description": "Collector instance identity. Owner: status_mappers.go coordinatorToMap."},
      "collector_kind": {"type": "string", "description": "Collector family the instance belongs to. Owner: status_mappers.go coordinatorToMap."},
      "mode": {"type": "string", "description": "Collector operating mode. Owner: status_mappers.go coordinatorToMap."},
      "enabled": {"type": "boolean", "description": "Whether the instance is enabled. Owner: status_mappers.go coordinatorToMap."},
      "bootstrap": {"type": "boolean", "description": "Whether the instance is a bootstrap instance. Owner: status_mappers.go coordinatorToMap."},
      "claims_enabled": {"type": "boolean", "description": "Whether the instance may claim work. Owner: status_mappers.go coordinatorToMap."},
      "display_name": {"type": "string", "description": "Human-readable instance name. Owner: status_mappers.go coordinatorToMap."},
      "last_observed_at": {"type": "string", "format": "date-time", "description": "When the instance was last observed. Owner: status_mappers.go coordinatorToMap."},
      "updated_at": {"type": "string", "format": "date-time", "description": "When the instance record was last updated. Owner: status_mappers.go coordinatorToMap."},
      "deactivated_at": {"type": ["string", "null"], "format": "date-time", "description": "Deactivation time, or null while the instance is active."}
    }}},
    "collector_backpressure": {"type": "array", "description": "Per-instance claim backpressure. Owner: status_mappers.go coordinatorToMap.", "items": {"type": "object", "description": "Claim backpressure for one collector instance. Owner: status_mappers.go coordinatorToMap.", "properties": {
      "collector_kind": {"type": "string", "description": "Collector family the backpressure belongs to. Owner: status_mappers.go coordinatorToMap."},
      "collector_instance_id": {"type": "string", "description": "Collector instance identity. Owner: status_mappers.go coordinatorToMap."},
      "pending": {"type": "integer", "description": "Backlogged items with no current claim for this instance. Owner: status_mappers.go coordinatorToMap."},
      "claimed": {"type": "integer", "description": "Items under an active claim for this instance. Owner: status_mappers.go coordinatorToMap."},
      "retrying": {"type": "integer", "description": "Items scheduled for another attempt for this instance. Owner: status_mappers.go coordinatorToMap."},
      "dead_letter": {"type": "integer", "description": "Terminal dead-letter items for this instance. Owner: status_mappers.go coordinatorToMap."}
    }}},
    "source_pacing": {"type": "array", "description": "Per-source reducer backpressure pacing. Owner: status_mappers.go coordinatorToMap.", "items": {"type": "object", "description": "Pacing state for one source system. Owner: status_mappers.go coordinatorToMap.", "properties": {
      "source_system": {"type": "string", "description": "Source system the pacing entry belongs to. Owner: status_mappers.go coordinatorToMap."},
      "slowed": {"type": "boolean", "description": "True while this source is slowed; the pacing block is absent when no source is slowed."},
      "oldest_claimable_age": {"type": "number", "description": "Age in seconds of the oldest claimable item."},
      "claimable_depth": {"type": "integer", "description": "Claimable items behind the pacing verdict. Owner: status_mappers.go coordinatorToMap."},
      "reason": {"type": "string", "description": "Why pacing engaged; absent when empty."},
      "engaged_since": {"type": "string", "format": "date-time", "description": "When pacing engaged; absent when zero."},
      "stuck": {"type": "boolean", "description": "True while the paced source is stuck; absent otherwise."}
    }}},
    "run_status_counts": {"type": "array", "description": "Run rows by status. Owner: status_mappers.go coordinatorToMap over namedCountsToSlice.", "items": ` + namedCountItemSchema + `},
    "work_item_status_counts": {"type": "array", "description": "Work items by status. Owner: status_mappers.go coordinatorToMap over namedCountsToSlice.", "items": ` + namedCountItemSchema + `},
    "completeness_counts": {"type": "array", "description": "Completeness rows by state. Owner: status_mappers.go coordinatorToMap over namedCountsToSlice.", "items": ` + namedCountItemSchema + `},
    "active_claims": {"type": "integer", "description": "Claims under an active claim or lease. Owner: status_mappers.go coordinatorToMap."},
    "overdue_claims": {"type": "integer", "description": "Claims past their claim_until without a terminal update. Owner: status_mappers.go coordinatorToMap."},
    "oldest_pending_age": {"type": "number", "description": "Age in seconds of the oldest pending claim."},
    "recent_failures": {"type": "object", "description": "Recent failure window; absent when the snapshot carries none.", "properties": {
      "window_seconds": {"type": "number", "description": "Failure window length in seconds. Owner: status_mappers.go coordinatorToMap."},
      "failed_runs": {"type": "integer", "description": "Runs that failed inside the window. Owner: status_mappers.go coordinatorToMap."},
      "blocked_completeness": {"type": "integer", "description": "Completeness rows blocked inside the window. Owner: status_mappers.go coordinatorToMap."},
      "terminal_work_items": {"type": "integer", "description": "Work items that went terminal inside the window. Owner: status_mappers.go coordinatorToMap."}
    }}
  }}`

	// diagnosticPathItemSchema is one hostedDiagnosticTarget: a named
	// follow-up route for the operator. The set is fixed in code.
	diagnosticPathItemSchema = `{"type": "object", "description": "One hosted diagnostic target: a named follow-up route for the operator. Owner: hosted readiness diagnostics.", "properties": {
    "name": {"type": "string", "description": "Stable diagnostic target name (admin_status, index_status, collector_status, repository_coverage)."},
    "path": {"type": "string", "description": "Route path of the follow-up read, with {repo_id} still templated for repository_coverage."}
  }}`

	// semanticQueueSchema is the audit-safe semantic extraction queue, as
	// served by the standalone semantic route and, when populated, by the
	// index summary. Counts only: no source identifiers, prompts, provider
	// responses, or credentials.
	semanticQueueSchema = `{"type": "object", "description": "Audit-safe semantic extraction queue aggregates without source identifiers, prompts, provider responses, or credentials. Present only when the queue section is populated.", "properties": {
    "total": {"type": "integer", "description": "Queue rows known across all states. Owner: extraction_observability_json.go extractionQueueStatusJSON."},
    "pending": {"type": "integer", "description": "Queued rows with no current claim. Owner: extraction_observability_json.go extractionQueueStatusJSON."},
    "claimed": {"type": "integer", "description": "Rows under an active claim. Owner: extraction_observability_json.go extractionQueueStatusJSON."},
    "retrying": {"type": "integer", "description": "Rows scheduled for another attempt. Owner: extraction_observability_json.go extractionQueueStatusJSON."},
    "succeeded": {"type": "integer", "description": "Rows that reached a terminal success state. Owner: extraction_observability_json.go extractionQueueStatusJSON."},
    "dead_letter": {"type": "integer", "description": "Rows in dead_letter after exhausting retries. Owner: extraction_observability_json.go extractionQueueStatusJSON."},
    "skipped": {"type": "integer", "description": "Rows skipped without processing. Owner: extraction_observability_json.go extractionQueueStatusJSON."},
    "no_provider": {"type": "integer", "description": "Rows with no provider available to process them. Owner: extraction_observability_json.go extractionQueueStatusJSON."},
    "policy_denied": {"type": "integer", "description": "Rows denied by policy. Owner: extraction_observability_json.go extractionQueueStatusJSON."},
    "budget_exhausted": {"type": "integer", "description": "Rows dropped because the extraction budget was exhausted. Owner: extraction_observability_json.go extractionQueueStatusJSON."},
    "unsafe": {"type": "integer", "description": "Rows refused as unsafe. Owner: extraction_observability_json.go extractionQueueStatusJSON."},
    "provider_unavailable": {"type": "integer", "description": "Rows whose provider was unavailable. Owner: extraction_observability_json.go extractionQueueStatusJSON."},
    "unchanged": {"type": "integer", "description": "Rows already current that needed no work. Owner: extraction_observability_json.go extractionQueueStatusJSON."},
    "stale": {"type": "integer", "description": "Stale rows awaiting reprocessing. Owner: extraction_observability_json.go extractionQueueStatusJSON."},
    "status_counts": {"type": "array", "description": "Queue rows by status; absent when none.", "items": ` + namedCountItemSchema + `},
    "source_class_counts": {"type": "array", "description": "Queue rows by source class; absent when none.", "items": ` + namedCountItemSchema + `},
    "failure_class_counts": {"type": "array", "description": "Queue rows by failure class; absent when none.", "items": ` + namedCountItemSchema + `},
    "provider_profile_counts": {"type": "array", "description": "Queue rows by provider profile; absent when none.", "items": {"type": "object", "description": "Queue rows for one provider profile. Owner: extraction_observability_json.go extractionProviderProfileQueueCountsJSON.", "properties": {
      "provider_kind": {"type": "string", "description": "Provider family behind the rows. Owner: extraction_observability_json.go extractionProviderProfileQueueCountsJSON."},
      "provider_profile_id": {"type": "string", "description": "Provider profile identity. Owner: extraction_observability_json.go extractionProviderProfileQueueCountsJSON."},
      "provider_profile_class": {"type": "string", "description": "Provider profile class. Owner: extraction_observability_json.go extractionProviderProfileQueueCountsJSON."},
      "count": {"type": "integer", "description": "Rows for this provider profile. Owner: extraction_observability_json.go extractionProviderProfileQueueCountsJSON."}
    }}},
    "policy_decision_counts": {"type": "array", "description": "Policy-guard decisions; absent when none.", "items": {"type": "object", "description": "One policy-guard decision count. Owner: extraction_observability_json.go extractionDecisionCountsJSON.", "properties": {
      "state": {"type": "string", "description": "Decision state. Owner: extraction_observability_json.go extractionDecisionCountsJSON."},
      "reason": {"type": "string", "description": "Decision reason. Owner: extraction_observability_json.go extractionDecisionCountsJSON."},
      "count": {"type": "integer", "description": "Decisions with this state and reason. Owner: extraction_observability_json.go extractionDecisionCountsJSON."}
    }}},
    "guard_decision_counts": {"type": "array", "description": "Guard decisions; absent when none.", "items": {"type": "object", "description": "One guard decision count. Owner: extraction_observability_json.go extractionDecisionCountsJSON.", "properties": {
      "state": {"type": "string", "description": "Decision state. Owner: extraction_observability_json.go extractionDecisionCountsJSON."},
      "reason": {"type": "string", "description": "Decision reason. Owner: extraction_observability_json.go extractionDecisionCountsJSON."},
      "count": {"type": "integer", "description": "Decisions with this state and reason. Owner: extraction_observability_json.go extractionDecisionCountsJSON."}
    }}},
    "updated_at": {"type": "string", "format": "date-time", "description": "Queue read time; absent when unset."}
  }}`

	// semanticBudgetSchema is the redacted semantic extraction token, cost,
	// and budget decision aggregate. Present only when populated.
	semanticBudgetSchema = `{"type": "object", "description": "Redacted semantic extraction token, cost, and budget decision aggregates. Present only when populated.", "properties": {
    "estimated_input_tokens": {"type": "integer", "description": "Estimated input tokens for the budget window. Owner: extraction_observability_json.go extractionBudgetStatusJSON."},
    "estimated_output_tokens": {"type": "integer", "description": "Estimated output tokens for the budget window. Owner: extraction_observability_json.go extractionBudgetStatusJSON."},
    "estimated_cost_micros": {"type": "integer", "description": "Estimated cost in micro-cents for the budget window. Owner: extraction_observability_json.go extractionBudgetStatusJSON."},
    "actual_input_tokens": {"type": "integer", "description": "Actual input tokens consumed in the budget window. Owner: extraction_observability_json.go extractionBudgetStatusJSON."},
    "actual_output_tokens": {"type": "integer", "description": "Actual output tokens consumed in the budget window. Owner: extraction_observability_json.go extractionBudgetStatusJSON."},
    "actual_cost_micros": {"type": "integer", "description": "Actual cost in micro-cents consumed in the budget window. Owner: extraction_observability_json.go extractionBudgetStatusJSON."},
    "remaining_tokens": {"type": "integer", "description": "Tokens remaining in the budget window. Owner: extraction_observability_json.go extractionBudgetStatusJSON."},
    "remaining_cost_micros": {"type": "integer", "description": "Micro-cents remaining in the budget window. Owner: extraction_observability_json.go extractionBudgetStatusJSON."},
    "exhausted": {"type": "integer", "description": "Budget windows exhausted. Owner: extraction_observability_json.go extractionBudgetStatusJSON."},
    "decision_counts": {"type": "array", "description": "Budget decisions by state, reason, and unit; absent when none.", "items": {"type": "object", "description": "One budget decision count. Owner: extraction_observability_json.go extractionBudgetDecisionCountsJSON.", "properties": {
      "state": {"type": "string", "description": "Budget decision state. Owner: extraction_observability_json.go extractionBudgetDecisionCountsJSON."},
      "reason": {"type": "string", "description": "Budget decision reason. Owner: extraction_observability_json.go extractionBudgetDecisionCountsJSON."},
      "budget_unit": {"type": "string", "description": "Budget unit the decision applies to. Owner: extraction_observability_json.go extractionBudgetDecisionCountsJSON."},
      "count": {"type": "integer", "description": "Decisions with this state, reason, and unit. Owner: extraction_observability_json.go extractionBudgetDecisionCountsJSON."}
    }}}
  }}`

	// semanticAuditSchema is the class-based semantic extraction audit
	// aggregate. Present only when populated.
	semanticAuditSchema = `{"type": "object", "description": "Class-based semantic extraction audit aggregates without principals, source identifiers, prompts, or provider responses. Present only when populated.", "properties": {
    "actor_class_counts": {"type": "array", "description": "Audit rows by actor class. Owner: extraction_observability_json.go extractionAuditStatusJSON.", "items": ` + namedCountItemSchema + `},
    "acl_state_counts": {"type": "array", "description": "Audit rows by ACL state. Owner: extraction_observability_json.go extractionAuditStatusJSON.", "items": ` + namedCountItemSchema + `},
    "last_processed_at": {"type": "string", "format": "date-time", "description": "When the audit last processed a row. Owner: extraction_observability_json.go extractionAuditStatusJSON."}
  }}`

	// awsMaterializationSchema is the aws_materialization read model served on
	// the index status summary and its legacy alias: totals plus the
	// per-domain rows. Owner: query awsMaterializationStatusToMap over
	// status_mappers.go domainBacklogsToSlice.
	awsMaterializationSchema = `{"type": "object", "description": "AWS materialization work aggregates across all domains. Owner: query.StatusHandler.getIndexStatus over awsMaterializationStatusToMap.", "properties": {
    "outstanding": {"type": "integer", "description": "Domain work items not in a terminal state. Owner: query awsMaterializationStatusToMap."},
    "pending": {"type": "integer", "description": "Outstanding items with no current claim. Owner: query awsMaterializationStatusToMap."},
    "in_flight": {"type": "integer", "description": "Items under an active claim. Owner: query awsMaterializationStatusToMap."},
    "blocked": {"type": "integer", "description": "Items held by a queue blockage. Owner: query awsMaterializationStatusToMap."},
    "retrying": {"type": "integer", "description": "Items scheduled for another attempt. Owner: query awsMaterializationStatusToMap."},
    "dead_letter": {"type": "integer", "description": "Terminal dead-letter items. Owner: query awsMaterializationStatusToMap."},
    "failed": {"type": "integer", "description": "Terminal failures outside dead_letter. Owner: query awsMaterializationStatusToMap."},
    "domains": {"type": "array", "description": "Per-domain AWS materialization work aggregates. Owner: query awsMaterializationStatusToMap over status_mappers.go domainBacklogsToSlice.", "items": ` + awsMaterializationDomainItemSchema + `}
  }}`

	// semanticProviderProfilesSchema is one hosted provider profile row set
	// served on the semantic extraction route and the index summary. Owner:
	// query semanticProviderProfilesToMaps over
	// status/semantic.ProviderProfilesJSON.
	semanticProviderProfilesSchema = `{"type": "array", "description": "Hosted provider profiles; present only when at least one profile is configured.", "items": {"type": "object", "description": "One hosted semantic provider profile. Owner: query semanticProviderProfilesToMaps over status/semantic.ProviderProfilesJSON.", "properties": {
    "profile_id": {"type": "string", "description": "Provider profile identity. Owner: status/semantic.ProviderProfilesJSON."},
    "display_name": {"type": "string", "description": "Human-readable profile name; present only when set. Owner: status/semantic.ProviderProfilesJSON."},
    "provider_kind": {"type": "string", "enum": ["anthropic", "openai_compatible", "deepseek", "minimax", "gemini", "bedrock", "azure_openai", "ollama", "internal_gateway"], "description": "Provider family. Owner: status/semantic.ProviderProfilesJSON."},
    "credential_source_kind": {"type": "string", "enum": ["kubernetes_secret", "vault_secret_handle", "environment_variable", "cloud_workload_identity", "local_dev_profile"], "description": "Where the profile credential is stored. Owner: status/semantic.ProviderProfilesJSON."},
    "credential_configured": {"type": "boolean", "description": "Whether the profile credential is configured. Owner: status/semantic.ProviderProfilesJSON."},
    "model_id": {"type": "string", "description": "Model identity; present only when set. Owner: status/semantic.ProviderProfilesJSON."},
    "embedding_dimensions": {"type": "integer", "minimum": 1, "description": "Embedding dimensions; present only when set. Owner: status/semantic.ProviderProfilesJSON."},
    "endpoint_profile_id": {"type": "string", "description": "Endpoint profile identity; present only when set. Owner: status/semantic.ProviderProfilesJSON."},
    "source_classes": {"type": "array", "description": "Source classes the profile serves. Owner: status/semantic.ProviderProfilesJSON.", "items": {"type": "string", "enum": ["documentation", "diagrams_images", "tickets_chat", "code_hints", "search_documents", "agent_reasoning"]}},
    "source_policy_configured": {"type": "boolean", "description": "Whether a source policy is configured for the profile. Owner: status/semantic.ProviderProfilesJSON."},
    "state": {"type": "string", "enum": ["configured", "unconfigured", "healthy", "unhealthy"], "description": "Profile health state. Owner: status/semantic.ProviderProfilesJSON."},
    "reason": {"type": "string", "description": "Human-readable reason behind the state; present only when set. Owner: status/semantic.ProviderProfilesJSON."},
    "detail": {"type": "string", "description": "Profile detail; present only when set. Owner: status/semantic.ProviderProfilesJSON."},
    "updated_at": {"type": "string", "format": "date-time", "description": "When the profile was last updated; present only when set. Owner: status/semantic.ProviderProfilesJSON."}
  }}}`

	// semanticExtractionSchema is the semantic extraction status read model
	// served on the standalone semantic route and, when populated, on the
	// index summary. Counts only, no source identifiers, prompts, or
	// provider responses. Owner: query semanticExtractionStatusToMap.
	semanticExtractionSchema = `{"type": "object", "description": "Semantic extraction readiness and redacted aggregates. Owner: query semanticExtractionStatusToMap.", "properties": {
    "state": {"type": "string", "enum": ["unavailable", "available", "available_but_disabled_for_scope", "disabled_by_policy", "provider_unhealthy"], "description": "Extraction readiness state. Owner: query semanticExtractionStatusToMap."},
    "reason": {"type": "string", "description": "Human-readable reason behind the state. Owner: query semanticExtractionStatusToMap."},
    "provider_configured": {"type": "boolean", "description": "Whether a semantic provider is configured. Owner: query semanticExtractionStatusToMap."},
    "documentation_observations_enabled": {"type": "boolean", "description": "Whether documentation observations are enabled. Owner: query semanticExtractionStatusToMap."},
    "code_hints_enabled": {"type": "boolean", "description": "Whether code hints are enabled. Owner: query semanticExtractionStatusToMap."},
    "deterministic_paths_affected": {"type": "boolean", "description": "Whether deterministic paths are affected. Owner: query semanticExtractionStatusToMap."},
    "deterministic_documentation_unblocked": {"type": "boolean", "description": "True when deterministic documentation is unblocked. Owner: query semanticExtractionStatusToMap."},
    "queue": ` + semanticQueueSchema + `,
    "budget": ` + semanticBudgetSchema + `,
    "audit": ` + semanticAuditSchema + `,
    "provider_profiles": ` + semanticProviderProfilesSchema + `,
    "detail": {"type": "string", "description": "Status detail; present only when set. Owner: query semanticExtractionStatusToMap."},
    "updated_at": {"type": "string", "format": "date-time", "description": "When the status was last updated; present only when set. Owner: query semanticExtractionStatusToMap."},
    "supported_states": {"type": "array", "description": "Closed extraction state vocabulary. Owner: query semanticExtractionStatusToMap over semantic.ExtractionSupportedStates.", "items": {"type": "string"}},
    "supported_provider_profile_states": {"type": "array", "description": "Closed provider-profile state vocabulary. Owner: query semanticExtractionStatusToMap over semantic.ProviderProfileSupportedStates.", "items": {"type": "string"}}
  }}`

	// terraformStateSchema is the terraform_state read model served on the
	// index status summary and its legacy alias. Owner: query
	// terraformStateStatusToMap over the tfstate.Report rows.
	terraformStateSchema = `{"type": "object", "description": "Terraform state warning summary and recent warnings. Owner: query terraformStateStatusToMap.", "properties": {
    "warning_summary": {"type": "array", "description": "Warning counts by kind, reason, scope class, severity, and actionability. Owner: query terraformStateStatusToMap.", "items": {"type": "object", "description": "One warning aggregate. Owner: query terraformStateStatusToMap.", "properties": {
      "warning_kind": {"type": "string", "description": "Warning kind. Owner: query terraformStateStatusToMap."},
      "reason": {"type": "string", "description": "Warning reason. Owner: query terraformStateStatusToMap."},
      "scope_class": {"type": "string", "description": "Scope class the warning applies to. Owner: query terraformStateStatusToMap."},
      "severity": {"type": "string", "description": "Warning severity. Owner: query terraformStateStatusToMap."},
      "actionability": {"type": "string", "description": "Warning actionability. Owner: query terraformStateStatusToMap."},
      "count": {"type": "integer", "description": "Warnings in this aggregate. Owner: query terraformStateStatusToMap."}
    }}},
    "recent_warnings": {"type": "array", "description": "Most recent terraform state warnings. Owner: query terraformStateStatusToMap.", "items": {"type": "object", "description": "One recent terraform state warning. Owner: query terraformStateStatusToMap.", "properties": {
      "safe_locator_hash": {"type": "string", "description": "Non-reversible hash of the state locator; never the raw locator. Owner: query terraformStateStatusToMap."},
      "backend_kind": {"type": "string", "description": "Terraform backend kind. Owner: query terraformStateStatusToMap."},
      "warning_kind": {"type": "string", "description": "Warning kind. Owner: query terraformStateStatusToMap."},
      "reason": {"type": "string", "description": "Warning reason. Owner: query terraformStateStatusToMap."},
      "severity": {"type": "string", "description": "Warning severity. Owner: query terraformStateStatusToMap."},
      "actionability": {"type": "string", "description": "Warning actionability. Owner: query terraformStateStatusToMap."},
      "source": {"type": "string", "description": "Warning source. Owner: query terraformStateStatusToMap."},
      "source_handle": {"type": "string", "description": "Addressable handle for the warning source. Owner: query terraformStateStatusToMap."}
    }}}
  }}`

	// awsMaterializationDomainItemSchema is one row of the
	// aws_materialization domains array served on the index status summary
	// and its legacy alias. Owner:
	// go/internal/query/aws_materialization_status.go:144
	// domainBacklogToMap (normal rows); blockage-only rows (:61-70) carry
	// zero counters and omit oldest_age, so no member is required.
	awsMaterializationDomainItemSchema = `{"type": "object", "description": "Per-domain AWS materialization work aggregates. Blockage-only rows carry zero counters and omit oldest_age.", "properties": {
    "domain": {"type": "string", "description": "Materialization domain name; one row per domain in the AWS materialization set."},
    "outstanding": {"type": "integer", "description": "Domain work items not in a terminal state."},
    "pending": {"type": "integer", "description": "Outstanding items with no current claim or lease."},
    "in_flight": {"type": "integer", "description": "Items under an active claim or lease."},
    "blocked": {"type": "integer", "description": "Items held by a queue blockage on this domain."},
    "retrying": {"type": "integer", "description": "Items scheduled for another attempt."},
    "dead_letter": {"type": "integer", "description": "Terminal dead-letter items."},
    "failed": {"type": "integer", "description": "Terminal failures outside dead_letter."},
    "oldest_age": {"type": "number", "description": "Age in seconds of the oldest backlog item; absent on blockage-only rows."}
  }}`
)
