// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannerworker

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	scannerworkerv1 "github.com/eshu-hq/eshu/sdk/go/factschema/scannerworker/v1"
)

// WarningAnalyzer emits an explicit scanner-worker warning fact. It is safe for
// hosted runtime proof before a concrete heavy analyzer is configured because
// it never emits clean findings.
type WarningAnalyzer struct {
	Reason string
}

// Analyze emits one warning source fact for the claimed target. The payload is
// built through factschema.EncodeScannerWorkerWarning so this fallback producer
// stays in lockstep with the scanner_worker.warning contract the reducer
// decodes. It sets only the common-core fields every warning carries; the
// image-analysis fields (image reference/digest, evidence source, extraction
// reason) are left unset because the fallback has only the claim's target scope,
// never image evidence, and the contract makes those fields optional.
func (a WarningAnalyzer) Analyze(_ context.Context, input ClaimInput) (AnalyzerResult, error) {
	reason := strings.TrimSpace(a.Reason)
	if reason == "" {
		reason = "analyzer_not_configured"
	}
	stableKey := facts.StableID(facts.ScannerWorkerWarningFactKind, map[string]any{
		"analyzer":            string(input.Analyzer),
		"target_kind":         string(input.Target.Kind),
		"target_locator_hash": input.Target.LocatorHash,
		"generation_id":       input.GenerationID,
		"reason":              reason,
	})
	payload, err := factschema.EncodeScannerWorkerWarning(scannerworkerv1.Warning{
		Analyzer:          string(input.Analyzer),
		TargetKind:        string(input.Target.Kind),
		TargetLocatorHash: input.Target.LocatorHash,
		Reason:            reason,
		WarningClass:      "scanner_worker_warning",
		AnalysisStatus:    "not_scanned",
		CoverageStatus:    "unsupported",
	})
	if err != nil {
		return AnalyzerResult{}, err
	}
	fact := facts.Envelope{
		FactID:           facts.ScannerWorkerWarningFactKind + ":" + stableKey,
		ScopeID:          input.Target.ScopeID,
		GenerationID:     input.GenerationID,
		FactKind:         facts.ScannerWorkerWarningFactKind,
		StableFactKey:    stableKey,
		SchemaVersion:    facts.ScannerWorkerSchemaVersionV1,
		CollectorKind:    string(scope.CollectorScannerWorker),
		FencingToken:     input.FencingToken,
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       input.ObservedAt,
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem: string(scope.CollectorScannerWorker),
			ScopeID:      input.Target.ScopeID,
			GenerationID: input.GenerationID,
			FactKey:      stableKey,
		},
	}
	return AnalyzerResult{
		Output: FactOutput{
			TargetCount: 1,
			ResultCount: 0,
			Facts:       []facts.Envelope{fact},
		},
	}, nil
}
