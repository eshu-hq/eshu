// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestECSBindingHandleIncompleteCensusBlocksDeclarationBind is the
// eshu-hq/eshu#11084 failing-first regression: an incomplete declaration
// census must block declaration binds (zero rows plus a visible skip),
// never bind on the retained prefix. A competing declaration beyond the
// read cannot veto a bind, so every uniqueness leg scanned over the
// prefix is unsound.
func TestECSBindingHandleIncompleteCensusBlocksDeclarationBind(t *testing.T) {
	t.Parallel()

	censusResult := ecsBindingHandleCensus()
	censusResult.Complete = false
	censusResult.Declarations = nil
	census := &stubECSBindingCensus{result: censusResult}
	handler, writer := ecsBindingHandleHandler(census)
	result, err := handler.Handle(context.Background(), workloadCloudRelationshipIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if len(writer.writtenRows) != 0 {
		t.Fatalf("written rows = %d, want 0: an incomplete census must block declaration binds, never bind on a prefix", len(writer.writtenRows))
	}
	if !strings.Contains(result.EvidenceSummary, "0 ECS declaration-bound edge(s)") {
		t.Errorf("summary lacks zero ECS-bound count: %q", result.EvidenceSummary)
	}
	if strings.Contains(result.EvidenceSummary, "census truncated") {
		t.Errorf("summary keeps the retired truncation annotation: %q", result.EvidenceSummary)
	}
}

// TestExtractWorkloadECSBindingRowsIncompleteCensusTalliesBlocked pins
// the complete-or-blocked contract at the extractor: the named
// ecs_census_incomplete tally point, zero declaration rows.
func TestExtractWorkloadECSBindingRowsIncompleteCensusTalliesBlocked(t *testing.T) {
	t.Parallel()

	const digest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	envelopes := []facts.Envelope{
		ecsBindingServiceEnvelope("fact-svc-api", "api", "prod", map[string]any{"environment": "prod"}),
		ecsBindingTaskEnvelope("fact-task-api", "api", "prod", []string{digest}),
	}
	rows, tally, _, err := ExtractWorkloadECSBindingRows(
		envelopes,
		ECSBindingCensusResult{},
		map[string][]string{"api": {"prod"}},
	)
	if err != nil {
		t.Fatalf("ExtractWorkloadECSBindingRows returned error: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("bound rows = %d, want 0 under an incomplete census", len(rows))
	}
	if got := tally.skipped[ecsBindingSkipCensusIncomplete]; got != 1 {
		t.Errorf("ecs_census_incomplete skips = %d, want 1 (tally=%#v)", got, tally.skipped)
	}
}
