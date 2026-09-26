// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

// SpanQueryFreshnessServiceChangedSince wraps the bounded service-scope
// changed-since delta read (#1943) that diffs a prior service materialization
// generation's evidence snapshot set against the current active generation's set
// in service_evidence_snapshots.
const SpanQueryFreshnessServiceChangedSince = "query.freshness_service_changed_since"

const (
	// SpanAttrServiceChangedSinceServiceID records the resolved service id of the
	// service-scope changed-since diff.
	SpanAttrServiceChangedSinceServiceID = "eshu.service_changed_since.service_id"
	// SpanAttrServiceChangedSinceSinceGenerationID records the prior service
	// generation the diff compared against.
	SpanAttrServiceChangedSinceSinceGenerationID = "eshu.service_changed_since.since_generation_id"
	// SpanAttrServiceChangedSinceCurrentGenerationID records the current active
	// service generation the diff compared against.
	SpanAttrServiceChangedSinceCurrentGenerationID = "eshu.service_changed_since.current_generation_id"
	// SpanAttrServiceChangedSinceChangedCount records the total added, updated,
	// retired, and superseded evidence keys across all families.
	SpanAttrServiceChangedSinceChangedCount = "eshu.service_changed_since.changed_count"
	// SpanAttrServiceChangedSinceUnavailable records whether the diff could not be
	// computed because the service had no current active generation.
	SpanAttrServiceChangedSinceUnavailable = "eshu.service_changed_since.unavailable"
	// SpanAttrServiceChangedSinceGrantRefused records that the caller's
	// repository grant refused the lineage read before it ran (#5167). The
	// route answers a refusal with the same body an unknown service gets, so
	// this attribute is the only thing that tells an operator the two apart.
	// It is set only on a refusal; its absence on a served request is the
	// other half of the signal.
	SpanAttrServiceChangedSinceGrantRefused = "eshu.service_changed_since.grant_refused"
	// SpanAttrServiceChangedSinceGrantRefusedReason records which refusal
	// applied, drawn from the closed ServiceChangedSinceGrantRefusal*
	// vocabulary below. It never carries a service id, tenant, workspace,
	// repository, or scope value, so the attribute stays low-cardinality and
	// non-identifying.
	SpanAttrServiceChangedSinceGrantRefusedReason = "eshu.service_changed_since.grant_refused_reason"
	// SpanAttrServiceChangedSinceUnattributed records that the served diff read
	// an unattributed legacy lineage (scope_id NULL), which only an unscoped
	// caller can resolve (#6475). A steady rate means legacy lineage rows are
	// still being read and have not been re-materialized under a scope.
	SpanAttrServiceChangedSinceUnattributed = "eshu.service_changed_since.unattributed"
	// SpanAttrServiceChangedSinceAmbiguousScopeCount records how many admitted
	// scope ids a 409 ambiguity answer listed (#6475). It is a count, never the
	// scope ids themselves, so it stays low-cardinality and non-identifying.
	SpanAttrServiceChangedSinceAmbiguousScopeCount = "eshu.service_changed_since.ambiguous_scope_count"
)

// The closed vocabulary for SpanAttrServiceChangedSinceGrantRefusedReason.
// Operators alert on these strings, so adding a value is a deliberate contract
// change rather than an implementation detail.
const (
	// ServiceChangedSinceGrantRefusalEmptyGrant marks a scoped caller whose
	// grant names no repository and no scope. The refusal happens before any
	// store call; the lineage SQL would also resolve nothing for it.
	ServiceChangedSinceGrantRefusalEmptyGrant = "empty_grant"
	// ServiceChangedSinceGrantRefusalNotGranted marks a scoped caller for whom
	// the requested service id holds lineage rows, but none in a scope its
	// grant admits (#6475): the lineage belongs to another tenant's scope, or
	// it is an unattributed legacy lineage no scoped caller may read.
	ServiceChangedSinceGrantRefusalNotGranted = "not_granted"
)
