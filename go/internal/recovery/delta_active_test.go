// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recovery

import (
	"fmt"
	"testing"
)

// TestDeltaActiveScopesReportEmpty pins the wire form when no re-projected
// generation was a delta: non-nil maps and no detail, so a client can always
// read the fields.
func TestDeltaActiveScopesReportEmpty(t *testing.T) {
	t.Parallel()

	report := DeltaActiveScopes{}.Report()
	if report.Total != 0 || report.ByOutcome == nil || report.SampleScopeIDs == nil || report.Detail != "" {
		t.Fatalf("empty report = %+v, want zero total, non-nil maps, empty detail", report)
	}
}

// TestDeltaActiveScopesReportCountsAndSamples pins exact counts, the bounded
// sample, and the operator detail once any scope is delta-active.
func TestDeltaActiveScopesReportCountsAndSamples(t *testing.T) {
	t.Parallel()

	var scopes DeltaActiveScopes
	for i := range DeltaActiveScopeSampleLimit + 2 {
		scopes.Add(DeltaActiveOutcomeReindexRequested, fmt.Sprintf("scope-%02d", i))
	}
	scopes.Add(DeltaActiveOutcomeReindexUnsupported, "scope-ref")

	report := scopes.Report()
	if got, want := report.Total, DeltaActiveScopeSampleLimit+3; got != want {
		t.Fatalf("Total = %d, want %d", got, want)
	}
	if got, want := report.ByOutcome[DeltaActiveOutcomeReindexRequested], DeltaActiveScopeSampleLimit+2; got != want {
		t.Fatalf("ByOutcome[reindex_requested] = %d, want %d", got, want)
	}
	if got := len(report.SampleScopeIDs[DeltaActiveOutcomeReindexRequested]); got != DeltaActiveScopeSampleLimit {
		t.Fatalf("sample size = %d, want the %d cap", got, DeltaActiveScopeSampleLimit)
	}
	if report.Detail != DeltaActiveDetail {
		t.Fatalf("Detail = %q, want DeltaActiveDetail", report.Detail)
	}
	if got, want := fmt.Sprint(scopes.Outcomes()), "[reindex_requested reindex_unsupported]"; got != want {
		t.Fatalf("Outcomes() = %s, want %s", got, want)
	}
}

// TestDeltaActiveScopesReindexRequestsWritten pins the reindex_requests_written
// wire form: the exact count of reindex watermarks the refinalize wrote, and up
// to DeltaActiveScopeSampleLimit of their scope ids. Unsupported scopes get no
// watermark, so they never count.
func TestDeltaActiveScopesReindexRequestsWritten(t *testing.T) {
	t.Parallel()

	empty := DeltaActiveScopes{}.ReindexRequestsWritten()
	if empty.Count != 0 || empty.ScopeIDs == nil || len(empty.ScopeIDs) != 0 {
		t.Fatalf("empty ReindexRequestsWritten() = %+v, want zero count and a non-nil empty list", empty)
	}

	var scopes DeltaActiveScopes
	for i := range DeltaActiveScopeSampleLimit + 2 {
		scopes.Add(DeltaActiveOutcomeReindexRequested, fmt.Sprintf("scope-%02d", i))
	}
	scopes.Add(DeltaActiveOutcomeReindexUnsupported, "scope-ref")

	written := scopes.ReindexRequestsWritten()
	if got, want := written.Count, DeltaActiveScopeSampleLimit+2; got != want {
		t.Fatalf("Count = %d, want %d (unsupported scopes write no watermark)", got, want)
	}
	if got := len(written.ScopeIDs); got != DeltaActiveScopeSampleLimit {
		t.Fatalf("len(ScopeIDs) = %d, want the %d cap", got, DeltaActiveScopeSampleLimit)
	}
	if written.ScopeIDs[0] != "scope-00" {
		t.Fatalf("ScopeIDs[0] = %q, want scope-00", written.ScopeIDs[0])
	}
}
