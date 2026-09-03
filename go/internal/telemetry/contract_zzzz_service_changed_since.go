// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import "slices"

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
)

// The closed vocabulary for SpanAttrServiceChangedSinceGrantRefusedReason.
// Operators alert on these strings, so adding a value is a deliberate contract
// change rather than an implementation detail.
const (
	// ServiceChangedSinceGrantRefusalEmptyGrant marks a scoped caller whose
	// grant names no repository and no scope. The refusal happens before any
	// store call, because the correlation read's grant clause is permissive on
	// two empty arrays.
	ServiceChangedSinceGrantRefusalEmptyGrant = "empty_grant"
	// ServiceChangedSinceGrantRefusalNotGranted marks a scoped caller whose
	// grant resolved no service-catalog correlation for the requested service:
	// either another tenant owns it, or its catalog entity is gone.
	ServiceChangedSinceGrantRefusalNotGranted = "not_granted"
	// ServiceChangedSinceGrantRefusalSharedOwnership marks a scoped caller
	// whose grant covers some, but not all, of the service-catalog
	// correlations for the requested service id. Catalog service ids are
	// catalog-relative rather than tenant-qualified, and the lineage tables
	// carry no scope column, so a contested id has one lineage and no way to
	// say whose. A run of these is a genuine id collision across tenants, not
	// a misconfigured grant.
	ServiceChangedSinceGrantRefusalSharedOwnership = "shared_ownership"
	// ServiceChangedSinceGrantRefusalOwnershipUnwired marks a deployment with
	// no service-ownership store wired. It fails closed for every scoped
	// caller, so a run of these means a wiring bug, not a tenant boundary.
	ServiceChangedSinceGrantRefusalOwnershipUnwired = "ownership_unwired"
)

func init() {
	for idx, name := range spanNames {
		if name == SpanQueryFreshnessChangedSince {
			spanNames = slices.Insert(spanNames, idx+1, SpanQueryFreshnessServiceChangedSince)
			return
		}
	}
	spanNames = append(spanNames, SpanQueryFreshnessServiceChangedSince)
}
