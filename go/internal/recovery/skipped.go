// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recovery

import "sort"

// Skip reasons a refinalize reports for a scope it could not re-enqueue. They
// are a closed set so an operator, a dashboard, and a metric label all speak the
// same vocabulary, and so the label cardinality stays bounded.
const (
	// SkipReasonNoRecoverableGeneration means the scope has status failed but no
	// generation that is not superseded, so there is nothing to re-project. Only
	// a fresh collection can heal it.
	SkipReasonNoRecoverableGeneration = "no_recoverable_generation"

	// SkipReasonNewestGenerationNotFailed means the scope has status failed, but
	// its newest non-superseded generation is not itself failed (for example a
	// generation that arrived after the failure and is still pending). That
	// generation carries its own projector work, so a refinalize leaves it alone
	// rather than queueing a second projection of the same generation.
	SkipReasonNewestGenerationNotFailed = "newest_generation_not_failed"

	// SkipReasonNoActiveGeneration means the scope is neither active with an
	// active generation nor failed: for example a scope whose first generation
	// has not activated yet. Its own projector work activates it.
	SkipReasonNoActiveGeneration = "no_active_generation"

	// SkipReasonUnknownScope means an explicitly named scope id has no
	// ingestion_scopes row. It applies to named-scope requests only.
	SkipReasonUnknownScope = "unknown_scope"
)

// SkippedScopeSampleLimit bounds how many scope ids the report samples per
// reason. The counts are exact; the sample only lets an operator find the
// scopes to look at without an unbounded response body.
const SkippedScopeSampleLimit = 10

// SkippedScopes reports the scopes a refinalize considered but did not
// re-enqueue, grouped by reason. Without it a partial rebuild is invisible: the
// response says how many scopes were queued and nothing about the ones that
// were not, so a graph that comes back short looks like a graph that worked.
type SkippedScopes struct {
	// ByReason counts skipped scopes per SkipReason* value. It is always
	// non-nil after a successful refinalize, empty when nothing was skipped.
	ByReason map[string]int

	// Samples names up to SkippedScopeSampleLimit skipped scope ids per reason,
	// in ascending scope id order.
	Samples map[string][]string
}

// Add records one skipped scope.
func (s *SkippedScopes) Add(reason, scopeID string) {
	if s.ByReason == nil {
		s.ByReason = make(map[string]int)
	}
	if s.Samples == nil {
		s.Samples = make(map[string][]string)
	}
	s.ByReason[reason]++
	if len(s.Samples[reason]) < SkippedScopeSampleLimit {
		s.Samples[reason] = append(s.Samples[reason], scopeID)
	}
}

// Total returns how many scopes were skipped across every reason.
func (s SkippedScopes) Total() int {
	total := 0
	for _, count := range s.ByReason {
		total += count
	}
	return total
}

// Reasons returns the reasons that skipped at least one scope, sorted, so a
// caller that logs or emits per-reason signals does it in a stable order.
func (s SkippedScopes) Reasons() []string {
	reasons := make([]string, 0, len(s.ByReason))
	for reason, count := range s.ByReason {
		if count > 0 {
			reasons = append(reasons, reason)
		}
	}
	sort.Strings(reasons)
	return reasons
}

// SkippedScopesReport is the wire form of SkippedScopes shared by every
// operator response that carries the report, so the admin API and the runtime
// admin surface cannot spell it differently.
type SkippedScopesReport struct {
	// Total is the number of scopes skipped across every reason.
	Total int `json:"total"`
	// ByReason counts skipped scopes per reason. Never null: an empty object
	// means nothing was skipped.
	ByReason map[string]int `json:"by_reason"`
	// SampleScopeIDs names up to SkippedScopeSampleLimit skipped scope ids per
	// reason. Never null.
	SampleScopeIDs map[string][]string `json:"sample_scope_ids"`
}

// Report renders the wire form. It always returns non-nil maps and omits
// reasons with a zero count.
func (s SkippedScopes) Report() SkippedScopesReport {
	report := SkippedScopesReport{
		Total:          s.Total(),
		ByReason:       make(map[string]int, len(s.ByReason)),
		SampleScopeIDs: make(map[string][]string, len(s.Samples)),
	}
	for reason, count := range s.ByReason {
		if count > 0 {
			report.ByReason[reason] = count
		}
	}
	for reason, ids := range s.Samples {
		if len(ids) > 0 {
			report.SampleScopeIDs[reason] = ids
		}
	}
	return report
}
