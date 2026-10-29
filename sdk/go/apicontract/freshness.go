// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package apicontract

// FreshnessCause is the closed, prompt-facing reason a TruthFreshness state is
// not fresh. It explains WHY an answer is stale, building, or unavailable so a
// consumer can choose a bounded next check instead of treating the answer as
// wrong. Causality is distinct from correctness: a stale answer reflects truth
// that was correct at TruthFreshness.ObservedAt and has a known, named reason
// for lagging. The set is deliberately small and closed; handlers MUST NOT
// invent causes outside it, and MUST attach a cause only when they hold the
// evidence for it.
type FreshnessCause string

const (
	// FreshnessCausePendingRepoGeneration marks a freshness gap caused by a repo
	// whose generation has not yet completed; the next graph generation will
	// catch the answer up.
	FreshnessCausePendingRepoGeneration FreshnessCause = "pending_repo_generation"
	// FreshnessCauseReducerBacklog marks a freshness gap caused by queued reducer
	// projection that has not yet drained to the materialized graph or content.
	FreshnessCauseReducerBacklog FreshnessCause = "reducer_backlog"
	// FreshnessCauseDeadLetteredDomain marks a freshness gap caused by a
	// dead-lettered domain whose projection failed and is parked for repair.
	FreshnessCauseDeadLetteredDomain FreshnessCause = "dead_lettered_domain"
	// FreshnessCauseMissingCollectorCompletion marks a freshness gap caused by a
	// collector that has not reported a completed run for the requested coverage.
	FreshnessCauseMissingCollectorCompletion FreshnessCause = "missing_collector_completion"
	// FreshnessCauseContentCoverageUnavailable marks a freshness gap caused by
	// content coverage that is not yet indexed or available for the scope.
	FreshnessCauseContentCoverageUnavailable FreshnessCause = "content_coverage_unavailable"
	// FreshnessCauseUnsupportedProfile marks a freshness gap caused by an active
	// query profile that cannot serve authoritative truth for the capability.
	FreshnessCauseUnsupportedProfile FreshnessCause = "unsupported_profile"
	// FreshnessCauseRetentionExpired marks a freshness gap caused by history
	// that was deliberately pruned by a retention policy.
	FreshnessCauseRetentionExpired FreshnessCause = "retention_expired"
	// FreshnessCausePendingSearchVector marks a freshness gap caused by an
	// outstanding search-vector build: SearchVectorBuildRunner has active
	// scopes with pending vector rows for the semantic/hybrid search read
	// path. The cause clears once the runner publishes its search_vector_ready
	// completion signal (RunOnce completing a bounded sweep with zero pending
	// scopes).
	FreshnessCausePendingSearchVector FreshnessCause = "pending_search_vector"
)

// freshnessCauses is the closed enumeration of valid causes.
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

// ValidFreshnessCause reports whether cause is a member of the closed cause
// enumeration. Callers that surface a cause from an upstream signal MUST gate on
// this so an unknown value never reaches the wire.
func ValidFreshnessCause(cause FreshnessCause) bool {
	_, ok := freshnessCauses[cause]
	return ok
}

// FreshnessNextCheck is a bounded, recommended follow-up call that drills into
// the freshness cause: a status, generation, coverage, citation, or queue
// surface a consumer can call to learn when the answer will catch up. It reuses
// the recommended_next_calls convention (a tool or route plus optional bounded
// params) rather than inventing a new shape.
type FreshnessNextCheck struct {
	// Tool is the MCP tool a consumer can call to drill into the cause. Optional
	// when Route is set.
	Tool string `json:"tool,omitempty"`
	// Route is the HTTP route a consumer can call to drill into the cause.
	// Optional when Tool is set.
	Route string `json:"route,omitempty"`
	// Reason is a short, human-readable explanation of what the next check
	// resolves.
	Reason string `json:"reason,omitempty"`
	// Params carries bounded query parameters for the next check. It is optional
	// and never free-form prose.
	Params map[string]string `json:"params,omitempty"`
}
