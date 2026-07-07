// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannerworker

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

// TestWarningAnalyzerFactDecodesThroughTypedContract locks the second producer
// of scanner_worker.warning to the same typed contract the image analyzer uses.
// WarningAnalyzer runs whenever no concrete analyzer source is configured
// (cmd/scanner-worker/service.go returns it for both
// "sbom_generator_source_not_configured" and "analyzer_not_configured"), so a
// fact it emits must decode cleanly through factschema.DecodeScannerWorkerWarning
// exactly like an image-analyzer warning. The fallback only ever has the claim's
// target scope, never image identity or extracted evidence, so image_reference,
// image_digest, evidence_source, and extraction_reason are legitimately absent
// and must not be required by the contract.
func TestWarningAnalyzerFactDecodesThroughTypedContract(t *testing.T) {
	t.Parallel()

	input := testClaimInput(t)
	result, err := WarningAnalyzer{Reason: "analyzer_not_configured"}.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("Analyze() error = %v, want nil", err)
	}
	fact := result.Output.Facts[0]

	decoded, err := factschema.DecodeScannerWorkerWarning(factschema.Envelope{
		FactKind:      fact.FactKind,
		SchemaVersion: fact.SchemaVersion,
		Payload:       fact.Payload,
	})
	if err != nil {
		t.Fatalf("DecodeScannerWorkerWarning() error = %v, want nil (the non-image fallback warning must satisfy the typed contract)", err)
	}
	if got, want := decoded.Analyzer, string(input.Analyzer); got != want {
		t.Fatalf("Analyzer = %q, want %q", got, want)
	}
	if got, want := decoded.Reason, "analyzer_not_configured"; got != want {
		t.Fatalf("Reason = %q, want %q", got, want)
	}
	if got, want := decoded.TargetLocatorHash, input.Target.LocatorHash; got != want {
		t.Fatalf("TargetLocatorHash = %q, want %q", got, want)
	}
	if decoded.ImageReference != nil {
		t.Fatalf("ImageReference = %q, want nil for the non-image fallback warning", *decoded.ImageReference)
	}
	if decoded.ImageDigest != nil {
		t.Fatalf("ImageDigest = %q, want nil for the non-image fallback warning", *decoded.ImageDigest)
	}
	if decoded.EvidenceSource != nil {
		t.Fatalf("EvidenceSource = %q, want nil for the non-image fallback warning", *decoded.EvidenceSource)
	}
	if decoded.ExtractionReason != nil {
		t.Fatalf("ExtractionReason = %q, want nil for the non-image fallback warning", *decoded.ExtractionReason)
	}
}

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
