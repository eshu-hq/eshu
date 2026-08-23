// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

// FreshnessCause is the closed reason a truth response is not fresh.
type FreshnessCause string

// Freshness causes form the closed reason vocabulary for non-fresh truth.
const (
	// FreshnessCausePendingRepoGeneration identifies an incomplete repository generation.
	FreshnessCausePendingRepoGeneration      FreshnessCause = "pending_repo_generation"
	FreshnessCauseReducerBacklog             FreshnessCause = "reducer_backlog"
	FreshnessCauseDeadLetteredDomain         FreshnessCause = "dead_lettered_domain"
	FreshnessCauseMissingCollectorCompletion FreshnessCause = "missing_collector_completion"
	FreshnessCauseContentCoverageUnavailable FreshnessCause = "content_coverage_unavailable"
	FreshnessCauseUnsupportedProfile         FreshnessCause = "unsupported_profile"
	FreshnessCauseRetentionExpired           FreshnessCause = "retention_expired"
	FreshnessCausePendingSearchVector        FreshnessCause = "pending_search_vector"
)

var freshnessCauses = map[FreshnessCause]struct{}{
	FreshnessCausePendingRepoGeneration:      {},
	FreshnessCauseReducerBacklog:             {},
	FreshnessCauseDeadLetteredDomain:         {},
	FreshnessCauseMissingCollectorCompletion: {},
	FreshnessCauseContentCoverageUnavailable: {},
	FreshnessCauseUnsupportedProfile:         {},
	FreshnessCauseRetentionExpired:           {},
	FreshnessCausePendingSearchVector:        {},
}

// ValidFreshnessCause reports whether cause belongs to the closed enumeration.
func ValidFreshnessCause(cause FreshnessCause) bool {
	_, ok := freshnessCauses[cause]
	return ok
}

// FreshnessNextCheck is a bounded follow-up call for one freshness cause.
type FreshnessNextCheck struct {
	Tool   string            `json:"tool,omitempty"`
	Route  string            `json:"route,omitempty"`
	Reason string            `json:"reason,omitempty"`
	Params map[string]string `json:"params,omitempty"`
}

var freshnessCauseNextChecks = map[FreshnessCause]FreshnessNextCheck{
	FreshnessCausePendingRepoGeneration: {
		Tool: "get_index_status", Route: "GET /api/v0/status",
		Reason: "check repo generation progress; the answer catches up when the pending generation completes",
	},
	FreshnessCauseReducerBacklog: {
		Tool: "get_index_status", Route: "GET /api/v0/status",
		Reason: "check reducer queue depth; the answer catches up as the projection backlog drains",
	},
	FreshnessCauseDeadLetteredDomain: {
		Tool: "get_index_status", Route: "GET /api/v0/status",
		Reason: "inspect the dead-lettered domain; projection is parked for repair and will not advance until cleared",
	},
	FreshnessCauseMissingCollectorCompletion: {
		Tool: "list_collectors", Route: "GET /api/v0/status",
		Reason: "check collector completion; coverage fills in once the collector reports a completed run",
	},
	FreshnessCauseContentCoverageUnavailable: {
		Tool: "build_evidence_citation_packet", Route: "POST /api/v0/evidence/citations",
		Reason: "request content coverage; citation hydration becomes available once content is indexed",
	},
	FreshnessCauseUnsupportedProfile: {
		Tool: "get_index_status", Route: "GET /api/v0/status",
		Reason: "the active profile cannot serve authoritative truth for this capability; switch to an authoritative profile",
	},
	FreshnessCauseRetentionExpired: {
		Tool: "get_generation_lifecycle", Route: "GET /api/v0/freshness/generations",
		Reason: "the requested history was pruned by retention; inspect the retained generation window",
	},
	FreshnessCausePendingSearchVector: {
		Tool: "get_index_status", Route: "GET /api/v0/status",
		Reason: "check the search-vector build sweep; the answer catches up once the pending scopes reach zero and search_vector_ready is published",
	},
}

// FreshnessCauseNextCheck returns the bounded follow-up for a known cause.
func FreshnessCauseNextCheck(cause FreshnessCause) (FreshnessNextCheck, bool) {
	check, ok := freshnessCauseNextChecks[cause]
	return check, ok
}

// WithFreshnessCause attaches a proven cause to a non-fresh envelope.
func WithFreshnessCause(truth *TruthEnvelope, cause FreshnessCause) {
	if truth == nil || truth.Freshness.State == FreshnessFresh || !ValidFreshnessCause(cause) {
		return
	}
	truth.Freshness.Cause = cause
	if check, ok := freshnessCauseNextChecks[cause]; ok {
		next := check
		truth.Freshness.NextCheck = &next
	}
}
