// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// TestComputeChangedSinceDeltaRefusesDeltaGenerationWithoutDiff pins the
// #7282 refusal at the reader: when either end of the window is a delta
// generation, the reader answers baseline_not_comparable from the two resolve
// statements and never issues the fact_records diff.
func TestComputeChangedSinceDeltaRefusesDeltaGenerationWithoutDiff(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name         string
		currentDelta bool
		sinceDelta   bool
	}{
		{name: "delta current generation", currentDelta: true},
		{name: "delta since generation", sinceDelta: true},
		{name: "both delta", currentDelta: true, sinceDelta: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			queryer := &fakeQueryer{responses: []fakeRows{
				{rows: [][]any{{"scope-r", "repository", "repo-r", "gen-current", observed, tt.currentDelta, false}}},
				{rows: [][]any{{"gen-prior", observed.Add(-time.Hour), tt.sinceDelta}}},
			}}
			summary, err := NewStatusStore(queryer).ComputeChangedSinceDelta(context.Background(), statuspkg.ChangedSinceFilter{
				ScopeID: "scope-r", SinceGenerationID: "gen-prior", SampleLimit: 25,
			})
			if err != nil {
				t.Fatalf("ComputeChangedSinceDelta() error = %v", err)
			}
			if !summary.Unavailable || summary.UnavailableReason != statuspkg.ChangedSinceUnavailableBaselineNotComparable {
				t.Fatalf("unavailable=%v reason=%q, want baseline_not_comparable", summary.Unavailable, summary.UnavailableReason)
			}
			if summary.SinceIsDelta != tt.sinceDelta || summary.CurrentIsDelta != tt.currentDelta {
				t.Fatalf("since_is_delta=%v current_is_delta=%v, want %v and %v",
					summary.SinceIsDelta, summary.CurrentIsDelta, tt.sinceDelta, tt.currentDelta)
			}
			if summary.SinceGenerationID != "gen-prior" || summary.CurrentActiveGenerationID != "gen-current" {
				t.Fatalf("window = %s -> %s, want gen-prior -> gen-current", summary.SinceGenerationID, summary.CurrentActiveGenerationID)
			}
			for _, category := range summary.Categories {
				if !category.Unavailable {
					t.Fatalf("category %s unavailable = false, want true", category.Category)
				}
			}
			if len(queryer.queries) != 2 {
				t.Fatalf("statements = %d, want 2 (scope and prior resolve, no diff)", len(queryer.queries))
			}
			for _, query := range queryer.queries {
				if strings.Contains(query, "fact_records") {
					t.Fatalf("a refused window must not read fact_records:\n%s", query)
				}
			}
		})
	}
}

// TestChangedSinceResolveQueriesReadIsDeltaFromTheResolvedRow pins that each
// is_delta flag comes from the same scope_generations row as the generation id
// it guards, so the check and the id cannot come from different reads.
func TestChangedSinceResolveQueriesReadIsDeltaFromTheResolvedRow(t *testing.T) {
	t.Parallel()

	if !strings.Contains(resolveChangedSinceScopeQuery, "COALESCE(active_generation.is_delta, false) AS current_is_delta") {
		t.Fatalf("scope resolve must read is_delta from the active generation row:\n%s", resolveChangedSinceScopeQuery)
	}
	if !strings.Contains(resolveChangedSinceGenerationQuery, "generation.is_delta") {
		t.Fatalf("prior resolve must read is_delta from the resolved generation row:\n%s", resolveChangedSinceGenerationQuery)
	}
}
