// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "strings"

// ReplatformingSourceState is the provider-neutral per-item evidence state used
// by replatforming plans and migration packets. It lets API, MCP, and CLI
// clients compare AWS, GCP, and Azure evidence with one vocabulary without
// flattening provider-specific source truth: providers keep their own fact and
// status names, and only the query-facing item state is normalized into this
// taxonomy.
//
// The taxonomy is a superset of the multi-cloud collector contract per-item
// states (see docs/public/reference/multi-cloud-collector-contract.md) plus the
// ambiguous, unknown, and rejected states that replatforming planning needs to
// keep conflicting, unproven, and safety-gated findings distinct from confident
// evidence.
type ReplatformingSourceState string

const (
	// ReplatformingSourceStateExact means provider evidence and reducer-owned
	// canonical identity agree for the item; for AWS this is a resource managed
	// by Terraform across cloud, state, and config.
	ReplatformingSourceStateExact ReplatformingSourceState = "exact"
	// ReplatformingSourceStateDerived means a deterministic correlation exists
	// but it is not direct, full provider-plus-config proof.
	ReplatformingSourceStateDerived ReplatformingSourceState = "derived"
	// ReplatformingSourceStatePartial means the collector could read only part
	// of the configured scope or content family for the item.
	ReplatformingSourceStatePartial ReplatformingSourceState = "partial"
	// ReplatformingSourceStateAmbiguous means multiple deterministic ownership
	// signals conflict and must not be promoted to a single owner.
	ReplatformingSourceStateAmbiguous ReplatformingSourceState = "ambiguous"
	// ReplatformingSourceStateStale means the latest accepted evidence is older
	// than the configured freshness window.
	ReplatformingSourceStateStale ReplatformingSourceState = "stale"
	// ReplatformingSourceStateUnavailable means the source was configured but
	// unreachable, unauthorized, or rate-limited without current evidence.
	ReplatformingSourceStateUnavailable ReplatformingSourceState = "unavailable"
	// ReplatformingSourceStateUnsupported means the provider, tier, API,
	// resource family, or relationship type does not expose this evidence.
	ReplatformingSourceStateUnsupported ReplatformingSourceState = "unsupported"
	// ReplatformingSourceStateUnknown means coverage or permission gaps keep the
	// item's evidence unproven. It is the fail-safe state for unrecognized
	// inputs so a new source status never silently masquerades as confident.
	ReplatformingSourceStateUnknown ReplatformingSourceState = "unknown"
	// ReplatformingSourceStateRejected means a safety gate rejected promoting the
	// read-only finding into ownership truth or migration automation. It wins
	// over the evidence-derived state so an unsafe item is never presented as
	// ready.
	ReplatformingSourceStateRejected ReplatformingSourceState = "rejected"
)

// allReplatformingSourceStates is the canonical, stable order of the taxonomy.
// Order is part of the contract: docs, capability rows, and clients rely on it.
var allReplatformingSourceStates = []ReplatformingSourceState{
	ReplatformingSourceStateExact,
	ReplatformingSourceStateDerived,
	ReplatformingSourceStatePartial,
	ReplatformingSourceStateAmbiguous,
	ReplatformingSourceStateStale,
	ReplatformingSourceStateUnavailable,
	ReplatformingSourceStateUnsupported,
	ReplatformingSourceStateUnknown,
	ReplatformingSourceStateRejected,
}

// AllReplatformingSourceStates returns the taxonomy states in canonical order.
// The returned slice is a copy; callers may mutate it freely.
func AllReplatformingSourceStates() []ReplatformingSourceState {
	out := make([]ReplatformingSourceState, len(allReplatformingSourceStates))
	copy(out, allReplatformingSourceStates)
	return out
}

// Valid reports whether s is a member of the taxonomy.
func (s ReplatformingSourceState) Valid() bool {
	for _, known := range allReplatformingSourceStates {
		if s == known {
			return true
		}
	}
	return false
}

// ReplatformingSourceStateForManagementStatus maps an AWS IaC management status
// deterministically into the provider-neutral taxonomy. Unrecognized or empty
// input maps to unknown so a future AWS status cannot silently present as
// confident evidence. This mapping mirrors the AWS runtime-drift outcome
// semantics and adds no new AWS fact names.
func ReplatformingSourceStateForManagementStatus(status string) ReplatformingSourceState {
	switch strings.TrimSpace(status) {
	case managementStatusManagedByTerraform:
		return ReplatformingSourceStateExact
	case managementStatusTerraformStateOnly,
		managementStatusTerraformConfigOnly,
		managementStatusCloudOnly,
		managementStatusManagedByOtherIaC:
		return ReplatformingSourceStateDerived
	case managementStatusAmbiguous:
		return ReplatformingSourceStateAmbiguous
	case managementStatusStaleIaCCandidate:
		return ReplatformingSourceStateStale
	case managementStatusUnknown:
		return ReplatformingSourceStateUnknown
	default:
		return ReplatformingSourceStateUnknown
	}
}

// ReplatformingSourceStateForMultiCloudQueryState adopts a multi-cloud collector
// contract per-item state (exact, derived, partial, stale, unavailable, or
// unsupported) into the taxonomy. GCP and Azure keep their provider-specific
// fact names; only the query-facing item state is normalized. Unrecognized or
// empty input maps to unknown.
func ReplatformingSourceStateForMultiCloudQueryState(state string) ReplatformingSourceState {
	switch candidate := ReplatformingSourceState(strings.TrimSpace(state)); candidate {
	case ReplatformingSourceStateExact,
		ReplatformingSourceStateDerived,
		ReplatformingSourceStatePartial,
		ReplatformingSourceStateStale,
		ReplatformingSourceStateUnavailable,
		ReplatformingSourceStateUnsupported:
		return candidate
	default:
		return ReplatformingSourceStateUnknown
	}
}

// ResolveReplatformingSourceState returns the effective taxonomy state for an
// item, applying the rejected safety gate on top of the evidence-derived state.
// When promotionRejected is true the item is rejected regardless of its
// management status, so a safety-gated finding is never reported as ready.
func ResolveReplatformingSourceState(managementStatus string, promotionRejected bool) ReplatformingSourceState {
	if promotionRejected {
		return ReplatformingSourceStateRejected
	}
	return ReplatformingSourceStateForManagementStatus(managementStatus)
}
