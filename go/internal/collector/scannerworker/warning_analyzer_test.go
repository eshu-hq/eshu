// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannerworker

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestWarningAnalyzerEmitsExplicitSourceWarning(t *testing.T) {
	t.Parallel()

	input := testClaimInput(t)
	result, err := WarningAnalyzer{Reason: "analyzer_not_configured"}.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("Analyze() error = %v, want nil", err)
	}
	if result.Output.TargetCount != 1 {
		t.Fatalf("TargetCount = %d, want 1", result.Output.TargetCount)
	}
	if result.Output.ResultCount != 0 {
		t.Fatalf("ResultCount = %d, want 0 warning-only output", result.Output.ResultCount)
	}
	if len(result.Output.Facts) != 1 {
		t.Fatalf("facts = %d, want 1 warning fact", len(result.Output.Facts))
	}
	fact := result.Output.Facts[0]
	if fact.FactKind != facts.ScannerWorkerWarningFactKind {
		t.Fatalf("FactKind = %q, want %q", fact.FactKind, facts.ScannerWorkerWarningFactKind)
	}
	if fact.ScopeID != input.Target.ScopeID || fact.GenerationID != input.GenerationID {
		t.Fatalf("fact scope/generation = (%q,%q), want (%q,%q)", fact.ScopeID, fact.GenerationID, input.Target.ScopeID, input.GenerationID)
	}
	if fact.FencingToken != input.FencingToken {
		t.Fatalf("FencingToken = %d, want %d", fact.FencingToken, input.FencingToken)
	}
	if got := fact.Payload["target_locator_hash"]; got != input.Target.LocatorHash {
		t.Fatalf("target_locator_hash = %v, want %q", got, input.Target.LocatorHash)
	}
	if err := ValidateFactOutput(input, result.Output); err != nil {
		t.Fatalf("ValidateFactOutput() error = %v, want nil", err)
	}
}
