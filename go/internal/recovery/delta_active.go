// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recovery

import "sort"

// Delta-active outcomes a refinalize reports for a scope whose re-projected
// generation is a delta (scope_generations.is_delta). They are a closed set so
// the response, the log, and the metric label share one vocabulary and the
// label cardinality stays bounded (#7797).
const (
	// DeltaActiveOutcomeReindexRequested means the scope is a git
	// default-branch repository scope, and the refinalize recorded a
	// per-repository reindex watermark for it in the same transaction. The
	// next sync cycle of the owning git ingester forces a full re-parse.
	DeltaActiveOutcomeReindexRequested = "reindex_requested"

	// DeltaActiveOutcomeReindexUnsupported means the scope is not a git
	// default-branch repository scope (for example a ref scope or another
	// collector's scope), so no reindex watermark can force a full snapshot.
	// Only that collector's next full generation repairs the graph.
	DeltaActiveOutcomeReindexUnsupported = "reindex_unsupported"
)

// DeltaActiveScopeSampleLimit bounds how many scope ids the report samples per
// outcome. The counts are exact.
const DeltaActiveScopeSampleLimit = 10

// DeltaActiveDetail is the operator message a refinalize returns when it
// re-projected at least one delta generation.
const DeltaActiveDetail = "A delta generation carries only the files that changed since its baseline, so " +
	"re-projecting it onto an empty graph restores only those files. The graph for these scopes is " +
	"incomplete until a full generation activates. For reindex_requested scopes a per-repository reindex " +
	"watermark was recorded: the owning git ingester forces a full re-parse on its next sync cycle, and " +
	"the request waits while that ingester runs no sync cycles. reindex_unsupported scopes need a full " +
	"generation from their own collector."

// DeltaActiveScopes reports the scopes a refinalize re-projected through a
// delta generation, grouped by outcome. Without it a rebuild of a
// delta-active scope reports success while the graph holds only the delta's
// files (#7797).
type DeltaActiveScopes struct {
	// ByOutcome counts delta-active scopes per DeltaActiveOutcome* value. It
	// is empty when no re-projected generation was a delta.
	ByOutcome map[string]int

	// Samples names up to DeltaActiveScopeSampleLimit scope ids per outcome,
	// in the order they were added (ascending scope id for a refinalize).
	Samples map[string][]string
}

// Add records one delta-active scope under outcome.
func (d *DeltaActiveScopes) Add(outcome, scopeID string) {
	if d.ByOutcome == nil {
		d.ByOutcome = make(map[string]int)
	}
	if d.Samples == nil {
		d.Samples = make(map[string][]string)
	}
	d.ByOutcome[outcome]++
	if len(d.Samples[outcome]) < DeltaActiveScopeSampleLimit {
		d.Samples[outcome] = append(d.Samples[outcome], scopeID)
	}
}

// Total returns how many delta-active scopes were re-projected across every
// outcome.
func (d DeltaActiveScopes) Total() int {
	total := 0
	for _, count := range d.ByOutcome {
		total += count
	}
	return total
}

// Outcomes returns the outcomes that hold at least one scope, sorted, so a
// caller that emits per-outcome signals does it in a stable order.
func (d DeltaActiveScopes) Outcomes() []string {
	outcomes := make([]string, 0, len(d.ByOutcome))
	for outcome, count := range d.ByOutcome {
		if count > 0 {
			outcomes = append(outcomes, outcome)
		}
	}
	sort.Strings(outcomes)
	return outcomes
}

// DeltaActiveScopesReport is the wire form of DeltaActiveScopes shared by every
// refinalize response, so the admin API and the runtime admin surface cannot
// spell it differently.
type DeltaActiveScopesReport struct {
	// Total is the number of scopes re-projected through a delta generation.
	Total int `json:"total"`
	// ByOutcome counts delta-active scopes per outcome. Never null.
	ByOutcome map[string]int `json:"by_outcome"`
	// SampleScopeIDs names up to DeltaActiveScopeSampleLimit scope ids per
	// outcome. Never null.
	SampleScopeIDs map[string][]string `json:"sample_scope_ids"`
	// Detail is DeltaActiveDetail when Total is non-zero, else empty.
	Detail string `json:"detail"`
}

// Report renders the wire form. It always returns non-nil maps and omits
// outcomes with a zero count.
func (d DeltaActiveScopes) Report() DeltaActiveScopesReport {
	report := DeltaActiveScopesReport{
		Total:          d.Total(),
		ByOutcome:      make(map[string]int, len(d.ByOutcome)),
		SampleScopeIDs: make(map[string][]string, len(d.Samples)),
	}
	for outcome, count := range d.ByOutcome {
		if count > 0 {
			report.ByOutcome[outcome] = count
		}
	}
	for outcome, ids := range d.Samples {
		if len(ids) > 0 {
			report.SampleScopeIDs[outcome] = ids
		}
	}
	if report.Total > 0 {
		report.Detail = DeltaActiveDetail
	}
	return report
}

// ReindexRequestsWrittenReport is the wire form of the per-repository reindex
// watermarks one refinalize wrote (#7797), shared by every refinalize response.
type ReindexRequestsWrittenReport struct {
	// Count is the exact number of scopes that got a reindex watermark.
	Count int `json:"count"`
	// ScopeIDs names up to DeltaActiveScopeSampleLimit of those scopes in
	// ascending order. Never null.
	ScopeIDs []string `json:"scope_ids"`
}

// ReindexRequestsWritten reports the scopes under
// DeltaActiveOutcomeReindexRequested: the refinalize wrote a
// repository_reindex_requests row for each of them in its transaction.
func (d DeltaActiveScopes) ReindexRequestsWritten() ReindexRequestsWrittenReport {
	ids := append([]string{}, d.Samples[DeltaActiveOutcomeReindexRequested]...)
	return ReindexRequestsWrittenReport{
		Count:    d.ByOutcome[DeltaActiveOutcomeReindexRequested],
		ScopeIDs: ids,
	}
}
