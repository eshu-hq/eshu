// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"testing"
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
	censusResult.Truncated = true
	census := &stubECSBindingCensus{result: censusResult}
	handler, writer := ecsBindingHandleHandler(census)
	result, err := handler.Handle(context.Background(), workloadCloudRelationshipIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if len(writer.writtenRows) != 0 {
		t.Fatalf("written rows = %d, want 0: an incomplete census must block declaration binds, never bind on a prefix", len(writer.writtenRows))
	}
	if !strings.Contains(result.EvidenceSummary, "1 candidate(s) skipped") {
		t.Errorf("summary lacks blocked-census skip: %q", result.EvidenceSummary)
	}
}
