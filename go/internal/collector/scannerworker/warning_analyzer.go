// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannerworker

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// WarningAnalyzer emits an explicit scanner-worker warning fact. It is safe for
// hosted runtime proof before a concrete heavy analyzer is configured because
// it never emits clean findings.
type WarningAnalyzer struct {
	Reason string
}

// Analyze emits one warning source fact for the claimed target.
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
		Payload: map[string]any{
			"analyzer":            string(input.Analyzer),
			"target_kind":         string(input.Target.Kind),
			"target_locator_hash": input.Target.LocatorHash,
			"reason":              reason,
			"warning_class":       "scanner_worker_warning",
			"analysis_status":     "not_scanned",
			"coverage_status":     "unsupported",
		},
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
