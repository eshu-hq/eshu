// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestStreamingFactWriterRefusesMismatchedScopeGenerationPair pins the
// collector-path half of the invariant the #7279 retention statements rely on:
// every fact_records row carries its generation's own scope_id. The streaming
// writer (IngestionStore.CommitScopeGeneration's only fact path) refuses an
// envelope whose scope or generation differs from the generation being
// committed, before it writes anything, and scope.ScopeGeneration validation
// already ties the generation to that scope.
func TestStreamingFactWriterRefusesMismatchedScopeGenerationPair(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		envelope facts.Envelope
		want     string
	}{
		{"other-scope", facts.Envelope{FactID: "f", ScopeID: "scope-b", GenerationID: "gen-a"}, "scope_id"},
		{"other-generation", facts.Envelope{FactID: "f", ScopeID: "scope-a", GenerationID: "gen-b"}, "generation_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			database := &fakeExecQueryer{}
			stream := make(chan facts.Envelope, 1)
			stream <- tc.envelope
			close(stream)
			_, err := upsertStreamingFacts(context.Background(), database, stream, "scope-a", "gen-a", nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("upsertStreamingFacts() error = %v, want a %s mismatch refusal", err, tc.want)
			}
			if len(database.execs) != 0 || len(database.queries) != 0 {
				t.Fatalf("mismatched envelope reached the database: %d execs, %d queries", len(database.execs), len(database.queries))
			}
		})
	}
}
