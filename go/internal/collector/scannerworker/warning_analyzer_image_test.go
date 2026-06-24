// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannerworker

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestWarningAnalyzerMarksImageTargetAsUnsupportedNotScanned(t *testing.T) {
	t.Parallel()

	input := testClaimInput(t)
	input.Analyzer = AnalyzerImageUnpacking
	input.Target.Kind = TargetImage
	result, err := WarningAnalyzer{Reason: "analyzer_not_configured"}.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("Analyze() error = %v, want nil", err)
	}
	if err := ValidateFactOutput(input, result.Output); err != nil {
		t.Fatalf("ValidateFactOutput() error = %v, want nil", err)
	}
	warning := result.Output.Facts[0]
	if warning.FactKind != facts.ScannerWorkerWarningFactKind {
		t.Fatalf("FactKind = %q, want %q", warning.FactKind, facts.ScannerWorkerWarningFactKind)
	}
	if got, want := warning.Payload["target_kind"], string(TargetImage); got != want {
		t.Fatalf("target_kind = %#v, want %q", got, want)
	}
	if got, want := warning.Payload["analysis_status"], "not_scanned"; got != want {
		t.Fatalf("analysis_status = %#v, want %q", got, want)
	}
	if got, want := warning.Payload["coverage_status"], "unsupported"; got != want {
		t.Fatalf("coverage_status = %#v, want %q", got, want)
	}
	if _, exists := warning.Payload["impact_status"]; exists {
		t.Fatalf("warning payload includes impact_status: %#v", warning.Payload)
	}
}
