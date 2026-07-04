// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "strings"

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

// freshnessCauses is the closed enumeration of valid causes. Validation and the
// cause→next-call mapping both read it so a new cause cannot be added without a
// matching next-check entry.
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

// asRecommendedNextCall renders the next check in the recommended_next_calls map
// shape used by evidence_citation.go and the answer packet, so the same wire
// convention carries freshness drilldowns.
func (n FreshnessNextCheck) asRecommendedNextCall() map[string]any {
	call := map[string]any{}
	if tool := strings.TrimSpace(n.Tool); tool != "" {
		call["tool"] = tool
	}
	if route := strings.TrimSpace(n.Route); route != "" {
		call["route"] = route
	}
	if reason := strings.TrimSpace(n.Reason); reason != "" {
		call["reason"] = reason
	}
	if len(n.Params) > 0 {
		params := make(map[string]any, len(n.Params))
		for k, v := range n.Params {
			params[k] = v
		}
		call["params"] = params
	}
	return call
}

// freshnessCauseNextChecks maps each closed cause to its bounded next check. The
// map is total over freshnessCauses; the matrix test asserts every enum value
// has an entry so a new cause cannot ship without a drilldown.
var freshnessCauseNextChecks = map[FreshnessCause]FreshnessNextCheck{
	FreshnessCausePendingRepoGeneration: {
		Tool:   "get_index_status",
		Route:  "GET /api/v0/status",
		Reason: "check repo generation progress; the answer catches up when the pending generation completes",
	},
	FreshnessCauseReducerBacklog: {
		Tool:   "get_index_status",
		Route:  "GET /api/v0/status",
		Reason: "check reducer queue depth; the answer catches up as the projection backlog drains",
	},
	FreshnessCauseDeadLetteredDomain: {
		Tool:   "get_index_status",
		Route:  "GET /api/v0/status",
		Reason: "inspect the dead-lettered domain; projection is parked for repair and will not advance until cleared",
	},
	FreshnessCauseMissingCollectorCompletion: {
		Tool:   "list_collectors",
		Route:  "GET /api/v0/status",
		Reason: "check collector completion; coverage fills in once the collector reports a completed run",
	},
	FreshnessCauseContentCoverageUnavailable: {
		Tool:   "build_evidence_citation_packet",
		Route:  "POST /api/v0/evidence/citations",
		Reason: "request content coverage; citation hydration becomes available once content is indexed",
	},
	FreshnessCauseUnsupportedProfile: {
		Tool:   "get_index_status",
		Route:  "GET /api/v0/status",
		Reason: "the active profile cannot serve authoritative truth for this capability; switch to an authoritative profile",
	},
	FreshnessCauseRetentionExpired: {
		Tool:   "get_generation_lifecycle",
		Route:  "GET /api/v0/freshness/generations",
		Reason: "the requested history was pruned by retention; inspect the retained generation window",
	},
	FreshnessCausePendingSearchVector: {
		Tool:   "get_index_status",
		Route:  "GET /api/v0/status",
		Reason: "check the search-vector build sweep; the answer catches up once the pending scopes reach zero and search_vector_ready is published",
	},
}

// FreshnessCauseNextCheck returns the bounded next check for a cause and whether
// the cause is a known enum member. An unknown cause returns the zero next check
// and false, so a handler that somehow holds an invalid cause attaches nothing.
func FreshnessCauseNextCheck(cause FreshnessCause) (FreshnessNextCheck, bool) {
	check, ok := freshnessCauseNextChecks[cause]
	return check, ok
}

// WithFreshnessCause attaches a proven freshness cause and its bounded next
// check to a TruthEnvelope's freshness. It is the single, explicit path from a
// known upstream signal to a wire cause. It is a no-op (and leaves freshness
// untouched) when:
//
//   - the envelope is nil,
//   - the freshness state is fresh (a fresh answer has no cause to explain), or
//   - the cause is not a member of the closed enumeration.
//
// This is the guardrail behind the acceptance rule "no handler invents
// freshness causes it cannot prove": a handler calls this only when it holds the
// evidence, and an unprovable or invalid cause never reaches the envelope.
func WithFreshnessCause(truth *TruthEnvelope, cause FreshnessCause) {
	if truth == nil {
		return
	}
	if truth.Freshness.State == FreshnessFresh {
		return
	}
	if !ValidFreshnessCause(cause) {
		return
	}
	truth.Freshness.Cause = cause
	if check, ok := freshnessCauseNextChecks[cause]; ok {
		next := check
		truth.Freshness.NextCheck = &next
	}
}
