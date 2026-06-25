// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

// WarningKind enumerates the bounded GCP collection warning kinds. Each value is
// a low-cardinality enum safe for telemetry labels and durable status keys.
const (
	// WarningKindPartialPermission marks a scope or content family the collector
	// could not fully read because a project, folder, or content-type permission
	// was missing.
	WarningKindPartialPermission = "partial_permission"
	// WarningKindUnsupported marks a content family or relationship tier the
	// provider does not expose, for example missing Security Command Center tier
	// for relationship content.
	WarningKindUnsupported = "unsupported"
	// WarningKindQuota marks a scan that hit Cloud Asset Inventory quota or
	// throttle limits before completing.
	WarningKindQuota = "quota"
	// WarningKindUnavailable marks a scan that could not reach Cloud Asset
	// Inventory or received a retry-exhausted provider availability failure.
	WarningKindUnavailable = "unavailable"
	// WarningKindStale marks a generation that was rejected because a newer
	// generation already owns the shard.
	WarningKindStale = "stale_generation"
	// WarningKindRedaction marks a resource whose payload required redaction
	// beyond the normal label/member fingerprinting policy.
	WarningKindRedaction = "redaction"
	// WarningKindPageTokenExpired marks a paginated scan whose continuation token
	// expired before the shard finished.
	WarningKindPageTokenExpired = "page_token_expired" // #nosec G101 -- warning-kind label for a pagination token expiry, not a credential
)

// Outcome enumerates the bounded per-shard source states a warning reports.
// These mirror the multi-cloud contract per-path evidence states.
const (
	// OutcomePartial means the collector read only part of the configured scope or
	// content family.
	OutcomePartial = "partial"
	// OutcomeUnsupported means the provider, tier, API, or content family does not
	// expose this evidence.
	OutcomeUnsupported = "unsupported"
	// OutcomeUnavailable means the source was configured but unreachable,
	// unauthorized, or rate-limited without current evidence.
	OutcomeUnavailable = "unavailable"
	// OutcomeStale means the accepted generation is older than the configured
	// freshness window.
	OutcomeStale = "stale"
)

var validWarningKinds = map[string]struct{}{
	WarningKindPartialPermission: {},
	WarningKindUnsupported:       {},
	WarningKindQuota:             {},
	WarningKindUnavailable:       {},
	WarningKindStale:             {},
	WarningKindRedaction:         {},
	WarningKindPageTokenExpired:  {},
}

var validOutcomes = map[string]struct{}{
	OutcomePartial:     {},
	OutcomeUnsupported: {},
	OutcomeUnavailable: {},
	OutcomeStale:       {},
}

// ValidWarningKind reports whether kind is one of the bounded warning kinds.
func ValidWarningKind(kind string) bool {
	_, ok := validWarningKinds[kind]
	return ok
}

// ValidOutcome reports whether outcome is one of the bounded source-state
// outcomes.
func ValidOutcome(outcome string) bool {
	_, ok := validOutcomes[outcome]
	return ok
}
