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
	queueSnapshotSchema = `{"type": "object", "description": "Aggregate queue pressure and progress signals. Scoped callers omit this section; see withheld_sections.", "properties": {
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
	healthSchema = `{"type": "object", "description": "Process health summary. Scoped callers omit this section; see withheld_sections.", "properties": {
    "state": {"type": "string", "description": "Aggregate health state token."},
    "reasons": {"type": "array", "items": {"type": "string"}, "description": "Machine-readable reasons behind a non-healthy state; empty when healthy."}
  }}`

	// stageSummaryItemSchema is one row of stageSummariesToSlice.
	stageSummaryItemSchema = `{"type": "object", "properties": {
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
	domainBacklogItemSchema = `{"type": "object", "properties": {
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
    "stage": {"type": "string"},
    "domain": {"type": "string"},
    "conflict_domain": {"type": "string"},
    "conflict_key": {"type": "string", "description": "Raw scope id both sides claim; a raw identifier, withheld from scoped callers."},
    "holder_domain": {"type": "string", "description": "Domain currently holding the conflict key."},
    "blocked": {"type": "integer", "description": "Items held by this blockage."},
    "oldest_age": {"type": "number", "description": "Age in seconds of the oldest held item."}
  }}`

	// scopeActivitySchema is the scopeActivityToMap read model.
	scopeActivitySchema = `{"type": "object", "description": "Scope generation activity counters. Scoped callers omit this section; see withheld_sections.", "properties": {
    "active": {"type": "integer", "description": "Scopes on the current active generation."},
    "changed": {"type": "integer", "description": "Scopes whose generation changed since the last observation."},
    "unchanged": {"type": "integer", "description": "Scopes steady on the same generation."}
  }}`

	// namedCountItemSchema is one row of namedCountsToSlice: a machine
	// vocabulary name with its count, projected as an array so the empty
	// set renders [] rather than null.
	namedCountItemSchema = `{"type": "object", "properties": {
    "name": {"type": "string", "description": "Machine vocabulary name of the counted class."},
    "count": {"type": "integer", "description": "Items in this class."}
  }}`

	// coordinatorSchema is the coordinatorToMap read model served on hosted
	// readiness and the index status summary. Optional members are absent,
	// never null, when the underlying snapshot has no value for them.
	coordinatorSchema = `{"type": "object", "description": "Workflow coordinator snapshot: registered instances, claim backpressure, source pacing, status counts, and claim age. Scoped callers omit this section; see withheld_sections.", "properties": {
    "collector_instances": {"type": "array", "items": {"type": "object", "properties": {
      "instance_id": {"type": "string"},
      "collector_kind": {"type": "string"},
      "mode": {"type": "string"},
      "enabled": {"type": "boolean"},
      "bootstrap": {"type": "boolean"},
      "claims_enabled": {"type": "boolean"},
      "display_name": {"type": "string"},
      "last_observed_at": {"type": "string", "format": "date-time"},
      "updated_at": {"type": "string", "format": "date-time"},
      "deactivated_at": {"type": ["string", "null"], "format": "date-time", "description": "Deactivation time, or null while the instance is active."}
    }}},
    "collector_backpressure": {"type": "array", "items": {"type": "object", "properties": {
      "collector_kind": {"type": "string"},
      "collector_instance_id": {"type": "string"},
      "pending": {"type": "integer"},
      "claimed": {"type": "integer"},
      "retrying": {"type": "integer"},
      "dead_letter": {"type": "integer"}
    }}},
    "source_pacing": {"type": "array", "items": {"type": "object", "properties": {
      "source_system": {"type": "string"},
      "slowed": {"type": "boolean", "description": "True while this source is slowed; the pacing block is absent when no source is slowed."},
      "oldest_claimable_age": {"type": "number", "description": "Age in seconds of the oldest claimable item."},
      "claimable_depth": {"type": "integer"},
      "reason": {"type": "string", "description": "Why pacing engaged; absent when empty."},
      "engaged_since": {"type": "string", "format": "date-time", "description": "When pacing engaged; absent when zero."},
      "stuck": {"type": "boolean", "description": "True while the paced source is stuck; absent otherwise."}
    }}},
    "run_status_counts": {"type": "array", "items": ` + namedCountItemSchema + `},
    "work_item_status_counts": {"type": "array", "items": ` + namedCountItemSchema + `},
    "completeness_counts": {"type": "array", "items": ` + namedCountItemSchema + `},
    "active_claims": {"type": "integer"},
    "overdue_claims": {"type": "integer"},
    "oldest_pending_age": {"type": "number", "description": "Age in seconds of the oldest pending claim."},
    "recent_failures": {"type": "object", "description": "Recent failure window; absent when the snapshot carries none.", "properties": {
      "window_seconds": {"type": "number"},
      "failed_runs": {"type": "integer"},
      "blocked_completeness": {"type": "integer"},
      "terminal_work_items": {"type": "integer"}
    }}
  }}`

	// diagnosticPathItemSchema is one hostedDiagnosticTarget: a named
	// follow-up route for the operator. The set is fixed in code.
	diagnosticPathItemSchema = `{"type": "object", "properties": {
    "name": {"type": "string", "description": "Stable diagnostic target name (admin_status, index_status, collector_status, repository_coverage)."},
    "path": {"type": "string", "description": "Route path of the follow-up read, with {repo_id} still templated for repository_coverage."}
  }}`
)
